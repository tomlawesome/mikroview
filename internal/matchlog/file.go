// SPDX-License-Identifier: AGPL-3.0-only

package matchlog

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

const (
	kindRecord = "record"
	kindUpdate = "update"
)

// maxLineBytes bounds one JSON line bufio.Scanner will accept. A Record
// embeds a full store.Event, whose Raw field is deliberately unclamped
// and can reach the syslog TCP listener's 64KB line limit (see #244's
// memory_test.go, worstCaseLine) -- JSON string-escaping can expand that
// further, so this is set well above it rather than at bufio.Scanner's
// 64KB default, which would silently fail to scan exactly the worst-case
// lines #244 measured.
const maxLineBytes = 256 * 1024

// fileLine is the on-disk shape of one line. A "record" line carries
// every field; an "update" line -- written when Append collapses a
// repeat into an already-open record -- carries only ID and LastSeen.
// Count is deliberately not stored on either kind: it is derived at read
// time from how many lines contributed to a record (Query/replay), which
// removes any need to track or trust a running count across writes.
type fileLine struct {
	Kind        string      `json:"kind"`
	ID          string      `json:"id"`
	EntryID     string      `json:"entryId,omitempty"`
	Tuple       Tuple       `json:"tuple,omitempty"`
	Event       store.Event `json:"event,omitempty"`
	FirstSeen   time.Time   `json:"firstSeen,omitempty"`
	LastSeen    time.Time   `json:"lastSeen"`
	Provisional bool        `json:"provisional,omitempty"`
}

// FileStore is the append-only, JSON-lines file backend. The only
// in-memory state it keeps is index: one entry per currently-open
// (not-yet-capacity-refused) Tuple, mapping to that record's ID.
// Bounded by capacity -- once full, index stops growing (ErrCapacityReached
// refuses new tuples) -- so this is a small, fixed-size cost relative to
// the log itself, not a second copy of it: at capacity 500,000 and a
// realistic ~80-byte map entry, that is on the order of 40MB, which is
// deliberately paid once (see this package's doc comment on why a
// zero-memory design cannot also collapse repeats -- something has to
// know a tuple is already open, and the alternative was scanning the
// whole file per Append, which is not viable at this capacity).
//
// Everything else -- Query's results, replay's one-time startup pass --
// streams from disk rather than being held permanently, per #243 section
// 3's "must not cost the rest of the app anything at rest."
type FileStore struct {
	mu       sync.Mutex
	path     string
	f        *os.File
	capacity int
	index    map[string]string // Tuple.key(entryID) -> record ID
}

// Open opens (creating if needed) a match log at path with the given
// capacity -- the maximum number of distinct records it will ever hold;
// see ErrCapacityReached. capacity must be positive: this package has no
// operator-facing default of its own to fall back to, the caller (the
// eventual config wiring) owns that decision the same way #244 gave
// store.maxMemory its own validated default.
//
// A file that already has content is replayed once, synchronously, to
// rebuild index -- the one place this store pays a cost proportional to
// its own size rather than to a query's result, and it is paid once at
// startup rather than on every operation.
func Open(path string, capacity int) (*FileStore, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("matchlog: capacity must be positive, got %d", capacity)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("matchlog: opening %s: %w", path, err)
	}
	s := &FileStore{path: path, f: f, capacity: capacity, index: make(map[string]string)}
	if err := s.replay(); err != nil {
		f.Close()
		return nil, err
	}
	return s, nil
}

// replay rebuilds index from every "record" line already on disk. Reads
// through a separate handle from s.f (the append-mode write handle)
// rather than relying on the platform detail that O_APPEND doesn't
// affect where reads start -- opening a distinct read-only handle for
// this one-time pass is obviously correct instead.
//
// A line that fails to decode -- a torn write left by an unclean
// shutdown, landing exactly at EOF -- is skipped rather than treated as
// fatal, the same "a corrupted store must never block startup" rule
// every other optional store in this codebase follows (see
// internal/audit.Open's doc comment).
func (s *FileStore) replay() error {
	rf, err := os.Open(s.path)
	if err != nil {
		return fmt.Errorf("matchlog: replaying %s: %w", s.path, err)
	}
	defer rf.Close()

	sc := bufio.NewScanner(rf)
	sc.Buffer(make([]byte, 64*1024), maxLineBytes)
	for sc.Scan() {
		var l fileLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			continue
		}
		if l.Kind == kindRecord {
			s.index[l.Tuple.key(l.EntryID)] = l.ID
		}
		// kindUpdate needs no index change: it only ever touches a
		// tuple replay has already indexed from its "record" line,
		// which -- being append-only -- always precedes any update
		// referencing it.
	}
	return sc.Err()
}

