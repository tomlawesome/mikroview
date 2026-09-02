// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #198: "Clear all" with click-again confirm, and a split Clear
// button with "Permanently clear".
//
// Two real port_scan flags from two different sources -- one drives the
// split button and its dropdown, the other survives to be swept up by
// Clear all -- so both actions run against real server state rather than
// a mocked click handler.
//
// Assertions are scoped to specific rows (by their target IP) and to
// count *deltas*, not fixed totals: the synthetic burst this needs to
// trigger a real port_scan also trips a real rule_spike on the shared
// log-prefix's own hit rate (confirmed by hand -- every request logged
// as `scan-src`, and ~40 of them inside its window is exactly what
// rule_spike watches for). That is the detector working correctly on
// synthetic traffic shaped like a real one, not a defect to work around
// by asserting an exact row count that real detector timing can't
// actually promise.
//
// #539: the "Permanently clear" step below creates a real, server-side
// exclusion for 198.51.100.78. That used to be the one piece of state
// this scenario left behind on exit. Every *other* target it touches
// only ever gets plain-cleared, and a plain clear is cheap to be
// independent of: Store.add (internal/flags/store.go) revives a cleared
// flag on the very next matching event, so feeding a fresh scan at a
// previously-cleared target just works, on this run or any later one.
// An exclusion does not revive -- Store.add's excluded check is a
// silent, permanent no-op by design (the whole point of "never flag
// this again") -- so a second run against the same server fed a fresh
// scan at 198.51.100.78, waited the full waitForFlag timeout for a flag
// that could now never arrive, and failed with a message that read as
// detector-timing flakiness while the server's own flag list showed
// exactly what happened: the target present and already `(cleared)`.
// (That shape is the tell for this bug specifically -- #450 is a
// different failure, where the awaited target is absent from the list
// entirely because the detector never raised it at all.)
//
// The fix is not a new address per run -- the split-button/Clear-all
// flows below are about *these specific* interactions, not about
// picking IPs the server has never seen, and burning a fresh exclusion
// on every single run would leave real permanent state accumulating on
// a shared server forever. Instead this scenario resets the one piece
// of state it is responsible for, at both ends: on the way in, in case
// an earlier run of this same scenario left the exclusion in place
// (crashed before its own cleanup, or predates this fix); and on the
// way out, once the assertions that need the exclusion to exist have
// run. Either half alone is enough to make repeat runs pass; both
// together also mean the exclusion is not just sitting there,
// unnoticed, on the assumption that a future run will clear it.
// resetExclusion is a no-op when there is nothing to remove --
// DELETE on an unknown exclusion ID is documented as such
// (internal/api/flags.go's handleExclusionRemove) -- so calling it
// unconditionally, whether or not this run actually created one, is
// safe.
//
// The startup reset runs over its own short-lived API request context
// (Playwright's request.newContext, no browser page attached) rather
// than through session()'s page, and deliberately before feedPortScan
// rather than after session(): App.svelte's flagsState.refresh() fires
// once on login and then every STATS_REFRESH_MS (5s) after that, on its
// own timer, unrelated to which view is on screen -- it is not
// re-triggered by navigating into the Flags view. Logging the real
// browser in first and feeding the scans after it (which an earlier
// version of this fix did, to reuse that page for the reset) starts
// that 5s clock before this run's own flags exist, so a fast
// waitForFlag -- which polls the server directly and can return in well
// under a second -- can land back in this scenario before App.svelte's
// own next refresh has caught up, and the Flags view then renders from
// a stale pre-scan snapshot that still shows both targets cleared. That
// produced exactly this bug's symptom (a row that should be active
// reading as cleared) for a different reason than #539 itself, on every
// run after the first. Feeding first and logging the real session in
// afterward, as originally written, keeps that timer's first tick safely
// after this run's events already exist.

import { request } from 'playwright'
import { session, check, done, feedPortScan, waitForFlag, goTo } from './live-browser.mjs'

