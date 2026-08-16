// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// This file is issue #405's programmatic half of internal/detect's port
// onto this chassis. docs/decisions/evaluation-engine.md section 2 names
// what belongs here and why the kind is permanent rather than a stepping
// stone to "eventually everything is declarative":
//
//   - statistical baselines -- the EMA machinery (activity_spike,
//     global_spike, rule_spike, off_hours, low_slow_scan);
//   - absence-of-events checks -- device_silence, stale_rule, where
//     "nothing arrived" is not a predicate over an event because there is
//     no event;
//   - external-data lookups -- known_bad_ip's blocklist, netclass's
//     classification, reputation's remote scores.
//
// Pretending these are declarative would either dumb them down or turn
// the condition language into a programming language. Both are worse than
// saying plainly that some definitions are built in.
//
// # Shape
//
// There is deliberately no single ProgrammaticDefinition god-struct with
// a pluggable logic function. Each shipped programmatic definition is its
// own small type embedding programmaticBase, which carries the envelope
// and the emission path. That is what lets the optional interfaces work
// at all: Ticked, Replayable and NonReplayable are answered per
// definition by Go's own method sets, so device_silence can declare
// itself non-replayable while rule_spike implements Replay, and neither
// has to carry a field saying which it is. A single struct with a
// function field would have to fake all of that with booleans, and
// Replayability (replayability.go) would have nothing real to classify.
//
// # Shipped-only
//
// See this package's doc comment (engine.go) for the invariant and how it
// is enforced: nothing an operator can send creates or edits programmatic
// logic. A shipped programmatic definition is exactly as tunable as a
// shipped declarative one -- enabled, scope, params -- and no more.

// programmaticBase is the envelope and emission path every shipped
// programmatic definition shares. Embedded, not wrapped, so each
// definition satisfies Evaluated with only its own Evaluate to write.
type programmaticBase struct {
	def   Definition
	order int

	// OnRoutedEmission is the seam main.go wires onto a real
	// flags.Store, exactly as on DeclarativeDefinition. nil is a valid
	// no-op: the definition still updates its own state and simply
	// produces nothing observable.
	OnRoutedEmission func(RoutedEmission)
}

// ID satisfies Evaluated.
func (p *programmaticBase) ID() string { return p.def.ID }

// Kind satisfies Evaluated -- always KindProgrammatic's string form.
func (p *programmaticBase) Kind() string { return string(p.def.Kind) }

// Definition returns a copy of the envelope this definition wears.
func (p *programmaticBase) Definition() Definition { return p.def }

// EvaluationOrder satisfies Ordered. Every programmatic definition
// answers it (the field is on the shared base), which is harmless: the
// zero value is the ordinary rank, so only the two reinforcement
// definitions that actually set it are ordered at all.
func (p *programmaticBase) EvaluationOrder() int { return p.order }

// SetSink wires this definition's emission sink. Separate from
// construction because main.go picks the sink per definition (see
// ShippedDeclarativeSink) after building it, the same shape the
// declarative kind uses.
func (p *programmaticBase) SetSink(sink func(RoutedEmission)) { p.OnRoutedEmission = sink }

// active reports whether this definition should look at e at all --
// enabled, and in scope. Every programmatic Evaluate starts with this,
// the same two gates DeclarativeDefinition.Evaluate applies before its
// own conditions run.
func (p *programmaticBase) active(e store.Event) bool {
	return p.def.Enabled && scopeMatches(p.def.Scope, e)
}

// emit renders, routes and delivers one emission. target/confidence/
// country/eventTime are the fields a definition's own logic determines;
// everything structural (the definition id, the route, the sink) is
// handled here so no definition open-codes it.
//
// Detail is passed already-rendered rather than as a template because a
// programmatic definition's sentence is genuinely computed -- "%.1f
// hits/s vs a baseline of %.1f ... %.1fσ above normal" has no
// evidence-set equivalent, and forcing it through RenderEmission would
// mean inventing tokens for float formatting, which is the DSL this
// codebase has already decided not to build. The #379 discipline that
// motivates RenderEmission still applies and is still checked, just
// differently: a programmatic Detail may only name values its own
// logic computed for the window it describes, which is exactly what the
// characterization pins assert byte-for-byte.
func (p *programmaticBase) emit(em Emission) {
	em.DefinitionID = p.def.ID
	routed, err := Route(p.def, em)
	if err != nil {
		logger.Error(fmt.Sprintf("programmatic definition %q: Route failed: %v", p.def.ID, err))
		return
	}
	if p.OnRoutedEmission != nil {
		p.OnRoutedEmission(routed)
	}
}

