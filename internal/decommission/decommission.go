// SPDX-License-Identifier: AGPL-3.0-only

// Package decommission holds the retirement of a network segment as a
// state machine rather than as an event (issue #460, owner ruling
// 2026-08-17).
//
// The owner's framing, preserved because it is the whole design:
// *decommissioning isn't done when the config is deleted -- it's done
// when the traffic stops.* Deleting a subnet from the router does not
// remove it from mikroview's map while its traffic persists. It enters a
// draining state, and it leaves only after a clean window -- measured in
// hours, not days -- during which nothing at all was seen to or from the
// retired range.
//
// One object, one state machine: the draining segment IS the watch. There
// is no separate "decommission watch" artifact that suggests its own
// retirement later; the clean window's expiry is the retirement.
//
// # What this package is and is not
//
// This package owns the record and the state rule. It does not evaluate
// events (internal/engine's DecommissionWatches does, wearing the
// chassis's envelope), it does not decide when a segment departed
// (internal/routerstate observes that from the pushed address table), and
// it does not talk to the router -- nothing here initiates anything, per
// AGENTS.md's observe-never-probe invariant.
//
// The split matters for one structural reason beyond tidiness:
// internal/routerstate and internal/engine may not import each other in
// either direction (internal/routerstate/isolation_test.go enforces it,
// #186 step 4d), so pushed router data cannot reach the evaluation
// machinery through a package edge. The enrichment a watch carries --
// which addresses in the range the router last named -- is therefore
// snapshotted into the Watch record as plain data at the moment the
// segment departs, by an explicit caller that argues its case
// (internal/api/decommission.go), and the engine only ever reads that
// frozen snapshot. That is also exactly what #460 asks for: "was
// 'garage-cam' until 2026-08-01" is a last-known claim about the past,
// not a live lookup.
package decommission

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

// State is how a retiring segment is painted, and what it is doing.
//
// The four values are the ones the record settles, and the reasoning for
// each boundary is worth stating because two separate owner rulings meet
// here: the 2026-08-17 lifecycle note ("active -> draining -> gone", with
// draining rendered as alerting) and #485's ratified surface, on which
// the ghost zone is "painted by its watch state (holding / broken)".
//
//   - StateHolding: the watch is running and nothing has ever straggled.
//     The clean window is counting down from creation. Quiet, and quiet
//     is a claim this state is entitled to make because StateBroken below
//     takes the cases where it would not be.
//   - StateDraining: traffic to or from the retired range has been seen.
//     The segment is still emptying. Alerting, per the owner's
//     preference for red/orange over greying out -- live traffic on a
//     dead segment is a problem being reported, not history being
//     remembered. Every observation resets the clean-window clock.
//   - StateBroken: nothing logs this range, so mikroview cannot tell
//     whether it is quiet. Not a quiet state and not an alerting one: an
//     unanswerable one. This is the existing broken-ring meaning (#546)
//     applied to a decommission watch, and it outranks the other two,
//     because "no straggler seen" from a pathway nobody is logging is the
//     absence-of-detection-presented-as-absence-of-threat failure.
//   - StateRetired: the clean window elapsed with nothing seen. The watch
//     is done and the zone leaves the map.
//
// Why "traffic seen at all" and not "traffic seen recently" separates
// draining from holding: the clean-window clock already carries recency
// (it restarts on every observation, so "retires in N hours" is the live
// countdown an operator reads), and a second, shorter recency threshold
// would be a number nothing in the record chooses. A segment that has
// straggled once is draining until it has been quiet long enough to
// retire -- which is precisely the sentence the owner ratified.
type State string

const (
	StateHolding  State = "holding"
	StateDraining State = "draining"
	StateBroken   State = "broken"
	StateRetired  State = "retired"
)

// DefaultCleanWindow is the hours-scale default the 2026-08-17 ruling
// calls for ("window measured in hours, not days -- param, hours-scale
// default"). Six hours is long enough that a device which only speaks on
// a slow timer -- a camera uploading hourly, a backup job -- gets a real
// chance to reveal itself, and short enough that a genuinely dead segment
// leaves the map within a working day rather than lingering as furniture.
//
// It is a parameter, not a constant, exactly as the ruling requires: see
// Watch.CleanWindow, which every state answer reads instead of this.
const DefaultCleanWindow = 6 * time.Hour

