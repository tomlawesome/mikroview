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
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/reputation"
)

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
)

// maxFlags bounds the store the same way every other buffer in mikroview
// has an explicit ceiling (see internal/store's ring buffer, the
// frontend's MAX_CLIENT_EVENTS). Flags are raised far less often than
// raw events, so this is a generous safety net rather than a limit
// expected to be hit in normal use. A var rather than a const so tests
// can shrink it without creating 1000+ flags.
var maxFlags = 1000

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
// detectors (critical_port, activity_spike, rule_spike, global_spike)
// have nothing here at all, since their Detail string already says
// everything there is to say.
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
	Target    string    `json:"target"` // source IP, or "global" for TypeGlobalSpike
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

// Store holds every known flag, active and cleared, keyed by a stable ID
// derived from (Type, Target) -- there is at most one entry per detector
// per target, ever; re-firing updates it in place, and clearing just
// marks it rather than deleting it, so recent history stays visible. The
// zero value is not usable; construct with Open.
type Store struct {
	mu   sync.RWMutex
	path string
	byID map[string]*Flag

	// lastPersist backs persistLocked's rate limiting -- see
	// persistMinInterval.
	lastPersist time.Time

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
func Open(path string) (*Store, error) {
	s := &Store{path: path, byID: make(map[string]*Flag)}
	if path == "" {
		return s, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return s, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}

	var list []*Flag
	if err := json.Unmarshal(data, &list); err != nil {
		return s, err
	}
	for _, f := range list {
		// A JSON array containing `null` (e.g. `[null]`, or a real entry
		// followed by one) unmarshals successfully into a nil *Flag --
		// valid JSON, so the err check above never catches it. Skipping
		// it here is what actually delivers this function's documented
		// "a malformed file is treated as empty rather than failing"
		// contract; relying on the unmarshal error alone doesn't cover
		// every way a file can be malformed.
		if f == nil {
			continue
		}
		s.byID[f.ID] = f
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

	f, ok := s.byID[id]
	isNew := !ok
	if !ok {
		f = &Flag{ID: id, Type: t, Target: target, FirstSeen: now}
		s.byID[id] = f
	} else if f.Cleared {
		isNew = true
		f.FirstSeen = now
		f.Cleared = false
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

	s.pruneLocked()
	s.persistLocked()
	return isNew, *f
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
	f.ClearedAt = now
	s.persistLocked()
	return true
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

	cleared := make([]*Flag, 0, len(s.byID))
	for _, f := range s.byID {
		if f.Cleared {
			cleared = append(cleared, f)
		}
	}
	sort.Slice(cleared, func(i, j int) bool { return cleared[i].ClearedAt.Before(cleared[j].ClearedAt) })

	for i := 0; i < over && i < len(cleared); i++ {
		delete(s.byID, cleared[i].ID)
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

// persistLocked writes the current state to disk if persistence is
// configured and enough time has passed since the last write -- see
// persistMinInterval. Write failures are swallowed rather than
// surfaced to Add/Clear's callers: the in-memory state (which every
// read goes through) stays correct either way, so a transient disk
// issue degrades to "won't survive a restart right now" rather than
// breaking live use.
func (s *Store) persistLocked() {
	if s.path == "" {
		return
	}
	if now := time.Now(); now.Sub(s.lastPersist) < persistMinInterval {
		return
	} else {
		s.lastPersist = now
	}
	data, err := json.MarshalIndent(s.listLocked(), "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	os.Rename(tmp, s.path) // same filesystem, so this is atomic
}