// ShippedDeps is everything a shipped programmatic definition may need
// beyond the event stream, injected at construction. One struct rather
// than a growing argument list, and interfaces rather than concrete
// types so a test supplies a fake without standing up a real blocklist
// download or device registry -- the same reasoning internal/detect gave
// for keeping reputationLookup/knownBadIPLookup/netClassLookup/DeviceLister
// as small interfaces of its own.
//
// Every field is optional. A nil dependency is an explicit "not
// configured" no-op, never an error: the definition that needs it simply
// never fires, exactly as internal/detect's own nil-checked optional
// fields behaved. That is what lets a deployment with no blocklist
// sources, no netclass sources and no entity store still build the whole
// shipped catalogue.
type ShippedDeps struct {
	// Flags is the flag store the reinforcement definitions
	// (known_bad_ip, netclass) raise confidence floors on. Unlike every
	// other definition, which reaches flags.Store only through its sink,
	// a reinforcement pass acts on flags *other* definitions raised, so
	// it needs the store itself -- see ConfidenceFloorRaiser.
	Flags ConfidenceFloorRaiser
	// Entities backs mail_sender's trusted-sender allowlist (#108).
	Entities EntityTagLookup
	// KnownBad backs known_bad_ip's local blocklist match (#113 Part B).
	KnownBad KnownBadIPLookup
	// NetClass backs netclass's direction-aware reinforcement (#114).
	NetClass NetClassLookup
	// Devices backs device_silence's "which devices should be talking"
	// question (#98).
	Devices DeviceLister
	// Rules backs stale_rule's "which rules have not fired" question
	// (#102).
	Rules StaleRuleLister
	// Rate backs global_spike's network-wide events-per-second reading.
	Rate EventRateSource
	// State is the engine-state store every baseline-backed definition
	// resumes from across a restart (#399/#400). nil is the expected
	// "persistence not configured" case: baselines start cold and warm
	// up again, which is what internal/detect did unconditionally.
	State *StateStore
}

// shippedProgrammaticBuilders maps a shipped definition id to the Go
// constructor for its logic -- the programmatic counterpart of
// shippedDeclarativeBuilders (shipped_declarative.go), and the reason the
// two kinds can share one envelope: a definition's Kind decides which
// table builds it, and nothing else in the chassis branches on Kind at
// all.
var shippedProgrammaticBuilders = map[string]func(Definition, ShippedDeps) (Evaluated, error){}

// registerShippedProgrammatic adds one builder to the table. Called from
// each definition's own file's init, so a definition's id, its logic and
// its registration all live together rather than in a list that has to be
// kept in step with a set of files.
func registerShippedProgrammatic(id string, build func(Definition, ShippedDeps) (Evaluated, error)) {
	if _, dup := shippedProgrammaticBuilders[id]; dup {
		panic("engine: duplicate shipped programmatic builder for " + id)
	}
	shippedProgrammaticBuilders[id] = build
}

// BuildShippedProgrammaticDefinition constructs the live Evaluated for a
// shipped programmatic Definition (def.Provenance.Origin ==
// ProvenanceShipped, def.Kind == KindProgrammatic), keyed by def.ID.
// Returns an error naming def.ID if no builder is registered for it --
// the programmatic counterpart of
// BuildShippedDeclarativeDefinition's own unknown-id error.
func BuildShippedProgrammaticDefinition(def Definition, deps ShippedDeps) (Evaluated, error) {
	if def.Kind != KindProgrammatic {
		return nil, fmt.Errorf("engine: programmatic definition %q has kind %q, want %q", def.ID, def.Kind, KindProgrammatic)
	}
	if def.Provenance.Origin != ProvenanceShipped {
		// Belt and braces against the invariant Definition.Validate
		// already enforces -- see this package's doc comment. A
		// programmatic definition that is not shipped has no Go logic
		// this binary could possibly supply, so refusing here means the
		// failure surfaces at construction rather than as a definition
		// that exists and silently evaluates nothing.
		return nil, fmt.Errorf("engine: programmatic definition %q has provenance %q -- the programmatic kind is shipped-only", def.ID, def.Provenance.Origin)
	}
	build, ok := shippedProgrammaticBuilders[def.ID]
	if !ok {
		return nil, fmt.Errorf("engine: %q has no shipped programmatic builder registered", def.ID)
	}
	if err := def.Validate(); err != nil {
		return nil, err
	}
	return build(def, deps)
}

// --- dependency interfaces -------------------------------------------

// ConfidenceFloorRaiser is the slice of *flags.Store the reinforcement
// definitions need: raise an existing flag's confidence floor, and
// (known_bad_ip only) nothing else. Narrowed to one method rather than
// taking *flags.Store so it is visible at the type level that a
// reinforcement pass can only ever strengthen an existing judgement --
// it cannot raise, clear, or lower a flag.
type ConfidenceFloorRaiser interface {
	RaiseConfidenceFloor(t FlagType, target string, floor int)
}

