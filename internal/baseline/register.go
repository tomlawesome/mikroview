// SPDX-License-Identifier: AGPL-3.0-only

// Package baseline is the line register behind the living topology's
// second always-on rule (issue #1016, round 49): "colour is the verdict;
// brightness is the baseline".
//
// A *line* is `source → destination · port · proto`. A line seen on at
// least N distinct days of the last M is *established* and recedes on
// the map -- thin, dim, no flow. A line off that pattern is
// *off-baseline*: full width, bright, flow dashes. The owner's purpose
// for it is the sieve (2026-09-07): "make traffic that should not be
// happening easy to see, and whittle the noise down to genuine
// candidates, without arbitrarily hiding anything that could be bad. An
// established network sits and talks on the same routes and ports, so
// what matters is a line off that pattern -- traffic more than devices."
//
// This package holds the recurrence half. Like internal/hosts it records
// nothing the feed did not already show and asks the network nothing --
// mikroview observes, it never probes (see AGENTS.md) -- so a line
// exists here only because an event carrying it arrived.
//
// # What counts as a line
//
// The host register's rule, plus a destination. Registers defers to
// hosts.Registers for the source half rather than copying it, so the two
// cannot drift: the source must be a non-public address arriving on an
// inbound interface. The destination is deliberately *not* constrained
// to private space -- a private host talking to somewhere new on the
// Internet is exactly the case the sieve exists to surface.
//
// # Recurrence, not history
//
// Each line carries a bitmap of which of the last MaxDays calendar days
// (server local date) it was seen on, bit 0 being the day the line's
// Anchor names. The bitmap rolls forward when the date changes; it is
// never recomputed from an event history, because no event history that
// far back is kept. That makes establishment O(1) to answer and costs
// four bytes per line.
//
// Reads roll the bitmap forward *virtually* (see Line.daysMask) rather
// than mutating, so a line last seen a week ago answers correctly
// without the register having to sweep every entry at midnight.
//
// # The cap
//
// MaxLines bounds the register. A line is keyed on four attacker-
// influenced fields, so it is combinatorially larger than the host
// register's address-only key and the ceiling matters more, not less: a
// spoofed source is one forged field in one syslog line. At the cap the
// oldest LastSeen *without* an expected mark is evicted, for the same
// reason internal/hosts evicts the oldest unmarked host -- an operator's
// own statement is the one thing here that cannot be rebuilt from the
// feed, so it is the last thing dropped.
//
// # Persistence
//
// Optional JSON through internal/persist, write-behind and rate-limited,
// exactly as internal/hosts and internal/flags do it -- never a disk
// write on the ingest path. Persistence is not a nicety here: a baseline
// that resets on restart would read every established line as new, which
// is precisely the state the feature exists to distinguish from.
package baseline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/bits"
	"sort"
	"strconv"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tomlawesome/mikroview/internal/hosts"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var persistLog = logging.New("baseline")

// MaxLines is the hard ceiling on how many lines the register holds --
// see the package comment for why it exists.
//
// Five times internal/hosts' MaxHosts, not because a real network has
// five times the lines (it has many more per host), but because the two
// are bounding different risks against different costs. A line is about
// 150 bytes, so this ceiling is a few megabytes: large enough that a
// settled network of a few hundred hosts never reaches it -- such a
// network repeats a small set of routes and ports, which is the very
// assumption the baseline rests on -- and small enough that a forged
// source, destination or port cannot grow the register without bound.
//
// Reaching it is itself a signal, and is logged as one (see shed).
const MaxLines = 50_000

// MaxDays is how many calendar days the recurrence bitmap can hold, set
// by its width: one uint32, bit 0 being the Anchor day. Of is validated
// against this, so a configured window can never silently exceed what
// the bitmap can answer for.
const MaxDays = 32

// Default thresholds, ratified by the owner on 2026-09-07 and written
// into docs/design/screens/city/DESIGN.md: "a line seen on 3 distinct
// days of the last 14 is established". Both are configurable.
const (
	DefaultDays = 3
	DefaultOf   = 14
)

