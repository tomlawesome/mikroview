// SPDX-License-Identifier: AGPL-3.0-only

// Package engine is the evaluation chassis described in
// docs/decisions/evaluation-engine.md -- the machine internal/detect and
// internal/watchlist each build by hand today, unified into one place.
// This first slice (#398) is deliberately just the plumbing: an ingest
// queue with one backpressure policy, one run/shutdown lifecycle, and
// one panic boundary. It carries no evaluation semantics at all --
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

// queueSize bounds the engine's ingest queue (see Enqueue/Run). This is
// the one place that reasoning is stated now -- it used to be written
// twice, independently, by detect.observeQueueSize and
// watchlist.evalQueueSize, the latter's own comment noting it "mirrors
// internal/detect.observeQueueSize's own sizing reasoning exactly".
//
// Sized to the same tier as main.go's raw syslog channel (4096), since
// Enqueue is offered once per stored event -- the same rate as ingestion
// itself, unlike internal/notify's much smaller queue, which only
// receives newly-raised flags, a far rarer event. It is a generous burst
// absorber, not a guarantee against sustained overload: nothing bounded
// can be one.
//
// A var, not a const, so tests can shrink it -- same convention as
// internal/detect.maxTrackedSources -- without needing thousands of
// events to fill the queue.
var queueSize = 4096

// dropLogInterval rate-limits the overload log line Enqueue emits on a
// full queue. Unifies detect.observeQueueDropLogInterval and
// watchlist.evalQueueDropLogInterval, which stated the same 30-second
// reasoning twice: logging every single drop would itself add load
// during exactly the sustained-overload condition being reported, so a
// periodic summary is enough to make an otherwise-invisible "evaluation
// silently fell behind" condition observable without that cost.
const dropLogInterval = 30 * time.Second

// drainTimeout bounds how long Run keeps draining the queue after ctx is
// cancelled, before drain gives up and Run returns anyway.
//
// This is a decision, not a detail: draining forever ("finish
// everything queued, however long that takes") can hang process exit on
// an unbounded backlog, and dropping everything the instant ctx cancels
// throws away events that were already accepted and are typically cheap
// to finish evaluating. A short bounded window gets the common case (a
// queue that is mostly empty, or catches up in milliseconds) fully
// drained, while capping worst-case shutdown latency to something in
// the same order as the rest of mikroview's shutdown sequence -- see
// main.go's own 5-second graceful-shutdown budget for httpServer.
//
// A var, not a const, so lifecycle tests can shrink it and stay fast
// rather than actually waiting out the production bound -- same
// convention as queueSize above.
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

// Engine is the chassis: one ingest queue, one backpressure policy, one
// run/shutdown lifecycle, one panic boundary per evaluated definition.
// See the package doc comment for what it deliberately does not do yet.
type Engine struct {
	queue chan store.Event
	done  chan struct{}

	dropped  atomic.Uint64
	dropGate *logging.Limiter

	mu   sync.Mutex
	defs map[string]*registration
	// order is defs' values sorted by (Ordered rank, ID) -- maintained
	// on Register rather than sorted per event, since the definition set
	// changes on an operator action and the sort runs on every single
	// ingested event. See Ordered for why the order has to exist at all.
	order []*registration
}

// New constructs an Engine with an empty queue and no registered
// definitions -- evaluating nothing until something registers one.
func New() *Engine {
	return &Engine{
		queue:    make(chan store.Event, queueSize),
		done:     make(chan struct{}),
		dropGate: logging.NewLimiter(dropLogInterval),
		defs:     make(map[string]*registration),
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

// Enqueue hands ev off to the evaluation goroutine (see Run) without
// ever blocking the caller -- a non-blocking select/default send,
// dropping ev if the queue is full. mikroview's ingest goroutine calls
// this the same way it calls detect.Detector.Enqueue and
// watchlist.Evaluator.Enqueue: a dropped event is still stored and
// broadcast normally, it just never reaches evaluation.
//
// A nil *Engine is a valid no-op, same convention
// watchlist.Evaluator.Enqueue uses for a nil receiver -- callers (tests
// in particular) that don't need the chassis at all can pass nil rather
// than constructing one solely to satisfy the signature.
func (e *Engine) Enqueue(ev store.Event) {
	if e == nil {
		return
	}
	select {
	case e.queue <- ev:
	default:
		e.recordDropped()
	}
}

// recordDropped tracks an Enqueue drop and logs a rate-limited summary
// that says what was actually lost -- detection/evaluation for those
// events, not merely "queue full". #380's first item is why this
// matters: the observable symptom of a starved evaluator is otherwise
// silence, and silence reads as "nothing is wrong" rather than as the
// coverage gap it is.
func (e *Engine) recordDropped() {
	total := e.dropped.Add(1)
	if _, ok := e.dropGate.Allow(); ok {
		logger.Warn(fmt.Sprintf("engine queue full -- %d event(s) dropped, detection/evaluation skipped for them (still stored/broadcast normally)", total))
	}
}

// Dropped reports how many events Enqueue has dropped since the engine
// was constructed, so a later issue can surface it (e.g. "dropped N
// events in the last hour") rather than leaving it visible only in the
// rate-limited log line above.
func (e *Engine) Dropped() uint64 {
	return e.dropped.Load()
}

// Run drains the queue, evaluating each event against every registered
// definition in turn, until ctx is done -- at which point it drains
// whatever is already queued for up to drainTimeout before stopping.
// Meant to run in its own goroutine, separate from whatever goroutine
// calls Enqueue, the same shape as detect.Detector.Run and
// watchlist.Evaluator.Run.
//
// Run closes the channel Done returns exactly once, on its way out --
// so a caller (main.go) can join on the engine having actually stopped
// rather than firing and forgetting.
func (e *Engine) Run(ctx context.Context) {
	defer close(e.done)
	for {
		select {
		case ev := <-e.queue:
			e.evaluateEvent(ev)
		case <-ctx.Done():
			e.drain()
			return
		}
	}
}

// Done returns a channel that is closed once Run has returned -- the
// join primitive main.go needs to observe shutdown completing, since a
// goroutine simply returning is otherwise unobservable from outside it.
func (e *Engine) Done() <-chan struct{} {
	return e.done
}

// drain evaluates whatever is already sitting in the queue when Run's
// ctx is cancelled, stopping as soon as the queue is empty or
// drainTimeout elapses, whichever comes first -- see drainTimeout's doc
// comment for why neither "drain everything" nor "drop everything" is
// the right unconditional answer.
func (e *Engine) drain() {
	deadline := time.Now().Add(drainTimeout)
	for {
		select {
		case ev := <-e.queue:
			e.evaluateEvent(ev)
		default:
			return
		}
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
