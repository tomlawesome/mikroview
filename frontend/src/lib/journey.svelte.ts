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

// The glass's own sentence, round 27's beat 3 verbatim: "Six cards.
// About two minutes." Six cards to two minutes is twenty seconds a
// card, so the estimate moves with the deck instead of being fixed
// prose -- #647 grew an admin's deck to seven, and a viewer's is the
// six the round drew.
export const TOUR_SECONDS_PER_CARD = 20

const NUMBER_WORDS = [
  'zero',
  'one',
  'two',
  'three',
  'four',
  'five',
  'six',
  'seven',
  'eight',
  'nine',
  'ten',
  'eleven',
  'twelve',
]

// Words up to twelve, digits past it -- the round writes "Six cards",
// not "6 cards", and a deck that ever grew past a dozen would read
// worse spelled out than numbered.
function inWords(n: number): string {
  return NUMBER_WORDS[n] ?? String(n)
}

// tourMinutes never rounds down to nothing: a short deck still takes a
// moment to walk, and "About zero minutes" is not a sentence.
export function tourMinutes(cardCount: number): number {
  return Math.max(1, Math.round((cardCount * TOUR_SECONDS_PER_CARD) / 60))
}

// tourLengthSentence is the glass's second line. Kept here rather than
// inline in JourneyGlass.svelte so the arithmetic and the wording are
// testable without driving a render.
export function tourLengthSentence(cardCount: number): string {
  const cards = `${inWords(cardCount)} ${cardCount === 1 ? 'card' : 'cards'}`
  const minutes = tourMinutes(cardCount)
  const length = minutes === 1 ? 'a minute' : `${inWords(minutes)} minutes`
  return `${cards.charAt(0).toUpperCase()}${cards.slice(1)}. About ${length}. It ends at the wizard either way.`
}

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

  // fromAttach moves to the waiting beat once the operator has (or says
  // they have) pasted the two lines. Rolls the deck to the fall first,
  // since beats 4 and 5 both play out "over the live fall" (round 27's
  // own words).
  fromAttach() {
    appState.view = 'fall'
    this.phase = 'connecting'
  }

  // arrived is the gate on beat 3 (#750 B1, owner ruling 2026-09-02):
  // round 27 draws "MikroView is flowing" *after* beat 2's "13:46 --
  // the first line lands", so the glass waits on a line actually
  // landing and never on a clock. Until one does, the waiting beat says
  // exactly that and keeps the two router lines up to paste. This
  // replaces a fixed 3.2s pause that claimed evidence had arrived when
  // nothing had -- a router whose certificate is not yet trusted never
  // reaches this instance, and the old beat told it that it had.
  get arrived(): boolean {
    return appState.events.length > 0
  }

  // Called by JourneyGlass.svelte the moment `arrived` turns true. A
  // one-way move: the client buffer is bounded and can age out, and a
  // glass that fell back to "nothing has arrived yet" after the fall had
  // plainly poured would be a lie in the other direction.
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
