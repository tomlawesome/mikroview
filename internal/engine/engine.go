// SPDX-License-Identifier: AGPL-3.0-only

// Package engine is the evaluation chassis described in
// docs/decisions/evaluation-engine.md -- the machine internal/detect and
// internal/watchlist each build by hand today, unified into one place.
// This first slice (#398) is deliberately just the plumbing: one way in
// from ingest, one run/shutdown lifecycle, and one panic boundary. It carries no evaluation semantics at all --
// there is no such thing as a detection or an expectation yet, only an
// Evaluated definition, the minimal shape #399/#401 grow into
// declarative and programmatic definitions. Until something registers a
// definition, an Engine evaluates nothing; wiring it into main.go
// alongside detect.Detector and watchlist.Evaluator is therefore a
// no-behaviour-change change.
//
// #402 adds the declarative kind's own match language on top of that
// chassis: structured (field, operator, value) conditions over a closed
// field set and a closed operator set, both enumerated once in
// conditions.go, plus threshold-over-window evaluation (declarative.go)
// and a dispatch pre-index (dispatch.go) so a large declarative
// definition set costs the ingest budget the handful of definitions an
// event could actually match, not all of them linearly.
// docs/decisions/evaluation-engine.md's own words on why the condition
// language stops where it does, quoted verbatim so the boundary is
// visible from this package's doc comment and not only from the ADR:
//
// "No DSL. Structured conditions only. If a real need outgrows them,
// that is a new ADR, not a quiet extension."
//
// #405 adds the programmatic kind (programmatic.go) -- built-in Go
// wearing the same envelope, for the definitions that cannot honestly be
// a form: statistical baselines, absence-of-events checks, external-data
// lookups. It comes with a second boundary, stated here beside the first
// because it is the same kind of promise and is enforced the same way --
// structurally, in one place, rather than by remembering:
//
// The programmatic kind is shipped-only. provenance=custom implies
// kind=declarative (Definition.Validate), so no request shape can
// express a custom programmatic definition; DefinitionsStore.Upsert
// refuses to replace a shipped definition wholesale, so no request shape
// can turn an existing declarative one programmatic either. What an
// operator may do to a shipped programmatic definition is exactly what
// they may do to a shipped declarative one and no more: enable or
// disable it, scope it, and override its declared params. Its Go logic
// is part of this binary, not part of its data -- which is precisely why
// the two kinds can share one envelope without the programmatic kind
// becoming a way to smuggle code in through the API.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/store"
)

var logger = logging.New("engine")

// batchSize is how many events one Since call copies out of the store
// (see Run). It is a lock-hold bound, not a backpressure bound: the
// store hands the batch over under its read lock, which the sole ingest
// writer waits on, so the batch is kept small enough that an engine
// catching up on a deep backlog never holds that lock for a visible
// stretch -- it takes the backlog in slices instead, releasing the lock
// between each. Nothing is lost by stopping at 512: the events stay in
// the ring and the very next iteration reads on from the same cursor.
//
// A var, not a const, so tests can shrink it -- same convention as
// internal/detect.maxTrackedSources -- without needing hundreds of
// events to reach a batch boundary.
var batchSize = 512

// logFloodInterval rate-limits the log lines this package emits from
// conditions that repeat at event rate -- the store outrunning the
// engine (see recordOutrun) and the match log failing (see
// matchlog_sink.go). Unifies detect.observeQueueDropLogInterval and
// watchlist.evalQueueDropLogInterval, which stated the same 30-second
// reasoning twice: logging every single occurrence would itself add load
// during exactly the overload condition being reported, so a periodic
// summary is enough to make an otherwise-invisible condition observable
// without that cost.
const logFloodInterval = 30 * time.Second

// drainTimeout bounds how long Run keeps reading batches forward after
// ctx is cancelled, before drain gives up and Run returns anyway.
//
// This is a decision, not a detail: draining forever ("evaluate
// everything still held, however long that takes") can hang process exit
// on a backlog the size of the whole retention window, and stopping the
// instant ctx cancels throws away events that are already stored and are
// typically cheap to finish evaluating. A short bounded window gets the
// common case (an engine that is caught up, or catches up in
// milliseconds) fully drained, while capping worst-case shutdown latency
// to something in the same order as the rest of mikroview's shutdown
// sequence -- see main.go's own 5-second graceful-shutdown budget for
// httpServer. What is left unevaluated is not lost bookkeeping: the
// cursor is not persisted, so a restart starts at the store's newest ID
// (see Run).
//
// A var, not a const, so lifecycle tests can shrink it and stay fast
// rather than actually waiting out the production bound -- same
// convention as batchSize above.
var drainTimeout = 2 * time.Second

