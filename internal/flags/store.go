// SPDX-License-Identifier: AGPL-3.0-only

// Package flags stores manually-clearable behavioral flags raised by
// internal/detect (port scans, per-source activity spikes, repeated
// critical-port attempts, global volume spikes).
//
// This is the one deliberate exception to mikroview's otherwise
// in-memory-only design (see SECURITY.md): a flag exists specifically to
// stay visible until a human looks at it and clears it, so unlike every
// other piece of state it's persisted to a small JSON file rather than
// reset on every restart. Persistence is optional (empty StorePath keeps
// flags in-memory only, same as everything else) so a deployment that
// hasn't mounted a volume for it still works, just without the
// survives-a-restart guarantee.
package flags

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"sort"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var persistLog = logging.New("flags")

type Type string

const (
	TypePortScan              Type = "port_scan"
	TypeActivitySpike         Type = "activity_spike"
	TypeCriticalPort          Type = "critical_port"
	TypeGlobalSpike           Type = "global_spike"
	TypeDistributedBruteForce Type = "distributed_brute_force"
	TypeOutboundAnomaly       Type = "outbound_anomaly"
	TypeInternalRecon         Type = "internal_recon"
	TypeRuleSpike             Type = "rule_spike"
	TypeRepeatedDrops         Type = "repeated_drops"
	// TypeLowSlowScan (issue #20): a port scan deliberately paced to stay
	// under TypePortScan's short-burst threshold -- judged over hours by
	// several independent signals (destination breadth, per-source EMA
	// baseline, drop/reject ratio, reputation) rather than one count. See
	// internal/detect/low_slow_scan.go.
	TypeLowSlowScan Type = "low_slow_scan"
	// TypeOffHoursActivity (issue #104): a host active during a clock
	// window it has no established history of being active in --
	// deliberately *not* "any activity inside a fixed off-hours window,"
	// since a naive version of that fires on trivial noise (a phone
	// syncing, a scheduled job) with nothing to judge it against. Judged
	// per hour-of-day against that specific host's own EMA baseline *for
	// that specific hour*, gated by both an absolute count floor and a
	// minimum number of distinct prior days of history at that hour --
	// see internal/detect/off_hours.go.
	TypeOffHoursActivity Type = "off_hours_activity"
	// TypeDeviceSilence (issue #98): a configured RouterOS device that
	// should be sending syslog has gone quiet for longer than
	// detect.Config.DeviceStaleAfter -- absence of events, not a pattern
	// within them, so it's raised by a periodic sweep (see
	// internal/detect/device_silence.go) rather than any per-event path.
	// Target is the device's configured ID, not an IP. Deliberately the
	// same string value as detect.DetectorDeviceSilence, like every
	// other detector/flag-type pair in this codebase (see
	// DetectorName's doc comment).
	TypeDeviceSilence Type = "device_silence"
	// TypeNewDevice (issue #103 phase 1): raised the first time
	// mikroview ever sees a given store.Event.SrcMAC, per
	// internal/device.MACRegistry's persisted history -- deterministic,
	// fires once per genuinely-new MAC, no threshold/window to tune.
	// Raised directly from the ingest path (main.go), not from
	// internal/detect like every other Type here, since it needs no
	// rolling-window state, just a yes/no "seen before" lookup.
	TypeNewDevice Type = "new_device"
	// TypeStaleRule (issue #102): a firewall rule that fired at some
	// point (recorded in internal/rules' long-lived usage record) but
	// hasn't fired again in a long time -- either dead weight or an
	// unnecessary hole, worth a human's attention either way. Target is
	// the rule label, not an IP, same non-IP-target precedent
	// TypeGlobalSpike already sets with "global". See
	// internal/detect/stale_rule.go.
	TypeStaleRule Type = "stale_rule"
	// TypeUnexpectedMailSender (issue #108): a LAN source originating an
	// outbound connection to an external destination on an SMTP port
	// (25, 465, 587) that isn't tagged "trusted-mail-sender" in
	// internal/entities' store -- a host with no established, admin-
	// acknowledged reason to send mail suddenly doing so is a strong,
	// simple, deterministic compromised-device/spambot signal, distinct
	// from TypeOutboundAnomaly (dest_spread.go), which only fires on
	// distinct-destination-*count* spread over a window -- a single new
	// SMTP connection to one destination wouldn't trip that on its own.
	// Deterministic like TypeNewDevice/TypeStaleRule above -- no
	// threshold or window to tune, just "has this untagged source ever
	// done this before." See internal/detect/mail_sender.go.
	TypeUnexpectedMailSender Type = "unexpected_mail_sender"
	// TypeKnownBadIP (issue #113 Part B): a source IP matching a
	// locally-cached CIDR range from a vetted, curated threat-intel feed
	// (Spamhaus DROP/EDROP by default -- see internal/blocklist's doc
	// comment for the full menu and why an arbitrary user-supplied URL
	// isn't offered instead). Raised directly, independent of any
	// behavioral threshold -- presence on a list Spamhaus is confident
	// is entirely malicious-controlled is itself the signal, not a
	// byproduct of volume or pattern. Target is the source IP, same
	// convention as TypePortScan/TypeActivitySpike/TypeCriticalPort.
	//
	// Also feeds RaiseConfidenceFloor for every other currently-active
	// source-IP-keyed flag on the same target (see
	// internal/detect/known_bad_ip.go's knownBadReinforcedTypes) -- the
	// same reinforcement role internal/detect's async AbuseIPDB-informed
	// checks already play for those flags (see maybeCheckReputation),
	// just synchronous, since a local lookup
	// needs no network round-trip to resolve. Raised directly from
	// internal/detect.Observe, not gated by DetectorName/Scope like
	// every per-event behavioral detector above -- same "no matching
	// detector-settings entry" exception new_device/stale_rule already
	// established (see frontend/src/lib/types.ts's FlagType doc
	// comment): there's no threshold to tune and no scope narrower than
	// "this exact list membership check."
	TypeKnownBadIP Type = "known_bad_ip"
)

