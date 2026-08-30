// SPDX-License-Identifier: AGPL-3.0-only
//
// The journey (#646): not a site surface, choreography across surfaces
// that mostly already exist. The door and admin-creation beats are
// #645's (AuthScreen.svelte/AuthSetup.svelte) -- this module owns what
// comes after: attach, connecting/building, the glass, the tour, and
// handing off to the full wizard, which itself ends back at the fall
// (see SetupWizard.svelte's leaveToLanding).
//
// Triggered exactly once, by AuthSetup.svelte right after a brand-new
// instance's admin account is created -- never by an ordinary sign-in.
// A returning admin with no router attached yet still gets the wizard
// offered (wizard.svelte.ts's own maybeAutoLaunch), just without this
// beat-by-beat walk: that rule predates #646 and stays exactly as it
// was for every path except the one this module owns.
import { appState } from './state.svelte'
import { authState } from './auth.svelte'
import { deckCards, type DeckCard } from './deckCards'
import { deckOrderState } from './deckOrder.svelte'
import { wizardState } from './wizard.svelte'

export type JourneyPhase = 'idle' | 'attach' | 'connecting' | 'glass' | 'touring'

// How long the "connecting and building" beat holds before it can move
// on its own. Not a wait for real evidence: #646's attach beat pastes
// only the two syslog lines (setupsteps.ts's syslogCommands), the same
// "whole of setup, for now" the owner ratified in round 27 -- a router
// whose certificate is not yet trusted will not actually reach this
// instance, so gating on a real event risks a beat that never ends. A
// fixed pause is the honest choice over a spinner that might hang
// forever; the wizard walks the fuller checklist afterward, on evidence.
export const CONNECTING_MS = 3200

class JourneyState {
  phase = $state<JourneyPhase>('idle')
  cardIndex = $state(0)

  get active(): boolean {
    return this.phase !== 'idle'
  }

  // The deck's own real card list, in the operator's own kept order --
  // the same table Deck.svelte itself renders from, so the tour's "1 OF
  // N" always matches what the roll rail actually shows (#647 grew this
  // to seven for an admin; nothing here hardcodes a count).
  get cards(): DeckCard[] {
    return deckOrderState.apply(deckCards(authState.role === 'admin'))
  }

  // begin is called once, by AuthSetup.svelte, right after register()
  // succeeds. A page reload mid-journey (or any other sign-in) never
  // calls this -- it lands on the ordinary authenticated app, and
  // wizard.svelte.ts's maybeAutoLaunch covers "no router yet" from there.
  begin() {
    this.phase = 'attach'
    this.cardIndex = 0
  }

  // fromAttach moves to the connecting beat once the operator has (or
  // says they have) pasted the two lines. Rolls the deck to the fall
  // first, since beats 4 and 5 both play out "over the live fall"
  // (round 27's own words).
  fromAttach() {
    appState.view = 'fall'
    this.phase = 'connecting'
  }

  fromConnecting() {
    this.phase = 'glass'
  }

  beginTour() {
    this.phase = 'touring'
    this.cardIndex = 0
    this.rollToCurrentCard()
  }

  // nextCard walks the deck one card at a time. Off the end, the tour
  // is over -- round 27's "it ends at the wizard either way" applies to
  // finishing the walk exactly as it does to skipping it.
  nextCard() {
    if (this.cardIndex >= this.cards.length - 1) {
      this.handOffToWizard()
      return
    }
    this.cardIndex += 1
    this.rollToCurrentCard()
  }

  private rollToCurrentCard() {
    const card = this.cards[this.cardIndex]
    if (card) appState.view = card.views[0]
  }

  // skipToWizard (the glass's quiet link) and leaveTour (the tour's own
  // "leave any time") both end the journey the same way -- there is no
  // path out of the first hour that does not end at the wizard, only
  // different lengths of walk to get there.
  skipToWizard() {
    this.handOffToWizard()
  }

  leaveTour() {
    this.handOffToWizard()
  }

  private handOffToWizard() {
    this.phase = 'idle'
    // Spends wizard.svelte.ts's own once-only auto-launch slot: this
    // journey is the thing that decided to open the wizard, on its own
    // schedule, so the ordinary first-run check must not also fire the
    // moment `active` above flips back to false and reopen what this
    // just handed off.
    wizardState.markAutoLaunchSpent()
    wizardState.launch()
  }
}

export const journeyState = new JourneyState()
