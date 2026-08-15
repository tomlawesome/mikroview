// SPDX-License-Identifier: AGPL-3.0-only

// Rule-regex matching, moved off the main thread (issue #157).
//
// What this replaces: isSafeRulePattern, a structural scan that rejected
// the shapes behind catastrophic backtracking -- a quantified group whose
// body holds another quantifier or an alternation. It caught every
// practical payload, and its own comment admitted it could not prove an
// accepted pattern was fast. A screen that is mostly right is the wrong
// shape for "this input decides whether the browser keeps responding":
// the failure is total, and the attacker chooses the input.
//
// This is a guarantee rather than a screen. The pattern is compiled and
// run inside a Worker, and the Worker is killed if it overruns. The main
// thread never executes a user-supplied regex, so no pattern can hang it
// -- including shapes nobody has thought of, which is exactly what a
// screen can never cover.
//
// The Worker is deliberately stateless: it is handed a pattern and a
// batch of events and answers which ones matched. Keeping the resulting
// Set on the main thread means eviction is handled where eviction already
// happens (AppState's slice(-MAX_CLIENT_EVENTS)) instead of being a
// second thing to keep in step across a process boundary -- and it means
// a terminated Worker loses nothing.

/** How long one batch gets before its Worker is terminated. */
export const RULE_MATCH_TIMEOUT_MS = 500

/** What the matcher needs from an event. */
export type MatchCandidate = { id: number; ruleLabel: string; raw: string }

/**
 * matchingIds is the whole of the matching logic, kept pure so it can be
 * tested directly and so the Worker entry point stays a thin shim --
 * anything running in there is unkillable short of terminating the whole
 * Worker, so there should be as little of it as possible.
 *
 * Throws on an invalid pattern, which the caller reports as such.
 */
export function matchingIds(pattern: string, candidates: readonly MatchCandidate[]): number[] {
  const re = new RegExp(pattern, 'i')
  const out: number[] = []
  for (const c of candidates) {
    if (re.test(c.ruleLabel) || re.test(c.raw)) out.push(c.id)
  }
  return out
}

export type MatchOutcome =
  | { status: 'ok'; ids: number[] }
  | { status: 'invalid' }
  | { status: 'too-slow' }

type WorkerLike = {
  postMessage(message: unknown): void
  terminate(): void
  onmessage: ((e: { data: unknown }) => void) | null
  onerror: ((e: unknown) => void) | null
}

/** Injected in tests; jsdom has no Worker. */
export type WorkerFactory = () => WorkerLike

function defaultFactory(): WorkerLike {
  return new Worker(new URL('./ruleMatcher.worker.ts', import.meta.url), {
    type: 'module',
  }) as unknown as WorkerLike
}

/**
 * RuleMatcher owns one Worker and replaces it whenever a pattern has to
 * be killed.
 *
 * A terminated Worker cannot be reused, and that is the point: the
 * runaway regex dies with it. Recreating one costs a few milliseconds and
 * only happens when a pattern actually misbehaves.
 */
export class RuleMatcher {
  private worker: WorkerLike | null = null
  private seq = 0
  private readonly factory: WorkerFactory
  // In-flight requests, keyed by the id sent to the worker.
  //
  // The handlers used to be assigned per run() on the shared worker, so
  // two overlapping calls meant the second one's assignment replaced the
  // first's -- and the first could then only ever settle via its own
  // timeout, reporting 'too-slow'. Per applyFilters' contract that
  // silently drops the active regex filter and shows every event
  // unfiltered while the UI claims the pattern was too slow, which bites
  // exactly when a filter matters most: state.svelte.ts's flushIncoming
  // fires every 175ms without awaiting, so any round trip longer than
  // that overlaps. One handler dispatching by id has no such window.
  private readonly pending = new Map<number, (outcome: MatchOutcome) => void>()

  constructor(factory: WorkerFactory = defaultFactory) {
    this.factory = factory
  }

  private ensure(): WorkerLike {
    if (!this.worker) {
      const worker = this.factory()
      worker.onmessage = (e: { data: unknown }) => {
        const data = e.data as { id: number; ids?: number[]; invalid?: boolean }
        const settle = this.pending.get(data.id)
        // A late reply from a request that already timed out, or from a
        // worker replaced since. Nothing to resolve.
        if (!settle) return
        if (data.invalid) {
          settle({ status: 'invalid' })
          return
        }
        settle({ status: 'ok', ids: data.ids ?? [] })
      }
      worker.onerror = () => {
        // The worker itself failed, which tells us nothing about which
        // request caused it -- so every in-flight one is unanswerable.
        this.kill('invalid')
      }
      this.worker = worker
    }
    return this.worker
  }

  // kill terminates the worker and settles everything still waiting on
  // it. Without that last part a killed worker left its callers pending
  // until their own timeouts, which is the stall this exists to end.
  private kill(status: 'too-slow' | 'invalid' = 'too-slow') {
    this.worker?.terminate()
    this.worker = null
    const waiting = [...this.pending.values()]
    this.pending.clear()
    const outcome: MatchOutcome = status === 'invalid' ? { status: 'invalid' } : { status: 'too-slow' }
    for (const settle of waiting) settle(outcome)
  }

  run(
    pattern: string,
    candidates: readonly MatchCandidate[],
    timeoutMs = RULE_MATCH_TIMEOUT_MS,
  ): Promise<MatchOutcome> {
    const id = ++this.seq
    const worker = this.ensure()

    return new Promise<MatchOutcome>((resolve) => {
      let settled = false
      const finish = (outcome: MatchOutcome) => {
        if (settled) return
        settled = true
        clearTimeout(timer)
        this.pending.delete(id)
        resolve(outcome)
      }

      const timer = setTimeout(() => {
        // Still spinning on the pattern, and it will never answer.
        // Killing it is the guarantee.
        finish({ status: 'too-slow' })
        this.kill()
      }, timeoutMs)

      this.pending.set(id, finish)
      worker.postMessage({ id, pattern, candidates })
    })
  }

  dispose() {
    this.kill()
  }
}

/**
 * mergeOutcome decides what a match result means for the current state.
 *
 * Extracted because getting it wrong is invisible: an earlier version
 * swallowed a non-ok outcome during incremental classification, leaving
 * the previous set in place. The newly-arrived events were then treated
 * as non-matching and quietly hidden, with nothing to say the filter had
 * stopped being accurate. Found only by driving a real browser -- the
 * pattern was cheap against the events already buffered and catastrophic
 * against ones that arrived afterwards, because the cost is a property of
 * the subject, not only of the pattern.
 *
 * Any outcome that is not 'ok' drops the filter and surfaces why.
 */
export function mergeOutcome(
  outcome: MatchOutcome,
  previous: ReadonlySet<number> | null,
  incremental: boolean,
): { matches: ReadonlySet<number> | null; status: 'idle' | 'invalid' | 'too-slow' } {
  if (outcome.status !== 'ok') {
    return { matches: null, status: outcome.status }
  }
  if (!incremental) return { matches: new Set(outcome.ids), status: 'idle' }
  const next = new Set(previous ?? [])
  for (const id of outcome.ids) next.add(id)
  return { matches: next, status: 'idle' }
}
