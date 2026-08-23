// SPDX-License-Identifier: AGPL-3.0-only

// How many events the client keeps in memory for live filtering. Deeper
// history is fetched on demand via "load older" against the server's
// much larger retained buffer, not held in the browser.
//
// 20,000 is a chosen number, not a derived one -- said plainly because
// the previous value (5,000) was also chosen, said nothing about it, and
// cost an afternoon to establish that (#342).
//
// Memory is not the constraint and does not need to be modelled.
// Measured 2026-08-13 in a real Chromium page, heap read via CDP after a
// forced GC: ~525 bytes per event, so 20,000 costs ~10 MB and 50,000
// ~26 MB. That figure excludes the reactive wrapper Svelte puts around
// each object, so call it several times more -- tens of megabytes is
// still nothing on any device that can run this page at all. This is why
// the count stays a count rather than becoming the memory budget
// internal/config's assumedBytesPerEvent uses for the server ring: a
// budget absorbs variance between deployments, and here every plausible
// answer is free.
//
// The number actually worth watching is the cost of re-filtering a
// buffer of *reactive* objects on every render. #342 named that as the
// figure to watch and then quoted the *plain*-object one (2.4 ms for
// 50,000) as if it answered the question, which it does not -- a proxied
// property read is far more expensive than a plain one, and the whole
// ageFiltered -> liveFiltered -> rendered chain runs on every flush and
// every 250 ms tick.
//
// That gap is now closed at the source rather than by lowering this
// number: `events` and `frozenPool` are `$state.raw` (see
// state.svelte.ts), so the buffer holds plain objects and the plain
// figure above is the one that applies. Raising this cap again is a
// question about memory and render volume, not about proxy overhead.
export const MAX_CLIENT_EVENTS = 20000

// How many rows are actually rendered in the DOM at once. Kept well below
// MAX_CLIENT_EVENTS so scrolling stays smooth without needing a
// virtual-scroll library.
export const MAX_RENDERED_ROWS = 800
