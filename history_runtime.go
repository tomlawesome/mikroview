// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tomlawesome/mikroview/internal/api"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/retention"
	"github.com/tomlawesome/mikroview/internal/settings"
	"github.com/tomlawesome/mikroview/internal/store"
)

// historyRuntime owns the on-disk event history for the life of the
// process: which store is open, whether one is open at all, and what
// happens at the moment an admin moves the control (#910).
//
// It exists because retention became a *runtime* setting rather than a
// config-file one, and "turn it on" is not a flag flip. It has to open
// an encrypted store, hand it what memory already holds, and start
// taking live events -- with no gap and no duplicate at the seam --
// while "turn it off" has to close that store and delete every file
// before the operator's request returns. Somewhere has to hold those
// sequences; the alternative was spreading them across the API handler
// and the ingest loop, where the ordering would be nobody's
// responsibility.
//
// Where it deliberately does not live: internal/retention, which knows
// nothing about settings, keys-as-configuration or the ring, and
// internal/api, which knows nothing about keys or day files. Same
// boundary the retainedDays adapter in history.go keeps, for the same
// reason.
//
// It also *is* the engine's corpus adapter (Days/ReplayDay below), and
// that is the point: the store underneath can be swapped for nil and
// back while a replay is running, and every reader goes on holding one
// stable object. Handing the API server a *retention.Store directly
// would mean re-wiring the server on every change.
type historyRuntime struct {
	log *slog.Logger
	dir string
	// key is nil when no usable key file is mounted. That is the
	// "cannot run at all" state: there is no unencrypted mode, so the
	// control is refused rather than shown as available and dead.
	key  *retention.Key
	ring *store.Store
	set  *settings.Store

	// cur is the open store, or nil when retention is off. Atomic
	// because Append reads it on the ingest path for every event while
	// an admin may be swapping it.
	cur atomic.Pointer[retention.Store]

	// mu serialises ApplyHistory end to end, so two admins cannot
	// interleave an open with a purge.
	mu       sync.Mutex
	days     int
	maxBytes int64

	// holding, held and heldMu are the barrier that makes turning it on
	// exact -- see turnOn. holding is read on the ingest path, so it is
	// an atomic rather than a lock: the ordinary case pays one atomic
	// load per event and never touches heldMu.
	holding atomic.Bool
	heldMu  sync.Mutex
	held    []store.Event
	// highWater is the newest ring ID the backfill reached. Only ever
	// written and read inside ApplyHistory, under mu.
	highWater uint64
}

// errNoHistoryKey is what ApplyHistory returns when nothing can be
// retained on this instance. Distinguished from a failure because it is
// not one: it is a deployment that has not mounted a key, which the API
// reports as a conflict with a sentence saying what to do about it.
var errNoHistoryKey = errors.New("no history key file is mounted")

// newHistoryRuntime brings the history up as this instance's settings
// leave it.
//
// The stored settings win over config.yaml's, for the reason
// openStoreSettings gives for the memory figure: the stored one is the
// more recent statement and was made with the actual consequence on
// screen, while the file's was written before the instance had ever
// seen traffic. history.keyFile and history.dir are not part of that --
// they stay config-only, so a browser can never point the history at a
// different key or a different disk.
func newHistoryRuntime(log *slog.Logger, cfg config.Config, set *settings.Store, ring *store.Store) *historyRuntime {
	effective := cfg
	if stored, ok := set.History(); ok {
		if stored.Enabled != cfg.History.Enabled || stored.Days != cfg.History.Days || stored.MaxBytes != cfg.History.MaxBytes {
			log.Info(fmt.Sprintf(
				"on-disk event history: using the stored settings (%s), set from Settings, rather than config.yaml's (%s) -- delete %s to go back to the file's figures",
				describeHistorySettings(stored.Enabled, stored.Days, stored.MaxBytes),
				describeHistorySettings(cfg.History.Enabled, cfg.History.Days, cfg.History.MaxBytes),
				cfg.Store.SettingsStorePath))
		}
		effective.History.Enabled = stored.Enabled
		effective.History.Days = stored.Days
		effective.History.MaxBytes = stored.MaxBytes
	}

	r := &historyRuntime{
		log:      log,
		dir:      historyDirectory(cfg),
		ring:     ring,
		set:      set,
		days:     effective.History.Days,
		maxBytes: effective.History.MaxBytes,
	}
	// The key is loaded here as well as inside openHistory because the
	// runtime has to be able to reopen the store later, and re-reading
	// the file at that point would mean a control that quietly starts
	// using different key material than the one this process started
	// with. LoadKey's own errors are reported by openHistory a line
	// below, so this one is deliberately silent.
	if key, err := retention.LoadKey(cfg.History.KeyFile); err == nil {
		r.key = key
	}
	r.cur.Store(openHistory(log, effective))
	return r
}

