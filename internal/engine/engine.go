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
	def    Evaluated
	streak int
	fault  *Fault
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
	e.defs[d.ID()] = &registration{def: d}
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
	regs := make([]*registration, 0, len(e.defs))
	for _, r := range e.defs {
		regs = append(regs, r)
	}
	e.mu.Unlock()

	for _, r := range regs {
		e.evaluateOne(r, ev)
	}
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
