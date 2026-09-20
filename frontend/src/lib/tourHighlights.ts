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
// Every ring now measures itself off a `selector`; the four values that
// follow it are only the fallback for a card that has not rendered that
// furniture yet. Which element each ring names, and why, is recorded on
// #750 -- not guessed at here.
//
// Selectors are scoped to their own card because the deck keeps every
// card mounted: a bare `span.switch` matches both metrics and the
// docket, and the ring would measure whichever came first.
//
// #1235: the label is the ring's tag, a few words; `says` is the
// sentence -- what this is and what it is for, in the app's own voice.
// It lives here with the stop rather than in JourneyTour so the copy
// for a stop is one line, next to what it names. JourneyTour shows the
// current card's sentences in the bar at the foot of the screen, the
// one place that never covers the thing being pointed at; the fall's
// three are shown together, the way its three rings already are
// (#1215 item 7). tourHighlights.test.ts holds the rules each
// sentence keeps to.
export interface TourHighlight {
  label: string
  // #1235: one sentence, under ~90 characters, ending in a full stop.
  says: string
  // Measured off the live render when present; the four values below
  // are the fallback for when it matches nothing.
  selector?: string
  // #1215 item 3: true only when the named element is itself a box --
  // a card, a field, a button, anything with its own visible border --
  // so the ring traces that perimeter exactly. Absent (the default,
  // false) means the ring stands off the element with a clear margin
  // all round instead. A data attribute here rather than sniffing the
  // live element's computed border: simpler, and honest about which
  // rings were actually looked at (see #750 for what each ring names).
  box?: boolean
  top: string
  left: string
  width: string
  height: string
}

export const TOUR_HIGHLIGHTS: Record<string, TourHighlight[]> = {
  fall: [
    { label: 'the brink — now arrives here', says: 'each event lands on this line as it happens, then falls into the hour below.', selector: '.card[data-card="fall"] line.nowline', top: '9%', left: '4%', width: '92%', height: '7%' },
    { label: 'a band per boundary — click reaches in', says: 'one band per firewall crossing; click a band to open its traffic in the stream.', selector: '.card[data-card="fall"] g.band-head', top: '18%', left: '4%', width: '16%', height: '34%' },
    { label: 'the held hour — scroll looks back', says: 'the last hour is held beneath the line; scroll down to read back through it.', selector: '.card[data-card="fall"] div.fall-foot', top: '80%', left: '4%', width: '18%', height: '9%' },
  ],
  topography: [
    // rect.isl.waist paints its own fill and stroke (app.css: fill
    // var(--bg-elevated), stroke var(--border)) -- a drawn card, not a
    // bare click target, so the ring traces it rather than standing off.
    { label: 'the router as the waist — subnets below, the internet above', says: 'the router sits at the waist, with your subnets below it and the internet above.', selector: '.card[data-card="topography"] rect.isl.waist', box: true, top: '30%', left: '38%', width: '24%', height: '30%' },
  ],
  metrics: [
    { label: 'one hour, three views — seismograph, register, table', says: 'the same hour of traffic drawn three ways; switch to whichever reads best.', selector: '.card[data-card="metrics"] span.switch', top: '10%', left: '6%', width: '40%', height: '10%' },
  ],
  live: [
    { label: 'every event, live — search and filter as it fills', says: 'every event as it arrives; type here to keep only the lines that match.', selector: '.card[data-card="live"] .filterline', top: '12%', left: '55%', width: '38%', height: '9%' },
  ],
  docket: [
    { label: 'flags, watchlist and audit — one card, three tabs', says: 'what MikroView flagged, what it is watching for, and who changed what.', selector: '.card[data-card="docket"] span.switch', top: '10%', left: '6%', width: '50%', height: '9%' },
  ],
  entities: [
    { label: 'routers, named entities, and what MikroView has discovered', says: 'the routers that push here, what you have named, and what MikroView found on its own.', selector: '.card[data-card="entities"] .og:first-of-type', top: '10%', left: '6%', width: '55%', height: '9%' },
  ],
  engineroom: [
    { label: 'the shelf — deck order, ingest, detection, memory, account', says: 'drag cards to reorder your deck; the shelves below set what comes in and what is kept.', selector: '.card[data-card="engineroom"] .stshelf', top: '10%', left: '6%', width: '55%', height: '9%' },
  ],
  'log-every-rule': [
    // button.drop paints its own dashed border and elevated fill
    // (LogEveryRule.svelte's .drop) -- a drawn field, so the ring
    // traces it. One ring: Analyse and the results do not exist until
    // an export is in, and the never-stored line stays a footnote by
    // #1134's ruling, here as on the page.
    { label: 'one drop zone — drop, click or paste the router export', says: 'the export comes back with logging switched on for every rule not logging yet.', selector: '.card[data-card="log-every-rule"] button.drop', box: true, top: '34%', left: '28%', width: '44%', height: '16%' },
  ],
}

// ── ring geometry (#1215) ────────────────────────────────────────────
//
// Pure so it can be unit tested without a real layout: jsdom's
// getBoundingClientRect() is always zero (JourneyTour.rings.test.ts's
// own note), so proving the box/non-box/hairline shapes has to happen
// against plain numbers rather than a mounted component.
export interface Rect {
  top: number
  left: number
  width: number
  height: number
}

// #750: a rule or a 1px line measures only a sliver. Padded to a
// visible band, centred on the element, rather than ringing a hairline.
export const HAIRLINE_MIN_PX = 28

// #1215 item 2: the ring stands off a non-box target by this much on
// every side, so the element sits inside it with breathing space.
export const RING_MARGIN_PX = 12

/** Turns a raw measured (or fallback) rect into the ring's own rect.
 * The hairline pad (#750) applies regardless of shape -- a thin box
 * still needs padding up to a visible band. The margin (#1215 item 2)
 * applies only when `isBox` is false; a box target (item 3) is traced
 * exactly, with no added margin. */
export function fitRing(rect: Rect, isBox: boolean): Rect {
  let { top, left, width, height } = rect

  if (height < HAIRLINE_MIN_PX) {
    top -= (HAIRLINE_MIN_PX - height) / 2
    height = HAIRLINE_MIN_PX
  }
  if (width < HAIRLINE_MIN_PX) {
    left -= (HAIRLINE_MIN_PX - width) / 2
    width = HAIRLINE_MIN_PX
  }

  if (!isBox) {
    top -= RING_MARGIN_PX
    left -= RING_MARGIN_PX
    width += RING_MARGIN_PX * 2
    height += RING_MARGIN_PX * 2
  }

  return { top, left, width, height }
}