// maxFlags bounds the store the same way every other buffer in mikroview
// has an explicit ceiling (see internal/store's ring buffer, the
// frontend's MAX_CLIENT_EVENTS). Flags are raised far less often than
// raw events, so this is a generous safety net rather than a limit
// expected to be hit in normal use. A var rather than a const so tests
// can shrink it without creating 1000+ flags.
var maxFlags = 1000

// maxFlagsHardCeiling bounds the store even when every flag is still
// active. maxFlags above is a soft target that only sheds *cleared*
// flags; without a hard stop, unauthenticated input can grow the active
// set without limit (see pruneLocked). A var so tests can shrink it.
var maxFlagsHardCeiling = 5000

// flagTimeSeriesMinutes is how much history Store.TimeSeries covers, at
// 1-minute resolution -- same window/resolution as
// internal/store/ring.go's Stats.TimeSeries, for visual/temporal
// consistency between EventsChart and FlagsChart on the frontend.
const flagTimeSeriesMinutes = 60

// Evidence is structured supporting detail beyond the free-text Detail
// string -- which detector populates which field:
//   - Ports: port_scan's distinct ports touched (capped, see
//     internal/detect's maxEvidencePorts).
//   - Hosts: distributed_brute_force's distinct source IPs,
//     outbound_anomaly's/internal_recon's distinct destinations
//     (capped, see internal/detect's maxEvidenceHosts).
//   - NAT: repeated_drops' triggering event's NAT translation info,
//     when present.
//
// Zero value (all fields empty/nil) is valid and common -- most
// detectors (critical_port, activity_spike, rule_spike, global_spike,
// stale_rule, unexpected_mail_sender) have nothing here at all, since
// their Detail string already says everything there is to say.
type Evidence struct {
	Ports []int    `json:"ports,omitempty"`
	Hosts []string `json:"hosts,omitempty"`
	NAT   *NATInfo `json:"nat,omitempty"`
}

// NATInfo is one event's NAT translation detail (store.Event's
// NatIP/NatPort/NatRaw), attached to a flag's Evidence when the
// triggering event had one.
type NATInfo struct {
	IP   string `json:"ip,omitempty"`
	Port int    `json:"port,omitempty"`
	Raw  string `json:"raw,omitempty"`
}

