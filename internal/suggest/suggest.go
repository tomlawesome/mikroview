// SPDX-License-Identifier: AGPL-3.0-only

// Package suggest is #243 slice 5: watchlist entries suggested from
// data RouterOS has already pushed (internal/routerstate), rather than
// an operator having to already know what to watch. Every suggestion is
// a Candidate the operator reviews and explicitly acts on -- nothing
// here ever creates a watchlist.Entry by itself.
//
// Three states, not a binary accept/reject (settled with the repo owner
// across #243's slice 5 design conversation, recorded on the issue):
//
//   - Off: undecided. The default for every newly generated candidate,
//     and the default view an operator sees.
//   - On: accepted -- a real watchlist.Entry now exists for it. See is
//     EntryID.
//   - Hide: explicitly declined, but reversible -- only ever undone by
//     an operator deliberately looking at the Hide view and flipping it
//     back. Never comes back on its own; there is deliberately no
//     "reappears after some time" behaviour, since that would make
//     Hide's meaning depend on how long ago it happened, not on what
//     the operator decided.
//
// Sync is the "stay in sync with the router" half of the design: it
// never removes a candidate and never changes an existing one's Status
// -- it only adds genuinely new candidates (always at Off) and, for an
// On candidate whose generating justification no longer appears,
// flips Stale so the operator gets a signal, not a silent change. See
// Sync's own doc comment.
//
// This package knows nothing about watchlist.Entry, matchlog, or how a
// candidate gets generated from routerstate -- those are internal/api's
// job (turning an accepted Candidate into a real Entry) and this
// package's sibling generator code (slice 5b) respectively. Kept
// separate the same way internal/matchlog doesn't know about
// internal/watchlist even though watchlist is its only real caller: a
// smaller, independently testable surface.
package suggest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var persistLog = logging.New("suggest")

// Kind identifies what a Candidate would become if accepted.
type Kind string

const (
	// KindDevice becomes an inverted watchlist.Entry scoped to Source --
	// "this device should only ever reach X".
	KindDevice Kind = "device"
	// KindPort becomes a non-inverted watchlist.Entry watching Ports,
	// generated from a firewall rule that already blocks them.
	KindPort Kind = "port"
	// KindAddressList becomes a non-inverted watchlist.Entry scoped to a
	// router's address list, generated from a firewall rule that already
	// scopes by one (#274 item 2).
	//
	// This was reserved and unimplemented for a long time, because the
	// matching engine held only a fixed stored identity and could not
	// answer "is this address currently in that list". It can now --
	// watchlist.AddressListRef plus AddressListMembership -- and the
	// live part is what makes the suggestion honest: RouterOS edits
	// these lists itself, so an entry expanded into today's members
	// would be stale the first time that happened.
	KindAddressList Kind = "addressList"
)

// Status is where a Candidate sits in the operator's review -- see this
// package's doc comment for what each value means and how it's reached.
type Status string

const (
	StatusOff  Status = "off"
	StatusOn   Status = "on"
	StatusHide Status = "hide"
)