// faultThreshold is the repeat-panic policy decided on issue #398 itself
// (see the issue's "Repeat-panic policy" comment, owner-ratified
// 2026-08-16): a definition that panics on this many *consecutive*
// evaluations is marked faulted and skipped, rather than either
// silently continuing to burn CPU and log volume for no detection, or
// silently disabling itself and lying to the operator about coverage.
// A successful evaluation resets the streak (see registration.streak),
// so an intermittently-panicking definition never faults purely from
// accumulated history -- only from an unbroken run of failures.
const faultThreshold = 3

// Evaluated is the minimal shape the chassis needs from one definition
// -- an id, a kind (declarative/programmatic, per
// docs/decisions/evaluation-engine.md section 2), and a way to evaluate
// one event. Deliberately thin: #399/#401 own what a definition actually
// *is* (its envelope, its params, its intent) and grow this interface as
// needed rather than this issue pre-empting that design.
//
// Evaluate must not retain e beyond the call, and any error it wants to
// surface belongs in whatever intent-specific side effect it performs
// (raising a flag, recording a match) -- the chassis's only interest in
// the call is whether it returns or panics.
type Evaluated interface {
	// ID uniquely identifies the definition across the engine's
	// lifetime; used to key registration, fault state, and log lines.
	ID() string
	// Kind names the definition's kind (e.g. "declarative",
	// "programmatic") for logging and fault reporting -- not
	// interpreted by the chassis itself.
	Kind() string
	// Evaluate runs the definition against one event. Called on the
	// engine's single evaluation goroutine (see Run) -- like
	// detect.Detector.Observe and watchlist.Evaluator's per-event pass,
	// implementations take no lock of their own for engine-driven
	// access.
	Evaluate(e store.Event)
}

// Ordered is implemented by a definition that has to be evaluated after
// (or before) others for the *same* event, rather than in whatever order
// the engine happens to hold them.
//
// Almost nothing needs this, and a definition that implements it without
// a real reason is a definition that has quietly made itself dependent
// on another one. The reason that does exist is reinforcement: a
// definition whose whole job is to raise the confidence floor of flags
// *other* definitions just raised for this same event
// (internal/detect's known_bad_ip and netclass passes, whose own doc
// comments record that they must run last "so any flag newly raised by
// this same event already exists in fs by the time RaiseConfidenceFloor
// is called -- calling this any earlier would silently miss reinforcing
// a flag raised later in the same pass"). flags.Store.RaiseConfidenceFloor
// no-ops on a target it does not yet know about, so getting this wrong
// costs a silently missing confidence floor, not an error.
//
// internal/detect enforced that ordering by writing the calls last in
// one function. The engine cannot: definitions are separate registrations,
// and a map has no order. Declaring the order is how the same guarantee
// survives the port -- and evaluateEvent iterating a *sorted* slice is
// what makes it an invariant rather than an accident of Go's map
// iteration. See TestEvaluationOrderIsDeterministicAndRespectsOrdered.
type Ordered interface {
	// EvaluationOrder returns this definition's evaluation rank for one
	// event. Lower runs first; the zero value (what a definition that
	// does not implement this interface gets) is the ordinary rank
	// everything else evaluates at. Ties break on ID, so the order is
	// total and stable across process restarts, not merely consistent
	// within one run.
	EvaluationOrder() int
}

// ReinforcementOrder is the rank a reinforcement definition declares --
// see Ordered. A named constant rather than a bare number at each call
// site so "runs after every flag-raiser" is stated once, and so the gap
// to the default rank (0) is visibly large enough that an intermediate
// rank can be introduced later without renumbering anything.
const ReinforcementOrder = 100