// Append implements Store.
func (s *FileStore) Append(entryID string, tuple Tuple, event store.Event, t time.Time) error {
	return s.appendProvisional(entryID, tuple, event, t, false)
}

// AppendProvisional implements Store.
func (s *FileStore) AppendProvisional(entryID string, tuple Tuple, event store.Event, t time.Time, provisional bool) error {
	return s.appendProvisional(entryID, tuple, event, t, provisional)
}

func (s *FileStore) appendProvisional(entryID string, tuple Tuple, event store.Event, t time.Time, provisional bool) error {
	if tuple.Source.Empty() {
		return ErrEmptyIdentity
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := tuple.key(entryID)
	if id, open := s.index[key]; open {
		// Provisional is fixed at creation -- a collapsed repeat only
		// ever touches ID/LastSeen, same as before this field existed.
		return s.writeLineLocked(fileLine{Kind: kindUpdate, ID: id, LastSeen: t})
	}
	if len(s.index) >= s.capacity {
		return ErrCapacityReached
	}

	id := newID()
	if err := s.writeLineLocked(fileLine{
		Kind: kindRecord, ID: id, EntryID: entryID, Tuple: tuple, Event: event,
		FirstSeen: t, LastSeen: t, Provisional: provisional,
	}); err != nil {
		return err
	}
	// Only added after a successful write: a failed write must not leave
	// index pointing at a record that isn't actually on disk.
	s.index[key] = id
	return nil
}

// writeLineLocked encodes l as one JSON line and appends it in a single
// Write call -- built as one byte slice first (JSON, then '\n') rather
// than two separate writes, to keep the window in which a concurrent
// Query could observe a torn line as small as a single write(2) syscall
// can make it. fsync's every write: matches are rare by this feature's
// own premise (#243 section 4), so the cost is affordable, and it is
// what actually backs "a match survives a restart" against an unclean
// shutdown, not just a clean one -- a Write that returns successfully
// without a Sync can still be sitting in the OS page cache, not on disk,
// when the process is killed.
func (s *FileStore) writeLineLocked(l fileLine) error {
	b, err := json.Marshal(l)
	if err != nil {
		return fmt.Errorf("matchlog: encoding a record: %w", err)
	}
	b = append(b, '\n')
	if _, err := s.f.Write(b); err != nil {
		return fmt.Errorf("matchlog: writing to %s: %w", s.path, err)
	}
	if err := s.f.Sync(); err != nil {
		return fmt.Errorf("matchlog: syncing %s: %w", s.path, err)
	}
	return nil
}

// Query implements Store.
//
// Opens its own read handle rather than taking s.mu across file I/O, so
// a slow query does not block Append -- correctness does not depend on
// serializing with writers, only on reading an append-only file, whose
// already-written bytes never change. The one accepted race: a line
// still mid-write when Query's scanner reaches it can be read as
// incomplete JSON and is skipped like any other torn line (see replay's
// doc comment) -- that match simply is not in this query's results and
// will be on the next one, which is an acceptable eventual-consistency
// window for a monitoring feature, not a transactional one.
// It runs in two passes, and that shape is the point.
//
// A single pass has to keep a whole Record -- Tuple and the entire
// store.Event, Raw line included -- for every record matching q.Source,
// because Limit cannot be applied until the ordering is known, and the
// ordering is by LastSeen, which is not final until the last line has
// been read (a kindUpdate line arbitrarily far down the file moves it).
// So Limit: 1 still materialised everything: measured at 237 MiB of heap
// and 1.86s to return one record, against a 164 MiB log holding 20,000
// records for one MAC. GET /api/watchlist/matches reaches this on the
// lowest-privilege credential mikroview issues -- a read-only API token
// -- with no rate limiter in front of it, so the cost is repeatable at
// will.
//
// The first pass therefore keeps only what ordering needs: an ID and
// three timestamps per matching record, tens of bytes rather than
// kilobytes. Once the window filter and Limit have chosen which records
// to return, the second pass reads the full payloads for those IDs
// alone. It costs a second read of a file the first pass had to read
// whole anyway, and bounds what is *retained* by Limit rather than by
// how much history the log holds. See #285.
func (s *FileStore) Query(ctx context.Context, q Query, yield func(Record) bool) error {
	if q.Source.Empty() {
		return ErrEmptyIdentity
	}
	limit := clampLimit(q.Limit)

	// Pass one: ordering metadata only.
	type meta struct {
		id        string
		firstSeen time.Time
		lastSeen  time.Time
		count     uint64
	}
	acc := make(map[string]*meta)
	err := s.scanLines(func(l *fileLine) {
		switch l.Kind {
		case kindRecord:
			if !q.Source.MatchesSource(l.Tuple.Source) {
				return
			}
			// FirstSeen is fixed when the record is created, so a record
			// entirely at or after the window can be discarded here
			// rather than carried to the end. LastSeen cannot be
			// filtered on yet -- a later update moves it forward, which
			// is exactly how a long-running match stays visible.
			if !q.Until.IsZero() && !l.FirstSeen.Before(q.Until) {
				return
			}
			acc[l.ID] = &meta{id: l.ID, firstSeen: l.FirstSeen, lastSeen: l.LastSeen, count: 1}
		case kindUpdate:
			if m, ok := acc[l.ID]; ok {
				m.lastSeen = l.LastSeen
				m.count++
			}
		}
	})
	if err != nil {
		return err
	}

	selected := make([]*meta, 0, len(acc))
	for _, m := range acc {
		if m.lastSeen.Before(q.Since) {
			continue // entirely before the window
		}
		selected = append(selected, m)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].lastSeen.After(selected[j].lastSeen) })
	if len(selected) > limit {
		selected = selected[:limit]
	}
	if len(selected) == 0 {
		return nil
	}

	// Pass two: the payloads for the chosen records, and nothing else.
	wanted := make(map[string]*meta, len(selected))
	for _, m := range selected {
		wanted[m.id] = m
	}
	byID := make(map[string]Record, len(selected))
	err = s.scanLines(func(l *fileLine) {
		if l.Kind != kindRecord {
			return
		}
		m, ok := wanted[l.ID]
		if !ok {
			return
		}
		byID[l.ID] = Record{
			ID: l.ID, EntryID: l.EntryID, Tuple: l.Tuple, Event: l.Event,
			FirstSeen: l.FirstSeen, LastSeen: m.lastSeen, Count: m.count,
			Provisional: l.Provisional,
		}
	})
	if err != nil {
		return err
	}

	for _, m := range selected {
		if err := ctx.Err(); err != nil {
			return err // the caller went away
		}
		// A record present in pass one but not pass two means the file
		// was truncated or rewritten underneath us. Skipping is
		// consistent with how a torn line is already handled.
		r, ok := byID[m.id]
		if !ok {
			continue
		}
		if !yield(r) {
			break
		}
	}
	return nil
}

// scanLines opens its own read handle and calls fn for each parsable
// line. Unparsable lines are skipped, matching replay's handling of a
// torn write.
//
// Its own handle rather than s.mu across file I/O, so a slow query does
// not block Append -- correctness does not depend on serialising with
// writers, only on reading an append-only file, whose already-written
// bytes never change.
func (s *FileStore) scanLines(fn func(*fileLine)) error {
	f, err := os.Open(s.path)
	if err != nil {
		return fmt.Errorf("matchlog: querying %s: %w", s.path, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxLineBytes)
	for sc.Scan() {
		var l fileLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			continue
		}
		fn(&l)
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("matchlog: reading %s: %w", s.path, err)
	}
	return nil
}

// Stats implements Store.
func (s *FileStore) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{Count: len(s.index), Capacity: s.capacity, Full: len(s.index) >= s.capacity}
}

// Close implements Store.
func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.f.Close()
}