// describeHistorySettings puts the three figures in one phrase, for the
// startup line that names both candidates.
func describeHistorySettings(enabled bool, days int, maxBytes int64) string {
	if !enabled {
		return "off"
	}
	return fmt.Sprintf("on, %d days, %s", days, config.ByteSize(maxBytes))
}

// Append hands one event to whatever store is open, or drops it if none
// is. Nil-safe on the receiver for the same reason
// retention.Store.Append is: retention being off is the ordinary case,
// and the ingest path should not carry a check for it.
func (r *historyRuntime) Append(e store.Event) {
	if r == nil {
		return
	}
	if r.holding.Load() {
		r.heldMu.Lock()
		if r.holding.Load() {
			r.held = append(r.held, e)
			r.heldMu.Unlock()
			return
		}
		r.heldMu.Unlock()
	}
	r.cur.Load().Append(e)
}

// Days satisfies engine.RetainedDays. Nothing open means no days, not
// an error: memory-only is a first-class mode.
func (r *historyRuntime) Days() ([]string, error) {
	if st := r.cur.Load(); st != nil {
		return st.Days()
	}
	return nil, nil
}

// ReplayDay satisfies engine.RetainedDays.
func (r *historyRuntime) ReplayDay(day string, cutoff time.Time, visit func(store.Event)) (int, error) {
	if st := r.cur.Load(); st != nil {
		return st.ReplayDay(day, cutoff, visit)
	}
	return 0, nil
}

// Flush writes whatever is buffered. Nil-safe, and a no-op when
// retention is off.
func (r *historyRuntime) Flush() error {
	if r == nil {
		return nil
	}
	if st := r.cur.Load(); st != nil {
		return st.Flush()
	}
	return nil
}

// Close flushes the last batch and releases the open file. It does not
// purge: shutting down is not turning the feature off.
func (r *historyRuntime) Close() error {
	if r == nil {
		return nil
	}
	return r.cur.Load().Close()
}

// HistorySettings reports the state, reading the directory for what is
// actually held rather than trusting what was asked for.
func (r *historyRuntime) HistorySettings() api.HistorySettings {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.settingsLocked()
}

func (r *historyRuntime) settingsLocked() api.HistorySettings {
	st := r.cur.Load()
	out := api.HistorySettings{
		Keyed:    r.key != nil,
		Enabled:  st != nil,
		Days:     r.days,
		MaxBytes: r.maxBytes,
	}

	files, err := retention.DaysHeld(r.dir)
	if err != nil {
		// Reported, never guessed at: a directory that cannot be listed
		// is not the same fact as an empty one, and held stays null
		// rather than claiming nothing is kept.
		r.log.Warn("could not read what the on-disk event history holds", "dir", r.dir, "err", err)
		return out
	}
	if len(files) == 0 {
		return out
	}

	held := api.HistoryHeld{
		Days:   len(files),
		Oldest: files[0].Day,
		Newest: files[len(files)-1].Day,
	}
	today := retention.Today()
	for _, f := range files {
		held.Bytes += f.Bytes
		// The newest day that is not still being written to. A partial
		// day multiplied out would tell an operator their month fits
		// when it does not.
		if f.Day != today {
			out.BytesPerDay = f.Bytes
		}
	}
	out.Held = &held
	// Both halves, because either alone would overstate: a store that
	// has cap-pruned at some point but is now holding its full day
	// count is not capped today, and a short window with no cap prune
	// behind it is short for some other reason (a young deployment,
	// most often) that a "full" reading would misname.
	out.Capped = held.Days < r.days && st.Capped()
	return out
}

