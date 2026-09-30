// SPDX-License-Identifier: AGPL-3.0-only
//
// The wizard's way in and way out (#1386): the state the journey runs
// on, and its two doors. DESIGN.md, "The way in, and the way out": one
// journey, played in on Enter at the door and again on Finish, onto the
// fall; both a short crossfade under reduced motion.
//
// Not to be confused with lib/journey.svelte.ts (#646), the older
// brand-new-instance walk. Its pre-wizard beats are retired (owner,
// 2026-09-30, on #1386): a brand-new install now plays this way in
// straight after the admin account is made (AuthSetup's Continue).
//
// The sequence on the way in: a sign-in that lands a session -- the
// password alone, or the second factor after it, or AuthSetup's
// Continue -- calls signedIn(); the door stays mounted (holdDoor) while the shell loads
// underneath; once the ledger and the device list are known,
// WizardJourney.svelte asks decide(): if the wizard would auto-launch,
// the journey launches it and plays (the wizard mounts under the door,
// hidden until the swap); otherwise the door is simply released, the
// shell being already there.

import { appState } from './state.svelte'
import { authState } from './auth.svelte'
import { wizardState } from './wizard.svelte'
import { reducedMotion } from './wizardJourney'

export type JourneyPhase = 'idle' | 'in' | 'out'

class WizardJourneyState {
  phase = $state<JourneyPhase>('idle')
  /** the sign-in door stays mounted over the shell while this is true */
  holdDoor = $state(false)
  /** a successful sign-in is waiting for the shell to load before deciding */
  pending = $state(false)
  /** how far the door's stack (in) or the wizard's main (out) has slid */
  slideY = $state(0)
  /** the arriving groups: the bar's chips, the strip; body rows strike individually */
  bar = $state(false)
  strip = $state(false)
  /** the fall's body and axis, on the way out */
  fallBody = $state(false)
  /** the step rows have struck; the rail stands (the way in) */
  rows = $state(false)
  /** the ride has landed on the bar; the bar's own wordmark takes over */
  landed = $state(false)

  /** WizardJourney.svelte registers the way out here while mounted, so
   * wizardRun.finish() can ask for it without importing a component. */
  wayOutHandler: ((swap: () => void) => boolean) | null = null

  get active(): boolean {
    return this.phase !== 'idle'
  }

  /** enter is the door's Enter: a sign-in just succeeded. */
  enter(): void {
    if (this.active) return
    this.pending = true
    this.holdDoor = true
  }

  /** signedIn arms the way in once a sign-in step has settled, if it
   * actually landed an admin session. Called after every step that can
   * finish signing in, not only the password: with a second factor
   * owed the password step leaves 'pending-factor' and no role yet. */
  signedIn(): void {
    if (authState.state === 'authenticated' && authState.isAdmin) this.enter()
  }

  /** decide is called once the shell has loaded under the held door.
   * Returns 'play' when the wizard is launching and the journey should
   * run, 'release' when the door simply comes down. */
  decide(): 'play' | 'release' {
    if (!this.pending) return 'release'
    this.pending = false
    if (!wizardState.wouldAutoLaunch(appState.devices.length > 0)) {
      this.holdDoor = false
      return 'release'
    }
    wizardState.markAutoLaunchSpent()
    if (reducedMotion()) {
      // The short crossfade: the door comes down and the wizard's own
      // entrance (Wizard.svelte's away -> live) is all that plays.
      this.holdDoor = false
      wizardState.launch()
      return 'release'
    }
    // The phase is set before the launch so Wizard.svelte mounts already
    // knowing the journey drives its entrance.
    this.phase = 'in'
    wizardState.launch()
    return 'play'
  }

  /** wayOut plays Finish's journey; `swap` (show the fall, close the
   * wizard) runs under its cover. Returns false -- and does nothing --
   * where the journey cannot play, so the caller swaps outright. */
  wayOut(swap: () => void): boolean {
    if (reducedMotion() || !this.wayOutHandler) return false
    return this.wayOutHandler(swap)
  }

  /** begin marks the journey running in a direction; end clears it. */
  begin(phase: 'in' | 'out'): void {
    this.phase = phase
    this.slideY = 0
    this.bar = false
    this.strip = false
    this.fallBody = false
    this.rows = false
    this.landed = false
  }

  end(): void {
    this.phase = 'idle'
    this.holdDoor = false
    this.pending = false
    this.slideY = 0
    this.bar = false
    this.strip = false
    this.fallBody = false
    this.rows = false
    this.landed = false
  }

  /** the door is gone (the swap on the way in) */
  dropDoor(): void {
    this.holdDoor = false
  }
}

export const wizardJourney = new WizardJourneyState()