// Candidate is one suggested watchlist entry, generated from data
// RouterOS has pushed, not yet or already acted on.
type Candidate struct {
	// ID is a stable, content-derived identity -- the same candidate
	// generated again on a later Sync must produce the same ID, so its
	// Status survives being regenerated. Built by the generator that
	// created it (slice 5b), not by this package -- see that code for
	// the per-Kind key shape.
	ID     string `json:"id"`
	Kind   Kind   `json:"kind"`
	Status Status `json:"status"`
	// Stale is set by Sync when an On candidate's generating
	// justification stops appearing in a later scan -- e.g. the rule
	// that blocked these ports was changed or removed. Never set for
	// Off/Hide (nothing has been acted on yet, so there's nothing to
	// warn about), and never cleared except by Sync seeing the
	// candidate's justification hold again. See Sync's doc comment.
	Stale bool `json:"stale,omitempty"`

	// Name is a short, human-readable label for display -- a device's
	// hostname, or a summary of which rule/ports this covers.
	Name string `json:"name"`
	// Justification is why this was suggested, for display and for
	// Sync's own "does this still hold" comparison -- e.g. "blocked by
	// rule D|rdp-test| on chain-1".
	Justification string `json:"justification"`
	// RouterDevice is which router (routerstate's own device key, the
	// ingest token's Device) this candidate was derived from.
	RouterDevice string `json:"routerDevice"`

	// Source is set for KindDevice -- the device this candidate would
	// watch if accepted.
	Source matchlog.Identity `json:"source,omitempty"`
	// Ports is set for KindPort -- the ports this candidate would watch
	// if accepted.
	Ports []int `json:"ports,omitempty"`
	// AddressList is set for KindAddressList -- see that Kind's doc
	// comment.
	AddressList string `json:"addressList,omitempty"`

	// EntryID is set once Status == StatusOn: the real watchlist.Entry.ID
	// this candidate became. Empty otherwise.
	EntryID string `json:"entryId,omitempty"`

	FirstSeen time.Time `json:"firstSeen"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ErrInvalidCandidate is returned by Sync for a candidate with no ID or
// Kind -- both are required for Sync to do its job (recognise it again
// next time, and know what accepting it would create).
var ErrInvalidCandidate = errors.New("suggest: a candidate must have an id and a kind")

// ErrNotOff is returned by Accept/Hide when the candidate is not
// currently Off -- both are operator actions taken from the default,
// undecided state; there is no direct Off-skipping path.
var ErrNotOff = errors.New("suggest: the candidate is not in the off state")

// ErrNotHidden is returned by Unhide when the candidate is not currently
// Hide.
var ErrNotHidden = errors.New("suggest: the candidate is not hidden")

// ErrNotFound is returned by Accept/Hide/Unhide for an unknown ID.
var ErrNotFound = errors.New("suggest: no candidate with that id")

const maxTextLen = 256

// validText mirrors internal/watchlist.validText (itself mirroring
// internal/entities.validateEntityText) -- same reasoning: this renders
// directly in the UI and lands in a persisted file an admin can read
// back.
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

// storeFile is the on-disk shape.
type storeFile struct {
	Candidates []*Candidate `json:"candidates"`
}

// maxCandidates bounds the store -- generated from routerstate data
// (devices, rules), not operator-typed, so this is a safety net against
// a pathological router pushing an enormous rule/device set, mirroring
// every other explicit-ceiling map in this codebase (routerstate's own
// maxDevices/maxRecordsPerKind).
var maxCandidates = 10000

// Store holds every suggestion candidate. The zero value is not usable;
// construct with Open or OpenWithBackend.
type Store struct {
	mu         sync.RWMutex
	backend    persist.Backend
	version    int64
	candidates map[string]*Candidate
}

// Open loads path if it exists (a missing file is the expected first-run
// case) and returns a Store that persists to it from then on. An empty
// path keeps everything in-memory only, the same optional-persistence
// contract every other small store in this codebase follows.
func Open(path string) (*Store, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend.
func OpenWithBackend(b persist.Backend) (*Store, error) {
	s := &Store{backend: b, candidates: make(map[string]*Candidate)}

	data, version, err := persist.LoadDocument(context.Background(), b)
	if err != nil {
		return s, err
	}
	if data == nil {
		return s, nil
	}
	s.version = version

	var file storeFile
	if err := json.Unmarshal(data, &file); err != nil {
		return s, err
	}
	for _, c := range file.Candidates {
		s.candidates[c.ID] = c
	}
	return s, nil
}

// List returns every candidate, sorted by ID for a stable, deterministic
// order -- a caller wanting a different order (by kind, by status) sorts
// the result itself, same convention internal/watchlist.Store.List uses.
func (s *Store) List() []Candidate {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Candidate, 0, len(s.candidates))
	for _, c := range s.candidates {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get returns the candidate with the given ID, or false if none exists.
func (s *Store) Get(id string) (Candidate, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.candidates[id]
	if !ok {
		return Candidate{}, false
	}
	return *c, true
}

// Sync merges a freshly generated batch of candidates into the store.
// This is the "stay in sync with the router" mechanism, run periodically
// in the background rather than exposed as a manual action (settled in
// #243's slice 5 design conversation: a separate "soft refresh" button
// would only ever do what this already does automatically).
//
// For each candidate in fresh:
//   - New ID: inserted at StatusOff, Stale false, FirstSeen/UpdatedAt now.
//   - Existing ID: Name/Justification/RouterDevice/Source/Ports/
//     AddressList are refreshed to the latest generated values (a rule's
//     comment can change without the rule itself going away), Stale is
//     cleared, UpdatedAt bumps -- but Status and EntryID are left
//     completely untouched. Accepting or hiding a candidate is an
//     operator decision this package never overwrites.
//
// Existing candidates NOT present in fresh are never removed and never
// have their Status changed here -- except an On candidate gets Stale
// set to true, since its generating justification (the rule, the
// device) no longer exists to produce it. An Off/Hide candidate that
// stops being generated is left exactly as it was: mild clutter is an
// acceptable cost for never silently discarding an operator's Hide
// decision, or a not-yet-decided suggestion, out from under them.
func (s *Store) Sync(fresh []Candidate) error {
	for _, c := range fresh {
		if c.ID == "" || c.Kind == "" {
			return ErrInvalidCandidate
		}
		if !validText(c.Name) || !validText(c.Justification) || !validText(c.RouterDevice) {
			return fmt.Errorf("suggest: candidate %q has invalid text", c.ID)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	freshIDs := make(map[string]bool, len(fresh))
	for _, c := range fresh {
		freshIDs[c.ID] = true
		existing, ok := s.candidates[c.ID]
		if !ok {
			if len(s.candidates) >= maxCandidates {
				// Refuse only the new candidate, not the whole sync --
				// existing candidates (including whatever an operator has
				// already accepted) must never be disturbed by a capacity
				// ceiling hit on unrelated new ones.
				continue
			}
			cp := c
			cp.Status = StatusOff
			cp.Stale = false
			cp.FirstSeen = now
			cp.UpdatedAt = now
			s.candidates[c.ID] = &cp
			continue
		}
		existing.Name = c.Name
		existing.Justification = c.Justification
		existing.RouterDevice = c.RouterDevice
		existing.Source = c.Source
		existing.Ports = c.Ports
		existing.AddressList = c.AddressList
		existing.Stale = false
		existing.UpdatedAt = now
	}

	for id, existing := range s.candidates {
		if freshIDs[id] {
			continue
		}
		if existing.Status == StatusOn {
			existing.Stale = true
			existing.UpdatedAt = now
		}
	}

	s.persistLocked()
	return nil
}

// Accept moves a candidate from Off to On, recording entryID -- the real
// watchlist.Entry.ID it became. The caller (internal/api) creates that
// Entry first; this only records that it happened. Returns ErrNotFound
// or ErrNotOff without mutating anything on failure.
func (s *Store) Accept(id, entryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.candidates[id]
	if !ok {
		return ErrNotFound
	}
	if c.Status != StatusOff {
		return ErrNotOff
	}
	c.Status = StatusOn
	c.EntryID = entryID
	c.UpdatedAt = time.Now()
	s.persistLocked()
	return nil
}

// Hide moves a candidate from Off to Hide.
func (s *Store) Hide(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.candidates[id]
	if !ok {
		return ErrNotFound
	}
	if c.Status != StatusOff {
		return ErrNotOff
	}
	c.Status = StatusHide
	c.UpdatedAt = time.Now()
	s.persistLocked()
	return nil
}

// Unhide moves a candidate from Hide back to Off -- the only way a
// hidden candidate is ever seen again, and only ever by an operator
// deliberately doing this from the Hide view. See this package's doc
// comment.
func (s *Store) Unhide(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.candidates[id]
	if !ok {
		return ErrNotFound
	}
	if c.Status != StatusHide {
		return ErrNotHidden
	}
	c.Status = StatusOff
	c.UpdatedAt = time.Now()
	s.persistLocked()
	return nil
}

// MarkHiddenByEntry finds the On candidate whose EntryID matches entryID
// and forces it to Hide, regardless of its current status. Called when
// the watchlist.Entry it became is deleted through the normal entry
// page, not this candidate table -- settled in #243's slice 5 design
// conversation: deleting an entry you explicitly created signals "I
// don't want this", not "reconsider me later", so it goes straight to
// Hide rather than back to Off. A no-op, not an error, if no candidate
// tracks that entry -- most entries are created directly, not from a
// suggestion, and that is the expected common case, not a fault.
func (s *Store) MarkHiddenByEntry(entryID string) {
	if entryID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.candidates {
		if c.EntryID == entryID {
			c.Status = StatusHide
			c.EntryID = ""
			c.UpdatedAt = time.Now()
			s.persistLocked()
			return
		}
	}
}

// Reset wipes every candidate -- the tracking half of the "nuke" action
// (#243 slice 5's deliberate, confirm-gated, fully destructive reset).
// The watchlist entries themselves are a separate store; the caller
// (internal/api) is responsible for wiping both together.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candidates = make(map[string]*Candidate)
	s.persistLocked()
}

func (s *Store) persistLocked() {
	if s.backend == nil {
		return
	}
	candidates := make([]*Candidate, 0, len(s.candidates))
	for _, c := range s.candidates {
		candidates = append(candidates, c)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })

	data, err := json.MarshalIndent(storeFile{Candidates: candidates}, "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding suggestion candidates for persistence failed: %v -- this change exists only in memory and will be lost on restart", err))
		return
	}
	version, conflicted, err := persist.SaveWithRetry(context.Background(), s.backend, data, s.version)
	if err != nil {
		persistLog.Error(fmt.Sprintf("writing suggestion candidates to %s failed: %v -- this change exists only in memory and will be lost on restart", s.backend.Describe(), err))
		return
	}
	if conflicted {
		persistLog.Warn(fmt.Sprintf("suggestion candidates were modified by another process while this change was pending (%s); this change was applied on top", s.backend.Describe()))
	}
	s.version = version
}
