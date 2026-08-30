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
// Positions are percentages of the card's own box, bound as CSS custom
// properties by JourneyTour.svelte (never a static style attribute --
// the app's CSP forbids those; see AuthScreen.svelte's own fall for why
// that matters). They are hand-placed approximations of where each
// card's own furniture sits, not measured off a live render.
export interface TourHighlight {
  label: string
  top: string
  left: string
  width: string
  height: string
}

export const TOUR_HIGHLIGHTS: Record<string, TourHighlight[]> = {
  fall: [
    { label: 'the brink — now arrives here', top: '9%', left: '4%', width: '92%', height: '7%' },
    { label: 'a band per boundary — click reaches in', top: '18%', left: '4%', width: '16%', height: '34%' },
    { label: 'the held hour — scroll looks back', top: '80%', left: '4%', width: '18%', height: '9%' },
  ],
  topography: [
    { label: 'the router as the waist — subnets below, the internet above', top: '30%', left: '38%', width: '24%', height: '30%' },
  ],
  metrics: [
    { label: 'one hour, three views — seismograph, register, table', top: '10%', left: '6%', width: '40%', height: '10%' },
  ],
  live: [
    { label: 'every event, live — search and filter as it fills', top: '12%', left: '55%', width: '38%', height: '9%' },
  ],
  docket: [
    { label: 'flags, watchlist and audit — one card, three tabs', top: '10%', left: '6%', width: '50%', height: '9%' },
  ],
  entities: [
    { label: 'routers, named entities, and what mikroview has discovered', top: '10%', left: '6%', width: '55%', height: '9%' },
  ],
  engineroom: [
    { label: 'the shelf — deck order, ingest, detection, memory, account', top: '10%', left: '6%', width: '55%', height: '9%' },
  ],
}