// maxKeyLength/maxReasonLength bound an expected mark's key and reason,
// the same ceilings and reasoning as internal/hosts': generous next to
// any real four-part line key (two IPv6 addresses, a port and a protocol
// name reach about 110 characters) or human-written note, tight enough
// that the UI and the audit trail stay renderable.
const (
	maxKeyLength    = 160
	maxReasonLength = 400
)

// Outcome is the worst verdict a line drew on a given day. Only two
// values reach the map, because only two are verdicts: the design's
// table is "colour is the verdict", accept green and refused red.
type Outcome string

const (
	// OutcomeAccept: nothing on this line was refused today.
	OutcomeAccept Outcome = "accept"
	// OutcomeDrop: at least one event on this line was dropped or
	// rejected today. Worse than accept, and sticky for the day -- a line
	// that was refused once has been refused, and a later accept does not
	// unsay it.
	OutcomeDrop Outcome = "drop"
)

// worse reports whether b is a worse outcome than a. Only drop is worse
// than anything, which keeps "worst outcome today" a one-line rule.
func worse(a, b Outcome) bool { return b == OutcomeDrop && a != OutcomeDrop }

// Config is the establishment threshold: seen on Days distinct days out
// of the last Of. Held on the Register so a reader cannot answer
// "established?" against a different threshold than the API reports.
type Config struct {
	Days int `json:"days"`
	Of   int `json:"of"`
}

// DefaultConfig is the ratified 3-of-14.
func DefaultConfig() Config { return Config{Days: DefaultDays, Of: DefaultOf} }

// Normalise clamps a configured threshold to something answerable,
// returning the usable value. Out-of-range input falls back to the
// ratified default rather than to an arbitrary edge: a window of zero
// days would make every line established and a window of zero length
// would make none, and both are worse than the documented default.
// internal/config validates and warns before this is reached; this is
// the belt to that braces, for callers constructing a Register directly.
func (c Config) Normalise() Config {
	if c.Of <= 0 || c.Of > MaxDays {
		c.Of = DefaultOf
	}
	if c.Days <= 0 || c.Days > c.Of {
		c.Days = DefaultDays
	}
	if c.Days > c.Of {
		c.Days = c.Of
	}
	return c
}

// Expected is an operator's statement that a line is meant to be there.
//
// It is the only way a line leaves the bright state early (DESIGN.md,
// "Saying it is expected"): a reason, recorded with who said it. By and
// At are always set server-side, from the session and the clock -- a
// caller never gets to sign a mark with somebody else's name or backdate
// it, the same convention hosts.Mark and coverage.Declaration follow.
type Expected struct {
	Reason string    `json:"reason"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
}

// Line is one entry in the register: a source/destination/port/protocol
// the feed has shown, how its recurrence stands, and whatever an
// operator has said about it.
type Line struct {
	Key   string `json:"key"`
	SrcIP string `json:"srcIp"`
	DstIP string `json:"dstIp"`
	// Port is 0 when the event carried no destination port (store.Event
	// uses the same convention), which the key renders as an empty field
	// and the API as JSON null.
	Port  int    `json:"port,omitempty"`
	Proto string `json:"proto,omitempty"`

	// Days is the recurrence bitmap: bit n set means the line was seen on
	// the day n days before Anchor. Bit 0 is Anchor itself.
	Days uint32 `json:"days"`
	// Anchor is the day number (see dayNumber) bit 0 of Days refers to.
	Anchor int64 `json:"anchor"`

	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`

	// CountToday and FirstSeenToday describe the Anchor day only, and are
	// reset by the roll forward. The card on a bright element lists
	// exactly these two alongside the line (DESIGN.md).
	CountToday     uint64    `json:"countToday"`
	FirstSeenToday time.Time `json:"firstSeenToday,omitzero"`
	// OutcomeToday is the worst verdict seen on the Anchor day.
	OutcomeToday Outcome `json:"outcomeToday,omitempty"`

	Expected *Expected `json:"expected,omitempty"`
}