// MinCleanWindow and MaxCleanWindow bound what an operator may configure.
// The floor keeps the window meaningfully longer than the ingest jitter
// and push cadence around it (routerstate's own pushes are 15-30 minutes
// apart), so a watch cannot retire before the evidence that would have
// contradicted it could plausibly have arrived. The ceiling is the
// "hours, not days" ruling made checkable rather than remembered.
const (
	MinCleanWindow = time.Hour
	MaxCleanWindow = 48 * time.Hour
)

// Straggler is one address inside a retiring range that the router had a
// name for, frozen at the moment the segment departed.
//
// Snapshotted rather than looked up live, for the import-edge reason in
// the package comment, and honest by construction about the absence rule
// AGENTS.md states: an entry exists only where a push actually covered
// that address. A range no push ever named yields no stragglers at all,
// and a violation from it is reported as a bare address rather than
// dressed in a guess.
type Straggler struct {
	// Address is the exact address the router named.
	Address string `json:"address"`
	// Name is what the router called it -- a DHCP lease hostname, a DNS
	// static entry's name.
	Name string `json:"name"`
	// Source names the pushed table the name came from, using
	// internal/routerstate's own HostSource* vocabulary so the two cannot
	// drift apart. Empty for an address seen in the ARP table with no
	// name attached.
	Source string `json:"source,omitempty"`
	// SeenAt is when the push that carried this name arrived -- the
	// "until 2026-08-01" half of #460's worked example.
	SeenAt time.Time `json:"seenAt"`
}

// Watch is one retiring segment: the record, the clock, and everything
// needed to paint it.
//
// This single struct is what "one object, one state machine" means. There
// is no second artifact for the expectation, no third for the map's
// ghost: the map draws this, the watchlist lists this, and the engine
// evaluates this.
type Watch struct {
	ID string `json:"id"`
	// CIDR is the retired range, canonicalised to its network prefix, so
	// two watches for the same range are the same watch however the
	// router happened to write the address.
	CIDR string `json:"cidr"`
	// Device and Interface are where the segment stood. Interface is the
	// zone identity the topography keys on (see frontend zones), so a
	// ghost is drawn in the lane its zone occupied rather than appended
	// somewhere new.
	Device    string `json:"device"`
	Interface string `json:"interface"`
	// Name is the zone's operator-facing name at the moment it departed
	// -- the pushed address comment where there was one, else the
	// interface. Frozen, because the thing that would supply it is gone.
	Name string `json:"name"`

	// CreatedAt is when the watch was accepted, which is also when its
	// first clean window started.
	CreatedAt time.Time `json:"createdAt"`
	// CleanWindow is how long this range must stay silent to retire. Per
	// watch, not global: a range that hosted a nightly job honestly needs
	// a longer window than a range of desk phones.
	CleanWindow time.Duration `json:"cleanWindow"`

	// LastTrafficAt is the most recent observation to or from the range,
	// zero if there has never been one. It is also the clock: the clean
	// window is measured from here when it is set, and from CreatedAt
	// when it is not. Keeping one field rather than a separate
	// clockStartedAt is deliberate -- two fields that must agree are two
	// fields that can disagree, and every reset writes the same instant
	// to both anyway.
	LastTrafficAt time.Time `json:"lastTrafficAt,omitzero"`
	// TrafficCount is how many observations this watch has taken. Not a
	// threshold -- one straggler is a violation -- but the operator's
	// answer to "is this one stubborn device or the whole subnet".
	TrafficCount int `json:"trafficCount"`

	// Covered records whether anything is logging traffic that would
	// reach this range. False makes the watch StateBroken: it cannot
	// answer, and says so rather than reporting quiet.
	Covered bool `json:"covered"`

	// Detached is the force-remove escape hatch (owner ruling,
	// 2026-08-17). The segment leaves the map immediately; the watch
	// survives as a standalone entry in the watchlist and finishes the
	// same job, retiring on the same clean window. Nothing is silently
	// dropped either way -- the two paths converge on the same end state.
	Detached bool `json:"detached"`
	// ForcedAt/ForcedBy/ForcedReason are the recorded override the #385
	// pattern requires alongside the heavy warning. Present only when
	// Detached.
	ForcedAt     time.Time `json:"forcedAt,omitzero"`
	ForcedBy     string    `json:"forcedBy,omitempty"`
	ForcedReason string    `json:"forcedReason,omitempty"`

	// RetiredAt is set once the clean window has been observed to
	// elapse. It is a recorded fact rather than a recomputation, so a
	// watch that retired stays retired even if the clock is later
	// adjusted -- StateAt still derives retirement from the window for a
	// watch that has not been swept yet, so nothing depends on the sweep
	// having run.
	RetiredAt time.Time `json:"retiredAt,omitzero"`

	// LastKnown is the frozen enrichment (see Straggler).
	LastKnown []Straggler `json:"lastKnown,omitempty"`
	// ReplayCount is the receipt shown at the moment of the offer: how
	// many stragglers this watch would have caught over the corpus it was
	// replayed against. Kept because the number is the argument for the
	// watch existing, and an operator revisiting it a day later deserves
	// to see what convinced them.
	ReplayCount int `json:"replayCount"`
	// ReplaySpan is the "in the last N hours" the count was measured
	// over. A count without its span is not a receipt.
	ReplaySpan time.Duration `json:"replaySpan"`
}