// Ticked is implemented by a definition whose firing condition is not a
// property of any event, so no amount of Evaluate calls can ever
// establish it: "no syslog from this device for fifteen minutes", "this
// rule has not fired in thirty days", "the network-wide rate is four
// times its own baseline". docs/decisions/evaluation-engine.md section 2
// names absence-of-events detectors as one of the reasons the
// programmatic kind is permanent rather than a stepping stone -- there
// is no event for a condition to match against.
//
// internal/detect drove these from main.go tickers calling Check(now)
// directly on three concrete types. Tick is the same shape, moved onto
// the chassis so those definitions get the same envelope, the same panic
// boundary, the same fault reporting and the same enabled/scope handling
// as every other definition, instead of three bespoke call sites.
//
// Tick is called from Engine.Tick, which is NOT the evaluation
// goroutine -- see that method's own doc comment for the concurrency
// contract implementations owe.
type Ticked interface {
	// Tick runs this definition's periodic check as of now.
	Tick(now time.Time)
	// TickInterval is how often Tick is meant to run. Declared by the
	// definition rather than chosen by the caller because the cadence is
	// part of what the definition means: global_spike's EMA advances one
	// sample per tick, so halving the interval halves the wall-clock
	// span its baseline covers, while device_silence's cadence only
	// decides how promptly an already-true condition is noticed. A
	// single shared cadence would silently retune the first while barely
	// touching the second -- which is exactly what folding
	// internal/detect's three separate tickers (10s, 1m, and an
	// operator-configured stale-rule sweep) into one would have done.
	//
	// Engine.Tick honours this: a driver may call it as often as it
	// likes, and each definition still runs at its own declared rate.
	TickInterval() time.Duration
}

// Snapshotted is implemented by definitions whose in-memory windows
// are worth carrying across a restart (#795). Baselines already survive
// through StateStore (state.go); what does not is everything a
// definition holds in a Keyed[V] -- the per-minute buckets, distinct-value
// rings and per-source day bookkeeping a baseline is actually compared
// against. A restart throws those away today, so a warm baseline spends
// its first window judging an empty ring.
//
// Owner decision on #795, 2026-09-02: this state is written whole every
// few minutes and read back on boot, separate from StateStore and
// deliberately holding no event lines -- counts, timestamps and
// identifiers only.
//
// Both methods are the definition's own state, whole: the chassis never
// interprets the bytes, it only routes them by definition ID (see
// Engine.ExportState/ImportState).
type Snapshotted interface {
	// ExportState renders this definition's carried-across state as
	// JSON. Called on the evaluation goroutine (Engine.ExportState
	// arranges that for a caller on any other one), so an implementation
	// reads its own single-writer state directly, exactly as its
	// Evaluate does.
	ExportState() (json.RawMessage, error)
	// ImportState restores raw, which was exported at taken, into a
	// definition evaluating as of now. Implementations drop whatever
	// the elapsed time has made meaningless rather than restoring it
	// stale, must be safe to call before any event has been evaluated,
	// and must never emit: a restored window is state to judge the next
	// event against, never a firing in its own right.
	ImportState(raw json.RawMessage, taken, now time.Time) error
}

// evaluationRank reports d's Ordered rank, or 0 for a definition that
// does not declare one.
func evaluationRank(d Evaluated) int {
	if o, ok := d.(Ordered); ok {
		return o.EvaluationOrder()
	}
	return 0
}

// Fault is the visible state of a definition the engine has stopped
// evaluating after it panicked faultThreshold times in a row. It is
// deliberately a first-class, always-readable value (see Engine.Faults)
// rather than a log line or a silent disable -- the policy this issue
// implements is that the engine's API always reports a coverage hole,
// never hides one.
type Fault struct {
	DefinitionID string
	Kind         string
	Reason       string
	At           time.Time
}

// registration is one definition's engine-side bookkeeping: the
// definition itself, its consecutive-panic streak, and its fault state
// if any. Guarded by Engine.mu -- streak and fault are mutated both from
// the evaluation goroutine (on every Evaluate) and from any caller of
// ClearFault (an operator action, not necessarily on that goroutine).
type registration struct {
	def Evaluated
	// rank is def's Ordered rank, read once at registration -- see
	// Ordered. Cached rather than re-derived per event because the type
	// assertion is the same answer every time and this sits on the
	// ingest path.
	rank int
	// lastTick is when this definition's Tick last ran -- see
	// Engine.Tick. The zero value means "never ticked", which counts as
	// due: a definition registered at boot runs on the driver's first
	// tick rather than waiting out a full interval first, so a device
	// that was already silent before this process started is reported
	// promptly rather than after another whole staleness window.
	lastTick time.Time
	streak   int
	fault    *Fault
}

// Source is where Run reads events from: the ring store itself in
// production (internal/store.Store.Since), which is what makes the
// engine's reach the whole retention window instead of a second bounded
// copy of the stream (#1109).
//
// An interface rather than a *store.Store so the chassis keeps the same
// "no opinion about where events come from" posture it had when it was
// fed a channel -- and so a test can hand it a source it controls
// precisely.
type Source interface {
	// Since returns the held events with ID > afterID in ascending ID
	// order, up to max of them, plus the oldest and newest IDs the source
	// still holds. See store.Store.Since for the full contract.
	Since(afterID uint64, max int) ([]store.Event, uint64, uint64)
}

