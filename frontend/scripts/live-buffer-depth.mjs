// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #244: the operator needs to know how far back the server's
// in-memory event ring actually reaches -- previously invisible
// anywhere, including the one real instance whose buffer had wrapped in
// under three minutes against a configured 24h retention with nothing
// on screen to say so.
//
// #244 originally answered that with a scene-bar `.buffer-depth`
// indicator reading "N% of buffer used" against the compiled-in
// capacity (200,000, `internal/config/config.go`). #691/#703 (round 30,
// 2026-08-31, commit a3fee31) replaced it: the owner's ruling was to
// state plainly how far back the buffer reaches in time, not a raw
// fullness percentage, and to fold that into the stream's own SPAN
// control on the filter line (FilterBar.svelte's `.spans`) rather than
// the scene bar -- see that component's own top-of-file comment and
// `lib/spans.ts`. `.buffer-depth` and format.ts's formatBufferDepth are
// gone from the UI as a result (the latter is now dead code, kept only
// by its own unit test -- noted, not touched here: removing it is a
// separate call from fixing this scenario).
//
// This checks the ratified successor end to end against a real
// /api/stats response: the reach words state how far back the buffer
// goes, and a span it cannot cover yet is disabled and says why -- the
// same "what happens once the ring can't hold everything" fact #244
// asked for, now told per span instead of as one generic tooltip.
//
// The shared live-check server runs the compiled-in default capacity
// (200,000), which a scenario feeding a few hundred events has no
// business trying to fill -- that would mean either flooding the shared
// instance every other scenario also runs against, or reconfiguring it
// out from under them. So this checks the reach/availability logic with
// a small feed, and leaves the exact wording's every branch to
// spans.test.ts's unit coverage, which can assert it precisely without
// a 200,000-event feed or a fortnight of wall-clock time.

import { session, feedSyslog, check, done, waitForStreamRows } from './live-browser.mjs'

const { page } = await session()
feedSyslog(100)
await waitForStreamRows(page, 50)

const reach = page.locator('.filterline .spans .reach')
check(await reach.isVisible(), 'the buffer reach indicator is visible on the filter line')

// The spans read the buffer from the polled stats, not from the rows the
// socket delivers, so the reach still says "nothing held yet" for up to
// one poll after the rows are on screen. Wait for the buffer to stop
// being empty; what it then says is what the check below judges.
await page.waitForFunction(
  () => !/nothing held yet/.test(document.querySelector('.filterline .spans .reach')?.textContent ?? ''),
  null,
  { timeout: 15000 },
)

const reachText = (await reach.textContent())?.trim() ?? ''
check(
  /^holding \d+ (s|min|h|d)$/.test(reachText),
  `states how far back the buffer reaches, in words (got "${reachText}")`,
)

// A fortnight of wall-clock reach cannot exist in this harness, so the
// 14 d span is always the one the buffer cannot cover yet -- the
// deterministic case #244's "what happens once it fills" tooltip asked
// for, now told on the span itself rather than as one generic tooltip.
const fortnight = page.locator('.filterline .spans button.span:text-is("14 d")')
check(await fortnight.isDisabled(), 'the 14 d span is disabled -- the buffer cannot reach back that far yet')

const fortnightTitle = (await fortnight.getAttribute('title')) ?? ''
check(
  /14 d of history is not held — the buffer is holding/.test(fortnightTitle),
  `the disabled span explains what is really held instead of just refusing (got "${fortnightTitle}")`,
)

done()