// ErrNotCovered and friends are the validation vocabulary.
var (
	ErrNoCIDR       = errors.New("decommission: a watch needs the range it retires")
	ErrBadCIDR      = errors.New("decommission: the retired range is not a CIDR")
	ErrWindowRange  = fmt.Errorf("decommission: the clean window must be between %s and %s", MinCleanWindow, MaxCleanWindow)
	ErrNoSuchWatch  = errors.New("decommission: no such watch")
	ErrAlreadyEnded = errors.New("decommission: that watch has already retired")
)

// NormaliseCIDR reduces a router-written address to the network prefix
// the watch is about.
//
// RouterOS writes an /ip/address row as the interface's own address with
// the prefix length ("192.0.2.1/24"), so the retired *range* is that
// masked to its network. Doing this once, here, is what makes the CIDR a
// usable identity: without it the same segment re-offered from a
// different router interface address would create a second watch for the
// same range.
func NormaliseCIDR(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ErrNoCIDR
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrBadCIDR, s)
	}
	return p.Masked().String(), nil
}

// Validate rejects a watch that could not answer the question it exists
// to ask.
func (w Watch) Validate() error {
	if _, err := NormaliseCIDR(w.CIDR); err != nil {
		return err
	}
	if w.CleanWindow < MinCleanWindow || w.CleanWindow > MaxCleanWindow {
		return ErrWindowRange
	}
	return nil
}

// clockFrom is the instant the current clean window started: the last
// observation if there has been one, else the watch's creation.
func (w Watch) clockFrom() time.Time {
	if !w.LastTrafficAt.IsZero() {
		return w.LastTrafficAt
	}
	return w.CreatedAt
}

// Remaining is how much longer this range must stay silent before it
// retires, at now. Zero once the window has elapsed.
//
// This is the number the ghost's caption reads ("retires in 4h"), and it
// is deliberately the same computation StateAt uses rather than a
// parallel one -- a countdown that could disagree with the state it sits
// beside is worse than no countdown.
func (w Watch) Remaining(now time.Time) time.Duration {
	if !w.RetiredAt.IsZero() {
		return 0
	}
	elapsed := now.Sub(w.clockFrom())
	if elapsed >= w.CleanWindow {
		return 0
	}
	return w.CleanWindow - elapsed
}