// ApplyHistory is the whole act of moving the control: store the three
// settings, then make them true of the running instance.
//
// Store first, for handleStoreSettingsUpdate's reason -- a change that
// took visible effect but was never written would be silently undone at
// the next restart. The lifecycle work after it cannot leave the
// instance disagreeing with the store in any way an operator would not
// see: the response reports the state afterwards, read back rather than
// assumed.
func (r *historyRuntime) ApplyHistory(enabled bool, days int, maxBytes int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.key == nil {
		return errNoHistoryKey
	}
	if err := r.set.SetHistory(settings.History{Enabled: enabled, Days: days, MaxBytes: maxBytes}); err != nil {
		return fmt.Errorf("storing the history settings: %w", err)
	}
	r.days, r.maxBytes = days, maxBytes

	st := r.cur.Load()
	switch {
	case !enabled:
		return r.turnOff(st)
	case st == nil:
		return r.turnOn(days, maxBytes)
	default:
		// Already on: apply the caps at once rather than at the next
		// flush, so a day count taken from 30 to 7 drops the days now,
		// while the operator is watching. See retention.SetCaps.
		return st.SetCaps(days, maxBytes)
	}
}

// turnOff closes the store and deletes every retained file.
//
// The purge runs whether or not a store was open, because a previous
// run may have left files behind and "off" has to mean the events are
// gone -- the same reason openHistory purges on its own off paths.
// PurgeDir rather than Store.Purge: deleting is the one operation on
// these files that needs no key, and it must work on the closed store
// too.
func (r *historyRuntime) turnOff(st *retention.Store) error {
	r.cur.Store(nil)
	if err := st.Close(); err != nil {
		r.log.Warn("could not flush the on-disk event history before deleting it", "err", err)
	}
	if err := retention.PurgeDir(r.dir); err != nil {
		return fmt.Errorf("deleting the retained event history: %w", err)
	}
	r.log.Info("on-disk event history turned off -- everything retained has been deleted", "dir", r.dir)
	return nil
}

// turnOn opens a store and gives it what memory already holds, then
// every event after (owner's ruling, 2026-09-03: turning it on is not a
// proposal, it takes the ring).
//
// The seam is the delicate part, and it is why live events are held
// aside for the length of the backfill rather than the store simply
// being swapped in first or last. Swap first and an event inserted into
// the ring while the backfill walks it gets written twice -- a
// threshold detector replaying that day would count it twice. Swap last
// and every event arriving during the walk is never written at all,
// leaving a hole that only appears once the ring has wrapped past it.
// Neither is acceptable in a file whose whole value is that its window
// can be trusted, and there is no instant at which the ring and the
// store can be swapped together.
//
// So: hold live events, walk the ring, then release the held ones the
// walk did not already reach. IDs are assigned by the ring in
// increasing order and the walk runs forward, so "the walk did not
// reach it" is exactly "its ID is above the highest the walk saw" --
// one comparison, no bookkeeping per event.
func (r *historyRuntime) turnOn(days int, maxBytes int64) error {
	r.holding.Store(true)
	defer r.release()

	st, err := retention.Open(retention.Options{
		Dir:      r.dir,
		Key:      r.key,
		Days:     days,
		MaxBytes: maxBytes,
	})
	if err != nil {
		return fmt.Errorf("opening the retained event history: %w", err)
	}
	r.cur.Store(st)

	var high uint64
	backfilled := 0
	if r.ring != nil {
		engine.NewMemoryCorpus(r.ring).Replay(func(e store.Event) {
			if e.ID > high {
				high = e.ID
			}
			backfilled++
			st.Append(e)
		})
	}
	r.highWater = high

	r.log.Info("on-disk event history turned on", "dir", r.dir, "days", days, "maxBytes", maxBytes,
		"backfilled", backfilled)
	return nil
}

// release writes the events held during a backfill and puts the ingest
// path back on its ordinary course. Deferred, so a failed open releases
// the barrier too -- the alternative is an instance that silently stops
// retaining because one open failed.
func (r *historyRuntime) release() {
	r.heldMu.Lock()
	held := r.held
	r.held = nil
	r.holding.Store(false)
	r.heldMu.Unlock()

	st := r.cur.Load()
	if st == nil {
		return
	}
	for _, e := range held {
		if e.ID > r.highWater {
			st.Append(e)
		}
	}
	// Flushed here rather than left to the five-second ticker so that
	// what the operator is told is on disk actually is: the response to
	// their PUT reports the days held, and a backfill still sitting in
	// a buffer would report none.
	if err := st.Flush(); err != nil {
		r.log.Warn("could not write the backfilled event history", "err", err)
	}
}
