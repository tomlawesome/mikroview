// SPDX-License-Identifier: AGPL-3.0-only

package device

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ErrNoPendingEnrolment is returned by BurnEnrolment for a device that
// exists but has no pending token.
var ErrNoPendingEnrolment = errors.New("device: no pending enrolment token for that device")

// ErrExpectedAddressRequired and ErrExpectedAddressInvalid are returned
// by MintEnrolment when the caller gives no expected sender address, or
// one that is not an IP address (issue #1291). The address is what the
// enrolment window binds to, so there is no meaningful token without
// one.
var (
	ErrExpectedAddressRequired = errors.New("device: an expected sender address is required to mint an enrolment token")
	ErrExpectedAddressInvalid  = errors.New("device: the expected sender address is not a valid IP address")
)

// enrolTokenLen/enrolTokenAlphabet/enrolTokenTTL are issue #1281's
// enrolment-token shape: 20 lowercase letters/digits, valid for 15
// minutes, single use. Short enough to type by hand if the paste ever
// fails, long enough (36^20, comfortably over 100 bits) that guessing
// one inside its TTL is not a practical attack -- and it never needs to
// be, since the listener gate refuses every line from an address that
// is not already sourceIp/acceptedIp until one of these actually
// arrives.
const (
	enrolTokenLen      = 20
	enrolTokenAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	enrolTokenTTL      = 15 * time.Minute
)

// maxRefusedAddresses bounds the refused-senders list the same way
// maxUnattributedSources bounds Source: an address costs nothing to add
// to this list (no credential needed, same reasoning as
// maxUnattributedSources' own doc comment), so without a cap it grows
// with whatever reaches the listener.
const maxRefusedAddresses = 256

// enrolLineRE matches issue #1281's enrolment marker anywhere in a raw
// syslog message: "mikroview-enrol <token>". The token itself is
// re-validated by hash lookup (TryEnrol), not by this pattern alone, so
// a malformed or already-used token is simply refused rather than
// trusted because it matched the shape.
var enrolLineRE = regexp.MustCompile(`mikroview-enrol ([a-z0-9]{20})`)

// pendingToken is one device's live enrolment token, hash only -- the
// raw value is returned once, from MintEnrolment, and never stored.
type pendingToken struct {
	hash      string
	expiresAt time.Time
	// expected is the one source address this token may be redeemed
	// from, normalised (issue #1291). A token is minted for a router the
	// operator can already name an address for, so the enrolment window
	// opens for that address alone rather than for everyone: before
	// #1291 a single pending token anywhere left the syslog port
	// reachable by any unknown address at all, which is a far wider door
	// than the one enrolment actually needs.
	expected string
}

