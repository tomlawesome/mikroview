// SPDX-License-Identifier: AGPL-3.0-only
//
// The deck tour (#646 beat 6, kept by #1386): card by card, ringing key
// controls with a hairline and a label -- JourneyTour.svelte draws it,
// this module runs it. What came before it on a brand-new install --
// attach, connecting, the glass, and the hand-off to the wizard -- is
// retired (owner, 2026-09-30, on #1386): the wizard's way in plays
// straight after the admin account is made (lib/wizardJourney.svelte.ts),
// and the tour comes after the wizard rather than before it.
//
// Two ways in, both here:
//
//  - Offered once, after the wizard's Finish has landed on the fall
//    (offerAfterFinish, drawn by TourOffer.svelte). Once per account,
//    not per browser: #1283 moved every per-person fact off
//    localStorage because a shared machine handed the next person the
//    previous operator's state, and "this person has been offered the
//    tour" is exactly that kind of fact -- per browser it would come
//    back on the operator's second machine and stay hidden from the
//    next account on this one. The flag is set when the offer is
//    answered (taken or declined), so a reload never re-offers; an
//    offer never answered (the page closed under it) can come again on
//    a later Finish.
//  - The account menu's "Take the tour", any time, every signed-in role
//    (AccountMenu.svelte).
//
// The tour ends where it began. Finishing the last card and leaving
// partway both roll the deck back to the card the tour started from:
// the tour is a detour, so nothing about taking it changes where the
// operator is in the app. From the offer that is the fall; from the
// menu it is whichever card they were reading.
import { appState, type View } from './state.svelte'
import { authState } from './auth.svelte'
import { deckCards, type DeckCard } from './deckCards'
import { deckOrderState } from './deckOrder.svelte'
import { preferencesState } from './preferences.svelte'

// The glass's own sentence, round 27's beat 3 verbatim: "Six cards.
// About two minutes." Six cards to two minutes is twenty seconds a
// card, so the estimate moves with the deck instead of being fixed
// prose -- #647 grew an admin's deck to seven, and a viewer's is the
// six the round drew.
export const TOUR_SECONDS_PER_CARD = 20

// How long after the way out has landed the offer rises: the fall, the
// bar and the groups have struck on by then (WizardJourney.svelte's
// wayOut clears its last cue at 5300 on the journey's clock, and done()
// comes after), so this is a beat of stillness, not a wait on anything.
export const OFFER_DELAY_MS = 900

// The per-account record's key (preferences.svelte.ts lists the
// contract: one key per module, named after the module).
const PREFS_KEY = 'tour'

type TourPrefs = { offered?: boolean }

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

// tourLengthSentence is the offer's second line. Kept here rather than
// inline in TourOffer.svelte so the arithmetic and the wording are
// testable without driving a render. The tail is the tour's end, which
// from the offer is always the fall the offer sits over.
export function tourLengthSentence(cardCount: number): string {
  const cards = `${inWords(cardCount)} ${cardCount === 1 ? 'card' : 'cards'}`
  const minutes = tourMinutes(cardCount)
  const length = minutes === 1 ? 'a minute' : `${inWords(minutes)} minutes`
  return `${cards.charAt(0).toUpperCase()}${cards.slice(1)}. About ${length}. It ends back here, on the fall.`
}

class TourState {
  active = $state(false)
  cardIndex = $state(0)
  /** the offer panel is up, over the fall */
  offering = $state(false)
  private returnTo: View = 'fall'
  private offered = false
  private offerTimer: ReturnType<typeof setTimeout> | undefined

  constructor() {
    preferencesState.register(PREFS_KEY, (value) => {
      this.offered = (value as TourPrefs | undefined)?.offered === true
    })
  }

  // The deck's own real card list, in the operator's own kept order --
  // the same table Deck.svelte itself renders from, with the same two
  // gates, so the tour's "1 OF N" always matches what the roll rail
  // actually shows (#647 grew this to seven for an admin; nothing here
  // hardcodes a count, and a user-tier account gets the user tier's
  // deck, not a viewer's).
  get cards(): DeckCard[] {
    return deckOrderState.apply(deckCards(authState.isAdmin, authState.canEdit))
  }

  // offerAfterFinish is called by wizardRun.finish() once the way out
  // has landed on the fall (or the hand-over happened outright). A walk
  // that lands on the fleet -- adding a router -- never calls this: the
  // tour's first stop is the fall, and rolling someone away from the
  // router they just added would be the tour taking over, not offering.
  offerAfterFinish() {
    if (this.offered || this.active || this.offering) return
    if (this.offerTimer) clearTimeout(this.offerTimer)
    this.offerTimer = setTimeout(() => {
      this.offerTimer = undefined
      this.offering = true
    }, OFFER_DELAY_MS)
  }

  // Taken or declined, the offer is spent for this account.
  acceptOffer() {
    this.spendOffer()
    this.begin()
  }

  declineOffer() {
    this.spendOffer()
  }

  private spendOffer() {
    this.offering = false
    this.offered = true
    preferencesState.set(PREFS_KEY, { offered: true } satisfies TourPrefs)
  }

  // begin starts on the deck's first card and remembers where the
  // operator was, so end() can put them back there.
  begin() {
    if (this.active) return
    this.returnTo = appState.view
    this.active = true
    this.cardIndex = 0
    this.rollToCurrentCard()
  }

  // nextCard walks the deck one card at a time. Off the end, the tour
  // is over and the deck rolls back to where it started.
  nextCard() {
    if (this.cardIndex >= this.cards.length - 1) {
      this.end()
      return
    }
    this.cardIndex += 1
    this.rollToCurrentCard()
  }

  private rollToCurrentCard() {
    const card = this.cards[this.cardIndex]
    if (card) appState.view = card.views[0]
  }

  // leave (the bar's "leave the tour") ends it the same way finishing
  // does: back where it started. Both exits put the operator back
  // where they were, so a card that caught their eye is one click away
  // on the rail, and the tour never leaves them somewhere they did not
  // choose.
  leave() {
    this.end()
  }

  end() {
    if (!this.active) return
    this.active = false
    this.cardIndex = 0
    appState.view = this.returnTo
  }

  /** Sign-out (auth.svelte.ts's clearSessionState): a pending or open
   * offer, and a tour in progress, are the previous operator's, not
   * the next one's. `offered` re-hydrates from the next account's own
   * record. */
  reset() {
    if (this.offerTimer) clearTimeout(this.offerTimer)
    this.offerTimer = undefined
    this.offering = false
    this.offered = false
    this.active = false
    this.cardIndex = 0
    this.returnTo = 'fall'
  }
}

export const tourState = new TourState()
