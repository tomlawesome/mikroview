// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { deckCardMounted } from './deckMount'

// #690: Deck.svelte used to mount a card's scene as soon as it became a
// neighbour of the active card (within one index, always) rather than
// when it was actually visited -- expensive for cards nobody scrolled
// toward, worst for the docket's unvirtualised Flags list. These pin
// the replacement rule: visited always mounts, a neighbour mounts only
// while the deck is physically rolling it into or out of view.
describe('deckCardMounted (#690)', () => {
  it('mounts the visited card even if nothing reports it visible yet', () => {
    expect(deckCardMounted(2, 2, 'metrics', new Set())).toBe(true)
  })

  it('does not mount a neighbouring card merely for being adjacent', () => {
    // index 3 is one away from the active index 2 (a neighbour), but
    // never appears in the visible set -- sitting at rest, nobody has
    // scrolled toward it.
    expect(deckCardMounted(3, 2, 'live', new Set())).toBe(false)
  })

  it('mounts a neighbouring card while the roll is carrying it through view', () => {
    expect(deckCardMounted(3, 2, 'live', new Set(['live']))).toBe(true)
  })

  it('does not mount a card two or more indices away even if it is (wrongly) reported visible', () => {
    expect(deckCardMounted(4, 2, 'docket', new Set(['docket']))).toBe(false)
  })

  it('treats a card with no key as unmountable', () => {
    expect(deckCardMounted(3, 2, undefined, new Set())).toBe(false)
  })

  it('stops mounting a former neighbour once it scrolls fully out of view', () => {
    // Simulates the docket having been visible mid-roll and then
    // settling back out: once its key drops out of the visible set it
    // no longer qualifies, even though it's still adjacent.
    expect(deckCardMounted(4, 3, 'docket', new Set())).toBe(false)
  })
})
