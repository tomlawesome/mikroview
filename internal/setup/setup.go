// SPDX-License-Identifier: AGPL-3.0-only

// Package setup records what mikroview has observed of a router's
// setup, so the guided wizard (issue #320) can tell an operator that a
// step worked instead of asking them to go and look.
//
// The whole point is that mikroview never connects to a router: it
// cannot poll one to check. But every step of the setup ends with the
// router arriving *here* -- fetching the CA, opening the syslog
// connection, sending a first event, pushing its tables -- so each one
// is observable from this side without dialling anything.
//
// Most of that is already recorded elsewhere and is read from there
// rather than duplicated: internal/device.Registry knows when a source
// first sent an event and how many it has sent, and
// internal/routerstate knows when each pushed table last arrived. This
// package holds only what nothing else does -- the CA fetch, the syslog
// connection itself (which happens before any event, and is what
// separates "the router cannot reach me" from "no rule is logging
// yet"), and how many events carried a decoded action, which is how the
// log-prefix step is checked.
//
// It also holds the wizard's ledger *marks* (#487) -- the operator's own
// decisions about steps that produced no evidence -- and those, unlike
// the observations above, are persisted. See "The ledger's own marks"
// at the foot of this file for why the two halves are treated
// differently.
//
// A third kind, *witnesses* (#1221), sits beside the marks: the server's
// own record that a step's evidence arrived at least once, kept so a
// restart that empties the memory-backed maps above does not read the
// step back as if it never happened. See "Witnesses" further down.
package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var persistLog = logging.New("setup")

// maxSources bounds every map here. The keys are source addresses, so
// they are chosen by whoever connects -- the syslog port takes no
// credentials, and /ca.crt is deliberately public. Same attacker-keyed
// growth internal/device.Registry and internal/rules bound, and bounded
// the same way: evict the least-recently-seen, never the whole map, so
// a flood cannot erase the record of the router the operator is
// actually setting up in one go.
const maxSources = 256

// Store is safe for concurrent use. Every Note* method is called from a
// hot path (the ingest goroutine, the accept loop, an HTTP handler), so
// they take the write lock only briefly and never allocate on the
// common path.
type Store struct {
	mu         sync.RWMutex
	caFetched  map[string]time.Time
	syslogSeen map[string]syslogObservation
	prefixes   map[string]prefixHealth
	// marks is the operator's own decisions about steps that produced no
	// evidence -- see "The ledger's own marks" at the foot of this file.
	// Keyed by step number; lazily created, since most instances never
	// record one.
	marks map[int]Mark
	// witnessed is the server's own record of a step it watched turn
	// satisfied, kept apart from marks -- see NoteWitnessed. A step has
	// exactly one *operator* outcome at a time (marks' own rule), but a
	// witness is not one: it must survive an operator later skipping or
	// forcing the same step, and must never itself displace what the
	// operator decided. Keyed by step number; lazily created.
	witnessed map[int]Mark
	// reports is the last logging page each device pushed (#1241) --
	// what the wizard left on that router, and which wizard wrote it.
	// Keyed by device id, persisted beside the marks; see
	// routersetup.go for why this one is written down when the
	// observations above are not.
	reports map[string]LoggingReport
	// address is the operator's own answer to "what address can your
	// router reach mikroview on?" (#1213) -- see SetAddress. Persisted
	// beside the marks, for the same reason: a restart mid-wizard must
	// not lose it, and every RouterOS command the wizard renders is
	// written against this value once it is set, never against the
	// browser's own host.
	address string
	// backend is where the marks are persisted, or nil when persistence
	// is switched off. Only the marks go through it -- the observations
	// above are re-made every run by definition.
	backend persist.Backend
	// version is the backend's token for the document as of the last
	// load or save -- see persist.SaveWithRetry.
	version int64
}

type syslogObservation struct {
	First time.Time
	Last  time.Time
}

// prefixHealth counts how many events from a device carried an action
// mikroview could decode from the rule's log-prefix. A router whose
// rules log without the prefix convention shows up as events arriving
// with none decoded -- which looks identical to "working" on every
// other measure, and is the single most common half-finished setup.
type prefixHealth struct {
	Events  uint64
	Decoded uint64
	Last    time.Time
}

// New returns an in-memory-only Store: observations are made afresh
// every run anyway, and marks recorded against it simply do not survive
// a restart. Use Open/OpenWithBackend where the marks must.
func New() *Store {
	return &Store{
		caFetched:  make(map[string]time.Time),
		syslogSeen: make(map[string]syslogObservation),
		prefixes:   make(map[string]prefixHealth),
		reports:    make(map[string]LoggingReport),
	}
}