// Flag is one raised, human-clearable signal.
type Flag struct {
	ID        string    `json:"id"`
	Type      Type      `json:"type"`
	Target    string    `json:"target"` // source IP, "global" for TypeGlobalSpike, a device ID for TypeDeviceSilence, or a rule label for TypeRuleSpike/TypeStaleRule
	Detail    string    `json:"detail"` // human-readable specifics, e.g. "23 distinct ports in 60s"
	Count     int       `json:"count"`  // times this detector has re-fired for this target since the flag was (re-)raised
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	Cleared   bool      `json:"cleared"`
	ClearedAt time.Time `json:"clearedAt,omitzero"`
	// Confidence is 0-100, set only by detectors that make a statistical
	// judgment call rather than a deterministic threshold crossing (e.g.
	// the per-host activity baseline -- see internal/detect/host_baseline.go).
	// nil (omitted from JSON) means "not scored" -- a plain threshold
	// detector is exactly as confident as the count it reports, so
	// attaching a number there would be noise, not signal.
	Confidence *int `json:"confidence,omitempty"`
	// ReputationFloor is the last reputation-informed minimum confidence
	// applied to this flag (see RaiseConfidenceFloor, internal/detect's
	// async AbuseIPDB-informed check) -- reapplied against Confidence on
	// every subsequent re-fire so a later, purely behavioral confidence
	// recompute never silently discards reputation evidence gathered
	// earlier in the same episode. nil means no reputation floor has
	// been applied (either not configured, not looked up yet, or the
	// target has no reputation data).
	ReputationFloor *int `json:"reputationFloor,omitempty"`
	// Reputation is a snapshot of the target's reputation *as of when
	// this episode's async lookup resolved*, not fetched live on read --
	// the point is "what did this look like when it fired." Only ever
	// set for single-IP detectors (see internal/detect's
	// maybeCheckReputation) -- group detectors' async check still
	// informs ReputationFloor/Confidence the same way, but there's no
	// single coherent snapshot to attach when many different IPs were
	// sampled.
	Reputation *reputation.Result `json:"reputation,omitempty"`
	// Country is the target's ISO 3166-1 alpha-2 code, from the same
	// GeoIP lookup already performed at ingest time
	// (store.Event.SrcCountry) -- captured synchronously at raise time,
	// no extra lookup. Empty for an internal target or when GeoIP isn't
	// configured.
	Country string `json:"country,omitempty"`
	// Evidence is structured supporting detail beyond Detail -- see
	// Evidence's own doc comment.
	Evidence Evidence `json:"evidence,omitzero"`
}

// FlagTimeBucket is one point in Store.TimeSeries: counts of newly-raised
// episodes by Type for a single one-minute window -- same shape
// convention as internal/store/ring.go's TimeBucket. ByType omits types
// with a zero count for that minute rather than listing every known
// Type every time. Counts episode starts (Add/AddWithConfidence/
// AddWithDetail's isNew), not every re-fire -- see bumpTimeSeriesLocked's
// doc comment for why.
type FlagTimeBucket struct {
	Time   time.Time       `json:"time"`
	ByType map[Type]uint64 `json:"byType"`
}

// Exclusion is one permanently-excluded (Type, Target) pair -- see
// Store.Exclude's doc comment. ID is the same flagID(Type, Target) key
// flags themselves use, included so a caller (the admin exclusions API)
// can operate on an entry without recomputing it and without ambiguity
// from splitting it back apart (Target can itself contain ":", e.g. an
// IPv6 address, which would make that split ambiguous).
type Exclusion struct {
	ID     string `json:"id"`
	Type   Type   `json:"type"`
	Target string `json:"target"`
}

// persistedState is the on-disk JSON shape written by persistLocked and
// read back by Open -- see both of their doc comments for why this is
// an object (flags + exclusions) rather than the bare `[]*Flag` array
// this package used before permanent exclusions existed, and how Open
// stays able to read a pre-upgrade file in that older shape.
type persistedState struct {
	Flags    []Flag      `json:"flags"`
	Excluded []Exclusion `json:"excluded,omitempty"`
}

