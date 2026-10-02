// SPDX-License-Identifier: AGPL-3.0-only
//
// #1363: one shared "this tab would lose work if it left right now"
// flag, replacing five separate `beforeunload` listeners -- Passkeys/
// RecoveryCodes/AuthenticatorOverlay and AuthEnrolFactor's ten-codes
// step, and LogEveryRule's unsaved export -- each wiring up its own.
// Freshness's safe-reload check (lib/freshness.svelte.ts) reads `held`
// to decide whether an upgrade can reload this tab by itself; the
// native `beforeunload` prompt installed below is the same signal,
// aimed at the browser's own navigation/close instead.
//
// A caller holds a guard for as long as its own unsaved state is live,
// releasing it the same way each of the five already did (an `$effect`
// whose cleanup fires the moment the guarded condition stops holding,
// or the component unmounts) -- just keyed by a stable id instead of
// installing a listener of its own.
import { SvelteSet } from 'svelte/reactivity'
import { configEditorState } from './configEditor.svelte'

class LeaveGuardState {
  private holders = new SvelteSet<string>()

  // ConfigEditor's own unsaved-text flag folds in here rather than
  // ConfigEditor calling hold()/release() itself: `edited` is already
  // reactive, so reading it is the whole of the integration.
  get held(): boolean {
    return this.holders.size > 0 || configEditorState.edited
  }

  hold(id: string): void {
    this.holders.add(id)
  }

  release(id: string): void {
    this.holders.delete(id)
  }
}

export const leaveGuard = new LeaveGuardState()

// Installed once, here, the moment anything imports this module --
// deliberately not behind a call from main.ts the way
// lib/cancelled.ts's installCancellationGuard is. Each of the five
// components this replaces used to own a working `beforeunload`
// listener the instant it mounted, in its own component test with no
// app shell around it; a shared listener has to keep that property; it
// cannot depend on a wiring step only main.ts performs.
if (typeof window !== 'undefined') {
  window.addEventListener('beforeunload', (e) => {
    if (!leaveGuard.held) return
    e.preventDefault()
    e.returnValue = ''
  })
}