// Engine is the chassis: one cursor over the event store, one
// run/shutdown lifecycle, one panic boundary per evaluated definition.
// See the package doc comment for what it deliberately does not do yet.
type Engine struct {
	src  Source
	done chan struct{}

	// nudge is the "there is something new" doorbell, not a queue: one
	// slot, non-blocking send (see Nudge), so any number of inserts
	// arriving while Run is busy coalesce into a single wake-up and the
	// ingest goroutine never waits on evaluation. What to evaluate is
	// read from src, never carried through here.
	nudge chan struct{}

	// cursor is the last ID this engine evaluated. Written only by the
	// evaluation goroutine, read by Lag from whichever goroutine serves
	// /api/stats, hence atomic.
	cursor atomic.Uint64

	// outrun counts events the store evicted before the engine reached
	// them -- the only loss left once evaluation reads from the store
	// rather than from a queue of its own, and a much rarer thing than
	// the queue overflow it replaces: it takes a flood that outruns the
	// entire retention window, not one that outruns 4096 slots.
	outrun     atomic.Uint64
	outrunGate *logging.Limiter

	// evaluatedEvents counts events that have reached evaluateEvent --
	// the one thing ImportState needs to know to refuse a warm-restart
	// import that has arrived too late to be safe (see its doc comment).
	// An atomic add per event, on the same path that already does one
	// for drops.
	evaluatedEvents atomic.Uint64

	// tasks is how a caller on another goroutine borrows the evaluation
	// goroutine for one function -- see runOnEvaluationGoroutine, and
	// ExportState for the one thing that needs it. Unbuffered: the point
	// is the rendezvous, not queueing work up.
	tasks chan func()

	mu   sync.Mutex
	defs map[string]*registration
	// order is defs' values sorted by (Ordered rank, ID) -- maintained
	// on Register rather than sorted per event, since the definition set
	// changes on an operator action and the sort runs on every single
	// ingested event. See Ordered for why the order has to exist at all.
	order []*registration
	// running records whether Run is currently driving evaluation.
	// Guarded by mu, and set under it before Run's first event, which is
	// what lets ExportState/ImportState decide between doing the work
	// inline and handing it to the evaluation goroutine without a window
	// where both could happen at once -- see runOnEvaluationGoroutine.
	running bool
}

// New constructs an Engine reading from src, with no registered
// definitions -- evaluating nothing until something registers one.
//
// A nil src is valid and means "no events ever arrive": Run still serves
// tasks and shuts down normally, which is what callers that only exercise
// the definition set (tests, ExportState) want.
func New(src Source) *Engine {
	return &Engine{
		src:        src,
		done:       make(chan struct{}),
		nudge:      make(chan struct{}, 1),
		tasks:      make(chan func()),
		outrunGate: logging.NewLimiter(logFloodInterval),
		defs:       make(map[string]*registration),
	}
}

// Register adds d to the set of definitions the engine evaluates on
// every future event, replacing any existing registration with the same
// ID (idempotent re-registration, e.g. after a definition edit) and
// clearing whatever fault/streak state that prior registration carried
// -- a freshly (re)registered definition always starts unfaulted.
//
// Safe to call concurrently with Run; the next event picks up the
// change.
func (e *Engine) Register(d Evaluated) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.defs[d.ID()] = &registration{def: d, rank: evaluationRank(d)}
	e.reorderLocked()
}

// Unregister removes the definition registered under id, so the engine
// stops evaluating it entirely -- the counterpart Register always
// implied and nothing needed until definitions became editable at
// runtime (issue #407): a deleted definition that stayed registered
// would keep evaluating, and keep raising, after the operator removed
// it.
//
// Reports whether anything was actually removed. Safe to call
// concurrently with Run; the next event picks up the change.
func (e *Engine) Unregister(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.defs[id]; !ok {
		return false
	}
	delete(e.defs, id)
	e.reorderLocked()
	return true
}

// reorderLocked rebuilds the evaluation order from defs. Called only
// from Register (the one place the definition set changes), never from
// the per-event path.
func (e *Engine) reorderLocked() {
	e.order = make([]*registration, 0, len(e.defs))
	for _, r := range e.defs {
		e.order = append(e.order, r)
	}
	sort.Slice(e.order, func(i, j int) bool {
		if e.order[i].rank != e.order[j].rank {
			return e.order[i].rank < e.order[j].rank
		}
		return e.order[i].def.ID() < e.order[j].def.ID()
	})
}