// Store holds every known flag, active and cleared, keyed by a stable ID
// derived from (Type, Target) -- there is at most one entry per detector
// per target, ever; re-firing updates it in place, and clearing just
// marks it rather than deleting it, so recent history stays visible. The
// zero value is not usable; construct with Open.
type Store struct {
	mu      sync.RWMutex
	backend persist.Backend
	// version is the backend's token for the document as of the last
	// load or save -- see persist.SaveWithRetry.
	version int64
	byID    map[string]*Flag
	// clearedCount tracks how many entries in byID are Cleared, so
	// pruneLocked can skip its scan entirely when there is nothing
	// evictable. Under a flood the store sits full of *active* flags, so
	// that scan otherwise ran on every Add and found nothing.
	clearedCount int

	// excluded holds every permanently-excluded (Type, Target) pair, same
	// flagID key as byID -- see Store.Exclude's doc comment. Checked at
	// the top of add() so an excluded pair never raises again.
	excluded map[string]Exclusion

	// lastPersist backs persistLocked's rate limiting -- see
	// persistMinInterval.
	lastPersist time.Time

	// minuteBuckets/minuteBucketTime implement the same lazily-reset
	// rolling-bucket trick as internal/store/ring.go's Store.minuteBuckets
	// (bucket i holds counts for unix minute minuteBucketTime[i], reset
	// the next time that slot is reused for a new minute), but counting
	// newly-raised flag episodes broken down by Type instead of raw
	// events by Action. A map per slot rather than ring.go's fixed
	// actionSlots array: Action has five, permanently fixed, values and
	// Insert is a hot path, so ring.go trades a bit of flexibility for
	// avoiding a map allocation on every call; Type already has 14
	// values, keeps growing as detectors are added, and add() is called
	// far less often (a detector firing, not every raw event), so a map
	// is the better trade here. Populated directly in add() itself, not
	// via the onRaise hook -- see bumpTimeSeriesLocked's doc comment for
	// why that distinction matters.
	minuteBuckets    [flagTimeSeriesMinutes]map[Type]uint64
	minuteBucketTime [flagTimeSeriesMinutes]int64

	// onRaise, if set via WithOnRaise, is called after a new flag episode
	// is raised (first-ever raise or a revival from Cleared -- the same
	// "isNew" Add/AddWithConfidence/AddWithDetail already report) --
	// never on a plain re-fire of an already-active flag, so a noisy
	// detector doesn't re-alert on every event. Must not block: see
	// internal/notify.Dispatcher.Enqueue's contract, the intended caller.
	onRaise func(Flag)
}

// WithOnRaise sets the hook called on every new flag episode -- see
// onRaise's own doc comment. Chainable, mirroring
// internal/detect.Detector.WithReputation's shape.
func (s *Store) WithOnRaise(fn func(Flag)) *Store {
	s.onRaise = fn
	return s
}

// Open loads path if it exists (a missing file is the expected first-run
// case, not an error) and returns a Store that persists to it from then
// on. An empty path is the expected "persistence not configured" case:
// a fully usable, in-memory-only Store is returned. A malformed file is
// treated as empty rather than failing -- a corrupted flags file should
// never block mikroview from starting, since flags are a helper signal,
// not critical state. Either way the returned Store is always safe to
// use unconditionally; a non-nil error is only ever informational, for
// the caller to log.
//
// Reads either of two on-disk shapes: the current `{"flags":[...],
// "excluded":[...]}` object persistLocked now writes, or the bare
// `[...]` array of flags this package wrote before permanent exclusions
// existed -- so a file from before this feature shipped still loads
// cleanly (with no exclusions, which is exactly correct for it) rather
// than being treated as malformed. Distinguished by the first
// non-whitespace byte, since persistLocked always writes one shape or
// the other, never something ambiguous between them.
func Open(path string) (*Store, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend -- a JSON file by
// default, or Postgres when configured (issue #131). A nil backend gives
// a usable, in-memory-only store.
func OpenWithBackend(b persist.Backend) (*Store, error) {
	s := &Store{backend: b, byID: make(map[string]*Flag), excluded: make(map[string]Exclusion)}

	data, version, err := persist.LoadDocument(context.Background(), b)
	if err != nil {
		return s, err
	}
	if data == nil {
		return s, nil
	}
	s.version = version

	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] == '[' {
		var list []*Flag
		if err := json.Unmarshal(data, &list); err != nil {
			return s, err
		}
		for _, f := range list {
			// A JSON array containing `null` (e.g. `[null]`, or a real
			// entry followed by one) unmarshals successfully into a nil
			// *Flag -- valid JSON, so the err check above never catches
			// it. Skipping it here is what actually delivers this
			// function's documented "a malformed file is treated as
			// empty rather than failing" contract; relying on the
			// unmarshal error alone doesn't cover every way a file can
			// be malformed.
			if f == nil {
				continue
			}
			s.byID[f.ID] = f
			if f.Cleared {
				s.clearedCount++
			}
		}
		return s, nil
	}

	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return s, err
	}
	for _, f := range state.Flags {
		f := f
		s.byID[f.ID] = &f
		if f.Cleared {
			s.clearedCount++
		}
	}
	for _, e := range state.Excluded {
		s.excluded[e.ID] = e
	}
	return s, nil
}

func flagID(t Type, target string) string {
	return string(t) + ":" + target
}

