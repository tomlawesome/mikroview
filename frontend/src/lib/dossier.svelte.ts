// SPDX-License-Identifier: AGPL-3.0-only

import { fetchHostDossier } from './api'
import type { HostDossier } from './types'

// Drives the single HostDossier sheet mounted at the app root (see
// App.svelte) -- the same singleton-plus-trigger shape as
// lib/ipLookup.svelte.ts and lib/nameEditor.svelte.ts, so only one card
// can ever be open and none of the four places that open it
// (the investigate popover, the Entities host row, the city building
// card, the topography host card) owns any state of its own.
//
// The trigger element is remembered so closing puts focus back where it
// came from. A dossier is opened from a map dot or a table row, and
// dropping focus to the top of the document on Esc would lose the
// operator's place in whatever they were reading.
class DossierState {
  ip = $state<string | null>(null)
  data = $state<HostDossier | null>(null)
  loading = $state(false)
  error = $state<string | null>(null)

  private trigger: HTMLElement | null = null
  private requestId = 0

  get isOpen(): boolean {
    return this.ip !== null
  }

  open(ip: string, trigger?: HTMLElement | null) {
    // Only remember the first trigger of an open run: re-opening for
    // another host from inside the card itself would otherwise leave
    // focus pointing at something in the card that is about to go.
    if (this.ip === null) this.trigger = trigger ?? null
    this.ip = ip
    this.load()
  }

  // The try-again the failure state offers. Same request, same id
  // bookkeeping, so a slow first answer landing after a retry cannot
  // overwrite the retry's own.
  retry() {
    if (this.ip !== null) this.load()
  }

  private load() {
    const ip = this.ip
    if (ip === null) return
    this.data = null
    this.error = null
    this.loading = true

    const id = ++this.requestId
    fetchHostDossier(ip).then(
      (d) => {
        if (id !== this.requestId || this.ip !== ip) return
        this.data = d
        this.loading = false
      },
      () => {
        if (id !== this.requestId || this.ip !== ip) return
        this.error = 'Could not assemble the dossier'
        this.loading = false
      },
    )
  }

  close() {
    if (this.ip === null) return
    this.ip = null
    this.data = null
    this.error = null
    this.loading = false
    // Bumped so a request still in flight cannot land into a closed
    // card and reopen its content.
    this.requestId++
    const t = this.trigger
    this.trigger = null
    t?.focus?.()
  }
}

export const dossierState = new DossierState()