// The flags tab is a table now, not a card grid (#688, commit 68fd460):
// one `tr.frow` per open flag (Flags.svelte:701). The section around it
// names itself with `aria-label="Active flags (N)"` (Flags.svelte:556)
// instead of pointing at a heading, because round 30 draws no heading
// over the table at all -- the count lives in the scene bar's own flag
// mark (Flags.svelte:552-555). Hence the prefix match: the label carries
// a live count, so it is never a fixed string.
//
// The section scope is load-bearing, not tidiness. The learning shelf
// below (Flags.svelte:646) draws its provisional flags as `tr.frow` too,
// so an unscoped `.frow` would count untrusted flags as open ones and
// quietly break every count delta below. Scoping also keeps `.card` --
// which still exists in the app shell, as the deck's own cards -- out of
// reach.
const ACTIVE = 'section[aria-label^="Active flags"] tr.frow'

// A click target that is deliberately not a control: the ratified table's
// last header cell is empty (Flags.svelte:613), so clicking it is a real
// "somewhere else" click that changes nothing. This used to be
// `h2:has-text("Active")`, and that heading is gone with the card grid --
// the only `h2` left on the surface is the learning shelf's own.
const ELSEWHERE = 'section[aria-label^="Active flags"] .ftable thead th:last-child'

const PERMANENT_EXCLUSION_ID = 'port_scan:198.51.100.78'

/**
 * resetExclusion clears any lingering permanent exclusion for the one
 * target this scenario permanently clears (see #539 note above), through
 * anything with Playwright's request-context shape (a page's own
 * `.request`, or a standalone `request.newContext()`). Safe to call at
 * any time, whether or not an exclusion currently exists.
 */
async function resetExclusion(requester) {
  return requester.delete(`${process.env.MV_URL}/api/flags/exclusions/${PERMANENT_EXCLUSION_ID}`, {
    headers: { 'X-Requested-With': 'mikroview' },
  })
}

// hasText against the whole row still finds a flag by its target: the IP
// is the row's `td.k` (its `button.wl`, or the plain "network-wide" span
// for a flag with nothing to point at) -- Flags.svelte:703-718. The
// drawer is a sibling `tr.drawer`, not nested inside the row, so an open
// drawer's text cannot make a row match something it does not carry.
function rowFor(page, text) {
  return page.locator(ACTIVE, { hasText: text })
}

async function activeCount(page) {
  return page.locator(ACTIVE).count()
}

// A throwaway login (its own session, no browser page) purely to clear
// a leftover exclusion before anything else touches the server -- see
// the #539 note above for why this has to happen before feedPortScan,
// not after the real session() login below.
{
  const api = await request.newContext({ ignoreHTTPSErrors: true })
  const loginResp = await api.post(`${process.env.MV_URL}/api/auth/login`, {
    data: { username: process.env.MV_USER, password: process.env.MV_PASS },
    headers: { 'X-Requested-With': 'mikroview' },
  })
  const resetResp = loginResp.ok() ? await resetExclusion(api) : loginResp
  check(resetResp.ok(), `startup: any exclusion left over from an earlier run was cleared (status ${resetResp.status()})`)
  await api.dispose()
}

feedPortScan(20, '198.51.100.77')
feedPortScan(20, '198.51.100.78')

const { page } = await session()

async function openMenuView(label) {
  await goTo(page, label)
}

// Server-side first (#354): a locator timeout here cannot say whether
// the flag was never raised or merely had not been rendered yet.
const firstRaised = []
for (const ip of ['198.51.100.77', '198.51.100.78']) {
  const raised = await waitForFlag(page, ip)
  check(raised.ok, raised.message)
  firstRaised.push(raised)
}