// Add raises a flag for (t, target), or updates it in place if one is
// already active. A *cleared* flag for the same (t, target) is revived
// as a fresh episode (FirstSeen and Count reset) rather than left
// cleared -- once a human has dismissed a flag, the behavior recurring
// is worth a new signal, not a silently-suppressed repeat of something
// they already looked at. Reports whether this call started a new
// episode (first-ever raise, or a revival) as opposed to updating an
// already-active flag in place -- internal/detect uses this to avoid
// re-triggering a reputation lookup on every re-fire of an ongoing flag
// (see RaiseConfidenceFloor).
//
// No production detector calls this directly anymore -- every real
// call site was migrated to AddWithConfidence/AddWithDetail once
// confidence scoring landed (issue #39/#57). Kept as the simplest
// lifecycle primitive (raise/re-fire/clear/revive/prune) for tests that
// don't care about confidence scoring, the same reasoning New/
// DefaultConfig are kept in internal/detect for tests that don't need
// their own full configurability.
func (s *Store) Add(t Type, target, detail string, now time.Time) bool {
	isNew, f := s.add(t, target, detail, nil, Evidence{}, "", now)
	s.maybeNotify(isNew, f)
	return isNew
}

// AddWithConfidence is Add, but for a detector that can express how
// confident it is in this specific flag (0-100) rather than a simple
// deterministic threshold crossing -- see Flag.Confidence.
func (s *Store) AddWithConfidence(t Type, target, detail string, confidence int, now time.Time) bool {
	isNew, f := s.add(t, target, detail, &confidence, Evidence{}, "", now)
	s.maybeNotify(isNew, f)
	return isNew
}

// AddWithDetail is AddWithConfidence, plus structured evidence and a
// country code, for a detector whose behavior is best explained by
// exactly what was touched, not just a count -- see Evidence and
// Flag.Country.
func (s *Store) AddWithDetail(t Type, target, detail string, confidence int, evidence Evidence, country string, now time.Time) bool {
	isNew, f := s.add(t, target, detail, &confidence, evidence, country, now)
	s.maybeNotify(isNew, f)
	return isNew
}

// maybeNotify fires onRaise for a newly-raised episode -- called after
// add() has returned (its deferred unlock has already fired), so the
// hook never runs while s.mu is held.
func (s *Store) maybeNotify(isNew bool, f Flag) {
	if isNew && s.onRaise != nil {
		s.onRaise(f)
	}
}

func (s *Store) add(t Type, target, detail string, confidence *int, evidence Evidence, country string, now time.Time) (bool, Flag) {
	id := flagID(t, target)

	s.mu.Lock()
	defer s.mu.Unlock()

	// A permanently excluded (Type, Target) never raises, silently -- no
	// entry is created, updated, or revived, and the caller sees "not a
	// new episode" so nothing downstream (notifications, reputation
	// lookups) fires either. See Store.Exclude's doc comment for why
	// this is a deliberate permanent no-op rather than a raise that then
	// gets auto-cleared.
	if _, excluded := s.excluded[id]; excluded {
		return false, Flag{}
	}

	f, ok := s.byID[id]
	isNew := !ok
	if !ok {
		f = &Flag{ID: id, Type: t, Target: target, FirstSeen: now}
		s.byID[id] = f
	} else if f.Cleared {
		isNew = true
		f.FirstSeen = now
		f.Cleared = false
		s.clearedCount--
		f.ClearedAt = time.Time{}
		f.Count = 0
		f.ReputationFloor = nil // a revived flag starts its confidence history fresh
		f.Reputation = nil      // ...and its detail history, including any stale reputation snapshot
	}
	f.Detail = detail
	f.Confidence = mergeConfidence(confidence, f.ReputationFloor)
	f.Evidence = evidence
	f.Country = country
	f.LastSeen = now
	f.Count++

	if isNew {
		s.bumpTimeSeriesLocked(t, now)
	}

	s.pruneLocked()
	s.persistLocked()
	return isNew, *f
}

// bumpTimeSeriesLocked records one new-or-revived episode of Type t at
// time now in the rolling per-minute bucket history (see
// minuteBuckets/minuteBucketTime's doc comment). Called directly from
// add() itself -- store-internal bookkeeping, not routed through the
// pluggable onRaise callback -- because onRaise is a single-slot hook
// already claimed by internal/notify.Dispatcher.Enqueue (see main.go's
// fs.WithOnRaise(dispatcher.Enqueue)); wiring this counter through
// WithOnRaise a second time would silently overwrite that assignment
// (whichever caller sets it last wins, with no compile-time or runtime
// signal) and stop every SMTP/Pushover/webhook notification. Must be
// called with s.mu already held.
func (s *Store) bumpTimeSeriesLocked(t Type, now time.Time) {
	minute := now.Unix() / 60
	idx := minute % flagTimeSeriesMinutes
	if idx < 0 {
		idx += flagTimeSeriesMinutes
	}
	if s.minuteBucketTime[idx] != minute {
		s.minuteBucketTime[idx] = minute
		s.minuteBuckets[idx] = make(map[Type]uint64, 1)
	}
	s.minuteBuckets[idx][t]++
}