// FlagType mirrors flags.Type without importing it into this interface's
// signature -- see ConfidenceFloorRaiser. flags.Type is itself a string,
// so a *flags.Store satisfies this only through the adapter main.go
// wires (FlagsConfidenceFloorRaiser, flags_sink.go), which is the one
// place the conversion lives.
type FlagType = string

// EntityTagLookup is the slice of *entities.Store mail_sender needs: has
// this host been tagged as a legitimate mail sender?
type EntityTagLookup interface {
	HasTag(entityType, id, tag string) bool
}

// KnownBadIPLookup is the slice of *blocklist.Blocklist known_bad_ip
// needs. Returns the matched list's label and range, which is what the
// flag's Detail names.
type KnownBadIPLookup interface {
	MatchIP(ip string) (label, cidr string, ok bool)
}

// NetClassLookup is the slice of *netclass.Lookup the netclass
// reinforcement needs.
type NetClassLookup interface {
	// LookupClass returns whether ip matched, the category
	// ("tor"/"vpn"/"datacenter"/"privacyRelay"), and the matched list's
	// label.
	LookupClass(ip string) (matched bool, category, label string)
}

// DeviceInfo is one configured device as device_silence sees it.
type DeviceInfo struct {
	ID         string
	Name       string
	LastSeen   time.Time
	Configured bool
}

// DeviceLister is the slice of *device.Registry device_silence needs.
type DeviceLister interface {
	ListDevices() []DeviceInfo
}

// RuleUsage is one rule's usage record as stale_rule sees it.
type RuleUsage struct {
	Rule      string
	FirstSeen time.Time
	LastSeen  time.Time
	Count     int
}

// StaleRuleLister is the slice of *rules.Store stale_rule needs.
type StaleRuleLister interface {
	StaleRules(maxAge time.Duration, now time.Time) []RuleUsage
}

// EventRateSource is the slice of *store.Store global_spike needs: the
// network-wide events-per-second figure its baseline is built on.
// main.go already computes this for its own ticker; this is the same
// number, asked for rather than pushed in.
type EventRateSource interface {
	EventsPerSecond() float64
}

// baselinePersistInterval bounds how often one key's Baseline state is
// handed to the StateStore. StateStore.Set is write-behind, but it
// re-encodes the whole document on every call, so calling it per event
// would put a marshal on the ingest path -- exactly what #400's "baseline
// state must never be a write per event" rules out. A minute is far
// finer than the store's own 5-minute flush cadence, so it costs nothing
// in freshness, and coarse enough that even a definition tracking
// thousands of keys does a bounded amount of work.
var baselinePersistInterval = time.Minute

// keyedBaseline is one key's Baseline plus the bookkeeping baselineSet
// needs around it.
type keyedBaseline struct {
	b *Baseline
	// lastPersisted is when this key's state last reached the
	// StateStore -- see baselinePersistInterval.
	lastPersisted time.Time
}

// baselineSet is the per-key EMA baseline machinery every
// baseline-backed programmatic definition shares: bounded per-key
// storage (Keyed), warm resume from the engine-state store on first
// sight of a key, and coarse write-back.
//
// It exists so #368's history floor is declared once per definition and
// then structurally unavoidable, rather than being a condition each
// detector remembers to check. internal/detect had the opposite shape --
// four detectors each hand-rolling prime/compare/update, three of them
// with a history floor and rule_spike without -- which is exactly how
// #368 happened.
type baselineSet struct {
	defID string
	// primeWindow is how long a key must have been observed before its
	// baseline may be primed at all (Baseline.Reading's own gate). It is
	// declared per definition rather than always being the definition's
	// window, because whether a still-filling window's reading is
	// dangerous depends on what the reading *is*:
	//
	//   - rule_spike's reading is count/window.Seconds(), which climbs
	//     purely as the ring fills. Priming from it is #368 exactly, so
	//     its prime window is its full window.
	//   - low_slow_scan's, activity_spike's and global_spike's baselines
	//     were primed by internal/detect on the very first reading, and
	//     each has its own separate firing floor (LowSlowScanMinObservation,
	//     hostActivityMinSamples, and none respectively). Deferring their
	//     priming would change when they can fire -- low_slow_scan's
	//     earliest possible flag would move from 45 minutes to three
	//     hours -- which is a behaviour change #405 is not licensed to
	//     make. They pass zero, priming on first reading exactly as
	//     before.
	//
	// Zero means "prime on the first reading": Baseline.Reading's gate is
	// observedFor < window, which is never true for a zero window.
	primeWindow time.Duration
	// floor gates firing (Snapshot.Ready), independently of priming.
	floor   BaselineFloor
	cadence UpdateCadence
	state   *StateStore
	keyed   *Keyed[*keyedBaseline]
	// zeroSeeded starts a cold key already primed at zero, so its very
	// first reading is folded in by the ordinary EMA update rather than
	// becoming the baseline outright.
	//
	// Only off_hours wants this, and it wants it because its statistic is
	// genuinely a different thing. Every other baseline here answers
	// "what is this key's normal rate", and seeding that from the first
	// observation is right -- one observation is the best estimate
	// available. off_hours answers "how many events does this host
	// produce during this clock hour, on how many distinct prior days",
	// and internal/detect accumulated it from a standing start of zero:
	// the first day observed at an hour moves the baseline by
	// emaAlpha * count, not to count. Priming instead would make one
	// night's traffic the whole baseline, which is exactly the
	// single-busy-night false positive OffHoursMinSampleDays exists to
	// rule out. Reproducing it is not a stylistic choice; the pinned
	// Detail strings carry the resulting baseline value.
	zeroSeeded bool
}