// dayNumber maps an instant to the server's local calendar date, as a
// stable integer that subtracts.
//
// Built by rebuilding the local Y/M/D at UTC midnight rather than by
// dividing the Unix time, so the answer is one integer per calendar day
// regardless of the zone's offset and regardless of daylight saving:
// a 23-hour or 25-hour local day still advances the number by exactly
// one. "Server local date" is the design's wording, and it is the right
// one -- an operator reading "seen on 3 of the last 14 days" means their
// own days.
func dayNumber(t time.Time) int64 {
	y, m, d := t.Local().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400
}

// daysMask returns the recurrence bitmap as of day `today`, narrowed to
// the last `of` days: the stored bitmap shifted forward by however many
// days have passed since Anchor, without mutating the line.
//
// This is what lets the register skip a midnight sweep. A line last seen
// a fortnight ago is stale in storage and correct on read, which is the
// cheaper half to be exact about -- reads are per request, rolls would
// be per line per day.
func (l *Line) daysMask(of int, today int64) uint32 {
	delta := today - l.Anchor
	// A negative delta means the line's Anchor is in the future relative
	// to the caller's clock -- possible from a device clock skewed ahead,
	// since ReceivedAt is ours but the roll is driven by event time. The
	// bitmap is read unshifted rather than discarded: treating it as zero
	// would silently unestablish a settled line over a clock wobble.
	if delta < 0 {
		delta = 0
	}
	if delta >= MaxDays {
		return 0
	}
	m := l.Days << uint(delta)
	if of >= MaxDays {
		return m
	}
	return m & ((1 << uint(of)) - 1)
}

// DistinctDays is how many of the last cfg.Of days the line was seen on,
// as of now.
func (l *Line) DistinctDays(cfg Config, now time.Time) int {
	return bits.OnesCount32(l.daysMask(cfg.Of, dayNumber(now)))
}

// Established reports whether the line is on the pattern, and so recedes
// on the map.
//
// Two ways to be established, and the mark is checked first because it
// is unconditional: an expected line is established "from then on"
// (DESIGN.md) whatever its recurrence says, which is the whole point of
// offering the operator the button.
func (l *Line) Established(cfg Config, now time.Time) bool {
	if l.Expected != nil {
		return true
	}
	return l.DistinctDays(cfg, now) >= cfg.Days
}

// SeenOn reports whether the line was seen on the calendar day of now.
func (l *Line) SeenOn(now time.Time) bool {
	return l.Anchor == dayNumber(now) && l.CountToday > 0
}

// KeyFor builds the register's key for a line: `src|dst|port|proto`,
// with the port field empty when the event carried none.
//
// This exact string is the line's identity everywhere -- Go, the API and
// the browser -- so the three cannot disagree about which line an
// operator just called expected. It follows internal/hosts' and
// internal/coverage's pipe-separated style so all three read alike in a
// URL and in an audit line.
func KeyFor(srcIP, dstIP string, port int, proto string) string {
	p := ""
	if port != 0 {
		p = strconv.Itoa(port)
	}
	return srcIP + "|" + dstIP + "|" + p + "|" + proto
}

// Registers reports whether an event puts a line in the register.
//
// The source half is hosts.Registers itself, called rather than copied:
// the design draws lines between the same hosts the presence register
// holds, so a line whose source is not a host by that rule would be a
// line drawn from nowhere. Calling the exported predicate makes drift
// between the two impossible, which a private copy -- the convention
// this codebase uses for *unexported* helpers -- could not promise.
//
// The destination only has to be present. It is deliberately not held to
// the same non-public rule: the sieve's most valuable single case is a
// private host reaching somewhere new on the Internet.
func Registers(iface, srcIP, dstIP string, port int, proto string) bool {
	if !hosts.Registers(iface, srcIP) {
		return false
	}
	if dstIP == "" {
		return false
	}
	// A key the expected endpoints could never address is not worth
	// holding: validation there would refuse it, so it could never be
	// marked, and it would sit consuming one of MaxLines' slots forever.
	// Same reasoning as hosts.Registers' own closing check.
	return validateText(KeyFor(srcIP, dstIP, port, proto), maxKeyLength) == nil
}