// Nudge tells the evaluation goroutine there is something new in the
// store, without handing it anything and without ever blocking the
// caller: a non-blocking send on a one-slot channel, so a doorbell rung
// while the engine is already busy costs nothing and is not lost -- the
// engine reads forward from its cursor when it next looks, and a cursor
// does not care how many times it was rung.
//
// mikroview's ingest goroutine calls this once per stored event, right
// after store.Insert. Missing a nudge cannot lose an event, only delay
// it: the next nudge, from the next arrival, still finds everything
// behind the cursor.
//
// A nil *Engine is a valid no-op, same convention Tick and ExportState
// use for a nil receiver -- callers (tests in particular) that don't need
// the chassis at all can pass nil rather than constructing one solely to
// satisfy the signature.
func (e *Engine) Nudge() {
	if e == nil {
		return
	}
	select {
	case e.nudge <- struct{}{}:
	default:
		// Already rung and not yet answered: one wake-up covers both.
	}
}

// Lag reports how far behind the store's newest event this engine is
// (behind), how old the oldest thing it has not evaluated yet is
// (behindSeconds), and how many events it never got to at all because
// the store evicted them first (outrun, a lifetime count).
//
// The first two are a "late" measure and the third is a "lost" measure,
// which is the whole distinction this design bought: being behind is a
// backlog that will be worked through, and only outrun is a coverage
// gap. /api/stats reports all three (see internal/api/rest.go) so the UI
// can say so in those terms.
//
// behindSeconds reads the next unevaluated event's ReceivedAt rather
// than timing evaluation itself: "the oldest thing not yet looked at is
// 4 seconds old" is a statement an operator can act on, where a rate is
// not. Zero when caught up -- there is no next event to be late for.
func (e *Engine) Lag() (behind uint64, behindSeconds float64, outrun uint64) {
	if e == nil {
		return 0, 0, 0
	}
	cursor := e.cursor.Load()
	next, _, newestHeld := e.read(cursor, 1)
	if newestHeld > cursor {
		behind = newestHeld - cursor
	}
	if len(next) > 0 {
		if age := time.Since(next[0].ReceivedAt).Seconds(); age > 0 {
			behindSeconds = age
		}
	}
	return behind, behindSeconds, e.outrun.Load()
}

// read is Source.Since with the nil-source case folded in, so every
// caller below can read unconditionally. A nil source reports an empty
// held range, which is also what "no events, none evicted" looks like.
func (e *Engine) read(afterID uint64, max int) ([]store.Event, uint64, uint64) {
	if e.src == nil {
		return nil, 0, 0
	}
	return e.src.Since(afterID, max)
}

// Run evaluates every event the store holds past this engine's cursor,
// against every registered definition in turn, waking on a Nudge and
// reading forward in batches until it is caught up -- then waiting for
// the next one. When ctx is done it keeps reading for up to drainTimeout
// before stopping. Meant to run in its own goroutine, separate from
// whatever goroutine calls Nudge, the same shape the queue-fed version
// had.
//
// The cursor starts at the store's newest ID, not at zero: whatever the
// store already holds either has been evaluated (this process's own
// earlier events) or belongs to a previous process, and re-raising a
// restored warm store's flags on every restart would be a worse answer
// than starting level. That also means the first batch can never look
// like eviction outran the engine, because there is nothing before the
// cursor to have been evicted.
//
// Run closes the channel Done returns exactly once, on its way out --
// so a caller (main.go) can join on the engine having actually stopped
// rather than firing and forgetting.
func (e *Engine) Run(ctx context.Context) {
	defer close(e.done)
	_, _, newestHeld := e.read(0, 0)
	e.cursor.Store(newestHeld)
	// Set after the cursor, not before: running is what tells the rest of
	// the engine that evaluation has begun (see setRunning), so it must
	// not be true for the window in which the cursor still says zero.
	e.setRunning(true)
	defer e.setRunning(false)
	for {
		select {
		case <-e.nudge:
			e.catchUp()
		case fn := <-e.tasks:
			// Work another goroutine needs done with this one's
			// exclusive access to definition state -- see
			// runOnEvaluationGoroutine. Serviced between batches, exactly
			// like an event was, so it can never interleave with one.
			fn()
		case <-ctx.Done():
			e.drain()
			return
		}
	}
}