func newBaselineSet(defID string, primeWindow time.Duration, floor BaselineFloor, cadence UpdateCadence, state *StateStore) *baselineSet {
	return &baselineSet{
		defID:       defID,
		primeWindow: primeWindow,
		floor:       floor,
		cadence:     cadence,
		state:       state,
		keyed:       NewKeyed[*keyedBaseline](),
	}
}

// zeroSeed marks this set's cold keys as starting primed at zero -- see
// the field's own doc comment. Returns s for chaining at construction.
func (s *baselineSet) zeroSeed() *baselineSet {
	s.zeroSeeded = true
	return s
}

// get returns key's Baseline, constructing it (or resuming it from
// persisted state) if this is the first time key has been seen. For a
// definition that needs to read a baseline without folding a reading in,
// or to fold on a cadence of its own rather than once per call -- see
// off_hours, whose baseline advances once per calendar day while its
// firing check runs on every event.
func (s *baselineSet) get(key string, now time.Time) *Baseline {
	return s.keyed.GetOrCreate(key, now, func() *keyedBaseline { return &keyedBaseline{b: s.newBaseline(key, now)} }).b
}

// persist offers key's current state to the StateStore, subject to
// baselinePersistInterval. Called by a definition that folds readings
// through get rather than through reading.
func (s *baselineSet) persist(key string, now time.Time) {
	kb, ok := s.keyed.Get(key)
	if !ok {
		return
	}
	s.maybePersist(key, kb, now)
}

func (s *baselineSet) newBaseline(key string, now time.Time) *Baseline {
	if s.state != nil {
		if persisted, ok := s.state.Get(s.defID, key); ok {
			return RestoreBaseline(s.primeWindow, s.floor, s.cadence, persisted)
		}
	}
	if s.zeroSeeded {
		return RestoreBaseline(s.primeWindow, s.floor, s.cadence,
			BaselineState{Primed: true, FirstSeen: now})
	}
	return NewBaseline(s.primeWindow, s.floor, s.cadence)
}

// reading folds one reading into key's baseline and returns the Snapshot
// as it stood before it -- Baseline.Reading's own contract, so a firing
// decision compares against the baseline as it was, not as it becomes.
//
// A key seen for the first time resumes from persisted state when there
// is any, so a restart does not throw away a warm baseline and spend
// another whole warm-up blind. That is half of what closes #368: the
// other half is Baseline.Reading refusing to prime at all inside the
// first window, so the very first sample is a fully-observed rate rather
// than a still-filling ring's artificially low one.
func (s *baselineSet) reading(key string, now time.Time, value float64) Snapshot {
	kb := s.keyed.GetOrCreate(key, now, func() *keyedBaseline { return &keyedBaseline{b: s.newBaseline(key, now)} })
	before := kb.b.Reading(now, value)
	s.maybePersist(key, kb, now)
	return before
}

// snapshot reports key's baseline without folding in a reading -- for a
// definition that needs to consult a baseline it is not currently
// advancing.
func (s *baselineSet) snapshot(key string, now time.Time) (Snapshot, bool) {
	kb, ok := s.keyed.Get(key)
	if !ok {
		return Snapshot{}, false
	}
	return kb.b.Snapshot(now), true
}

func (s *baselineSet) maybePersist(key string, kb *keyedBaseline, now time.Time) {
	if s.state == nil {
		return
	}
	if !kb.lastPersisted.IsZero() && now.Sub(kb.lastPersisted) < baselinePersistInterval {
		return
	}
	kb.lastPersisted = now
	s.state.Set(s.defID, key, kb.b.State())
}