// storeFile is the on-disk shape: an object wrapping the line list,
// mirroring internal/hosts' and internal/coverage's storeFile.
type storeFile struct {
	Lines []*Line `json:"lines"`
}

// Register holds every line the feed has shown, keyed by KeyFor. The
// zero value is not usable; construct with Open.
type Register struct {
	mu sync.RWMutex
	// wb is nil when persistence is not configured, the same "nil means
	// off" convention internal/hosts and internal/flags follow.
	wb    *persist.WriteBehind
	byKey map[string]*Line

	cfg Config

	// lastEncode/deferred implement the encode-side half of the rate
	// limit -- see persistLocked.
	lastEncode time.Time
	deferred   bool

	// shed counts observations dropped at the cap because every entry
	// carried an expected mark, over this process's lifetime.
	shed uint64
}

// Open loads path if it exists (a missing file is the expected first-run
// case, not an error) and returns a Register that persists to it from
// then on. An empty path is the expected "persistence not configured"
// case: a fully usable, in-memory-only Register is returned. A document
// that exists but cannot be read or parsed is a hard error, the same
// fail-closed contract as internal/hosts.Open -- a live backend must
// never silently overwrite a document it could not read (#378).
func Open(path string, cfg Config) (*Register, error) {
	if path == "" {
		return OpenWithBackend(nil, cfg)
	}
	return OpenWithBackend(persist.NewFileBackend(path), cfg)
}

