// SPDX-License-Identifier: AGPL-3.0-only

package decommission

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// storeLog is this package's logger, named the same way every sibling
// store names its own.
var storeLog = slog.Default().With("subsystem", "decommission")

// persistMinInterval debounces the write-behind writer. A decommission
// watch changes on two cadences: rarely (created, forced, retired) and
// on every straggler observation (the clock reset). The second is the one
// that matters here -- a subnet still full of chatty devices would
// otherwise write the document per event -- so the interval is the same
// order as the definitions store's, coarse against traffic and invisible
// against an operator action.
const persistMinInterval = 2 * time.Second

// maxWatches bounds how many decommission watches one deployment holds,
// the same explicit-ceiling convention internal/routerstate's maxDevices
// and internal/engine's own bounded maps follow.
//
// Real estates retire a handful of segments a year, so this is a safety
// net rather than a limit anyone approaches: watches are created from
// router pushes, and a router flapping its address table must not be able
// to grow this without bound. Refusing the newest is right here (unlike
// routerstate's evict-oldest), because an existing watch is a commitment
// an operator made and a new one is merely an offer they can make again.
const maxWatches = 512

// Store holds every decommission watch, persisted as one document.
//
// Safe for concurrent use: the evaluation goroutine records observations
// through RecordTraffic while request goroutines create, force and list.
// Every read hands back copies -- the same deep-copy-at-the-goroutine-
// boundary contract internal/engine's definitions store adopted for #376,
// and for the same reason: a Watch handed out by value whose LastKnown
// slice still aliased the live backing array would race the next
// observation.
type Store struct {
	mu      sync.RWMutex
	watches map[string]*Watch
	wb      *persist.WriteBehind

	// onChange is the seam the API layer wires so an accepted or retired
	// watch reaches the engine on the next event rather than the next
	// restart -- the same next-event-effect contract
	// engine.DefinitionsStore.SetOnChange established (#407).
	onChange func()
}

// document is the persisted shape. A map keyed by id, like every sibling
// document, so a watch that a future binary does not understand is
// preserved rather than dropped on rewrite.
type document struct {
	Watches map[string]*Watch `json:"watches"`
}

