// SPDX-License-Identifier: AGPL-3.0-only

// Package droplist is issue #1223, stage 1 of the design ratified on
// #461: an admin-authored list of ranges to block, one Entry at a time,
// each typed in directly or raised from a flag's drawer -- never
// written by a detector (settled on #461: "the reviewed by a human
// property holds by construction, not by policy"). The fetched
// threat-intel feeds (#113 Part B: automatic, unattended, no human in
// the loop) are a separate, unrelated feature living in
// internal/blocklist.
//
// This stage is the store and its validation only: no API route, no UI,
// and nothing pushed to RouterOS. See Validate (validate.go) for the
// rules an Entry's CIDR must satisfy, and #1224/#1225 for what is
// deliberately not here yet.
//
// Persistence follows the exact convention internal/suggest.Store and
// internal/audit.Store already use: mutex-protected, optional atomic-
// write JSON persistence via internal/persist. Auditing lives here
// rather than in whatever caller eventually exposes this over HTTP,
// deliberately: this is an enforcement list, so no write may ever reach
// disk without also reaching the audit log -- moving that guarantee out
// to each caller would mean trusting every future caller to remember it.
package droplist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var entryPersistLog = logging.New("droplist")

// Entry is one operator-authored droplist entry.
type Entry struct {
	// CIDR is always canonical: Masked(), IPv4, /24-/32 -- see Validate.
	CIDR    netip.Prefix `json:"cidr"`
	AddedBy string       `json:"addedBy"`
	AddedAt time.Time    `json:"addedAt"`
	Reason  string       `json:"reason"`
	// FlagID is set when this entry was raised from a flag's drawer
	// rather than typed in from scratch -- flags.Flag.ID, empty for an
	// entry an admin created directly.
	FlagID string `json:"flagID,omitempty"`
}

// ErrExists is returned by Add for a CIDR (after canonicalisation) that
// already has an entry.
var ErrExists = errors.New("droplist: an entry for that range already exists")

// ErrNotFound is returned by Remove for a CIDR with no matching entry.
var ErrNotFound = errors.New("droplist: no entry for that range")

const maxTextLen = 256

