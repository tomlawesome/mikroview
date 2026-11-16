// SPDX-License-Identifier: AGPL-3.0-only

package matchlog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
	"github.com/tomlawesome/mikroview/internal/store"
)

const (
	kindRecord = "record"
	kindUpdate = "update"
)

// maxLineBytes bounds one encoded line. A Record embeds a full
// store.Event, whose Raw field is deliberately unclamped and can reach
// the syslog TCP listener's 64KB line limit (see #244's memory_test.go,
// worstCaseLine) -- JSON string-escaping can expand that further, so
// this is set well above it. A record above it is cut, not refused: its
// Raw is clamped to store.MaxRawBytes and written. Ingest already cuts
// Raw to that (main.go), so this only catches events stored before that
// cut existed. A line still over the limit after the cut is refused at
// write time rather than written as a frame every reader would stop at.
const maxLineBytes = 256 * 1024

var errLineTooLong = errors.New("line too long")

// storeName is the match log's name among the stores -backup carries,
// and is bound into every frame's additional data.
const storeName = "match_log"

// Format is the match log's on-disk framing (#1470): the sealed,
// append-only framing internal/retention's day files use, with its own
// magic, key-derivation info and additional data, so no frame of one
// opens as the other. One frame is one line -- a record or an update --
// rather than a gzipped batch, because each line is written and synced
// on its own (matches are rare by design, #243 section 4) and a one-line
// gzip gains nothing. The additional data names the store, not the path,
// so a log moved with its data directory still opens (as #1450 for the
// other stores).
//
// The default file name, matchlog.jsonl, is historical: the file has not
// held JSON lines since v0.7.0.
var Format = retention.FrameFormat{
	Magic:         "MVMLG",
	Version:       1,
	KeyInfo:       "mikroview/match-log/v1/",
	AADPrefix:     []byte("MVMLG\x01" + storeName),
	MaxFrameBytes: maxLineBytes + frameOverhead,
	Description:   "match log",
}

// frameOverhead is the AES-GCM nonce and tag around one line.
const frameOverhead = 12 + 16

// fileLine is the plaintext of one frame. A "record" line carries
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

// FileStore is the append-only, sealed file backend: one frame of
// Format per line, under the operator's retention key (rule 2 of the
// #853 amendment to docs/decisions/event-retention.md: nothing MikroView
// writes to disk is in the clear). The only in-memory state it keeps is
// index: one entry per currently-open (not-yet-capacity-refused) Tuple,
// mapping to that record's ID.
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
	key      *retention.Key
	log      *retention.FrameLog
	capacity int
	index    map[string]string // Tuple.key(entryID) -> record ID
}

// Open opens (creating if needed) a sealed match log at path under key,
// with the given capacity -- the maximum number of distinct records it
// will ever hold; see ErrCapacityReached. capacity must be positive: this
// package has no operator-facing default of its own to fall back to, the
// caller (the eventual config wiring) owns that decision the same way
// #244 gave store.maxMemory its own validated default.
//
// key must not be nil: there is no unsealed mode (rule 2). A file that
// is not a sealed match log is refused and left exactly as it is: a
// plaintext JSON-lines log from an earlier release with
// persist.ErrPlaintextDocument (#1202 ruling 19 -- never read, never
// sealed in place), anything else as not a match log.
//
// A file that already has content is replayed once, synchronously, to
// rebuild index -- the one place this store pays a cost proportional to
// its own size rather than to a query's result, and it is paid once at
// startup rather than on every operation. A torn last frame (a crash
// mid-write) is cut back to the last whole frame; a frame of whole
// length that does not open (wrong key, or altered bytes) fails Open.
func Open(path string, capacity int, key *retention.Key) (*FileStore, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("matchlog: capacity must be positive, got %d", capacity)
	}
	if key == nil {
		return nil, errors.New("matchlog: the match log is sealed and needs history.keyFile; there is no unsealed mode")
	}
	before, err := sniff(path)
	if err != nil {
		return nil, err
	}
	l, err := retention.OpenFrameLog(path, key, Format)
	if err != nil {
		return nil, fmt.Errorf("matchlog: %s: %w", path, err)
	}
	if before > 0 {
		if fi, err := os.Stat(path); err == nil && fi.Size() != before {
			logging.New("matchlog").Warn(fmt.Sprintf("the match log at %s ended in a partial frame (an unclean shutdown mid-write); cut back from %d to %d bytes", path, before, fi.Size()))
		}
	}
	s := &FileStore{path: path, key: key, log: l, capacity: capacity, index: make(map[string]string)}
	if err := s.replay(); err != nil {
		_ = l.Close()
		return nil, err
	}
	return s, nil
}

