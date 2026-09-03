// SPDX-License-Identifier: AGPL-3.0-only

package retention

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/store"
)

var logger = logging.New("retention")

// Defaults for the two operator settings, from
// docs/decisions/event-retention.md's sizing section: at the
// recommended logging posture a day packs and compresses to roughly
// 20MB, so thirty days is about 600MB and the byte cap is only reached
// by a deployment logging far more than it should -- which is exactly
// the case that needs a second bound, because the day count alone would
// let it fill the disk.
const (
	DefaultDays     = 30
	DefaultMaxBytes = 1 << 30 // 1 GiB
)

// Flush policy. A batch is written when either bound is hit.
//
// The window between flushes is what a hard kill loses: at the
// recommended 12-15 events/sec, five seconds is under a hundred events.
// Buying that back would mean sealing and syncing per event, which
// costs a nonce, a tag and an fsync each -- turning a compressed
// ~70 bytes into several hundred and putting a disk sync in the ingest
// path. The events are also still in the ring, so what is actually lost
// is the tail of the *retained* copy after a crash, not the events
// themselves.
const (
	defaultFlushInterval = 5 * time.Second
	defaultFlushEvents   = 500
)

// Options configures a Store.
type Options struct {
	// Dir is where the daily files live. Created 0700 if absent.
	Dir string
	// Key is the master key. Required: there is no unencrypted mode, so
	// a nil Key is a programming error rather than a configuration one --
	// callers decide whether retention runs at all by whether they have
	// a key, and Open refuses without one.
	Key *Key
	// Days and MaxBytes are the two caps. Zero or negative means the
	// default; see clamp.
	Days     int
	MaxBytes int64

	flushInterval time.Duration // test seam
	flushEvents   int           // test seam
}

// Store appends events to encrypted daily files and reads them back.
//
// One writer, many appenders: Append is called from the ingest path and
// only ever moves an event into a buffer under a short lock, so the
// cost paid on the ingest goroutine is a copy, never compression,
// encryption or a disk write. Those happen on this type's own goroutine
// when a batch is due.
type Store struct {
	dir      string
	key      *Key
	days     int
	maxBytes int64

	mu      sync.Mutex
	pending []record
	current *dayFile
	closed  bool

	flushEvents int
	wake        chan struct{}
	done        chan struct{}
	stop        chan struct{}
}

// Open prepares dir for retention and starts the flusher.
func Open(opts Options) (*Store, error) {
	if opts.Key == nil {
		return nil, ErrNoKey
	}
	if opts.Dir == "" {
		return nil, errors.New("retention: no directory configured")
	}
	if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("retention: creating %s: %w", opts.Dir, err)
	}
	days, maxBytes := clamp(opts.Days, opts.MaxBytes)
	interval := opts.flushInterval
	if interval <= 0 {
		interval = defaultFlushInterval
	}
	flushEvents := opts.flushEvents
	if flushEvents <= 0 {
		flushEvents = defaultFlushEvents
	}
	s := &Store{
		dir:         opts.Dir,
		key:         opts.Key,
		days:        days,
		maxBytes:    maxBytes,
		flushEvents: flushEvents,
		wake:        make(chan struct{}, 1),
		done:        make(chan struct{}),
		stop:        make(chan struct{}),
	}
	go s.run(interval)
	return s, nil
}

// clamp applies the defaults. A zero or negative cap is treated as
// unset rather than as "keep nothing": an operator who types 0 has not
// asked for their history to be deleted on the next flush, and a
// setting that destroys data when misread is a bad setting.
func clamp(days int, maxBytes int64) (int, int64) {
	if days <= 0 {
		days = DefaultDays
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	return days, maxBytes
}

// Append hands one event to retention. It never blocks on disk.
func (s *Store) Append(e store.Event) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.pending = append(s.pending, record{Event: e, ReceivedAt: e.ReceivedAt})
	due := len(s.pending) >= s.flushEvents
	s.mu.Unlock()
	if due {
		select {
		case s.wake <- struct{}{}:
		default: // a flush is already pending; nothing to add by queueing another
		}
	}
}

// run is the flusher goroutine.
func (s *Store) run(interval time.Duration) {
	defer close(s.done)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			if err := s.Flush(); err != nil {
				logger.Error("final flush failed", "err", err)
			}
			s.mu.Lock()
			s.current.Close()
			s.current = nil
			s.mu.Unlock()
			return
		case <-t.C:
			if err := s.Flush(); err != nil {
				logger.Error("flush failed", "err", err)
			}
		case <-s.wake:
			if err := s.Flush(); err != nil {
				logger.Error("flush failed", "err", err)
			}
		}
	}
}

// Flush writes everything buffered and applies the caps.
//
// Events are grouped by their own day rather than by the clock, so an
// event that arrives a moment after midnight lands in the day it
// belongs to and the file for a day is never a mixture. That matters
// because the day is mixed into the file's key: a misfiled event would
// not merely be untidy, it would be unreadable in the place a reader
// looks for it.
func (s *Store) Flush() error {
	s.mu.Lock()
	batch := s.pending
	s.pending = nil
	s.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}

	sort.SliceStable(batch, func(i, j int) bool {
		return batch[i].ReceivedAt.Before(batch[j].ReceivedAt)
	})

	var firstErr error
	for start := 0; start < len(batch); {
		day := dayOf(batch[start].ReceivedAt)
		end := start
		for end < len(batch) && dayOf(batch[end].ReceivedAt) == day {
			end++
		}
		if err := s.appendToDay(day, batch[start:end]); err != nil && firstErr == nil {
			firstErr = err
		}
		start = end
	}
	if err := s.prune(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// appendToDay writes one day's slice of a batch, opening or rolling the
// current file as needed.
func (s *Store) appendToDay(day string, batch []record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil || s.current.day != day {
		s.current.Close()
		d, err := openDayFile(s.dir, day, s.key)
		if err != nil {
			s.current = nil
			return err
		}
		s.current = d
	}
	return s.current.appendFrame(batch)
}

// Close flushes, stops the flusher and releases the open file.
func (s *Store) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	close(s.stop)
	<-s.done
	return nil
}