// catchUp reads batches forward from the cursor until one comes back
// empty -- "caught up" is a fact about the store, not about how many
// nudges have been answered, so a burst of 100,000 inserts behind one
// doorbell is evaluated in full.
func (e *Engine) catchUp() {
	for e.evaluateBatch(time.Time{}) {
	}
}

// evaluateBatch reads one batch forward from the cursor, evaluates it in
// ingest order and advances the cursor across it, reporting whether the
// batch held anything (i.e. whether there may be more behind it).
//
// A non-zero deadline stops it part-way through a batch; only shutdown
// passes one (see drain), because only shutdown has a reason not to
// finish what it has already copied out of the store.
func (e *Engine) evaluateBatch(deadline time.Time) bool {
	cursor := e.cursor.Load()
	events, oldestHeld, _ := e.read(cursor, batchSize)
	if oldestHeld > cursor+1 {
		// The ring wrapped -- or was reset, or resized smaller -- past
		// events this engine had not reached. Eviction only ever takes
		// from the oldest end, so everything from the cursor up to the
		// oldest survivor is gone for good, and the honest move is to
		// count it and carry on from what is left rather than pretend the
		// cursor is still meaningful.
		e.recordOutrun(oldestHeld - 1 - cursor)
		e.cursor.Store(oldestHeld - 1)
	}
	if len(events) == 0 {
		return false
	}
	for _, ev := range events {
		e.evaluateEvent(ev)
		// Per event, not per batch: the cursor is what Lag reads and what
		// a cut-short drain resumes nothing from, so it should never
		// claim more evaluation than has actually happened.
		e.cursor.Store(ev.ID)
		if !deadline.IsZero() && time.Now().After(deadline) {
			return false
		}
	}
	return true
}

// recordOutrun counts n events the store evicted before the engine
// reached them, and logs a rate-limited summary that says what was
// actually lost -- detection for those events, not merely "the buffer
// wrapped". #380's first item is why this matters: the observable
// symptom of an evaluator that never saw an event is otherwise silence,
// and silence reads as "nothing is wrong" rather than as the coverage
// gap it is.
func (e *Engine) recordOutrun(n uint64) {
	total := e.outrun.Add(n)
	if _, ok := e.outrunGate.Allow(); ok {
		logger.Warn(fmt.Sprintf("events arrived faster than they could be checked and left the memory window first -- %d event(s) never checked (they were stored and broadcast normally); raise store.maxMemory or find what is flooding", total))
	}
}

// setRunning flips the running flag under mu. Taking the lock here is
// what closes the gap between "is anything evaluating" and acting on the
// answer: ExportState/ImportState hold mu while they check it and while
// they do the work inline, so Run cannot start halfway through one.
func (e *Engine) setRunning(running bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.running = running
}

// runOnEvaluationGoroutine runs fn where it is safe to read and write
// the definitions' own state, and returns once fn has finished.
//
// A definition's windows (its Keyed values -- rings, day bookkeeping)
// are single-writer by design: Keyed's mutex protects the map, never the
// values inside it, and the per-event path deliberately takes no lock
// around a ring (see Keyed and CountRing's own doc comments, and
// internal/watchlist's measured cost of the alternative). So reading
// them from a second goroutine is a data race however carefully it is
// written -- which leaves borrowing the evaluation goroutine as the way
// a periodic snapshot writer (#795) reads that state without putting a
// lock on the ingest path.
//
// Run is servicing tasks: hand fn over and wait. Run has stopped, or
// never started: nothing else can be touching definition state, so fn
// runs here. The caller must hold mu, which is what makes the second
// case honest -- Run takes mu before its first event (see setRunning),
// so it cannot begin while fn is running inline.
func (e *Engine) runOnEvaluationGoroutine(fn func()) {
	if !e.running {
		fn()
		return
	}
	done := make(chan struct{})
	task := func() {
		defer close(done)
		fn()
	}
	// mu is released for the rendezvous: the evaluation goroutine takes
	// it itself on every event, so holding it here would deadlock.
	e.mu.Unlock()
	select {
	case e.tasks <- task:
		<-done
	case <-e.done:
		// Run stopped between the check above and this send. Nothing is
		// evaluating any more, so fn is safe here.
		fn()
	}
	e.mu.Lock()
}

// Done returns a channel that is closed once Run has returned -- the
// join primitive main.go needs to observe shutdown completing, since a
// goroutine simply returning is otherwise unobservable from outside it.
func (e *Engine) Done() <-chan struct{} {
	return e.done
}

