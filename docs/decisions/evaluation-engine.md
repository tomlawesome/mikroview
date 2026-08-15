# One evaluation engine, two intents: detection and expectation

Date: 2026-08-15. Phase 1 of #385, the v0.3.0 epic. This document is the
architecture the rest of v0.3.0 is built against; per the epic, no
significant UI investment lands on the detector or watchlist surfaces
until it is ratified.

## The problem

mikroview evaluates the event stream twice, through two subsystems that
are the same machine built twice by hand:

- `internal/detect` -- seventeen detectors watching for the unexpected,
  raising flags (`internal/flags`).
- `internal/watchlist` -- operator-authored expectations about what
  *should* happen, recording observations, promotions and violations
  (`internal/matchlog`).

The duplication is not an accusation; it is documented in the code
itself. `watchlist.Evaluator`'s queue constant says it *"mirrors
internal/detect.observeQueueSize's own sizing reasoning exactly"*, and
its drop-log constant *"mirrors internal/detect.observeQueueDropLogInterval's
reasoning"*. Both subsystems carry, separately: an ingest-side queue with
backpressure and drop accounting, a run/shutdown lifecycle, panic
isolation, per-source windowed state, a persisted settings/entries store
with optimistic-concurrency versioning, and per-item enable/disable.
Every improvement to one is a manual copy to the other, or -- as the
v0.2.0 review found -- a divergence: the two sides fail differently
under a stalled backend (#377, #380), hand out state under different
aliasing rules (#376), and enforce different honesty standards about
baselines that are still warming up (#368).

Meanwhile the owner's product direction (recorded in #385) needs things
neither subsystem can host alone: fully custom detectors, a builder UI,
auto-tune with receipts, and topography's "these port-sets should flow
here" edges -- which are watchlist-shaped assertions about
detector-shaped questions.

## The decision

**One evaluation chassis. Definitions as the unit of configuration. Two
intents deciding what an evaluation produces.**

### 1. The chassis

A single engine (`internal/engine`, name bikesheddable) owns everything
both subsystems currently duplicate:

- the ingest-side queue, backpressure, drop accounting and the
  rate-limited overload log line;
- the run loop, shutdown ordering and panic isolation;
- per-source and per-target windowed state, with eviction;
- evidence accumulation (sets of ports/hosts/rules seen -- not
  last-event-wins, which is the mechanism behind #379's wrong-naming
  findings);
- baseline management with a **history floor**: no definition may make a
  baseline judgement before a stated minimum of history exists, and an
  emission made during warm-up is marked provisional. This is #368's
  fix made a chassis contract instead of a per-detector patch;
- the persisted stores (definitions, per-definition settings, engine
  state), on both backends, persisting off the hot path with a context
  deadline -- #377 and #380's stall class becomes structurally
  impossible rather than individually fixed;
- copy-on-read for everything handed out of the engine (#376's class).

`detect.Detector` and `watchlist.Evaluator` both collapse onto this
chassis. During the port their behaviour is pinned by characterization
tests (`internal/detect/characterization_test.go` exists for exactly
this purpose and grows to cover the watchlist side): **the port itself
changes no user-visible behaviour** except where an issue explicitly
says otherwise.

### 2. Definitions

A definition is one thing the engine evaluates. Every definition,
whatever its kind, carries the same envelope:

```
id, name, intent (detection | expectation)
enabled, scope (hosts AND netclass, the existing #44 model)
params      -- typed, per-definition schema; what the UI renders and
               what auto-tune adjusts
provenance  -- shipped | custom, and for shipped: the default params,
               so "reset to default" and "how far am I from stock" are
               always answerable
```

Definitions come in two kinds, and this is the load-bearing honesty of
the design:

**Declarative** -- conditions + window + threshold + emission, expressed
as data. Match conditions are structured (field, operator, value --
ports, addresses, address-list membership, chain, action, interface,
time-of-day), **not a DSL**. This kind is what the builder UI edits and
what "fully custom detectors" means in v1. The current detectors whose
logic already is threshold-over-window -- port_scan, critical_port,
repeated_drops, distributed_brute_force, dest_spread, mail_sender,
off_hours, known_bad_ip and kin -- become shipped declarative
definitions, editable and cloneable.

**Programmatic** -- built-in Go, plugged into the same chassis, wearing
the same envelope (settings, scope, tuning params, evidence,
provenance), but whose logic stays code. This kind exists because some
of what mikroview does *cannot honestly be a form*: statistical
baselines (host_baseline, global_spike, rule_spike, the EMA confidence
machinery), absence-of-events detectors (device_silence, stale_rule --
"nothing arrived" is not a predicate over an event), and
external-data lookups (reputation). Pretending these are declarative
would either dumb them down or turn the condition language into a
programming language; both are worse than saying plainly that some
definitions are built in.

The inverted watchlist entry -- the observed/permitted/violation state
machine with live SourceList resolution -- is a **programmatic
expectation definition**. Non-inverted entries are declarative
expectation definitions.

### 3. Intents

Intent decides what an emission feeds, and nothing else:

- **detection** → the flag lifecycle: raise/re-fire/clear, count,
  confidence, reputation floor, exclusions -- `internal/flags`
  unchanged in role.
- **expectation** → the match log: observations, promotions,
  violations -- `internal/matchlog` unchanged in role.

The UI keeps the two intents distinct (Detect and Expect stay separate
sections; #385 phase 2), because operators reason in those two modes.
The *engine* does not care; a definition is a definition. That is the
entire trick: shared machinery below, preserved meaning above.

Exclusions become suppressions scoped to a definition -- which is what
"exclusions live with the feature they exclude for" (#385) means at the
data layer.

### 4. The tuning surface, because auto-tune builds on it

Every definition exposes, uniformly: enabled, scope, its typed params,
and **replay** -- "over the stored event window, these params would have
emitted N times; here are the emissions". Replay is the receipts
mechanism #385 phase 3's auto-tune is specified against ("at X this
would have fired 6 times, not 41 -- here are the 35 dropped"), and it
must be part of the engine's v1 contract even though the auto-tune UI
comes later: retrofitting replay under seventeen ported detectors would
be the expensive way round.

## Migration

- On-disk: the detector settings document (`byName` → `Settings`) and
  the watchlist entries document become a definitions store with a
  document version bump, migrated on first load, both backends. The
  migration follows the policy #378 forces on every store: an
  unreadable document **fails closed** -- it is never silently replaced.
- Shipped defaults are seeded as provenance=shipped definitions with
  today's default params. An operator's existing settings land as param
  overrides on those definitions; their watchlist entries land as
  expectation definitions. Nothing an operator authored is dropped, and
  `-backup`/`-restore` cover the definitions store from day one (#372's
  lesson).
- API: `/api/detectors` and `/api/watchlist` are replaced wholesale in
  v0.3.0 -- no compatibility routes. Owner decision, 2026-08-15: the
  sole deployment is the owner's own, and pre-1.0 removals are
  wholesale per AGENTS.md. The CHANGELOG communicates it, as always.

## What this fixes by construction

#368 (history floor), #376 (copy-on-read), #377/#380's stall items
(persist deadline off the hot path), #379's wrong-naming items (evidence
sets), and the queue/backpressure divergence. These issues close when
their phase-1 implementation lands, per #386 -- not before, and each
names the engine contract that retires it.

## What this deliberately does not do

- **No DSL.** Structured conditions only. If a real need outgrows them,
  that is a new ADR, not a quiet extension.
- **No promise that every built-in becomes declarative.** The
  programmatic kind is permanent, not transitional.
- **No behaviour changes smuggled in with the port.** Characterization
  first; intentional changes ride their own issues.
- **No new runtime dependencies**, per AGENTS.md.

## Costs, stated

- The port is large and touches the two most safety-critical consumers
  of the event stream. The characterization-test investment is the
  price of doing it without changing what operators see, and it is paid
  before the port, not after.
- Declarative dispatch must not cost what hand-fused loops do not: the
  engine pre-indexes definitions by their cheapest discriminating field
  (port, address class, chain) so an event consults the few definitions
  that could match, not all of them linearly. The existing ingest
  budget (#370's lesson: one hot-path scan can collapse throughput) is
  the benchmark gate: `make live-check`'s responsiveness assertions and
  a dispatch benchmark run before and after.
- Two kinds means two code paths to keep honest. The envelope being
  genuinely shared -- one settings model, one evidence model, one replay
  contract -- is what keeps "two kinds" from decaying back into "two
  subsystems".

## The open questions, decided (owner, 2026-08-15)

1. **The user-facing noun is "definition".** "Rule" collides fatally
   with RouterOS firewall rules, which mikroview displays constantly.
2. **Replay's corpus is the in-memory event window for v1** -- and every
   receipt states the window it covers ("would have fired 6 times in
   the last 36h"), so a short corpus can never overclaim.

   Recorded with the decision, because it shaped it: the owner's
   reservation that this fails under high bandwidth, and that
   *"statistically meaningful validation improves the longer we keep"*.
   Both are correct -- a receipt computed over one quiet day is a weaker
   basis for loosening a threshold than one computed over a fortnight
   that saw a weekend, a scan, and a backup window. So longer on-disk
   event retention is **an anticipated future decision, not a rejected
   one**: nothing in v1's design may assume the replay corpus is only
   ever the memory window, and when receipts prove too thin in
   practice, retention gets its own ADR with real sizing numbers from
   the running instance rather than estimates.
3. **No API compatibility routes.** See Migration above -- wholesale,
   per AGENTS.md, sole-deployment reality acknowledged.
