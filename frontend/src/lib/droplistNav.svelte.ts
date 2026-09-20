// SPDX-License-Identifier: AGPL-3.0-only
//
// #1225's flag-drawer handoff: a flag's "block…" action knows the
// source address and a reason to pre-fill the drop list's add form
// with, and cannot reach into Droplist.svelte's own private form state
// directly -- same problem topologyNav.svelte.ts's pendingWatchDraft
// solves for "watch this source"/"watch this pathway". One-shot slot,
// same shape: the source (Flags.svelte) fills it in and calls
// goToSection('engineroom/droplist'); Droplist.svelte takes it once,
// on its own mount or the instant it changes under an already-mounted
// Settings tab, and clears it so a later, unrelated visit never
// silently reopens it. It never submits on its own -- only prefills the
// fields and focuses the address input.
export interface PendingDroplistDraft {
  cidr: string
  reason: string
  flagID?: string
}

class DroplistNavState {
  pendingDraft = $state<PendingDroplistDraft | null>(null)

  requestDraft(fill: PendingDroplistDraft) {
    this.pendingDraft = fill
  }
}

export const droplistNavState = new DroplistNavState()
