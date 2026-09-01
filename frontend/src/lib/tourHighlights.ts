// SPDX-License-Identifier: AGPL-3.0-only
//
// The tour's per-card highlights (#646 beat 6, round 29's ratified
// shape: "highlight key handles/inputs/outputs" and "label/explain
// concisely" -- a ring in accent hairline, a few plain words each,
// "never over the top"). The fall's three rings are round 29's own
// demonstration verbatim (the-whole.html's beat-4 frame: "the brink —
// now arrives here", "a band per boundary — click reaches in", "the
// held hour — scroll looks back"); the other six cards extend the same
// principle -- one ring is enough once the card itself is walked, not
// merely shown.
//
// Positions are percentages of the viewport, bound as CSS custom
// properties by JourneyTour.svelte (never a static style attribute --
// the app's CSP forbids those; see AuthScreen.svelte's own fall for why
// that matters).
//
// Where a highlight names an element outright -- "a band per boundary",
// "three views", "three tabs", "search and filter" -- it carries a
// `selector` and JourneyTour measures the real thing off the live
// render. The percentages then serve only as the fallback for a card
// that has not rendered that furniture yet.
//
// The remaining entries are still hand-placed approximations, and a
// live measurement on 2026-09-01 showed several of them ring empty
// space: the fall's band ring sat at left 4% where the bands render at
// 40%, and the metrics ring was 40% of the viewport wide over a control
// that is 12% wide. Which element those rings should name is a visual
// call and is recorded on #750, not guessed at here.
//
// Selectors are scoped to their own card because the deck keeps every
// card mounted: a bare `span.switch` matches both metrics and the
// docket, and the ring would measure whichever came first.
export interface TourHighlight {
  label: string
  // Measured off the live render when present; the four values below
  // are the fallback for when it matches nothing.
  selector?: string
  top: string
  left: string
  width: string
  height: string
}

export const TOUR_HIGHLIGHTS: Record<string, TourHighlight[]> = {
  fall: [
    { label: 'the brink — now arrives here', top: '9%', left: '4%', width: '92%', height: '7%' },
    { label: 'a band per boundary — click reaches in', selector: '.card[data-card="fall"] g.band-head', top: '18%', left: '4%', width: '16%', height: '34%' },
    { label: 'the held hour — scroll looks back', top: '80%', left: '4%', width: '18%', height: '9%' },
  ],
  topography: [
    { label: 'the router as the waist — subnets below, the internet above', top: '30%', left: '38%', width: '24%', height: '30%' },
  ],
  metrics: [
    { label: 'one hour, three views — seismograph, register, table', selector: '.card[data-card="metrics"] span.switch', top: '10%', left: '6%', width: '40%', height: '10%' },
  ],
  live: [
    { label: 'every event, live — search and filter as it fills', selector: '.card[data-card="live"] .filterline', top: '12%', left: '55%', width: '38%', height: '9%' },
  ],
  docket: [
    { label: 'flags, watchlist and audit — one card, three tabs', selector: '.card[data-card="docket"] span.switch', top: '10%', left: '6%', width: '50%', height: '9%' },
  ],
  entities: [
    { label: 'routers, named entities, and what mikroview has discovered', top: '10%', left: '6%', width: '55%', height: '9%' },
  ],
  engineroom: [
    { label: 'the shelf — deck order, ingest, detection, memory, account', top: '10%', left: '6%', width: '55%', height: '9%' },
  ],
}