// StateAt is the whole state machine, as a pure function of the record
// and the clock.
//
// Pure on purpose: the state is never stored, so it cannot go stale
// against the record, and there is no transition that has to be driven by
// a tick arriving on time. A sweep (Store.Sweep) exists to *record*
// retirement so the map can stop drawing a ghost without waiting to be
// asked, but every reader gets the right answer whether or not the sweep
// has run.
//
// Order matters, and each rung is a claim about what mikroview can
// honestly say:
//
//  1. Already recorded as retired -- a fact, not a recomputation.
//  2. Broken -- it cannot see this range, so neither "quiet" nor
//     "draining" is a claim it is entitled to make. This outranks the
//     window: a watch nobody is feeding would otherwise "retire clean"
//     purely because no evidence could reach it, which is the exact
//     failure #546's broken ring exists to prevent.
//  3. The window elapsed with no observation since the clock started --
//     retired.
//  4. Traffic has been seen at all -- draining.
//  5. Otherwise holding.
func (w Watch) StateAt(now time.Time) State {
	if !w.RetiredAt.IsZero() {
		return StateRetired
	}
	if !w.Covered {
		return StateBroken
	}
	if now.Sub(w.clockFrom()) >= w.CleanWindow {
		return StateRetired
	}
	if !w.LastTrafficAt.IsZero() {
		return StateDraining
	}
	return StateHolding
}

// Contains reports whether ip falls inside this watch's retired range.
// Both directions of an event are asked separately by the caller -- see
// Matches -- because "any traffic to or from it" is the ratified rule.
func (w Watch) Contains(ip string) bool {
	if ip == "" {
		return false
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	p, err := netip.ParsePrefix(w.CIDR)
	if err != nil {
		return false
	}
	return p.Masked().Contains(addr)
}

// Matches reports whether an event's source or destination falls in the
// retired range -- the "to or from" the concept turns on. A retired range
// is not a direction: a forgotten firewall rule still steering packets
// *at* a dead range is as much a straggler as a device still speaking
// *from* one.
func (w Watch) Matches(srcIP, dstIP string) bool {
	return w.Contains(srcIP) || w.Contains(dstIP)
}

// NameFor returns the router's last-known name for ip and when it was
// last seen under that name, from this watch's frozen snapshot.
//
// The absence rule, restated because it is the point: no entry means no
// name, never a guessed one. A caller that gets ("", zero) reports the
// bare address.
func (w Watch) NameFor(ip string) (name string, seenAt time.Time) {
	for _, s := range w.LastKnown {
		if s.Address == ip {
			return s.Name, s.SeenAt
		}
	}
	return "", time.Time{}
}

// Describe renders the straggler sentence #460 asks for -- "traffic from
// 192.0.2.7 -- was 'garage-cam' until 2026-08-01" -- degrading to the
// bare address where no push ever named it.
func (w Watch) Describe(ip string) string {
	name, seenAt := w.NameFor(ip)
	if name == "" {
		return fmt.Sprintf("traffic from %s", ip)
	}
	if seenAt.IsZero() {
		return fmt.Sprintf("traffic from %s -- was %q", ip, name)
	}
	return fmt.Sprintf("traffic from %s -- was %q until %s", ip, name, seenAt.Format("2006-01-02"))
}

// RecordTraffic takes one observation: the count rises and the clean
// window starts again from this instant.
//
// The reset is the ruling ("any traffic to or from the retired CIDR is a
// violation; the clean-window clock resets"), and it is what makes the
// watch's end state mean something -- a segment retires because it went
// quiet and stayed quiet, never because enough wall-clock time passed
// while it kept talking.
//
// An observation at or before the current clock is ignored rather than
// rewinding it: events carry the router's own self-reported time, which
// internal/store documents as not monotonic with arrival order, and a
// skewed clock must not be able to shorten a window.
func (w *Watch) RecordTraffic(at time.Time) {
	if !w.RetiredAt.IsZero() {
		return
	}
	w.TrafficCount++
	if at.After(w.clockFrom()) {
		w.LastTrafficAt = at
	} else if w.LastTrafficAt.IsZero() {
		// First observation, but stamped at or before creation. The
		// count is real, so the clock must move off "never seen"
		// regardless -- otherwise the watch would report holding while
		// holding evidence to the contrary.
		w.LastTrafficAt = w.CreatedAt
	}
}