// TimeSeries returns the last flagTimeSeriesMinutes minutes of newly-
// raised-episode counts by Type, oldest first, one entry per minute
// including empty ones -- the same fixed-width-window shape convention
// as internal/store/ring.go's Stats.TimeSeries, for FlagsChart. Like
// ring.go's rolling buckets, this only ever reflects activity since this
// Store was constructed: no historical backfill/persistence across a
// restart (see this package's own doc comment on what mikroview does
// and doesn't persist).
func (s *Store) TimeSeries() []FlagTimeBucket {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.timeSeriesLocked()
}

func (s *Store) timeSeriesLocked() []FlagTimeBucket {
	nowMinute := time.Now().Unix() / 60
	out := make([]FlagTimeBucket, flagTimeSeriesMinutes)
	for i := 0; i < flagTimeSeriesMinutes; i++ {
		minute := nowMinute - int64(flagTimeSeriesMinutes-1-i)
		idx := minute % flagTimeSeriesMinutes
		if idx < 0 {
			idx += flagTimeSeriesMinutes
		}
		byType := make(map[Type]uint64, len(s.minuteBuckets[idx]))
		if s.minuteBucketTime[idx] == minute {
			for typ, c := range s.minuteBuckets[idx] {
				if c > 0 {
					byType[typ] = c
				}
			}
		}
		out[i] = FlagTimeBucket{Time: time.Unix(minute*60, 0).UTC(), ByType: byType}
	}
	return out
}

// mergeConfidence combines a detector's freshly computed confidence
// with any previously applied reputation floor -- the floor must
// survive a plain re-fire's confidence recompute, or a later, purely
// behavioral update would silently discard reputation evidence gathered
// earlier in the same episode.
func mergeConfidence(fresh, floor *int) *int {
	switch {
	case floor == nil:
		return fresh
	case fresh == nil || *floor > *fresh:
		v := *floor
		return &v
	default:
		return fresh
	}
}

// RaiseConfidenceFloor raises id's confidence to at least floor, if the
// flag is still known and floor is higher than its current score or
// previously applied floor. Never lowers an existing score -- a
// clean/unavailable reputation result is absence of evidence, not
// evidence of innocence. Called asynchronously by internal/detect, well
// after the triggering event -- safe to call from any goroutine, same
// as every other Store method.
func (s *Store) RaiseConfidenceFloor(t Type, target string, floor int) {
	id := flagID(t, target)

	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.byID[id]
	if !ok {
		return
	}

	changed := false
	if f.ReputationFloor == nil || floor > *f.ReputationFloor {
		v := floor
		f.ReputationFloor = &v
		changed = true
	}
	if f.Confidence == nil || floor > *f.Confidence {
		v := floor
		f.Confidence = &v
		changed = true
	}
	if changed {
		s.persistLocked()
	}
}

// ApplyReputationSnapshot records target's reputation lookup result on
// the flag (see Flag.Reputation) and, if it includes an AbuseIPDB
// score, raises the confidence floor from it too -- same floor-raise-
// only reasoning as RaiseConfidenceFloor: a clean/absent score is
// absence of evidence, not evidence of innocence. Only meaningful for
// the single-IP reputation path (see internal/detect's
// maybeCheckReputation) -- the group path has no single snapshot to
// attach and keeps using RaiseConfidenceFloor directly. No-ops if the
// flag is no longer known.
func (s *Store) ApplyReputationSnapshot(t Type, target string, snapshot reputation.Result) {
	id := flagID(t, target)

	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.byID[id]
	if !ok {
		return
	}

	f.Reputation = &snapshot

	// The applied floor is the strongest of two independent signals:
	// AbuseIPDB's abuse score, and its IsTor/UsageType fields (issue
	// #58) -- a Tor exit node or hosting-provider address is worth
	// floor-raising even when the target has no abuse reports at all,
	// since those are compositionally different kinds of evidence.
	floor := -1
	if snapshot.AbuseScore != nil {
		floor = *snapshot.AbuseScore
	}
	if riskFloor, ok := snapshot.RiskFloor(); ok && riskFloor > floor {
		floor = riskFloor
	}
	if floor >= 0 {
		if f.ReputationFloor == nil || floor > *f.ReputationFloor {
			v := floor
			f.ReputationFloor = &v
		}
		if f.Confidence == nil || floor > *f.Confidence {
			v := floor
			f.Confidence = &v
		}
	}
	s.persistLocked()
}