// drain keeps reading batches forward when Run's ctx is cancelled,
// stopping as soon as the engine is caught up or drainTimeout elapses,
// whichever comes first -- see drainTimeout's doc comment for why
// neither "evaluate everything held" nor "stop at once" is the right
// unconditional answer. The deadline is passed down into the batch so a
// single slow definition cannot overrun it by a whole batch's worth of
// events.
func (e *Engine) drain() {
	deadline := time.Now().Add(drainTimeout)
	for e.evaluateBatch(deadline) {
		if time.Now().After(deadline) {
			return
		}
	}
}

// evaluateEvent runs ev through every currently-registered, unfaulted
// definition. Takes a snapshot of the registration set under lock and
// then evaluates outside it, so a slow or panicking definition never
// holds Engine.mu -- Register/Faults/ClearFault stay responsive from
// other goroutines while evaluation is in progress.
func (e *Engine) evaluateEvent(ev store.Event) {
	e.evaluatedEvents.Add(1)
	e.mu.Lock()
	regs := append([]*registration(nil), e.order...)
	e.mu.Unlock()

	for _, r := range regs {
		e.evaluateOne(r, ev)
	}
}

// Tick drives every registered definition that implements Ticked, in the
// same order and behind the same per-definition panic boundary as
// evaluateEvent -- the chassis's home for the three checks main.go used
// to call directly on concrete internal/detect types from its own
// tickers (see Ticked).
//
// Called from whatever goroutine owns the caller's ticker, NOT from the
// evaluation goroutine, exactly as internal/detect's own
// GlobalSpikeDetector.Check/DeviceSilenceDetector.Check/StaleRuleDetector.Check
// were: a Ticked definition therefore owns its own concurrency safety
// for any state it shares with its own Evaluate. In practice the three
// shipped ones share none -- an absence-of-events definition has no
// per-event state to share, and global_spike's baseline is only ever
// touched from here -- which is why the chassis states the requirement
// rather than serializing on the caller's behalf and quietly making
// every tick contend with ingest.
func (e *Engine) Tick(now time.Time) {
	if e == nil {
		return
	}
	e.mu.Lock()
	regs := append([]*registration(nil), e.order...)
	e.mu.Unlock()

	for _, r := range regs {
		t, ok := r.def.(Ticked)
		if !ok {
			continue
		}
		if !e.tickDue(r, t, now) {
			continue
		}
		e.tickOne(r, t, now)
	}
}

// tickDue reports whether r is due to tick at now, per its own declared
// TickInterval, and records the tick if so -- see registration.lastTick.
// A non-positive interval is treated as "every call", the same
// permissive reading a zero threshold gets elsewhere in this package.
func (e *Engine) tickDue(r *registration, t Ticked, now time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	interval := t.TickInterval()
	if !r.lastTick.IsZero() && interval > 0 && now.Sub(r.lastTick) < interval {
		return false
	}
	r.lastTick = now
	return true
}

