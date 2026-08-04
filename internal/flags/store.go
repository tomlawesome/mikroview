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
)

type Type string

const (
	TypePortScan      Type = "port_scan"
	TypeActivitySpike Type = "activity_spike"
	TypeCriticalPort  Type = "critical_port"
	TypeGlobalSpike   Type = "global_spike"
)

// maxFlags bounds the store the same way every other buffer in mikroview
// has an explicit ceiling (see internal/store's ring buffer, the
// frontend's MAX_CLIENT_EVENTS). Flags are raised far less often than
// raw events, so this is a generous safety net rather than a limit
// expected to be hit in normal use. A var rather than a const so tests
// can shrink it without creating 1000+ flags.
var maxFlags = 1000

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
// they already looked at.
func (s *Store) Add(t Type, target, detail string, now time.Time) {
	id := flagID(t, target)

	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.byID[id]
	if !ok {
		f = &Flag{ID: id, Type: t, Target: target, FirstSeen: now}
		s.byID[id] = f
	} else if f.Cleared {
		f.FirstSeen = now
		f.Cleared = false
		f.ClearedAt = time.Time{}
		f.Count = 0
	}
	f.Detail = detail
	f.LastSeen = now
	f.Count++

	s.pruneLocked()
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

// persistLocked writes the current state to disk if persistence is
// configured. Write failures are swallowed rather than surfaced to
// Add/Clear's callers: the in-memory state (which every read goes
// through) stays correct either way, so a transient disk issue degrades
// to "won't survive a restart right now" rather than breaking live use.
func (s *Store) persistLocked() {
	if s.path == "" {
		return
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