// Clear marks id as cleared. It reports whether an active flag with that
// ID was found -- clearing an already-cleared or unknown ID is a no-op,
// not an error, since the caller (a browser tab that might be showing a
// stale list) can't always know which is which.
func (s *Store) Clear(id string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.byID[id]
	if !ok || f.Cleared {
		return false
	}
	f.Cleared = true
	s.clearedCount++
	f.ClearedAt = now
	s.persistLocked()
	return true
}

// ClearAndExclude is the "Clear and never flag this again" action:
// clears id's current episode (if still active) and permanently
// excludes its (Type, Target) going forward, in one atomic step under a
// single lock. Reports whether id was known at all, the same true/false
// contract as Clear -- an unknown ID is a no-op, not an error, for the
// same reason Clear's doc comment gives. Unlike Clear, this succeeds
// (and still records the exclusion) even if the flag was already
// cleared, since the point of this call is future suppression, not the
// current episode's state.
func (s *Store) ClearAndExclude(id string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.byID[id]
	if !ok {
		return false
	}
	if !f.Cleared {
		f.Cleared = true
		s.clearedCount++
		f.ClearedAt = now
	}
	if _, already := s.excluded[id]; !already {
		s.excluded[id] = Exclusion{ID: id, Type: f.Type, Target: f.Target}
	}
	s.persistLocked()
	return true
}

// Exclude permanently marks (t, target) as excluded -- from this call
// on, add() (and so every Add/AddWithConfidence/AddWithDetail call) is a
// silent no-op for that exact pair, forever, until a matching
// RemoveExclusion/RemoveExclusionByID call. This is deliberately
// permanent, not a timer: an earlier "snooze with expiry" design was
// rejected as pointless -- it either re-fires once the snooze ends
// (nothing was solved) or it doesn't (permanent exclusion was what was
// wanted all along). Idempotent: excluding an already-excluded pair is a
// no-op.
func (s *Store) Exclude(t Type, target string) {
	id := flagID(t, target)

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, already := s.excluded[id]; already {
		return
	}
	s.excluded[id] = Exclusion{ID: id, Type: t, Target: target}
	s.persistLocked()
}

// RemoveExclusion reverses Exclude for (t, target), letting that pair
// raise again going forward -- existing flag history (if any) is
// untouched either way. Reports whether an exclusion was actually
// present, same true/false contract as Clear.
func (s *Store) RemoveExclusion(t Type, target string) bool {
	return s.RemoveExclusionByID(flagID(t, target))
}

// RemoveExclusionByID is RemoveExclusion, keyed directly by the same ID
// Exclusion.ID/Flag.ID already use -- what the admin exclusions API
// (which lists Exclusion values, not raw (Type, Target) pairs) actually
// has on hand to act on.
func (s *Store) RemoveExclusionByID(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.excluded[id]; !ok {
		return false
	}
	delete(s.excluded, id)
	s.persistLocked()
	return true
}

// Excluded reports whether (t, target) is currently permanently
// excluded -- exposed for callers/tests that want to check without
// going through Add's side effects.
func (s *Store) Excluded(t Type, target string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.excluded[flagID(t, target)]
	return ok
}

