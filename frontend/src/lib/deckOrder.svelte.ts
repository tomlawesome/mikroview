// SPDX-License-Identifier: AGPL-3.0-only
//
// The deck's card order (#633, rounds 23-25: "the order you keep them —
// drag to reorder; sign-in lands on the first"). One small module per
// preference (theme, colorway, retention each have theirs) -- kept in
// the shared per-user preferences record (#1283), not synced any other
// way.

import { preferencesState } from './preferences.svelte'

// #1283: was its own localStorage key ('mikroview-deck-order').
const PREFS_KEY = 'deckOrder'

// The ratified default order -- deckCards.ts renders from this module's
// order, not the other way round, so this list is the one source of the
// default (a bare string[] to keep this module import-free). 'entities'
// and 'engineroom' only ever render for the user/admin tier, and
// 'fleet' only for a viewer (deckCards' own gate) -- harmless to carry
// all three here regardless of role, since apply() below only ever
// sorts the keys a session's own card list actually has. 'fleet' sits
// last deliberately, not by inheritance: the deck reads from the log
// outward -- the fall and stream first, the docket's judgements, then
// the machinery behind the log -- and Fleet is a viewer's machinery
// card exactly as Entities/Settings are a user's, so it takes the same
// tail position they do.
// 'log-every-rule' (#1134) joins the tail for the same reason: it is a
// tool for changing the router, not a reading of the log, and it is the
// edit tier's alone. A browser holding an order from before it existed
// picks it up from normalize() below, appended after the cards it
// already knows.
const DEFAULT_ORDER = [
  'fall',
  'topography',
  'metrics',
  'live',
  'docket',
  'entities',
  'engineroom',
  'log-every-rule',
  'fleet',
]

// A stored order survives cards being added or removed: unknown keys
// drop, missing keys append in their default position's relative order.
function normalize(keys: string[]): string[] {
  const known = keys.filter((k) => DEFAULT_ORDER.includes(k))
  const missing = DEFAULT_ORDER.filter((k) => !known.includes(k))
  return [...known, ...missing]
}

function sanitize(value: unknown): string[] {
  if (Array.isArray(value) && value.every((k) => typeof k === 'string')) return normalize(value)
  return [...DEFAULT_ORDER]
}

class DeckOrderState {
  order = $state<string[]>([...DEFAULT_ORDER])

  constructor() {
    preferencesState.register(PREFS_KEY, (value) => {
      this.order = sanitize(value)
    })
  }

  set(keys: string[]) {
    this.order = normalize(keys)
    preferencesState.set(PREFS_KEY, this.order)
  }

  /** Moves `key` to sit before `beforeKey` (or last when undefined). */
  move(key: string, beforeKey?: string) {
    const rest = this.order.filter((k) => k !== key)
    const at = beforeKey ? rest.indexOf(beforeKey) : rest.length
    rest.splice(at < 0 ? rest.length : at, 0, key)
    this.set(rest)
  }

  /** Sorts `items` (anything carrying a card key) into the kept order. */
  apply<T extends { key: string }>(items: T[]): T[] {
    return [...items].sort((a, b) => this.order.indexOf(a.key) - this.order.indexOf(b.key))
  }
}

export const deckOrderState = new DeckOrderState()
