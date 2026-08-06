// Package audit records who did what admin-privileged mutation in
// mikroview itself, when, and to what -- an accountability log for the
// humans operating mikroview, distinct from internal/flags (which is
// about behavior mikroview *observes on the network*, not actions taken
// *in* mikroview) and from internal/store (raw ingested firewall
// events). See issue #112.
//
// Persistence follows the exact convention already established by
// internal/flags.Store, internal/auth.Store, and internal/entities.Store:
// mutex-protected, optional atomic-write JSON persistence (an empty
// StorePath keeps everything in-memory only, same as every other optional
// store in this codebase -- see SECURITY.md).
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
)

var persistLog = logging.New("audit")

// Entry is one recorded admin mutation.
type Entry struct {
	// ID is a monotonically increasing sequence number, assigned in
	// Record's insertion order -- what Query's SinceID-less pagination
	// actually orders/dedupes against, since Timestamp alone isn't
	// guaranteed strictly increasing (two entries can share a wall-clock
	// millisecond).
	ID        uint64    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	// Actor is the acting username -- or a sentinel like "unauthenticated"
	// for the narrow callerIsAdminOrOpen zero-account bootstrap window,
	// where a mutation can happen before any account (and so any
	// username) exists. See internal/api's auditActor.
	Actor string `json:"actor"`
	// Action is a fixed short string identifying what happened, e.g.
	// "user.create", "entity.upsert", "token.revoke" -- deliberately a
	// plain string, not a closed Go enum, mirroring internal/entities.
	// Entity.Type's own "extensible, not gatekept by this package"
	// reasoning: callers (internal/api's handlers) own the vocabulary.
	Action string `json:"action"`
	// Target is what was acted on -- a username, an entity's "type:key",
	// a token name, a detector name, a flag ID -- whatever identifies the
	// specific thing this action applies to.
	Target string `json:"target"`
	// Detail is optional free-text context beyond Target, e.g. a created
	// user's role, or a detector's new enabled state.
	Detail string `json:"detail,omitempty"`
}

// maxEntries bounds the store the same way flags.Store's maxFlags does --
// a generous safety net for a log that grows only from rare, admin-
// driven, interactive actions (not a high-rate detection hot path), not a
// limit expected to be hit in normal use. A var rather than a const so
// tests can shrink it without creating 10000+ entries.
var maxEntries = 10000

// storeFile is the on-disk shape -- an object carrying both the entry
// list and the next sequence number to hand out, so ID allocation stays
// monotonic across a restart (never reused, even after pruneLocked has
// evicted the oldest entries) the same way internal/store's Event.ID
// keeps incrementing past what the ring buffer currently retains.
type storeFile struct {
	NextID  uint64  `json:"nextId"`
	Entries []Entry `json:"entries"`
}

// Store holds every known audit entry, oldest first, up to maxEntries.
// The zero value is not usable; construct with Open.
type Store struct {
	mu      sync.RWMutex
	path    string
	entries []Entry
	nextID  uint64
}

// Open loads path if it exists (a missing file is the expected first-run
// case, not an error) and returns a Store that persists to it from then
// on. An empty path is the expected "persistence not configured" case: a
// fully usable, in-memory-only Store is returned. A malformed file is
// treated as empty rather than failing -- a corrupted audit file should
// never block mikroview from starting, same as every other optional
// store in this codebase. Either way the returned Store is always safe
// to use unconditionally; a non-nil error is only ever informational,
// for the caller to log.
func Open(path string) (*Store, error) {
	s := &Store{path: path, nextID: 1}
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

	var file storeFile
	if err := json.Unmarshal(data, &file); err != nil {
		return s, err
	}
	s.entries = make([]Entry, 0, len(file.Entries))
	for _, e := range file.Entries {
		s.entries = append(s.entries, e)
		if e.ID >= s.nextID {
			s.nextID = e.ID + 1
		}
	}
	// A persisted NextID higher than any entry actually carries (e.g.
	// every entry at or above it was pruned in a previous run) must still
	// win, or a restart could hand out an ID that collides with one that
	// existed before pruning.
	if file.NextID > s.nextID {
		s.nextID = file.NextID
	}
	return s, nil
}

// Record appends a new entry for (actor, action, target, detail) at the
// current time and persists immediately -- audit entries are rare,
// admin-driven, interactive actions, not a high-rate hot path, so there's
// no debounce here, the same reasoning entities.Store's persistLocked
// doc comment gives for skipping flags.Store's rate limit.
func (s *Store) Record(actor, action, target, detail string) Entry {
	return s.record(actor, action, target, detail, time.Now())
}

func (s *Store) record(actor, action, target, detail string, now time.Time) Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	e := Entry{ID: s.nextID, Timestamp: now, Actor: actor, Action: action, Target: target, Detail: detail}
	s.nextID++
	s.entries = append(s.entries, e)
	s.pruneLocked()
	s.persistLocked()
	return e
}

// pruneLocked evicts the oldest entries once the store is over
// maxEntries -- unlike flags.Store's pruneLocked (which only ever evicts
// *cleared* flags, never active ones), every audit entry is equally
// eligible: there's no "still needs attention" distinction here, just a
// chronological log, so the oldest simply age out.
func (s *Store) pruneLocked() {
	over := len(s.entries) - maxEntries
	if over <= 0 {
		return
	}
	s.entries = s.entries[over:]
}

// persistLocked writes the current state to disk if persistence is
// configured. Write failures are swallowed rather than surfaced to
// Record's caller: the in-memory state (which every read goes through)
// stays correct either way, so a transient disk issue degrades to "won't
// survive a restart right now" rather than breaking the mutation that
// triggered this call.
func (s *Store) persistLocked() {
	if s.path == "" {
		return
	}
	data, err := json.MarshalIndent(storeFile{NextID: s.nextID, Entries: s.entries}, "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding %s for persistence failed: %v -- this change exists only in memory and will be lost on restart", s.path, err))
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		persistLog.Error(fmt.Sprintf("writing %s failed: %v -- this change exists only in memory and will be lost on restart", tmp, err))
		return
	}
	// Same filesystem, so the rename itself is atomic -- but it can
	// still fail (read-only remount, permissions change), and a
	// silent failure here means the caller believes a write landed
	// when it did not.
	if err := os.Rename(tmp, s.path); err != nil {
		persistLog.Error(fmt.Sprintf("replacing %s failed: %v -- this change exists only in memory and will be lost on restart", s.path, err))
		return
	}
}