// The split-button flow below needs both rows to actually exist, and
// the Clear all section further down needs the split-button flow to have
// run (it asserts against the one exclusion that flow creates). Running
// either against a flag that never reached the server used to crash the
// scenario on the first Playwright locator timeout instead of reporting
// the real, upstream reason (#450).
if (firstRaised.every((r) => r.ok)) {
  await openMenuView('Flags')
  // Land on a rendered flag row, not on the old card's `.type` label:
  // the type now sits in the row's `td.fmark`, alongside the family mark
  // (Flags.svelte:702). This wait is what threw before its first
  // assertion once #688 landed.
  await page.waitForSelector(`${ACTIVE} td.fmark`, { timeout: 15000 })

  check(await rowFor(page, '198.51.100.77').isVisible(), 'the first port scan raised its own flag')
  check(await rowFor(page, '198.51.100.78').isVisible(), 'the second port scan raised its own flag')

  // --- Split button: main segment behaves exactly like the old Clear ---
  // The actions live in the drawer now (#633, rounds 18-19): the row's
  // one affordance is the chevron, and Clear sits across the drawer's
  // foot -- so every clear below opens the drawer first.
  //
  // NOT RESELECTED, deliberately -- this whole section is stopped on a
  // recorded gap, not on drift, and nothing below it is a selector I can
  // honestly repoint. The split button is not merely elsewhere in the
  // ratified table; it does not exist. There is no `.split-main`,
  // `.split-arrow`, `.split-menu` or `.split-menu-item` anywhere in
  // frontend/src, and no "Permanently clear" control on any surface.
  // The drawer's whole action foot is `open in stream`, the watch
  // handoffs, the provisional verdicts and `clear with a note`
  // (Flags.svelte:810-865) -- a plain clear survives as an empty note
  // (Flags.svelte:846-847), but the arrow segment, its dropdown, the
  // keyboard path through it and the exclusion it created have no
  // counterpart at all.
  //
  // That is #688's own recorded gap list speaking, not an oversight to
  // patch over: Flags.svelte:19-26 names *exclusions* among what round
  // 29's scene deliberately does not carry, on the owner's 2026-08-31
  // ruling to build the ratified surface and record what falls out.
  // Exclusions.svelte is still in the tree and still describes itself as
  // Flags' admin-only Exclusions tab (#547), but nothing imports or
  // renders it any more, and the docket's tabs are flags / watchlist /
  // audit log (SceneBar.svelte:77-99) -- so the Exclusions-tab count
  // assertion below has no element to find either.
  //
  // Repointing these at `clear with a note` would not be reselection: it
  // would quietly turn assertions about the split button's segments and
  // its permanent-clear path into assertions about a different control
  // that makes a weaker claim. Left standing, failing honestly, until
  // the gap is resolved and it is clear what these should assert.

  // Pinned as absence rather than left to throw. `.split-main`'s waitFor is an
  // uncaught rejection, so leaving this standing does not fail honestly -- it
  // kills the scenario on this line and takes the Clear-all section below with
  // it, which is reselected, correct, and has nothing to do with this gap.
  //
  // Same treatment live-metrics-views gives the unmounted cross-section and
  // live-viewer-surfaces gives the missing READ-ONLY declaration: assert the
  // present truth, name the issue, keep the rest of the scenario running.
  //
  // #691 owns this one by name -- "Permanent exclusion -- `clearPermanent`
  // (`flags.svelte.ts:175`) -> `POST /api/flags/{id}/clear-permanent`. No UI
  // caller." -- together with the orphaned Exclusions.svelte behind the tab
  // count. When it comes back, restore the twelve assertions this replaced:
  //
  //   the old two-button row is gone, and `.split-main` is present
  //   `.split-arrow` is present for an admin, in the drawer
  //   the main segment clears exactly that one flag
  //   the arrow segment is focusable and labelled for the keyboard
  //   Enter opens the dropdown; Escape closes it; an outside click closes it
  //   the dropdown reopens on a click
  //   Tab reaches "Permanently clear" and Enter activates it
  //   the row goes, and the Exclusions tab count rises by one (#539, #547)
  check(
    (await page.locator('.split-main, .split-arrow, .split-menu, .split-menu-item').count()) === 0,
    "no split button renders on a flag row (#691 gap, not this scenario's to fix)",
  )

  // --- Clear all: click-again confirm ---

  // 20, not 15: the port_scan detector's threshold IS 15 distinct ports,
  // so feeding exactly that left no margin -- a single event lost anywhere
  // on the path means no flag, and the scenario fails for a reason that
  // has nothing to do with Clear all (#354).
  feedPortScan(20, '198.51.100.79')
  feedPortScan(20, '198.51.100.80')
  feedPortScan(20, '198.51.100.81')

  const secondRaised = []
  for (const ip of ['198.51.100.79', '198.51.100.80', '198.51.100.81']) {
    const raised = await waitForFlag(page, ip)
    check(raised.ok, raised.message)
    secondRaised.push(raised)
  }

  // Clear all needs all three rows actually present -- same reasoning
  // as the outer guard above (#450).
  if (secondRaised.every((r) => r.ok)) {
    await page.reload({ waitUntil: 'networkidle' })
    await openMenuView('Flags')
    await page.waitForSelector(`${ACTIVE} td.fmark`, { timeout: 15000 })

    // Wait for each flag rather than for "a row exists". Three scans were
    // just fed and the detector raises them independently, so waiting on the
    // first row to appear and then asserting all three are present is a
    // race -- it caught the third one missing on a local run. Waiting for
    // each makes the assertion about Clear all, which is what this section
    // is for.
    for (const ip of ['198.51.100.79', '198.51.100.80', '198.51.100.81']) {
      await rowFor(page, ip)
        .waitFor({ timeout: 20000 })
        .catch(() => {})
      check(await rowFor(page, ip).isVisible(), `flag for ${ip} is active before Clear all`)
    }

    // Clear all is the docket tab row's outlined bubble now (rounds
    // 28-29, owner-ratified): one click arms it alarm-red "confirm", a
    // second click clears, and a click anywhere else disarms -- the
    // hover-away and timeout disarms went with the old button.
    check(!(await page.isVisible('.docket .bubble.armed')), 'the bubble starts unarmed')
    await page.click('.docket .bubble:has-text("clear all")')
    await page.waitForTimeout(150)
    check(await page.isVisible('.docket .bubble.armed:has-text("confirm")'), 'one click arms it -- alarm-red, and relabelled confirm')

    const armedCount = await activeCount(page)

    // A click anywhere else disarms it without clearing, so an armed
    // bubble cannot ambush a later stray click. The "anywhere else" is
    // the table's own empty last header cell now (see ELSEWHERE): the
    // Active heading this used to click went with the card grid, and the
    // disarm listens on the window (Docket.svelte's onWindowClick), so
    // any inert element makes the same point.
    await page.click(ELSEWHERE)
    await page.waitForTimeout(150)
    check(!(await page.isVisible('.docket .bubble.armed')), 'a click anywhere else disarms it without a second click')
    check((await activeCount(page)) === armedCount, 'nothing was cleared by an arm that was never confirmed')

    // The real thing: arm, then confirm.
    await page.click('.docket .bubble:has-text("clear all")')
    await page.waitForTimeout(150)
    await page.click('.docket .bubble.armed:has-text("confirm")')
    await page.waitForTimeout(600)

    check((await activeCount(page)) === 0, 'the second click actually clears every active flag, including the extra rule_spike')

    // Regular clears only -- Clear all must never create an exclusion.
    //
    // Zero, not one. The baseline used to be the single exclusion the
    // split-button's permanent-clear left behind; that section is pinned as
    // absence above while #691 has no UI caller for clearPermanent, so nothing
    // creates an exclusion in this run any more. The claim is unchanged and
    // still worth making -- Clear all must not permanently exclude anything --
    // it is only the baseline it counts against that moved. Restore the 1 when
    // the split button comes back.
    const excludedResp = await page.request.get(`${process.env.MV_URL}/api/flags/exclusions`)
    const excludedBody = await excludedResp.json()
    check(
      (excludedBody.exclusions ?? []).length === 0,
      `Clear all created no exclusions (${(excludedBody.exclusions ?? []).length})`,
    )

    // Reload to confirm the clears persisted server-side, not just in the
    // optimistic client state.
    await page.reload({ waitUntil: 'networkidle' })
    await openMenuView('Flags')
    await page.waitForTimeout(500)
    // The empty state is round 26's honest cleared state now, drawn as
    // `.caempty` inside the active section (Flags.svelte:557-578): with
    // something just cleared it reads "Nothing open." and then says when
    // and where the cleared flags went. "Nothing flagged right now" was
    // the card grid's wording and is nowhere on the surface.
    check(
      await page
        .locator(`section[aria-label^="Active flags"] .caempty`, { hasText: 'Nothing open' })
        .isVisible(),
      'the cleared state survived a reload -- Clear all reached the server, not just the local optimistic update',
    )
  } else {
    const reasons = secondRaised.filter((r) => !r.ok).map((r) => r.message).join('; ')
    check(true, `skipped -- the Clear all flow cannot run without all three scan flags (${reasons})`)
  }
} else {
  const reasons = firstRaised.filter((r) => !r.ok).map((r) => r.message).join('; ')
  check(true, `skipped -- the split-button and Clear all flows cannot run without both scan flags (${reasons})`)
}

// Leave the server as this scenario found it, not just as the next run
// of this scenario needs it (#539): the startup reset above is what
// actually guarantees repeatability, but there is no reason to leave a
// real permanent exclusion sitting on a shared server for however long
// it is before this scenario runs again. Unconditional and safe either
// way -- see resetExclusion's doc comment.
await resetExclusion(page.request)

done()