// tickOne is evaluateOne for a tick: same fault gate, same
// consecutive-panic accounting, same recovery boundary. Kept separate
// rather than generalized over a closure so the two paths' stack traces
// stay honest about which one panicked.
func (e *Engine) tickOne(r *registration, t Ticked, now time.Time) {
	e.mu.Lock()
	if r.fault != nil {
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

	panicked, panicVal := e.tickRecovered(r, t, now)

	e.mu.Lock()
	defer e.mu.Unlock()
	if !panicked {
		r.streak = 0
		return
	}
	r.streak++
	if r.streak >= faultThreshold {
		r.fault = &Fault{
			DefinitionID: r.def.ID(),
			Kind:         r.def.Kind(),
			Reason:       fmt.Sprintf("panicked on %d consecutive ticks (last: %v)", r.streak, panicVal),
			At:           time.Now(),
		}
		logger.Error(fmt.Sprintf("definition %q (kind=%s) marked faulted after %d consecutive tick panics -- skipped until cleared; this is a coverage hole, see Engine.Faults/ClearFault", r.def.ID(), r.def.Kind(), r.streak))
	}
}

func (e *Engine) tickRecovered(r *registration, t Ticked, now time.Time) (panicked bool, panicVal any) {
	defer func() {
		if rec := recover(); rec != nil {
			panicked = true
			panicVal = rec
			logger.Error(fmt.Sprintf("recovered from panic ticking definition %q (kind=%s): %v\n%s", r.def.ID(), r.def.Kind(), rec, debug.Stack()))
		}
	}()
	t.Tick(now)
	return false, nil
}

// evaluateOne is the one-recover-boundary-per-definition promised by
// #398: a panic inside r.def.Evaluate is contained here and never
// escapes to end the whole Run loop (see evaluateRecovered), the same
// reasoning detect.Detector.observeRecovered and
// watchlist.Evaluator.evaluateRecovered give for isolating recovery to a
// single unit of work rather than deferring it in Run itself.
//
// A definition already marked faulted is skipped entirely -- faulted
// means "stopped evaluating this", not "keep panicking on it forever".
func (e *Engine) evaluateOne(r *registration, ev store.Event) {
	e.mu.Lock()
	if r.fault != nil {
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

	panicked, panicVal := e.evaluateRecovered(r, ev)

	e.mu.Lock()
	defer e.mu.Unlock()
	if !panicked {
		r.streak = 0
		return
	}
	r.streak++
	if r.streak >= faultThreshold {
		r.fault = &Fault{
			DefinitionID: r.def.ID(),
			Kind:         r.def.Kind(),
			Reason:       fmt.Sprintf("panicked on %d consecutive evaluations (last: %v)", r.streak, panicVal),
			At:           time.Now(),
		}
		logger.Error(fmt.Sprintf("definition %q (kind=%s) marked faulted after %d consecutive panics -- skipped until cleared; this is a coverage hole, see Engine.Faults/ClearFault", r.def.ID(), r.def.Kind(), r.streak))
	}
}

// evaluateRecovered calls r.def.Evaluate(ev), recovering and logging any
// panic with the definition's id and kind (the operator-actionable
// detail a bare recover would lose) rather than letting it escape and
// end the evaluation goroutine for good -- recover only unwinds as far
// as the nearest deferring function, so the defer has to live here, not
// in Run or evaluateEvent.
func (e *Engine) evaluateRecovered(r *registration, ev store.Event) (panicked bool, panicVal any) {
	defer func() {
		if rec := recover(); rec != nil {
			panicked = true
			panicVal = rec
			logger.Error(fmt.Sprintf("recovered from panic evaluating definition %q (kind=%s): %v\n%s", r.def.ID(), r.def.Kind(), rec, debug.Stack()))
		}
	}()
	r.def.Evaluate(ev)
	return false, nil
}

// Faults returns a snapshot of every currently-faulted definition,
// sorted by ID for a deterministic read -- the engine's API always
// reports its own coverage holes (see Fault's doc comment) rather than
// requiring a caller to know which definitions to ask about.
func (e *Engine) Faults() []Fault {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Fault, 0)
	for _, r := range e.defs {
		if r.fault != nil {
			out = append(out, *r.fault)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DefinitionID < out[j].DefinitionID })
	return out
}

// Learning answers id's learning-window state as of now (issue #639):
// an e.mu-guarded lookup in the existing defs map, type-asserting
// LearningReporter -- the same lookup-and-assert shape ClearFault uses,
// not a new traversal. ok is false for an unknown/unregistered id and
// for a definition with no warm-up concept (most of the catalogue),
// which is what lets a caller (see api.Server's narrow Learning field)
// omit the API field entirely rather than send a misleading zero value.
//
// No per-event cost: this is read only from admin API handlers, never
// from evaluateEvent's own hot path. A nil *Engine answers ok=false,
// same convention as Nudge/Tick above, so a caller need not nil-check
// before calling.
func (e *Engine) Learning(id string, now time.Time) (LearningState, bool) {
	if e == nil {
		return LearningState{}, false
	}
	e.mu.Lock()
	r, ok := e.defs[id]
	e.mu.Unlock()
	if !ok {
		return LearningState{}, false
	}
	lr, ok := r.def.(LearningReporter)
	if !ok {
		return LearningState{}, false
	}
	return lr.Learning(now)
}

// ClearFault re-arms a faulted definition -- the only way a fault is
// ever lifted, per #398's decided policy: explicit operator action (or a
// definition edit that calls this) never a timer, so a deterministic
// panic can't become a periodic CPU/log burn that nobody chose. Resets
// the consecutive-panic streak too, so re-arming gives the definition a
// genuinely clean slate rather than one panic away from faulting again.
//
// Reports whether id was actually faulted (and therefore cleared);
// clearing an unfaulted or unknown id is a no-op that reports false.
func (e *Engine) ClearFault(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, ok := e.defs[id]
	if !ok || r.fault == nil {
		return false
	}
	r.fault = nil
	r.streak = 0
	return true
}