// ListExclusions returns every currently-excluded (Type, Target) pair,
// sorted by ID for a stable display order -- the admin-only surface
// (see internal/api's callerIsAdminOrOpen-gated exclusions endpoints)
// that lets a permanent exclusion made by mistake actually be undone.
func (s *Store) ListExclusions() []Exclusion {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Exclusion, 0, len(s.excluded))
	for _, e := range s.excluded {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// List returns every known flag, active and cleared, most-recently-
// active first.
func (s *Store) List() []Flag {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listLocked()
}

func (s *Store) listLocked() []Flag {
	out := make([]Flag, 0, len(s.byID))
	for _, f := range s.byID {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

// pruneLocked evicts the oldest *cleared* flags once the store is over
// maxFlags, oldest-cleared-first. Active flags are never evicted --  in
// the (extremely unlikely) case that active flags alone exceed the cap,
// the store is simply allowed to hold more than maxFlags rather than
// discarding something a human hasn't looked at yet.
func (s *Store) pruneLocked() {
	over := len(s.byID) - maxFlags
	if over <= 0 {
		return
	}

	// Only pay for the cleared-flag scan if there are any. Once the
	// store sits above maxFlags with everything active -- which is the
	// steady state under a flood, since active flags are kept -- this
	// scan-and-sort ran on *every* Add and never found anything to
	// evict. Measured at 911ns/Add below maxFlags versus 416us at the
	// ceiling: a 457x permanent tax, not an occasional one.
	// clearedCount lets this skip the scan entirely in the steady state
	// under a flood -- everything active, nothing to evict -- instead of
	// walking every flag on every Add to rediscover that. That scan alone
	// measured ~127us per Add at the ceiling.
	if s.clearedCount > 0 {
		cleared := make([]*Flag, 0, s.clearedCount)
		for _, f := range s.byID {
			if f.Cleared {
				cleared = append(cleared, f)
			}
		}
		sort.Slice(cleared, func(i, j int) bool { return cleared[i].ClearedAt.Before(cleared[j].ClearedAt) })
		for i := 0; i < over && i < len(cleared); i++ {
			delete(s.byID, cleared[i].ID)
			s.clearedCount--
		}
	}

	// Hard ceiling. Preferring to evict cleared flags is right -- an
	// active flag is something a human still needs to see, so it should
	// outlive reviewed noise. But "never evict an active flag" is only
	// safe if the number of active flags is bounded, and it is not:
	// flag targets come from unauthenticated syslog, so an attacker can
	// mint distinct ones as fast as they can send packets.
	//
	// Sheds a batch rather than the exact overflow, for the same reason
	// detect.evictOldestByActivity does: evicting one at a time means a
	// full scan and sort per Add forever. A batch amortizes it across
	// the next several thousand insertions.
	if len(s.byID) <= maxFlagsHardCeiling {
		return
	}
	target := maxFlagsHardCeiling - maxFlagsHardCeiling/8
	all := make([]*Flag, 0, len(s.byID))
	for _, f := range s.byID {
		all = append(all, f)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].FirstSeen.Before(all[j].FirstSeen) })
	for i := 0; i < len(all) && len(s.byID) > target; i++ {
		if all[i].Cleared {
			s.clearedCount--
		}
		delete(s.byID, all[i].ID)
	}
}

// persistMinInterval rate-limits persistLocked's actual disk writes --
// a detector re-firing on an active flag calls this on every single
// matching event for as long as the condition holds, not once per
// episode, so sustained high-rate traffic (a real port scan, the exact
// condition detection exists to catch) used to mean a full JSON
// marshal + atomic rename on every event, directly on the detection
// hot path. The trade-off: the very latest state is only durably
// persisted once another persistLocked call arrives after this
// interval elapses, so a change made right before mikroview crashes or
// is killed can be lost for up to this long -- an explicit, bounded
// version of the same "best-effort, not critical state" trade-off this
// package's own doc comment already makes for flags persistence in
// general (in-memory state, which every read goes through, is always
// immediately correct regardless).
//
// A var rather than a const so tests that need every call to persist
// immediately (e.g. a round-trip test with no delay between calls) can
// shrink it, same convention as maxFlags/maxTrackedSources/
// maxTCPConnections elsewhere in this codebase.
var persistMinInterval = time.Second

// persistLocked writes the current state -- flags and exclusions alike,
// see persistedState -- to disk if persistence is configured and enough
// time has passed since the last write -- see persistMinInterval. Write
// failures are swallowed rather than surfaced to Add/Clear/Exclude's
// callers: the in-memory state (which every read goes through) stays
// correct either way, so a transient disk issue degrades to "won't
// survive a restart right now" rather than breaking live use.
func (s *Store) persistLocked() {
	if s.backend == nil {
		return
	}
	if now := time.Now(); now.Sub(s.lastPersist) < persistMinInterval {
		return
	} else {
		s.lastPersist = now
	}

	excluded := make([]Exclusion, 0, len(s.excluded))
	for _, e := range s.excluded {
		excluded = append(excluded, e)
	}
	sort.Slice(excluded, func(i, j int) bool { return excluded[i].ID < excluded[j].ID })

	data, err := json.MarshalIndent(persistedState{Flags: s.listLocked(), Excluded: excluded}, "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding flags for persistence failed: %v -- this change exists only in memory and will be lost on restart", err))
		return
	}
	version, conflicted, err := persist.SaveWithRetry(context.Background(), s.backend, data, s.version)
	if err != nil {
		persistLog.Error(fmt.Sprintf("writing flags to %s failed: %v -- this change exists only in memory and will be lost on restart",
			s.backend.Describe(), err))
		return
	}
	if conflicted {
		persistLog.Warn(fmt.Sprintf("flag store was modified by another process while this change was pending (%s); this change was applied on top",
			s.backend.Describe()))
	}
	s.version = version
}