// Check reports, without writing anything, whether the file at path is
// one Open would accept as a sealed match log: nil for an absent, empty
// or sealed file, persist.ErrPlaintextDocument (wrapped) for the
// JSON-lines log an earlier release wrote, another error otherwise. For
// a caller that will not open the log but has to say what is there --
// the Postgres backend, which never reads the file.
func Check(path string) error {
	_, err := sniff(path)
	return err
}

// sniff checks what is at path before anything opens it for writing,
// and reports its size (-1 when absent). Absent, empty, starting with
// Format's magic, or a header torn by a crash (Format.TornHeader, which
// OpenFrameLog starts again) is fine. A file that starts with '{' (after any
// whitespace) is the JSON-lines log an earlier release wrote, and is
// refused with persist.ErrPlaintextDocument; anything else is refused as
// not a match log. Neither is changed.
func sniff(path string) (int64, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return -1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("matchlog: opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("matchlog: stat %s: %w", path, err)
	}
	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return 0, fmt.Errorf("matchlog: reading %s: %w", path, err)
	}
	head = head[:n]
	switch {
	case n == 0, Format.HasMagic(head), Format.TornHeader(head):
		return fi.Size(), nil
	case bytes.HasPrefix(bytes.TrimLeft(head, " \t\r\n"), []byte("{")):
		return 0, fmt.Errorf("matchlog: store %q (%s): %w", storeName, path, persist.ErrPlaintextDocument)
	default:
		return 0, fmt.Errorf("matchlog: %s is not a match log", path)
	}
}

// replay rebuilds index from every "record" line already on disk.
//
// A frame that does not open, or opens but does not decode, is an error,
// not skipped: a torn tail is a length problem and OpenFrameLog has
// already cut it back, so what is left is either the wrong key or a file
// somebody has altered (rule 3 of the #853 amendment).
func (s *FileStore) replay() error {
	return s.scanLines(func(l *fileLine) {
		if l.Kind == kindRecord {
			s.index[l.Tuple.key(l.EntryID)] = l.ID
		}
		// kindUpdate needs no index change: it only ever touches a
		// tuple replay has already indexed from its "record" line,
		// which -- being append-only -- always precedes any update
		// referencing it.
	})
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
	rec := fileLine{
		Kind: kindRecord, ID: id, EntryID: entryID, Tuple: tuple, Event: event,
		FirstSeen: t, LastSeen: t, Provisional: provisional,
	}
	err := s.writeLineLocked(rec)
	if errors.Is(err, errLineTooLong) {
		// Cut the raw line and retry once (#1471).
		cut, was := store.ClampRaw(rec.Event.Raw)
		rec.Event.Raw = cut
		rec.Event.RawTruncated = rec.Event.RawTruncated || was
		err = s.writeLineLocked(rec)
	}
	if err != nil {
		return err
	}
	// Only added after a successful write: a failed write must not leave
	// index pointing at a record that isn't actually on disk.
	s.index[key] = id
	return nil
}

// writeLineLocked encodes l and appends it as one sealed frame, in a
// single Write call, then fsyncs. fsync's every write: matches are rare
// by this feature's own premise (#243 section 4), so the cost is
// affordable, and it is what actually backs "a match survives a restart"
// against an unclean shutdown, not just a clean one.
func (s *FileStore) writeLineLocked(l fileLine) error {
	b, err := json.Marshal(l)
	if err != nil {
		return fmt.Errorf("matchlog: encoding a record: %w", err)
	}
	if len(b) > maxLineBytes {
		return fmt.Errorf("matchlog: a record of %d bytes is over the %d-byte line limit: %w", len(b), maxLineBytes, errLineTooLong)
	}
	if err := s.log.Append(b); err != nil {
		return fmt.Errorf("matchlog: %w", err)
	}
	return nil
}