// days lists the retained files, oldest first.
func (s *Store) dayFiles() ([]dayEntry, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("retention: listing %s: %w", s.dir, err)
	}
	var out []dayEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		day, ok := dayFromFileName(e.Name())
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, dayEntry{day: day, path: filepath.Join(s.dir, e.Name()), size: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].day < out[j].day })
	return out, nil
}

type dayEntry struct {
	day  string
	path string
	size int64
}

// prune enforces both caps, oldest day first.
//
// The newest day is never deleted, even if it alone exceeds the byte
// cap. Deleting it would mean a deployment whose single day is over the
// cap retains nothing at all while reporting that retention is on --
// the cap is there to bound growth, not to make the feature silently
// inert. An operator in that position needs a bigger cap or less
// logging, and the receipt's window is what tells them so.
func (s *Store) prune() error {
	files, err := s.dayFiles()
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	var total int64
	for _, f := range files {
		total += f.size
	}

	var firstErr error
	drop := func(i int) {
		if err := os.Remove(files[i].path); err != nil && !errors.Is(err, os.ErrNotExist) {
			if firstErr == nil {
				firstErr = fmt.Errorf("retention: dropping %s: %w", files[i].path, err)
			}
			return
		}
		total -= files[i].size
		logger.Info("dropped a retained day", "day", files[i].day, "bytes", files[i].size)
	}

	i := 0
	for ; len(files)-i > s.days && i < len(files)-1; i++ {
		drop(i)
	}
	for ; total > s.maxBytes && i < len(files)-1; i++ {
		drop(i)
	}
	// A day dropped underneath the open file leaves the writer holding a
	// deleted path; reopening on the next flush is cheaper than tracking
	// it, and only happens when the current day is itself pruned, which
	// the guard above prevents.
	return firstErr
}

// Purge deletes every retained file.
//
// This is what turning the switch off does, per the ADR: off means the
// history goes, not that it lingers unreferenced. An operator turning
// retention off has said they do not want events on disk, and leaving
// yesterday's there would make that setting a lie.
func (s *Store) Purge() error {
	s.mu.Lock()
	s.pending = nil
	s.current.Close()
	s.current = nil
	s.mu.Unlock()

	files, err := s.dayFiles()
	if err != nil {
		return err
	}
	var firstErr error
	for _, f := range files {
		if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = fmt.Errorf("retention: purging %s: %w", f.path, err)
		}
	}
	if firstErr == nil {
		logger.Info("retained events purged", "days", len(files))
	}
	return firstErr
}

// PurgeDir deletes retained files without opening a Store.
//
// Turning the switch off means the files go even though nothing is
// running to write them -- most obviously when retention is off at
// startup and a previous run left a history behind. Doing that through
// Open would mean holding a key to delete files, which is the one
// operation on them that needs no key at all.
func PurgeDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("retention: listing %s: %w", dir, err)
	}
	var firstErr error
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, ok := dayFromFileName(e.Name()); !ok {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = fmt.Errorf("retention: purging %s: %w", e.Name(), err)
		}
	}
	return firstErr
}

// Window is what one pass over the retained files actually covered.
//
// Reported rather than assumed, for the same reason every receipt in
// this codebase reports its own window: the setting says thirty days,
// the disk says what it says, and the difference between them is
// exactly the thing an operator must never be misled about.
type Window struct {
	Start time.Time
	End   time.Time
	Count int
	// Days is how many day files contributed.
	Days int
	// Err is the first error met while reading. A pass that meets one
	// still reports what it read before it: a corrupt or foreign file
	// makes the window shorter, and a shorter window honestly reported
	// is the correct outcome.
	Err error
}

// Replay visits every retained event with ReceivedAt strictly before
// cutoff, oldest first.
//
// The cutoff exists because the ring and the files overlap: every event
// on disk was in memory first, and for as long as the ring holds it,
// both copies exist. The caller passes the oldest instant the ring
// itself still holds, and this pass stops there, so no event is visited
// twice and the seam between disk and memory has no gap in it.
func (s *Store) Replay(cutoff time.Time, visit func(store.Event)) Window {
	return ReplayDir(s.dir, s.key, cutoff, visit)
}

// ReplayDir is Replay without a running Store, for a reader that has a
// key and a directory and nothing else.
func ReplayDir(dir string, key *Key, cutoff time.Time, visit func(store.Event)) Window {
	var w Window
	s := &Store{dir: dir, key: key}
	files, err := s.dayFiles()
	if err != nil {
		w.Err = err
		return w
	}
	for _, f := range files {
		// A whole day after the cutoff cannot contribute: skip it
		// without opening it, so the common case (a long history, a ring
		// holding today) does not decrypt files it will discard.
		if cutoff.IsZero() || f.day <= dayOf(cutoff) {
			read := 0
			_, err := replayFile(f.path, f.day, key, func(e store.Event) {
				if !cutoff.IsZero() && !e.ReceivedAt.Before(cutoff) {
					return
				}
				if w.Start.IsZero() || e.ReceivedAt.Before(w.Start) {
					w.Start = e.ReceivedAt
				}
				if e.ReceivedAt.After(w.End) {
					w.End = e.ReceivedAt
				}
				w.Count++
				read++
				visit(e)
			})
			if read > 0 {
				w.Days++
			}
			if err != nil {
				w.Err = err
				return w
			}
		}
	}
	return w
}
