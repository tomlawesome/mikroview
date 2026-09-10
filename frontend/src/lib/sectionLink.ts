// SPDX-License-Identifier: AGPL-3.0-only

// #1001: one way to send the operator from a surface that states a
// problem to the surface that explains it. The ingest-loss banners are
// the first caller -- the ratified drawing at
// docs/design/concepts/ingest-loss-995 gives them a `details` link into
// the Settings readout -- but the mechanism is deliberately not theirs:
// view-jumps were already being written by hand in five places in
// Flags.svelte, and the scroll half in Watchlist.svelte, so a second
// ad-hoc pair here would have made seven.
//
// A target is named, not described. Callers pass a SectionId, so a
// typo is a compile error rather than a link that silently lands at the
// top of a page; a target registers by carrying the anchor below as its
// DOM id. The registry is static on purpose -- greppable in both
// directions, and runtime registration would be machinery for a
// problem one entry does not have.

import { tick } from 'svelte'
import { appState, type View } from './state.svelte'

export const SECTION_TARGETS = {
  // The anchor lives on EngineRoom.svelte's ingest .stsection.
  'engineroom/ingest': { view: 'engineroom', anchor: 'engineroom-ingest' },
} as const satisfies Record<string, { view: View; anchor: string }>

export type SectionId = keyof typeof SECTION_TARGETS

// How long to keep looking for the anchor after the view changes. The
// ingest section renders unconditionally once EngineRoom mounts, so
// this only has to absorb mount timing, not wait on data.
const ANCHOR_DEADLINE_MS = 1000

// goToSection switches to the target's view and scrolls its section
// into the middle of the viewport. Returns whether the anchor was
// found: callers ignore it -- landing on the right view is most of the
// value, and the drawing offers no error state for the rest -- but a
// test can assert on it.
export async function goToSection(id: SectionId): Promise<boolean> {
  const target = SECTION_TARGETS[id]
  appState.view = target.view
  await tick()

  return new Promise((resolve) => {
    const deadline = Date.now() + ANCHOR_DEADLINE_MS
    const look = () => {
      const el = document.getElementById(target.anchor)
      if (el) {
        // block: 'center' rather than the default 'start', so a sticky
        // header cannot cover what we just navigated to -- the same
        // reason Watchlist.svelte:1040 uses it.
        el.scrollIntoView({ block: 'center' })
        resolve(true)
        return
      }
      if (Date.now() >= deadline) {
        resolve(false)
        return
      }
      requestAnimationFrame(look)
    }
    look()
  })
}
