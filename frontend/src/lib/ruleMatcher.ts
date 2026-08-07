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

  constructor(factory: WorkerFactory = defaultFactory) {
    this.factory = factory
  }

  private ensure(): WorkerLike {
    if (!this.worker) this.worker = this.factory()
    return this.worker
  }

  private kill() {
    this.worker?.terminate()
    this.worker = null
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
        resolve(outcome)
      }

      const timer = setTimeout(() => {
        // Still spinning on the pattern, and it will never answer.
        // Killing it is the guarantee.
        this.kill()
        finish({ status: 'too-slow' })
      }, timeoutMs)

      worker.onmessage = (e) => {
        const data = e.data as { id: number; ids?: number[]; invalid?: boolean }
        // A late reply from a superseded request must not resolve this
        // one. The user types, so patterns supersede each other often.
        if (data.id !== id) return
        if (data.invalid) {
          finish({ status: 'invalid' })
          return
        }
        finish({ status: 'ok', ids: data.ids ?? [] })
      }
      worker.onerror = () => {
        this.kill()
        finish({ status: 'invalid' })
      }

      worker.postMessage({ id, pattern, candidates })
    })
  }

  dispose() {
    this.kill()
  }
}
