/**
 * The `expected ▸` action's scope and wording, shared by the 2D rib card
 * (Topography.svelte) and the city road card (City.svelte).
 *
 * One module because the two views are one product: a line has to read
 * the same on either side of the altitude slider, and two copies of this
 * wording drift apart the first time one of them is edited.
 *
 * **Per-line is the default** (owner decision, 2026-09-07, #1016). This
 * screen is a sieve -- "make it easy to see potential threats and to
 * whittle down the noise to genuine potential threats, without
 * arbitrarily hiding stuff that could be bad". Waving every line on an
 * element through on one click can retire something nobody looked at, so
 * marking more than one line is only ever reachable from a control that
 * says out loud how many it covers.
 */

/** What a submit covers: the one line named, or every line listed. */
export type ExpectedScope = { kind: 'one'; key: string } | { kind: 'all' }

/**
 * The lines a submit will actually write, in the order the card lists
 * them. `one` never widens: an unknown key -- the register refreshed
 * under an open form -- covers nothing rather than falling back to the
 * whole list.
 */
export function expectedTargets<T extends { key: string }>(scope: ExpectedScope, lines: readonly T[]): T[] {
  if (scope.kind === 'all') return [...lines]
  const only = lines.find((l) => l.key === scope.key)
  return only ? [only] : []
}

/**
 * The per-line control, one per listed line. Plain and unqualified,
 * because it is the default and it only ever marks the line it sits on.
 */
export const EXPECTED_ONE_LABEL = 'expected ▸'

/**
 * The bulk control. It leads with a different verb and carries its own
 * count, so it cannot be misread as the single-line action above it --
 * clicking something that looks like one line's action must never mark
 * several.
 */
export function expectedAllLabel(n: number): string {
  return `mark all ${n} expected ▸`
}

/**
 * Whether the bulk control is offered at all. With one line off the
 * baseline it would be a second button doing exactly what the first one
 * does, under a count of one.
 */
export function offersExpectedAll(n: number): boolean {
  return n > 1
}

/** The form's own label, the same on both surfaces. */
export const EXPECTED_LABEL = 'EXPECTED — WHY?'

/**
 * What the reason box asks for. It names the scope again, so an operator
 * who opened the bulk form and then looked away is told what they are
 * about to speak for before they type.
 */
export function expectedPlaceholder(scope: ExpectedScope, count: number): string {
  return scope.kind === 'one' ? 'why this line is meant to be here…' : `why these ${count} lines are meant to be here…`
}

/**
 * Who the statement is recorded as, and exactly what it covers. The
 * server stamps the author from the session; this is the card saying
 * back what it is about to do.
 */
export function expectedWho(username: string, scope: ExpectedScope, count: number): string {
  const who = `as ${username || 'you'}`
  return scope.kind === 'one' ? `${who} · this line only` : `${who} · all ${count} of these lines`
}