// Open loads path if it exists and returns a store that persists to it.
// An empty path is the expected "persistence not configured" case and
// yields a working in-memory store; a document that exists but cannot be
// parsed is a hard error rather than a store that would overwrite it,
// exactly as #378 settled for every other store here.
func Open(path string) (*Store, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend -- a JSON file by
// default, Postgres when configured.
func OpenWithBackend(b persist.Backend) (*Store, error) {
	s := &Store{watches: make(map[string]*Watch)}
	wb, _, err := persist.OpenWriteBehind(context.Background(), b, "the decommission watches", persist.WriteBehindOptions{
		MinInterval: persistMinInterval,
		OnSaveError: func(msg string) { storeLog.Error(msg) },
		OnConflict:  func(msg string) { storeLog.Warn(msg) },
	}, func(data []byte) error {
		var doc document
		if err := json.Unmarshal(data, &doc); err != nil {
			return err
		}
		for id, w := range doc.Watches {
			if id == "" || w == nil {
				continue
			}
			w.ID = id
			s.watches[id] = w
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.wb = wb
	return s, nil
}

// SetOnChange registers the callback fired after any mutation. Called
// with no locks held, so the callback may read this store back.
func (s *Store) SetOnChange(fn func()) {
	s.mu.Lock()
	s.onChange = fn
	s.mu.Unlock()
}

// Flush forces a pending write now. Close stops the writer.
func (s *Store) Flush(ctx context.Context) error { return s.wb.Flush(ctx) }
func (s *Store) Close(ctx context.Context) error { return s.wb.Close(ctx) }

// encodeAndMarkDirtyLocked re-encodes the whole document and hands it to
// the write-behind writer. Whole-document rather than per-watch, matching
// every sibling store: the document is small and bounded (maxWatches),
// and one canonical encoding is what makes "did this change" answerable.
// Shared by persistLocked and tryPersistLocked below.
func (s *Store) encodeAndMarkDirtyLocked() error {
	data, err := json.Marshal(document{Watches: s.watches})
	if err != nil {
		return fmt.Errorf("decommission: encoding the watch document failed: %w", err)
	}
	s.wb.MarkDirty(data)
	return nil
}

// tryPersistLocked is persistLocked's error-returning half, for the
// callers (Add, ForceRemove, Restore, Delete) that change a decommission
// watch an operator explicitly created, force-removed, restored or
// deleted -- see each one's own restore-on-error comment (R6). Every
// other caller (RecordTraffic's straggler clock reset, SetCovered's
// rule-derived coverage flag, Sweep's automatic retirement) keeps
// calling persistLocked, which stays fire-and-forget -- see that
// method's own doc comment for why.
//
// The write-behind writer never fails synchronously on its own --
// MarkDirty only hands off a snapshot for its own goroutine to save
// later. This calls its already-provided Flush escape hatch instead (see
// Flush's own doc comment) to force that save now and wait for it, under
// the same SaveTimeout bound every backend call in this codebase already
// carries.
func (s *Store) tryPersistLocked() error {
	if s.wb == nil {
		return nil
	}
	if err := s.encodeAndMarkDirtyLocked(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), persist.SaveTimeout)
	defer cancel()
	if err := s.wb.Flush(ctx); err != nil {
		return fmt.Errorf("writing decommission watches to the backend failed: %w", err)
	}
	return nil
}

// persistLocked is the swallow-and-log default every ordinary write
// uses -- see tryPersistLocked's own doc comment for exactly which
// callers use that instead and why. The in-memory state (which every
// read goes through) stays correct either way, so a transient disk
// issue degrades to "won't survive a restart right now" rather than
// breaking live use.
func (s *Store) persistLocked() {
	if s.wb == nil {
		return
	}
	if err := s.encodeAndMarkDirtyLocked(); err != nil {
		storeLog.Error("decommission: persisting the watch document failed", "error", err)
	}
}

// notify fires the change hook. Separate from persistLocked because it
// must run after the lock is released.
func (s *Store) notify() {
	s.mu.RLock()
	fn := s.onChange
	s.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// clone deep-copies a watch, so nothing handed out aliases live state.
func clone(w *Watch) Watch {
	out := *w
	if len(w.LastKnown) > 0 {
		out.LastKnown = append([]Straggler(nil), w.LastKnown...)
	}
	if w.LastStraggler != nil {
		sighting := *w.LastStraggler
		out.LastStraggler = &sighting
	}
	return out
}

// Add stores a new watch, returning it as stored.
//
// Refuses a second watch for a range already being watched: two watches
// for one CIDR would double every violation and give the map two ghosts
// for one zone, and the operator's second answer to the same offer is
// "yes" to a watch that already exists.
func (s *Store) Add(w Watch) (Watch, error) {
	cidr, err := NormaliseCIDR(w.CIDR)
	if err != nil {
		return Watch{}, err
	}
	w.CIDR = cidr
	if w.CleanWindow == 0 {
		w.CleanWindow = DefaultCleanWindow
	}
	if err := w.Validate(); err != nil {
		return Watch{}, err
	}
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now()
	}
	if w.ID == "" {
		w.ID = newID()
	}

	s.mu.Lock()
	for _, existing := range s.watches {
		if existing.CIDR == w.CIDR && existing.RetiredAt.IsZero() {
			s.mu.Unlock()
			return Watch{}, fmt.Errorf("decommission: %s is already being watched", w.CIDR)
		}
	}
	if len(s.watches) >= maxWatches {
		s.mu.Unlock()
		return Watch{}, fmt.Errorf("decommission: %d watches already exist -- refusing a new one", len(s.watches))
	}
	stored := w
	s.watches[w.ID] = &stored
	if err := s.tryPersistLocked(); err != nil {
		// A watch that only exists in memory must not be reported as
		// created (R6): a restart before the next good write would drop
		// it silently, and the operator was told it was being watched.
		delete(s.watches, w.ID)
		s.mu.Unlock()
		return Watch{}, fmt.Errorf("%w: saving the new watch: %v", ErrSaveFailed, err)
	}
	s.mu.Unlock()
	s.notify()
	return clone(&stored), nil
}

// Get returns one watch by id.
func (s *Store) Get(id string) (Watch, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w, ok := s.watches[id]
	if !ok {
		return Watch{}, false
	}
	return clone(w), true
}

// List returns every watch, newest first.
func (s *Store) List() []Watch {
	s.mu.RLock()
	out := make([]Watch, 0, len(s.watches))
	for _, w := range s.watches {
		out = append(out, clone(w))
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// Active returns every watch that has not retired -- what the engine
// evaluates and what the map draws.
//
// A retired watch is kept in the document rather than deleted: the whole
// point of the feature is that a decommission finished, and an operator
// asking "did that subnet ever go quiet" deserves the answer. Deleting
// the record would make a completed decommission indistinguishable from
// one that never happened.
func (s *Store) Active() []Watch {
	all := s.List()
	out := all[:0]
	for _, w := range all {
		if w.RetiredAt.IsZero() {
			out = append(out, w)
		}
	}
	return out
}

// Ghosts returns the watches the flat map should still draw as retiring
// zones: active, and not force-removed.
//
// Detached watches are deliberately absent. That is the force-remove
// ruling's own sentence -- the segment leaves the map now, the watch
// finishes the same job from the watchlist -- expressed once, here, so
// the map and the watchlist cannot disagree about which is which.
func (s *Store) Ghosts() []Watch {
	all := s.Active()
	out := all[:0]
	for _, w := range all {
		if !w.Detached {
			out = append(out, w)
		}
	}
	return out
}

// RecordTraffic takes one observation against every active watch whose
// range the event touched, resetting each one's clean window.
//
// Returns the watches that took it, as they now stand, so the caller can
// emit one violation per watch under that watch's own envelope.
//
// Called on the evaluation goroutine, once per matching event. It takes
// the write lock, which is why the engine side asks a lock-free prefix
// index first (see engine.DecommissionWatches) rather than calling this
// for every event that arrives.
//
// obs carries the rest of the event -- port, protocol, interface, rule,
// verdict -- so each watch that takes the observation also records what
// it was (Watch.RecordSighting). The sighting is written first, because
// both share the staleness guard and RecordTraffic is what advances the
// clock the guard reads.
func (s *Store) RecordTraffic(srcIP, dstIP string, at time.Time, obs Observation) []Watch {
	s.mu.Lock()
	var hit []Watch
	for _, w := range s.watches {
		if !w.RetiredAt.IsZero() || !w.Matches(srcIP, dstIP) {
			continue
		}
		w.RecordSighting(srcIP, dstIP, at, obs)
		w.RecordTraffic(at)
		hit = append(hit, clone(w))
	}
	if len(hit) > 0 {
		// The straggler clock reset this records is bookkeeping driven by
		// traffic, not an operator action -- losing it to a transient
		// disk issue costs at most one clean-window restart, the same
		// "won't survive a restart right now" degradation every ordinary
		// write in this codebase accepts, not a decision an operator was
		// told was kept. Stays on the swallow-and-log default.
		s.persistLocked()
	}
	s.mu.Unlock()
	if len(hit) > 0 {
		s.notify()
	}
	return hit
}

// SetCovered records whether anything is logging traffic that could
// reach a watch's range.
//
// Separate from RecordTraffic because coverage is a claim about the
// firewall's configuration, not about traffic: it changes when a rule
// changes, not when a packet arrives. A watch whose coverage answer is
// false reports StateBroken and never StateHolding -- see StateAt.
func (s *Store) SetCovered(id string, covered bool) error {
	s.mu.Lock()
	w, ok := s.watches[id]
	if !ok {
		s.mu.Unlock()
		return ErrNoSuchWatch
	}
	if w.Covered == covered {
		s.mu.Unlock()
		return nil
	}
	w.Covered = covered
	// Recomputed periodically from the firewall's own rule set (see this
	// method's own doc comment), not an operator action -- a value lost
	// to a transient disk issue is simply re-derived on the next
	// reconciliation pass, not a decision an operator was told was kept.
	// Stays on the swallow-and-log default.
	s.persistLocked()
	s.mu.Unlock()
	s.notify()
	return nil
}

// ForceRemove is the escape hatch (owner ruling, 2026-08-17): the
// segment leaves the map immediately, the watch survives.
//
// It does not end the watch and it does not shorten the clock. The
// override is recorded -- who, when, why -- per the #385 pattern, so the
// decision is auditable rather than merely effective, and the heavy
// warning that precedes it lives on the surface that offers it.
func (s *Store) ForceRemove(id, actor, reason string, at time.Time) (Watch, error) {
	s.mu.Lock()
	w, ok := s.watches[id]
	if !ok {
		s.mu.Unlock()
		return Watch{}, ErrNoSuchWatch
	}
	if !w.RetiredAt.IsZero() {
		s.mu.Unlock()
		return Watch{}, ErrAlreadyEnded
	}
	prevDetached, prevForcedAt, prevForcedBy, prevForcedReason := w.Detached, w.ForcedAt, w.ForcedBy, w.ForcedReason
	w.Detached = true
	w.ForcedAt = at
	w.ForcedBy = actor
	w.ForcedReason = reason
	out := clone(w)
	if err := s.tryPersistLocked(); err != nil {
		// A force-remove that only exists in memory must not be reported
		// as done (R6): the segment would reappear on the map after a
		// restart while the operator was told it was gone, with none of
		// the who/when/why #385 requires this override to carry.
		w.Detached, w.ForcedAt, w.ForcedBy, w.ForcedReason = prevDetached, prevForcedAt, prevForcedBy, prevForcedReason
		s.mu.Unlock()
		return Watch{}, fmt.Errorf("%w: saving the force-remove: %v", ErrSaveFailed, err)
	}
	s.mu.Unlock()
	s.notify()
	return out, nil
}

// Sweep records retirement for every watch whose clean window has
// elapsed, returning those that just retired.
//
// StateAt already answers "retired" without this, so a sweep that never
// ran would not make the map wrong -- it exists so the transition is a
// recorded fact with a timestamp, and so the change hook fires and the
// zone stops being drawn without waiting for someone to ask.
func (s *Store) Sweep(now time.Time) []Watch {
	s.mu.Lock()
	var retired []Watch
	for _, w := range s.watches {
		if !w.RetiredAt.IsZero() {
			continue
		}
		if w.StateAt(now) != StateRetired {
			continue
		}
		// Stamped at the instant the window actually elapsed, not at the
		// instant the sweep noticed. A sweep that runs every minute must
		// not make the retirement time a property of its own cadence.
		w.RetiredAt = w.clockFrom().Add(w.CleanWindow)
		retired = append(retired, clone(w))
	}
	if len(retired) > 0 {
		// An automatic transition on elapsed time, not an operator
		// action -- StateAt already answers "retired" from CleanWindow
		// alone (see this method's own doc comment), so a retirement
		// timestamp lost to a transient disk issue is re-derived, at
		// worst a little later, by the next sweep. Stays on the
		// swallow-and-log default.
		s.persistLocked()
	}
	s.mu.Unlock()
	if len(retired) > 0 {
		s.notify()
	}
	return retired
}

// UndoWindow is how long after a watch retires the operator can take the
// retirement back.
//
// #485's ratified sentence is that retirement is silent: the ghost
// leaves by itself and the map goes back to what it was, with one note
// line and an undo for the hour after. The hour is what makes the
// silence affordable -- an operator who looks up, sees a zone gone and
// realises the segment was not finished after all needs a way back that
// does not depend on having been watching at the moment it happened.
//
// It is bounded rather than permanent because an undo offered forever is
// not an undo: the note line stays either way, and a retirement a day
// old is history, which is re-watched by watching the range again rather
// than by reopening a decision that has since been acted on.
const UndoWindow = time.Hour

// Restore un-retires a watch, within UndoWindow of its retirement.
//
// The clock restarts from now, so the range must go quiet for a fresh
// full clean window. That is deliberate and not a rounding of the old
// clock forward: the range *was* quiet, and the watch retired honestly
// on that evidence -- the operator taking the retirement back is saying
// they do not believe the segment is finished, so the watch has to earn
// its retirement again rather than inherit the one just taken from it.
// A restored watch that retired again a minute later would be an undo
// button that undid nothing.
func (s *Store) Restore(id string, now time.Time) (Watch, error) {
	s.mu.Lock()
	w, ok := s.watches[id]
	if !ok {
		s.mu.Unlock()
		return Watch{}, ErrNoSuchWatch
	}
	if w.RetiredAt.IsZero() {
		s.mu.Unlock()
		return Watch{}, ErrNotRetired
	}
	if now.Sub(w.RetiredAt) > UndoWindow {
		s.mu.Unlock()
		return Watch{}, ErrUndoExpired
	}
	prevRetiredAt, prevLastTrafficAt := w.RetiredAt, w.LastTrafficAt
	w.RetiredAt = time.Time{}
	w.LastTrafficAt = now
	out := clone(w)
	if err := s.tryPersistLocked(); err != nil {
		// A restore that only exists in memory must not be reported as
		// done (R6): the watch would still read as retired after a
		// restart while the operator was told they had taken that back.
		w.RetiredAt, w.LastTrafficAt = prevRetiredAt, prevLastTrafficAt
		s.mu.Unlock()
		return Watch{}, fmt.Errorf("%w: saving the restore: %v", ErrSaveFailed, err)
	}
	s.mu.Unlock()
	s.notify()
	return out, nil
}

// Delete removes a watch outright -- the operator abandoning a
// decommission rather than completing one. Retiring is Sweep's job; this
// is "I no longer want to be asked about this range".
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	w, ok := s.watches[id]
	if !ok {
		s.mu.Unlock()
		return ErrNoSuchWatch
	}
	delete(s.watches, id)
	if err := s.tryPersistLocked(); err != nil {
		// A delete that only exists in memory must not be reported as
		// done (R6): the watch would reappear after a restart while the
		// operator was told they were no longer being asked about it.
		s.watches[id] = w
		s.mu.Unlock()
		return fmt.Errorf("%w: saving the delete: %v", ErrSaveFailed, err)
	}
	s.mu.Unlock()
	s.notify()
	return nil
}