// OpenWithBackend is Open against any persist.Backend -- a JSON file by
// default, or Postgres when configured.
func OpenWithBackend(b persist.Backend, cfg Config) (*Register, error) {
	r := &Register{byKey: make(map[string]*Line), cfg: cfg.Normalise()}

	wb, _, err := persist.OpenWriteBehind(context.Background(), b, "the baseline line register", persist.WriteBehindOptions{
		MinInterval: persistMinInterval,
		OnSaveError: func(msg string) { persistLog.Error(msg) },
		OnConflict:  func(msg string) { persistLog.Warn(msg) },
	}, func(data []byte) error {
		var file storeFile
		if err := json.Unmarshal(data, &file); err != nil {
			return err
		}
		for _, l := range file.Lines {
			// A JSON array containing `null` is syntactically valid and
			// unmarshals into a nil *Line -- skipped here so a malformed
			// document cannot crash startup by indexing through a nil
			// pointer, same defensive load as internal/hosts'.
			if l == nil || l.Key == "" {
				continue
			}
			// An expected mark with no reason is dropped rather than kept:
			// the reason *is* the mark (Expect refuses an empty one), so a
			// reasonless mark is a corrupt record that would silently hold
			// a line established and exempt from eviction forever.
			if l.Expected != nil && l.Expected.Reason == "" {
				l.Expected = nil
			}
			r.byKey[l.Key] = l
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	r.wb = wb
	return r, nil
}

// Config returns the threshold this register answers against.
func (r *Register) Config() Config {
	if r == nil {
		return DefaultConfig()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cfg
}

// Observe records that an event carrying this line arrived at time at,
// creating the line if this is the first time it has been seen and
// rolling its recurrence bitmap forward otherwise.
//
// Reports whether anything was recorded: false means the event is not a
// line by Registers' rule, or the register was full of expected entries.
//
// Called once per ingested event, on the single ingest goroutine (see
// main.go's ingestOneRecovered), beside hosts.Register.Observe and with
// the same cost: one mutex-protected map update and, at most once per
// persistMinInterval, one JSON encode -- never a disk write.
func (r *Register) Observe(iface, srcIP, dstIP string, port int, proto string, outcome Outcome, at time.Time) bool {
	// Nil-receiver safe, the same convention hosts.Register.Observe and
	// persist.WriteBehind follow: a caller with no register configured (a
	// test harness, a CLI mode that never serves the API) should not have
	// to nil-check on the ingest path.
	if r == nil || !Registers(iface, srcIP, dstIP, port, proto) {
		return false
	}
	if outcome != OutcomeDrop {
		outcome = OutcomeAccept
	}

	key := KeyFor(srcIP, dstIP, port, proto)
	today := dayNumber(at)

	r.mu.Lock()
	defer r.mu.Unlock()

	if l, ok := r.byKey[key]; ok {
		r.rollLocked(l, today)
		l.CountToday++
		if l.FirstSeenToday.IsZero() {
			l.FirstSeenToday = at
		}
		if worse(l.OutcomeToday, outcome) || l.OutcomeToday == "" {
			l.OutcomeToday = outcome
		}
		if at.After(l.LastSeen) {
			l.LastSeen = at
		}
		if at.Before(l.FirstSeen) {
			l.FirstSeen = at
		}
		// A day that was not already set is structural: it can flip the
		// line from off-baseline to established, which is a change worth
		// reaching disk promptly rather than at the next un-deferred
		// encode. A repeat sighting on a day already recorded is the
		// ordinary high-volume case and is rate-limited.
		structural := l.Days&1 == 0
		l.Days |= 1
		r.persistLocked(structural)
		return true
	}

	if len(r.byKey) >= MaxLines && !r.evictLocked() {
		r.shed++
		if r.shed == 1 || r.shed%1000 == 0 {
			persistLog.Warn(fmt.Sprintf(
				"the baseline line register is full at %d lines and every entry is marked expected, so %d newly-seen line(s) have been dropped -- "+
					"far more distinct source/destination/port/protocol combinations than a settled network produces, which is itself worth investigating",
				MaxLines, r.shed))
		}
		return false
	}

	r.byKey[key] = &Line{
		Key:            key,
		SrcIP:          srcIP,
		DstIP:          dstIP,
		Port:           port,
		Proto:          proto,
		Days:           1,
		Anchor:         today,
		FirstSeen:      at,
		LastSeen:       at,
		CountToday:     1,
		FirstSeenToday: at,
		OutcomeToday:   outcome,
	}
	r.persistLocked(true)
	return true
}

// rollLocked advances a line's recurrence bitmap to day `today` and
// clears the per-day counters if the date has changed.
//
// The bitmap is shifted, never recomputed: there is no event history
// reaching back fourteen days to recompute it from, and the shift is the
// whole reason the bitmap is the right shape for this question. A gap
// wider than the bitmap clears it, which is correct -- a line unseen for
// MaxDays has no recurrence left inside any valid window.
//
// Must be called with r.mu held.
func (r *Register) rollLocked(l *Line, today int64) {
	if l.Anchor == today {
		return
	}
	delta := today - l.Anchor
	if delta < 0 {
		// The event predates the line's anchor day (a device clock skewed
		// behind another's). Fold it into the anchor day rather than
		// rolling backwards, which would drop days already recorded.
		return
	}
	if delta >= MaxDays {
		l.Days = 0
	} else {
		l.Days <<= uint(delta)
	}
	l.Anchor = today
	l.CountToday = 0
	l.FirstSeenToday = time.Time{}
	l.OutcomeToday = ""
}

// evictLocked drops the entry with the oldest LastSeen that carries no
// expected mark, making room for one new line. Reports whether it found
// one: a register in which every entry is marked has nothing evictable,
// and the caller sheds the new observation instead of discarding an
// operator's decision. Must be called with r.mu held.
func (r *Register) evictLocked() bool {
	var oldest *Line
	for _, l := range r.byKey {
		if l.Expected != nil {
			continue
		}
		if oldest == nil || l.LastSeen.Before(oldest.LastSeen) {
			oldest = l
		}
	}
	if oldest == nil {
		return false
	}
	delete(r.byKey, oldest.Key)
	return true
}

// ErrInvalidExpected is returned when the key or reason fails
// validation: text that is empty, too long, not valid UTF-8, or carrying
// a control or Unicode format character. Same rule as
// internal/hosts.validateText, since both flow into the UI and the audit
// trail.
var ErrInvalidExpected = errors.New("baseline: key and reason must be valid text within the length limit, and a reason is required")

// ErrUnknownLine is returned by Expect for a key no event has ever
// registered. Marking a line the feed has never shown would invent
// traffic rather than record it, which is the one thing this package
// must not do.
var ErrUnknownLine = errors.New("baseline: no line with that key has been seen on the feed")

// ValidateKey reports whether key is text this package will address.
// Exported so an HTTP handler can check a key taken from a URL path
// *before* using it -- a key from a route is caller-controlled input,
// and it reaches the audit trail on the way past.
func ValidateKey(key string) error { return validateText(key, maxKeyLength) }

// Expect records an operator's statement that this line is meant to be
// there, which makes it established from now on.
//
// reason is required and is the substance of the mark -- "a reason that
// stays said" (DESIGN.md) -- so an empty one is refused rather than
// stored as a bare flag. by is the actor, taken from the session by the
// caller, never from a request body.
func (r *Register) Expect(key, reason, by string) (Line, error) {
	if r == nil {
		// No register configured: no line can have been seen, so this is
		// the same answer an unknown key gets. Nil-receiver safe for the
		// same reason Observe is -- a caller with no register (a test
		// harness, a CLI mode that never serves the API) should not have
		// to nil-check, and a 404 is a truthful answer here where a panic
		// is not.
		return Line{}, ErrUnknownLine
	}
	if err := validateText(key, maxKeyLength); err != nil {
		return Line{}, err
	}
	if err := validateText(reason, maxReasonLength); err != nil {
		return Line{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	l, ok := r.byKey[key]
	if !ok {
		return Line{}, ErrUnknownLine
	}
	l.Expected = &Expected{Reason: reason, By: by, At: time.Now()}
	r.persistLocked(true)
	return copyLine(l), nil
}

// Unexpect takes the statement back, putting the line back to whatever
// its own recurrence says it is. Reports whether a mark was actually
// there to remove -- an unknown key and an unmarked line both answer
// false, which the API turns into a 404 the same way handleHostMarkDelete
// does: the caller looked this key up from the list, so nothing to remove
// is a meaningful signal rather than routine noise.
func (r *Register) Unexpect(key string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	l, ok := r.byKey[key]
	if !ok || l.Expected == nil {
		return false
	}
	l.Expected = nil
	r.persistLocked(true)
	return true
}

// Get returns a copy of the line at key.
func (r *Register) Get(key string) (Line, bool) {
	if r == nil {
		return Line{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	l, ok := r.byKey[key]
	if !ok {
		return Line{}, false
	}
	return copyLine(l), true
}

// OffToday returns the lines seen today that are not established, sorted
// by key for a stable, deterministic order across calls.
//
// Only those. An established line is deliberately unrepresentable in
// this answer: on a busy network the established set *is* the entire
// traffic set, and the design's whole claim is that the payload is
// proportional to novelty rather than to volume. A caller that wants
// everything wants a different question than this feature asks.
func (r *Register) OffToday(now time.Time) []Line {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Line, 0, 32)
	for _, l := range r.byKey {
		if !l.SeenOn(now) || l.Established(r.cfg, now) {
			continue
		}
		out = append(out, copyLine(l))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// List returns every known line, sorted by Key. For tests and the backup
// path, not for the API -- see OffToday for why the API never serves
// this.
func (r *Register) List() []Line {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.listLocked()
}

func (r *Register) listLocked() []Line {
	out := make([]Line, 0, len(r.byKey))
	for _, l := range r.byKey {
		out = append(out, copyLine(l))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// copyLine deep-copies the one pointer field, so a caller holding a
// listed Line cannot reach back into the register through its Expected.
func copyLine(l *Line) Line {
	out := *l
	if l.Expected != nil {
		e := *l.Expected
		out.Expected = &e
	}
	return out
}

// persistMinInterval rate-limits persistLocked -- both the encode and,
// through persist.WriteBehind, the write. A var rather than a const so a
// test needing every call to persist immediately can shrink it, the same
// convention internal/hosts.persistMinInterval uses.
var persistMinInterval = time.Second

// persistLocked hands the current state to the write-behind writer,
// which coalesces it with whatever else is pending and writes it off this
// goroutine (see persist.WriteBehind).
//
// Same two-tier rule as internal/hosts, and needed here for the same
// reason: this is called on every ingested event, and marshalling up to
// MaxLines entries per event -- on the ingest goroutine, under the lock
// -- would be a real cost on exactly the sustained-traffic case the
// write-behind design exists to keep off the hot path. A *structural*
// change (a new line, a day newly set, a mark set or cleared) always
// encodes; a repeat sighting on a day already recorded encodes at most
// once per persistMinInterval.
//
// The trade-off, stated plainly: if the feed goes silent right after a
// deferred sighting, that last count bump reaches disk only when Flush or
// Close forces it. Losing it costs a few seconds of today's event count
// after an unclean kill, never a line, never a recorded day and never a
// mark -- the three things establishment and the operator's own
// statements actually rest on.
//
// Must be called with r.mu held.
func (r *Register) persistLocked(structural bool) {
	if r.wb == nil {
		return
	}
	now := time.Now()
	if !structural && now.Sub(r.lastEncode) < persistMinInterval {
		r.deferred = true
		return
	}

	list := r.listLocked()
	ptrs := make([]*Line, len(list))
	for i := range list {
		ptrs[i] = &list[i]
	}
	data, err := json.MarshalIndent(storeFile{Lines: ptrs}, "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding the baseline line register failed: %v -- this change exists only in memory and will be lost on restart", err))
		return
	}
	r.lastEncode = now
	r.deferred = false
	r.wb.MarkDirty(data)
}

// Flush forces whatever is currently dirty -- including an encode this
// register deferred, see persistLocked -- to the backend now, without
// waiting out the debounce interval, and blocks until that attempt
// finishes or ctx expires. A register with no backend configured is a
// safe no-op.
func (r *Register) Flush(ctx context.Context) error {
	r.encodeDeferred()
	return r.wb.Flush(ctx)
}

// Close stops the write-behind writer, flushing whatever is still dirty
// before returning -- main's shutdown joins on this so a line seen right
// before exit is not silently dropped. A register with no backend
// configured is a safe no-op. Not safe to call any mutating method after
// Close.
func (r *Register) Close(ctx context.Context) error {
	r.encodeDeferred()
	return r.wb.Close(ctx)
}

// encodeDeferred runs the encode persistLocked skipped, if there is one,
// so Flush and Close write the true latest state rather than the state as
// of the last un-deferred call.
func (r *Register) encodeDeferred() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deferred {
		r.persistLocked(true)
	}
}

// validateText rejects an empty string, text over maxLen runes, invalid
// UTF-8, or control/Unicode-format characters (the bidi overrides that
// let a string render in an order other than the one it is stored in).
// Same rule and same reasoning as internal/hosts.validateText: key and
// reason both flow into the UI and the audit trail.
func validateText(s string, maxLen int) error {
	if s == "" {
		return ErrInvalidExpected
	}
	if !utf8.ValidString(s) {
		return ErrInvalidExpected
	}
	if utf8.RuneCountInString(s) > maxLen {
		return ErrInvalidExpected
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ErrInvalidExpected
		}
	}
	return nil
}
