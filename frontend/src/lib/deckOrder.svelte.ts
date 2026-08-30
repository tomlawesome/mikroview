// SPDX-License-Identifier: AGPL-3.0-only
//
// The deck's card order (#633, rounds 23-25: "the order you keep them —
// drag to reorder; sign-in lands on the first"). Same
// small-module-per-preference shape as flagLayout.svelte.ts -- persisted
// per browser, not synced anywhere.

const STORAGE_KEY = 'mikroview-deck-order'

// The ratified default order -- deckCards.ts renders from this module's
// order, not the other way round, so this list is the one source of the
// default (a bare string[] to keep this module import-free).
const DEFAULT_ORDER = ['fall', 'metrics', 'live', 'docket']

function loadInitial(): string[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed) && parsed.every((k) => typeof k === 'string')) return normalize(parsed)
    }
  } catch {
    // ignore -- fall through to the default
  }
  return [...DEFAULT_ORDER]
}

// A stored order survives cards being added or removed: unknown keys
// drop, missing keys append in their default position's relative order.
function normalize(keys: string[]): string[] {
  const known = keys.filter((k) => DEFAULT_ORDER.includes(k))
  const missing = DEFAULT_ORDER.filter((k) => !known.includes(k))
  return [...known, ...missing]
}

class DeckOrderState {
  order = $state<string[]>(loadInitial())

  set(keys: string[]) {
    this.order = normalize(keys)
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(this.order))
    } catch {
      // storage unavailable -- won't persist across reloads
    }
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