// Open loads path if it exists (a missing file is the expected first-run
// case, not an error) and returns a Store that persists its marks there
// from then on. An empty path is the expected "persistence not
// configured" case: a fully usable, in-memory-only Store is returned. A
// document that exists but cannot be read or parsed is a hard error
// (issue #378): the caller gets (nil, err) rather than a store whose
// live backend would overwrite that document on the first write. See
// persist.Open. The same contract every sibling store here follows.
func Open(path string) (*Store, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend -- a JSON file by
// default, or Postgres when configured (issue #131).
func OpenWithBackend(b persist.Backend) (*Store, error) {
	s := New()
	s.backend = b
	if b == nil {
		return s, nil
	}

	version, existed, err := persist.Open(context.Background(), b, "the setup ledger", func(data []byte) error {
		var file storeFile
		if err := json.Unmarshal(data, &file); err != nil {
			return err
		}
		s.marks = make(map[int]Mark, len(file.Marks))
		s.witnessed = make(map[int]Mark, len(file.Marks))
		for _, m := range file.Marks {
			// Filtered on the way in, not trusted: a document written by
			// an older build, or edited by hand, must not put a step 9
			// into a ledger that has five steps.
			if m.Step < 1 || m.Step > maxStep {
				continue
			}
			// Routed by outcome into the map that outcome belongs to --
			// this is what keeps a witness from landing in marks[step]
			// (and so from being read as, or overwritten by, an operator
			// decision) after a round trip through disk.
			switch m.Outcome {
			case MarkSkipped, MarkForced:
				s.marks[m.Step] = m
			case MarkWitnessed:
				s.witnessed[m.Step] = m
			}
		}
		s.address = file.Address
		s.reports = make(map[string]LoggingReport, len(file.Reports))
		for _, r := range file.Reports {
			// Filtered on the way in like the marks above: a document
			// written by another build, or edited by hand, must not put a
			// deviceless report into the map the device API reads.
			if r.Device == "" {
				continue
			}
			s.reports[r.Device] = r
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

// NoteCAFetch records that source downloaded /ca.crt. This is step 1 of
// the setup landing: the router has reached mikroview over the network
// and taken its certificate authority.
func (s *Store) NoteCAFetch(source string, now time.Time) {
	if source == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	evictOldest(s.caFetched, maxSources, func(t time.Time) time.Time { return t })
	s.caFetched[source] = now
}

// NoteSyslogConnection records that source opened a syslog TLS
// connection. Distinct from an event arriving: a connected router with
// no logging rules produces this and nothing else, which is exactly the
// state the wizard needs to name.
func (s *Store) NoteSyslogConnection(source string, now time.Time) {
	if source == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.syslogSeen[source]; ok {
		prev.Last = now
		s.syslogSeen[source] = prev
		return
	}
	evictOldest(s.syslogSeen, maxSources, func(o syslogObservation) time.Time { return o.Last })
	s.syslogSeen[source] = syslogObservation{First: now, Last: now}
}

// NoteEvent records one ingested event and whether its action was
// decoded from a log-prefix. Keyed by device id, not source address, so
// it lines up with what the rest of the UI calls a device.
func (s *Store) NoteEvent(device string, actionDecoded bool, now time.Time) {
	if device == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.prefixes[device]
	if !ok {
		evictOldest(s.prefixes, maxSources, func(p prefixHealth) time.Time { return p.Last })
	}
	h.Events++
	if actionDecoded {
		h.Decoded++
	}
	h.Last = now
	s.prefixes[device] = h
}

// SourceObservation is what has been seen from one source address.
type SourceObservation struct {
	Source            string     `json:"source"`
	CAFetchedAt       *time.Time `json:"caFetchedAt,omitempty"`
	SyslogFirstSeenAt *time.Time `json:"syslogFirstSeenAt,omitempty"`
	SyslogLastSeenAt  *time.Time `json:"syslogLastSeenAt,omitempty"`
}

// DeviceObservation is the log-prefix health of one device.
type DeviceObservation struct {
	Device string `json:"device"`
	Events uint64 `json:"events"`
	// Decoded is how many of those carried an action decoded from a
	// log-prefix. Decoded == 0 with Events > 0 means the rules log but
	// do not follow the <A|D|R|L>|slug| convention.
	Decoded uint64 `json:"decoded"`
}

// Snapshot returns everything observed, for the wizard's status API.
func (s *Store) Snapshot() ([]SourceObservation, []DeviceObservation) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bySource := make(map[string]*SourceObservation)
	at := func(src string) *SourceObservation {
		o, ok := bySource[src]
		if !ok {
			o = &SourceObservation{Source: src}
			bySource[src] = o
		}
		return o
	}
	for src, t := range s.caFetched {
		when := t
		at(src).CAFetchedAt = &when
	}
	for src, obs := range s.syslogSeen {
		first, last := obs.First, obs.Last
		o := at(src)
		o.SyslogFirstSeenAt = &first
		o.SyslogLastSeenAt = &last
	}

	sources := make([]SourceObservation, 0, len(bySource))
	for _, o := range bySource {
		sources = append(sources, *o)
	}
	devices := make([]DeviceObservation, 0, len(s.prefixes))
	for d, h := range s.prefixes {
		devices = append(devices, DeviceObservation{Device: d, Events: h.Events, Decoded: h.Decoded})
	}
	return sources, devices
}

// evictOldest drops the least-recently-seen entry when m is at cap, so
// adding one more keeps the map bounded. One at a time rather than a
// batch: these maps are small and this runs at most once per new source.
func evictOldest[V any](m map[string]V, cap int, stamp func(V) time.Time) {
	if len(m) < cap {
		return
	}
	var oldestKey string
	var oldest time.Time
	for k, v := range m {
		t := stamp(v)
		if oldestKey == "" || t.Before(oldest) {
			oldestKey, oldest = k, t
		}
	}
	delete(m, oldestKey)
}

// --- The ledger's own marks ---------------------------------------------
//
// Everything above is evidence: something arrived here and was noted.
// A mark is the other half of the wizard's claim ledger (#487) -- the
// operator's own statement about a step that has *not* produced
// evidence: "skipped" (quiet, moves on) or "forced" (gone past with a
// heavy warning, and recorded).
//
// Marks live here, beside the evidence they qualify -- but unlike that
// evidence they are persisted, through the same optional atomic-write
// backend every sibling store uses (internal/audit, internal/flags,
// internal/auth, internal/entities).
//
// The two halves are treated differently because they are different
// kinds of thing. An observation is a fact about this process: what
// arrived here, when. A mark is a decision the operator made, and the
// design record makes it the feature -- "the forced-past line surfaces
// in the step list, the audit log, and every empty state whose silence
// it explains". A mark that did not survive a restart would take two of
// those three surfaces with it, leaving the step list and the Stream's
// empty state back at unexplained silence. A restart is most likely at
// upgrade, which is exactly when an operator is looking for the
// explanation.
//
// The record's "stateless beyond the evidence" is about the wizard's
// *progress* -- "finished" is not stored, closing never loses your
// place, reopening always rebuilds the ledger from what the server has
// observed. It is not about the record. A forced-past mark is the
// record, not progress.
//
// The audit entry stays alongside, and the two are not redundant: the
// audit line is history (it remains after evidence arrives and the step
// turns green), the mark is current state. Deriving one from the other
// would not work in either direction -- internal/audit prunes to
// maxEntries, so marks read back out of the log would silently vanish
// once enough entries accumulated.

// MarkOutcome is what the operator did with a step whose evidence had
// not arrived.
type MarkOutcome string

const (
	// MarkSkipped: moved past deliberately, no ceremony. The step's
	// consequence is stated in the ledger, not treated as a fault.
	MarkSkipped MarkOutcome = "skipped"
	// MarkForced: moved past after the heavy warning, with the exact
	// record quoted on the button that wrote it.
	MarkForced MarkOutcome = "forced"
	// MarkWitnessed: the server's own record that a step's evidence
	// arrived, written from handleSetupStatus the moment it computes the
	// step as satisfied -- never from a client. See NoteWitnessed. It is
	// not a client-writable outcome: NoteMark keeps refusing it, exactly
	// as it refuses anything else that is not skipped or forced, because
	// a caller that could write this could claim a step happened when it
	// did not.
	MarkWitnessed MarkOutcome = "witnessed"
)

// maxStep bounds the step numbers a mark may carry. Five steps, per the
// ratified design; a mark outside that range is a client bug or a probe,
// and either way has nothing to describe.
const maxStep = 5

// Mark is one recorded decision about one step.
type Mark struct {
	Step    int         `json:"step"`
	Outcome MarkOutcome `json:"outcome"`
	// Actor is the acting username, resolved server-side from the
	// session -- never taken from the request body, so the ledger cannot
	// be made to name somebody else. Empty on a MarkWitnessed entry:
	// nobody acted, mikroview simply watched something arrive.
	Actor string    `json:"actor"`
	At    time.Time `json:"at"`
	// Note is what was not observed at the moment a skipped or forced
	// mark was made ("no router has fetched /ca.crt"), which is what
	// makes a forced-past line explain a silence rather than merely
	// report a click. On a MarkWitnessed entry it carries the opposite:
	// the receipt of what *was* observed, which is the whole point of
	// witnessing it.
	Note string `json:"note,omitempty"`
}

// maxNote caps the free text a client may attach. Long enough for the
// observation lines the wizard actually sends, short enough that the
// field cannot be used as storage.
const maxNote = 200

// storeFile is the on-disk shape. Marks and witnesses, in one list
// distinguished by Outcome, and nothing else: the observations in the
// rest of this file are re-made from arriving traffic every run, and
// writing one down as itself (rather than as a witness that it once
// happened) would turn "a router fetched the CA at 14:02" into a claim
// about a router nobody is still watching -- which is exactly the
// difference between an observation and a witness (see NoteWitnessed).
type storeFile struct {
	Marks []Mark `json:"marks"`
	// Address is the operator's stored answer (#1213) -- see
	// Store.address's own comment. Omitted when empty, the "not answered
	// yet" case, so an old document without this field round-trips
	// unchanged.
	Address string `json:"address,omitempty"`
	// Reports is #1241's router setup reports, one per device that has
	// sent one. Omitted when empty so a ledger written before this
	// existed round-trips unchanged.
	Reports []LoggingReport `json:"reports,omitempty"`
}

// NoteMark records one step decision, replacing any previous mark for
// the same step: a step has exactly one outcome at a time, and changing
// one's mind is not a second claim. Reports whether the mark was
// accepted, so a caller can refuse to write an audit entry for input it
// rejected.
//
// Persists immediately, with no debounce -- a step decision is a rare,
// operator-driven, interactive action, not a hot path, the same
// reasoning audit.Store.Record gives.
func (s *Store) NoteMark(step int, outcome MarkOutcome, actor, note string, now time.Time) (Mark, bool) {
	if step < 1 || step > maxStep {
		return Mark{}, false
	}
	if outcome != MarkSkipped && outcome != MarkForced {
		return Mark{}, false
	}
	if len(note) > maxNote {
		note = note[:maxNote]
	}
	m := Mark{Step: step, Outcome: outcome, Actor: actor, At: now, Note: note}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.marks == nil {
		s.marks = make(map[int]Mark, maxStep)
	}
	s.marks[step] = m
	s.persistLocked()
	return m, true
}

// marksLocked is Marks without taking the lock, for callers that already
// hold it.
func (s *Store) marksLocked() []Mark {
	out := make([]Mark, 0, len(s.marks))
	for step := 1; step <= maxStep; step++ {
		if m, ok := s.marks[step]; ok {
			out = append(out, m)
		}
	}
	return out
}

// persistLocked writes the marks to the backend if persistence is
// configured.
//
// Write failures are logged loudly and swallowed rather than surfaced to
// NoteMark's caller, matching every sibling store: the in-memory state
// (which every read goes through) stays correct either way, so a
// transient disk problem degrades to "this will not survive a restart
// right now" rather than failing the decision the operator just made --
// which would be worse, since the audit entry for it is written either
// way.
func (s *Store) persistLocked() {
	if s.backend == nil {
		return
	}
	data, err := json.MarshalIndent(storeFile{
		Marks:   append(s.marksLocked(), s.witnessedLocked()...),
		Address: s.address,
		Reports: s.reportsLocked(),
	}, "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding the setup ledger for persistence failed: %v -- this decision exists only in memory and will be lost on restart", err))
		return
	}
	version, conflicted, err := persist.SaveWithRetry(context.Background(), s.backend, data, s.version)
	if err != nil {
		persistLog.Error(fmt.Sprintf("writing the setup ledger to %s failed: %v -- this decision exists only in memory and will be lost on restart", s.backend.Describe(), err))
		return
	}
	if conflicted {
		persistLog.Warn(fmt.Sprintf("the setup ledger was modified by another process while this decision was pending (%s); this decision was applied on top", s.backend.Describe()))
	}
	s.version = version
}

// Marks returns every recorded decision, ordered by step so the ledger
// renders in the same order it is read.
func (s *Store) Marks() []Mark {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.marksLocked()
}

// --- Witnesses (#1221) --------------------------------------------------
//
// Everything above this point, and the marks just above it, predate the
// gap issue #1221 names: a step whose evidence lives only in the maps at
// the top of this file (caFetched, syslogSeen, prefixes) or in a sibling
// store as memory-backed as this one (device event counts, pushed-table
// times) reads back to "waiting" on every restart, even though it
// genuinely happened -- mikroview simply has nowhere it wrote that down.
//
// A witness is the server's own answer to that, and deliberately not a
// persisted copy of the observation itself: the package comment above
// explains why an observation is never written down as such (it would
// turn "a router fetched the CA at 14:02" into a claim about a router
// nobody is still watching). A witness instead records that the step
// was *seen* satisfied, with the receipt it showed at that moment --
// close enough to the observation to explain itself, but never confused
// for a live reading. handleSetupStatus writes one the first time it
// computes a step as satisfied from live evidence; nothing else may.

// NoteWitnessed records that step was seen satisfied by live evidence,
// carrying the receipt it showed at that moment. Server-only -- there is
// no request path that reaches this, which is what makes the mark
// trustworthy as a record of an actual observation rather than a claim a
// client could fabricate (contrast NoteMark, which a client does reach,
// and which keeps refusing this very outcome for that reason).
//
// Writes once per step and is a no-op after that (reports the existing
// mark, still ok): the receipt from the first time a step was seen done
// describes the same fact a later one would, so there is nothing to gain
// from overwriting it, and every status poll would otherwise be a
// rewrite of the ledger file for a step that has nothing new to say.
//
// Kept in witnessed, a map of its own, rather than in marks[step]:
// NoteMark's doc is that a step has exactly one *operator* outcome at a
// time, and a witness is not one. It must not overwrite a skip or force
// already recorded there -- the operator's decision stands -- and it
// must not be erased by one recorded afterwards either, so a later skip
// of a step mikroview once watched happen does not un-remember that it
// did. Reading the two back together (which wins when both exist) is a
// rendering choice for the caller, not this store's to make.
func (s *Store) NoteWitnessed(step int, receipt string, now time.Time) (Mark, bool) {
	if step < 1 || step > maxStep {
		return Mark{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.witnessed[step]; ok {
		return m, true
	}
	if len(receipt) > maxNote {
		receipt = receipt[:maxNote]
	}
	m := Mark{Step: step, Outcome: MarkWitnessed, At: now, Note: receipt}
	if s.witnessed == nil {
		s.witnessed = make(map[int]Mark, maxStep)
	}
	s.witnessed[step] = m
	s.persistLocked()
	return m, true
}

// witnessedLocked is Witnessed without taking the lock, for callers that
// already hold it -- the same pattern marksLocked follows.
func (s *Store) witnessedLocked() []Mark {
	out := make([]Mark, 0, len(s.witnessed))
	for step := 1; step <= maxStep; step++ {
		if m, ok := s.witnessed[step]; ok {
			out = append(out, m)
		}
	}
	return out
}

// Witnessed returns every step the server has recorded seeing satisfied,
// ordered by step, the same as Marks.
func (s *Store) Witnessed() []Mark {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.witnessedLocked()
}

// --- The wizard's own address (#1213) -----------------------------------
//
// mikroview never connects to a router, so it cannot ask one which
// address reaches it -- the whole reason every other value in this file
// is an observation rather than a question. This one has no observable
// answer at all: nothing arrives here that says "you can reach me at
// this address", so it is the one field in the wizard that has to be
// asked rather than watched for. Persisted beside the marks for the same
// restart-survives-it reason, and kept separate from them because it is
// not a step outcome -- it has no step number, and evidence can never
// outrank it the way a witness outranks a stale mark.

// maxAddress caps the length defensively, mirroring maxNote -- the
// charset and structure are the caller's job (validSetupAddress in
// internal/api, #1095, checked before this is ever reached), this is
// only a backstop against an absurd value reaching the persisted
// document.
const maxAddress = 253

// SetAddress records the operator's answer, replacing any previous one:
// a step has exactly one current address, and changing one's mind (the
// instance moved) is not a second claim needing history the way a mark
// does. Editable at any time -- re-running setup on a moved instance
// must not require a reinstall. Reports whether the value was accepted;
// the caller has already run it past validSetupAddress, so this only
// rejects an empty value (there is no "clear the address" operation) or
// one implausibly long. No timestamp is kept: unlike a mark, there is
// only ever one current answer, with nothing about past ones worth
// reading back.
func (s *Store) SetAddress(address string) bool {
	if address == "" || len(address) > maxAddress {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.address = address
	s.persistLocked()
	return true
}

// Address returns the operator's stored answer, or "" if they have not
// answered yet -- the wizard then falls back to a candidate address, and
// then to the browser's own host.
func (s *Store) Address() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.address
}