// Refused is one syslog source address the listener gate has refused a
// line from: not yet sourceIp/acceptedIp, and the line carried no valid
// enrolment marker either. Issue #1281's GET /api/devices/refused.
type Refused struct {
	Address   string    `json:"ip"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	Lines     uint64    `json:"lines"`
}

// Enrolment is one device's pending-token state, as GET /api/devices
// reports it.
type Enrolment struct {
	Pending   bool      `json:"pending"`
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
}

// hashEnrolToken is the one place a raw enrolment token is ever hashed
// -- used identically by MintEnrolment (to compute what gets stored)
// and TryEnrol (to compute what gets looked up), so the two can never
// drift apart. Mirrors internal/auth's hashTokenValue.
func hashEnrolToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// randomEnrolToken returns enrolTokenLen characters drawn uniformly
// from enrolTokenAlphabet via crypto/rand, using rejection sampling so
// 256 not being a multiple of the alphabet's length introduces no bias.
func randomEnrolToken() string {
	const alphabet = enrolTokenAlphabet
	limit := (256 / len(alphabet)) * len(alphabet)
	out := make([]byte, enrolTokenLen)
	buf := make([]byte, 1)
	for i := range out {
		for {
			if _, err := rand.Read(buf); err != nil {
				// crypto/rand.Read failing means the OS's CSPRNG is
				// unavailable -- not a condition worth degrading
				// gracefully from for a token that grants a router
				// syslog-source identity. Same convention as
				// internal/auth's newID.
				panic("device: crypto/rand unavailable: " + err.Error())
			}
			if int(buf[0]) < limit {
				out[i] = alphabet[int(buf[0])%len(alphabet)]
				break
			}
		}
	}
	return string(out)
}

// MintEnrolment mints a fresh enrolment token for device, replacing any
// pending one -- this is also the "Reroll" affordance: minting again on
// an already-pending device simply invalidates the old token and hands
// back a new one. Returns the raw token (shown exactly once -- only its
// hash is ever stored) and its expiry.
func (r *Registry) MintEnrolment(device, expected string, now time.Time) (token string, expiresAt time.Time, err error) {
	// The expected address is required (issue #1291): it is what the
	// enrolment window binds to, and a token minted without one would be
	// the old global gate again under a new name.
	key := normalizeIP(strings.TrimSpace(expected))
	if key == "" {
		return "", time.Time{}, ErrExpectedAddressRequired
	}
	if _, parseErr := netip.ParseAddr(key); parseErr != nil {
		return "", time.Time{}, ErrExpectedAddressInvalid
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[device]; !ok {
		return "", time.Time{}, ErrDeviceNotFound
	}
	r.burnPendingLocked(device)

	token = randomEnrolToken()
	expiresAt = now.Add(enrolTokenTTL)
	hash := hashEnrolToken(token)
	r.pendingByDevice[device] = pendingToken{hash: hash, expiresAt: expiresAt, expected: key}
	r.pendingByHash[hash] = device
	return token, expiresAt, nil
}

// BurnEnrolment revokes device's pending enrolment token, if it has
// one. Reports ErrDeviceNotFound for an unknown device and
// ErrNoPendingEnrolment for a known one with nothing pending -- the two
// distinct 404s DELETE /api/devices/{id}/enrolment answers with.
func (r *Registry) BurnEnrolment(device string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[device]; !ok {
		return ErrDeviceNotFound
	}
	if _, ok := r.pendingByDevice[device]; !ok {
		return ErrNoPendingEnrolment
	}
	r.burnPendingLocked(device)
	return nil
}

// burnPendingLocked removes device's pending token, if any, from both
// index maps. A no-op for a device with nothing pending. Must be called
// with r.mu held.
func (r *Registry) burnPendingLocked(device string) {
	p, ok := r.pendingByDevice[device]
	if !ok {
		return
	}
	delete(r.pendingByDevice, device)
	delete(r.pendingByHash, p.hash)
}

// PendingEnrolment reports device's current pending-token state for GET
// /api/devices' enrolment field. An unknown device or one with nothing
// pending both answer {false, zero} -- the caller does not need to tell
// those apart, since either way there is nothing to show.
func (r *Registry) PendingEnrolment(device string) Enrolment {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.pendingByDevice[device]
	if !ok {
		return Enrolment{}
	}
	return Enrolment{Pending: true, ExpiresAt: p.expiresAt}
}

// VerifyPendingToken reports whether raw is device's current pending
// enrolment token and has not expired -- POST /api/setup/commands'
// check before rendering the "mikroview-enrol <raw>" line, so a stale
// or unrelated value the caller echoes back can never be embedded in a
// command the operator is told to paste. Never mutates: unlike TryEnrol
// this does not consume the token, since rendering a command must be
// safe to repeat.
func (r *Registry) VerifyPendingToken(device, raw string, now time.Time) bool {
	if raw == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.pendingByDevice[device]
	if !ok || now.After(p.expiresAt) {
		return false
	}
	return p.hash == hashEnrolToken(raw)
}

// AcceptsConnectionFrom reports whether host is the address some
// unexpired pending enrolment token was minted for -- the connection
// gate, narrowed by issue #1291.
//
// It replaces #1281's AcceptsUnknown, which asked only whether any
// token was pending anywhere and so left the syslog port reachable by
// every unknown address on the network for the whole life of any
// enrolment. The window enrolment actually needs is one address: the
// router the operator is enrolling, which they already named when they
// minted the token. Everyone else is refused at accept, before TLS and
// before a byte is read, exactly as an unknown address was before a
// token existed.
//
// Once every pending token is burned (TryEnrol) or expires, this
// reports false for every host and the port is closed to unknown
// addresses again. Called once per accepted TCP connection, not per
// line, so a full walk of pendingByDevice is cheap enough here even
// though Allowed's map lookups are preferred on the hotter per-line
// path.
func (r *Registry) AcceptsConnectionFrom(host string) bool {
	key := normalizeIP(host)
	if key == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := time.Now()
	for _, p := range r.pendingByDevice {
		if now.Before(p.expiresAt) && p.expected == key {
			return true
		}
	}
	return false
}

// Allowed reports whether host is already some device's sourceIp or
// acceptedIp -- the listener gate's fast path (internal/syslog.
// EnrolmentGate), one map lookup each. Registry satisfies
// syslog.EnrolmentGate structurally; that package declares the
// interface so it need not import this one.
func (r *Registry) Allowed(host string) bool {
	key := normalizeIP(host)
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.byIP[key]; ok {
		return true
	}
	_, ok := r.byAcceptedIP[key]
	return ok
}

// IsEnrolledAt reports whether device's own sourceIp or acceptedIp
// equals host -- issue #1281's ingest-side check: a push carries a
// valid token naming device, but the token itself carries no address
// (Ingest tokens carry no address by design), so the connection it
// arrives on has to be checked against the device it claims to be
// separately from Allowed, which only asks "is host anyone's".
func (r *Registry) IsEnrolledAt(device, host string) bool {
	key := normalizeIP(host)
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.byID[device]
	if !ok {
		return false
	}
	return (info.SourceIP != "" && normalizeIP(info.SourceIP) == key) ||
		(info.AcceptedIP != "" && normalizeIP(info.AcceptedIP) == key)
}

// TryEnrol inspects one line for the enrolment marker; if it names a
// device's current, unexpired, unused pending token, that device is
// enrolled at host (AcceptedIP/EnrolledAt set, the token burned) and
// TryEnrol reports true. Any other line -- no marker, an expired token,
// one that names no device, or one already burned -- reports false and
// changes nothing, leaving the caller to count it against host via
// Refuse.
//
// Re-enrolling an already-enrolled device (minting again, then a line
// arriving from a new address) replaces its AcceptedIP: the old address
// is removed from byAcceptedIP and the new one takes its place, exactly
// once, under this same lock.
func (r *Registry) TryEnrol(host string, line []byte) bool {
	m := enrolLineRE.FindSubmatch(line)
	if m == nil {
		return false
	}
	token := string(m[1])
	hash := hashEnrolToken(token)

	r.mu.Lock()
	defer r.mu.Unlock()

	device, ok := r.pendingByHash[hash]
	if !ok {
		return false
	}
	p := r.pendingByDevice[device]
	now := time.Now()
	if now.After(p.expiresAt) {
		return false
	}
	// The token is redeemable from the one address it was minted for and
	// nowhere else (issue #1291). The connection gate
	// (AcceptsConnectionFrom) already refuses every other address before
	// a byte is read, so reaching here from the wrong one means the
	// address was allowed for some other reason -- it is another
	// device's declared or enrolled address. Checking again here keeps
	// the rule true of the redemption itself rather than only of the
	// door in front of it, so no future change to the accept path can
	// quietly widen what a token accepts.
	if normalizeIP(host) != p.expected {
		r.refuseLocked(normalizeIP(host))
		deviceLog.Info("refused enrolling " + device + " at " + normalizeIP(host) + ": the token was minted for " + p.expected)
		return false
	}
	info, ok := r.byID[device]
	if !ok {
		// The device was deleted after the token was minted -- burn the
		// now-orphaned token rather than leave it redeemable forever.
		r.burnPendingLocked(device)
		return false
	}

	key := normalizeIP(host)
	// An address belongs to one router. Taking one another device
	// already holds -- declared in config.yaml, or enrolled earlier --
	// would leave both rows claiming it while Resolve's own order gave
	// every line to just one of them: the loser reads "Enrolled at
	// <address>" on its card and receives nothing, with nothing
	// anywhere saying why. Refuse instead, and leave the token unspent
	// so the operator can redeem it from the right address without
	// rerolling. The address is counted as refused, so the wizard's
	// warning box shows that something arrived and was not accepted.
	if held, taken := r.byIP[key]; taken && held.ID != device {
		r.refuseLocked(key)
		deviceLog.Info("refused enrolling " + device + " at " + key + ": declared as " + held.ID)
		return false
	}
	if held, taken := r.byAcceptedIP[key]; taken && held.ID != device {
		r.refuseLocked(key)
		deviceLog.Info("refused enrolling " + device + " at " + key + ": already enrolled as " + held.ID)
		return false
	}

	if info.AcceptedIP != "" {
		delete(r.byAcceptedIP, normalizeIP(info.AcceptedIP))
	}
	info.AcceptedIP = key
	info.EnrolledAt = now
	r.byAcceptedIP[key] = info
	// The router logged its own logging-action change before this line
	// reached us, so its address is already in the refused list; leaving
	// it there would show the router just enrolled as a wrong sender.
	delete(r.refused, key)
	r.burnPendingLocked(device)
	r.persistLocked()
	deviceLog.Info("enrolled " + device + " at " + key)
	return true
}

// refuseLocked records one refusal against host, whether it came from a
// rejected line (Refuse) or a rejected connection (RefuseConnection) --
// the two count identically into the one refused-senders list, since
// issue #1281's ruling is that a connection refused at accept time must
// show up in the wizard's warning box exactly like a refused line does.
// Must be called with r.mu held.
func (r *Registry) refuseLocked(host string) {
	key := normalizeIP(host)
	now := time.Now()
	ref, ok := r.refused[key]
	if !ok {
		ref = &Refused{Address: key, FirstSeen: now}
		r.refused[key] = ref
	}
	ref.LastSeen = now
	ref.Lines++
	r.pruneRefusedLocked()
}

// Refuse records that a line from host was neither already allowed nor
// a valid enrolment line -- the other half of the listener gate
// (internal/syslog.EnrolmentGate), feeding GET /api/devices/refused.
// The line's content is not retained, only that one arrived: this is a
// count against an unauthenticated address, not evidence to display.
func (r *Registry) Refuse(host string, line []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refuseLocked(host)
}

// RefuseConnection records that host's TCP connection was refused at
// accept time -- before any line, and before the TLS handshake, ever
// had a chance to run -- because host is neither Allowed nor is any
// enrolment token currently pending anywhere (see AcceptsUnknown).
// Counts into the same refused-senders list as Refuse, via the same
// Lines field, so the wizard's warning box shows a refused connection
// the same way it shows a refused line.
func (r *Registry) RefuseConnection(host string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refuseLocked(host)
}

// Refused returns every refused source address, in address order.
func (r *Registry) Refused() []Refused {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Refused, 0, len(r.refused))
	for _, ref := range r.refused {
		out = append(out, *ref)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

// EnrolFromPushedAddresses is the "Upgrading to 0.6.0" one-shot nudge
// (docs/upgrades.md): at the first boot after issue #1281 shipped, a
// device with no AcceptedIP whose pushed /ip/address table is the sole
// claimant of one of its own addresses is enrolled at it, exactly once,
// persisted, and logged at Info -- the same "sole claimant" evidence
// Resolve used to trust on every line, offered here as a one-time,
// visible migration step instead of a standing security decision.
// Naturally idempotent: it only ever looks at devices with no
// AcceptedIP yet, so a device this already enrolled (by this call or by
// a real token) is left alone on every later boot.
//
// Call once at startup, after the router-state store this reads
// (addresses) has loaded whatever it persists (routerstate itself is
// memory-only today, so in practice this only ever finds evidence from
// pushes that arrive before this call -- which is none, at boot; the
// nudge is offered again on the next call this process makes, if the
// caller chooses to call it more than once, but main.go calls it only
// at startup, matching the "once" the issue asks for as closely as a
// process with no persisted router state can). Returns the ids enrolled
// this call, for the boot log line.
func (r *Registry) EnrolFromPushedAddresses(addresses AddressTables, now time.Time) []string {
	if addresses == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// owners[address] = the one device whose pushed table carries it, or
	// "" once a second device also claims it (so it is dropped as
	// evidence, not attributed to the first one seen).
	owners := make(map[string]string)
	seenMultiple := make(map[string]bool)
	for _, dev := range addresses.Devices() {
		entries, _, ok := addresses.IPAddresses(dev)
		if !ok {
			continue
		}
		for _, e := range entries {
			addr := addressOf(e.Address)
			if addr == "" || seenMultiple[addr] {
				continue
			}
			if existing, ok := owners[addr]; ok && existing != dev {
				delete(owners, addr)
				seenMultiple[addr] = true
				continue
			}
			owners[addr] = dev
		}
	}

	// Invert to deviceID -> its sole-claimed addresses, so a
	// still-unenrolled device with several can be given a deterministic
	// pick rather than whichever the map happened to iterate last.
	byDevice := make(map[string][]string)
	for addr, dev := range owners {
		byDevice[dev] = append(byDevice[dev], addr)
	}

	var enrolled []string
	for dev, addrs := range byDevice {
		info, ok := r.byID[dev]
		if !ok || info.AcceptedIP != "" {
			continue
		}
		sort.Strings(addrs)
		key := normalizeIP(addrs[0])
		// Same rule as TryEnrol: never take an address another device
		// already holds.
		if held, taken := r.byIP[key]; taken && held.ID != dev {
			continue
		}
		if held, taken := r.byAcceptedIP[key]; taken && held.ID != dev {
			continue
		}
		info.AcceptedIP = key
		info.EnrolledAt = now
		r.byAcceptedIP[key] = info
		delete(r.refused, key)
		enrolled = append(enrolled, dev)
	}
	if len(enrolled) > 0 {
		sort.Strings(enrolled)
		r.persistLocked()
		for _, dev := range enrolled {
			deviceLog.Info("upgrade: enrolled " + dev + " at " + r.byID[dev].AcceptedIP + " (sole claimant of its own pushed address)")
		}
	}
	return enrolled
}