// validText mirrors internal/suggest.validText (itself mirroring
// internal/watchlist.validText and internal/entities.validateEntityText)
// -- same reasoning: Reason and AddedBy both render directly in the UI
// (a later stage) and land in a persisted file an admin can read back.
func validText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	if utf8.RuneCountInString(s) > maxTextLen {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

// Auditor is the one method of internal/audit.Store this package needs
// -- an interface rather than the concrete type so tests can fake it
// (or, for the CLI, decline to set one at all) without pulling in a real
// audit log. *audit.Store satisfies this directly.
type Auditor interface {
	Record(actor, action, target, detail string) audit.Entry
}

// OwnRanges is the one method of internal/routerstate.Store this package
// needs -- an interface, not the concrete type, so droplist stays
// independent of routerstate's ingest schema and tests can supply a
// fixed list without pushing a payload through a real Store. See
// routerstate.Store.OwnPrefixes.
type OwnRanges interface {
	OwnPrefixes() []netip.Prefix
}

// storeFile is the on-disk shape.
type storeFile struct {
	Entries []*Entry `json:"entries"`
}

// Store holds every droplist entry. The zero value is not usable;
// construct with Open or OpenWithBackend.
type Store struct {
	mu      sync.RWMutex
	backend persist.Backend
	version int64
	// entries is keyed by the entry's canonical CIDR string (Entry.CIDR.
	// String(), already Masked() -- see Validate), which is also the
	// natural uniqueness key: two entries for the same range make no
	// sense, and Add refuses the second with ErrExists.
	entries map[string]*Entry

	auditor Auditor
	own     OwnRanges

	// now is time.Now, overridden by tests -- same injectable-clock
	// convention as internal/audit's own tests use.
	now func() time.Time
}

// Open loads path if it exists (a missing file is the expected first-run
// case) and returns a Store that persists to it from then on. An empty
// path keeps everything in-memory only, the same optional-persistence
// contract every other small store in this codebase follows. A document
// that exists but cannot be read or parsed is a hard error (issue #378):
// the caller gets (nil, err) rather than a store whose live backend
// would overwrite that document on the first write. See persist.Open.
func Open(path string) (*Store, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend.
func OpenWithBackend(b persist.Backend) (*Store, error) {
	s := &Store{backend: b, entries: make(map[string]*Entry), now: time.Now}

	version, existed, err := persist.Open(context.Background(), b, "the droplist entry store", func(data []byte) error {
		var file storeFile
		if err := json.Unmarshal(data, &file); err != nil {
			return err
		}
		for _, e := range file.Entries {
			// A JSON array containing `null` is syntactically valid and
			// unmarshals into a nil *Entry -- skipped here so a
			// malformed document can't crash startup by indexing
			// through a nil pointer, same guard internal/suggest.Open
			// applies to its own candidates array.
			if e == nil {
				continue
			}
			s.entries[e.CIDR.String()] = e
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if existed {
		s.version = version
	}
	return s, nil
}

// SetAuditor wires the audit log every Add/Remove writes to. A nil
// auditor (the zero value, and what tests and the CLI get by leaving
// this unset) records nothing -- Add/Remove still work, they simply
// leave no accountability trail, which matters for a short-lived CLI
// invocation but never for the running server (main.go always sets a
// real one).
func (s *Store) SetAuditor(a Auditor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auditor = a
}

// SetOwnRanges wires the source of "what the router already considers
// its own" that Add's Validate call checks against. Nil (the default)
// means no such check ever fires -- the same "nothing pushed yet"
// degrade every other routerstate-dependent feature in this codebase
// uses, not an error.
func (s *Store) SetOwnRanges(r OwnRanges) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.own = r
}

// List returns every entry, sorted by CIDR -- by address and then by
// prefix length (netip.Prefix.Compare), a true numeric order rather
// than the lexical order sorting the string form would give.
func (s *Store) List() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CIDR.Compare(out[j].CIDR) < 0 })
	return out
}

// Add validates cidr (against the router's own ranges, if SetOwnRanges
// configured one), refuses a duplicate of an existing entry with
// ErrExists, persists the new entry, and records it to the audit log
// (action "droplist.add", target the canonical CIDR, detail reason plus
// " (from flag <id>)" when flagID is given) -- in that order, so nothing
// is ever audited that did not actually get stored, and nothing is ever
// stored that was not validated.
func (s *Store) Add(actor, cidr, reason, flagID string) (Entry, error) {
	if !validText(actor) || !validText(reason) || !validText(flagID) {
		return Entry{}, fmt.Errorf("droplist: addedBy, reason and flagID must be plain, bounded text")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var own []netip.Prefix
	if s.own != nil {
		own = s.own.OwnPrefixes()
	}
	p, err := Validate(cidr, own)
	if err != nil {
		return Entry{}, err
	}

	key := p.String()
	if _, exists := s.entries[key]; exists {
		return Entry{}, ErrExists
	}

	e := Entry{CIDR: p, AddedBy: actor, AddedAt: s.now(), Reason: reason, FlagID: flagID}
	cp := e
	s.entries[key] = &cp
	s.persistLocked()

	if s.auditor != nil {
		s.auditor.Record(actor, "droplist.add", key, auditDetail(reason, flagID))
	}
	return e, nil
}

// Remove deletes the entry for cidr, refusing an unknown range with
// ErrNotFound (including one that fails to parse at all -- it is just
// as absent from the store as any other unrecognised key), persists,
// then records the removal to the audit log (action "droplist.remove",
// same target/detail shape as Add).
func (s *Store) Remove(actor, cidr string) error {
	p, err := parseCIDR(cidr)
	if err != nil {
		return ErrNotFound
	}
	key := p.Masked().String()

	s.mu.Lock()
	defer s.mu.Unlock()

	e, exists := s.entries[key]
	if !exists {
		return ErrNotFound
	}
	reason, flagID := e.Reason, e.FlagID
	delete(s.entries, key)
	s.persistLocked()

	if s.auditor != nil {
		s.auditor.Record(actor, "droplist.remove", key, auditDetail(reason, flagID))
	}
	return nil
}

// auditDetail is the detail string Add/Remove record: the entry's
// reason, plus a trailing note of which flag raised it, when one did.
func auditDetail(reason, flagID string) string {
	if flagID == "" {
		return reason
	}
	return fmt.Sprintf("%s (from flag %s)", reason, flagID)
}

// persistLocked writes the current state to disk if persistence is
// configured. Write failures are swallowed rather than surfaced to the
// caller: the in-memory state (which every read goes through) stays
// correct either way, so a transient disk issue degrades to "won't
// survive a restart right now" rather than losing the mutation that
// triggered this call -- same contract as every other store's
// persistLocked in this codebase.
func (s *Store) persistLocked() {
	if s.backend == nil {
		return
	}
	entries := make([]*Entry, 0, len(s.entries))
	for _, e := range s.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].CIDR.Compare(entries[j].CIDR) < 0 })

	data, err := json.MarshalIndent(storeFile{Entries: entries}, "", "  ")
	if err != nil {
		entryPersistLog.Error(fmt.Sprintf("encoding droplist entries for persistence failed: %v -- this change exists only in memory and will be lost on restart", err))
		return
	}
	version, conflicted, err := persist.SaveWithRetry(context.Background(), s.backend, data, s.version)
	if err != nil {
		entryPersistLog.Error(fmt.Sprintf("writing droplist entries to %s failed: %v -- this change exists only in memory and will be lost on restart", s.backend.Describe(), err))
		return
	}
	if conflicted {
		entryPersistLog.Warn(fmt.Sprintf("droplist entries were modified by another process while this change was pending (%s); this change was applied on top", s.backend.Describe()))
	}
	s.version = version
}