// Query implements Store.
//
// Opens its own read handle rather than taking s.mu across file I/O, so
// a slow query does not block Append -- correctness does not depend on
// serializing with writers, only on reading an append-only file, whose
// already-written bytes never change. The one accepted race: a frame
// still mid-write when Query's reader reaches it is short, and the walk
// stops there like any torn tail -- that match simply is not in this
// query's results and will be on the next one, which is an acceptable
// eventual-consistency window for a monitoring feature, not a
// transactional one. A whole frame that does not open is an error, not
// fewer results.
// It runs in two passes, and that shape is the point.
//
// A single pass has to keep a whole Record -- Tuple and the entire
// store.Event, Raw line included -- for every record matching q.Source,
// because Limit cannot be applied until the ordering is known, and the
// ordering is by LastSeen, which is not final until the last line has
// been read (a kindUpdate line arbitrarily far down the file moves it).
// So Limit: 1 still materialised everything: measured at 237 MiB of heap
// and 1.86s to return one record, against a 164 MiB log holding 20,000
// records for one MAC. GET /api/matches reaches this on the
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
//
// Both passes live in window below, shared with Recent (#586), which is
// the same two passes without a source to narrow on.
func (s *FileStore) Query(ctx context.Context, q Query, yield func(Record) bool) error {
	if q.Source.Empty() {
		return ErrEmptyIdentity
	}
	source := q.Source
	return s.window(ctx, &source, q.Since, q.Until, q.Limit, yield)
}

// Recent implements Store.
//
// The same two passes Query documents above -- both call window below
// -- with the source filter dropped -- there is no identity to narrow on, which is the entire
// point of this mode (see RecentQuery). Sharing the implementation
// rather than writing a second one is deliberate: the two-pass shape
// exists to stop Limit-sized results costing history-sized memory, and a
// second copy of that reasoning is a second place for it to be lost.
//
// What "bounded" means on this backend, precisely, since it is not the
// same bound Postgres gets. Pass one still keeps ordering metadata --
// an ID and three timestamps, tens of bytes -- for every record in the
// log, because LastSeen is not final until the last line has been read
// (a kindUpdate line arbitrarily far down moves it), so nothing can be
// pruned early without dropping a record that a later update would have
// promoted into the result. That peak is bounded by the store's own
// capacity, the operator-set ceiling this backend already refuses to
// grow past (ErrCapacityReached), and it is the same peak the existing
// per-identity Query already pays whenever one identity owns most of
// the log. What Limit bounds -- here as there -- is what is *retained*:
// full Records, each carrying a whole store.Event with its unclamped Raw
// line, are read in pass two for the chosen IDs alone. That is the
// allocation an unbounded all-entries read would have made arbitrarily
// large, and it is capped by clampLimit before pass two runs.
func (s *FileStore) Recent(ctx context.Context, q RecentQuery, yield func(Record) bool) error {
	return s.window(ctx, nil, q.Since, q.Until, q.Limit, yield)
}

// window is the body of both Query and Recent: source nil means every
// entry's matches, source non-nil means only those matching it. Nothing
// else differs between the two.
func (s *FileStore) window(ctx context.Context, source *Identity, since, until time.Time, rawLimit int, yield func(Record) bool) error {
	limit := clampLimit(rawLimit)

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
			if source != nil && !source.MatchesSource(l.Tuple.Source) {
				return
			}
			// FirstSeen is fixed when the record is created, so a record
			// entirely at or after the window can be discarded here
			// rather than carried to the end. LastSeen cannot be
			// filtered on yet -- a later update moves it forward, which
			// is exactly how a long-running match stays visible.
			if !until.IsZero() && !l.FirstSeen.Before(until) {
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
		if m.lastSeen.Before(since) {
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

// scanLines opens its own read handle and calls fn for each line, oldest
// first. A short tail ends the walk silently (a write in progress, or a
// crash tail); a whole frame that does not open, or does not decode, is
// an error.
//
// Its own handle rather than s.mu across file I/O, so a slow query does
// not block Append -- correctness does not depend on serialising with
// writers, only on reading an append-only file, whose already-written
// bytes never change. A read racing Reset can meet frames under the new
// salt, which surfaces as that error; Reset is a test hook.
func (s *FileStore) scanLines(fn func(*fileLine)) error {
	err := retention.ReadFrames(s.path, s.key, Format, func(seq uint64, plain []byte) error {
		var l fileLine
		if err := json.Unmarshal(plain, &l); err != nil {
			return fmt.Errorf("frame %d opened but is not a match log line: %w", seq, err)
		}
		fn(&l)
		return nil
	})
	if err != nil {
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

// Reset implements Store: empties the log and drops the open-record
// index, so the next Append starts a fresh record rather than collapsing
// into one a previous scenario left open. The file starts again with a
// fresh header and salt (see retention.FrameLog.Reset).
func (s *FileStore) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.log.Reset(); err != nil {
		return fmt.Errorf("matchlog: %w", err)
	}
	s.index = make(map[string]string)
	return nil
}

// Close implements Store.
func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.log.Close()
}
