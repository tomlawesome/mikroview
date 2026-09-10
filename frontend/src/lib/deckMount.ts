// SPDX-License-Identifier: AGPL-3.0-only
//
// Deck.svelte's own mount rule (#690, replacing the old near()). A
// card's scene mounts once it's the one actually visited, or while the
// deck is physically rolling it into or out of view -- adjacency to the
// visited card is no longer enough by itself. The old rule mounted
// every card within one index of the active one regardless of whether
// anyone had scrolled toward it, which paid a full scene mount (worst
// on the docket's unvirtualised Flags list, #690's own measurement) for
// cards nobody had visited yet. Deck.svelte tracks "physically rolling
// past" with a low-threshold IntersectionObserver (see visibleKeys
// there) and passes the live set in here; this function stays pure so
// the rule itself is unit-testable without mounting any component.
export function deckCardMounted(
  index: number,
  activeIndex: number,
  cardKey: string | undefined,
  visibleKeys: ReadonlySet<string>,
): boolean {
  if (index === activeIndex) return true
  if (cardKey === undefined) return false
  return Math.abs(index - activeIndex) <= 1 && visibleKeys.has(cardKey)
}
