// SPDX-License-Identifier: AGPL-3.0-only
//
// The setup wizard against a real running mikroview.
//
// #1381 replaced the modal this scenario used to drive (SetupWizard.svelte,
// #487) with a full screen (Wizard.svelte), built from
// docs/design/screens/wizard/DESIGN.md -- the ratified record. Most of
// what this scenario used to cover has no home in that record any more
// (DESIGN.md, "Superseded: the wizard as a modal"), and is not ported
// here -- keeping the old assertions against selectors the new component
// never renders would just be a scenario that always fails, which is
// worse than one that says plainly what it no longer covers:
//
//  - The certificate step, the RouterOS version pick-list, and the
//    per-dialect command re-render it drove (#436) -- the record's five
//    steps have no step of their own for either; the certificate is one
//    part of Paste once's block, and RouterOS version is now read from a
//    router's own push (the bar's chip, wizardRun.ts's `ev.version`/
//    `ev.ahead`), never asked.
//  - "Forcing past" a waiting step, the amber heavy-warning box, and the
//    audit-log record it wrote -- DESIGN.md names this specifically as
//    not carried forward. wizardRun.svelte.ts never calls
//    wizardState.record()/markSetupStep any more: there is no server-side
//    mark for a skipped or forced step left to write.
//  - "Explicit close only" (✕, Esc, no click-outside) -- also named as
//    retired. The full-screen wizard has no veil and nothing to click
//    outside of.
//  - The history-key minting step (#1133) and the Register step (#1291)
//    -- DESIGN.md is explicit that "there is no separate router ledger:
//    the five steps are the router ledger", so instance-wide housekeeping
//    like the retention key has no place in it any more; where it now
//    lives is not this scenario's call to make.
//  - The finish reading the whole ledger back as its own modal pane --
//    also named as retired. Finish now leads straight out to the fall
//    (wizardRun.finish()), covered by live-journey.mjs.
//
// What still holds, and is checked here: the wizard is an action over the
// shell, not a route of its own; the rail reads the record's five titles,
// in order; and the record's own gate -- a step ahead of the furthest
// reached is locked, dashed and untitled with the reason, and nothing
// short of it can be forced open (DESIGN.md, "The model": "Gated").
//
// The wizard's own router-mint scenario drives The router and Mint the token
// for real, end to end, against the server's own answers. Paste once,
// Tag firewall rules and Where setup stands (StepPaste.svelte,
// StepTune.svelte, StepStand.svelte) are still under active build
// (#1382/#1384); a scenario walking the record's remaining steps and its
// Finish belongs beside that one once they render more than today's
// stand-in text, not invented against components not yet settled.

import { session, feedSyslog, check, done, goTo, waitForStreamRows } from './live-browser.mjs'

// dismissSetup: false, because this scenario drives the wizard itself.
// Auto-launch will not have fired: it is gated on the instance having no
// devices, and the harness declares a router the reset keeps -- so the
// door under test here is the relaunch one, which is the same door.
// A bare "Run setup…" reads the whole fleet's evidence when no router
// is named yet (wizardRun.svelte.ts's evidence getter, the `!id`
// branch), and this instance is shared with every scenario that ran
// before it in this shard -- so by now some source has almost always
// sent syslog or fetched /ca.crt, which would open the run past The
// router (on a later step) rather than on it. Stripped here for the
// same reason the wizard's own router-mint scenario overrides this same
// endpoint: not a fake shape, just the one field this shared instance
// cannot otherwise be made to show reliably on demand. Registered
// through session(), before sign-in: the run is placed from the status
// read at sign-in, so a mock added afterwards never reaches it.
const stripArrivals = async (route) => {
  const res = await route.fetch()
  const body = await res.json()
  body.sources = (body.sources ?? []).map(({ syslogFirstSeenAt, caFetchedAt, ...s }) => s)
  await route.fulfill({ response: res, body: JSON.stringify(body) })
}
const { page, consoleErrors } = await session({
  dismissSetup: false,
  mocksApi: true,
  routes: [['**/api/setup/status', stripArrivals]],
})

// Its own traffic, so the shell is not empty when the wizard opens over
// it -- but not read by anything below: the mock above hides whichever
// source it arrives from.
feedSyslog(20, 'live-wizard-rail-gate')
await waitForStreamRows(page, 20)

// --- Run setup… opens the wizard, over whatever page is showing --------
// It is an action, not a page: the shell behind it stays mounted, which
// is the whole difference from the view #320 first replaced, and still
// true of the full screen that replaced the modal in turn -- it covers
// the shell (Wizard.svelte's own comment) rather than routing away from
// it.
await goTo(page, 'Run setup…')
const wizard = page.locator('.page.wiz')
await wizard.waitFor({ state: 'visible' })
check(
  await page.locator('.card[aria-hidden="false"] .scene-bar').isVisible(),
  'the shell is still there behind the wizard — this is an action, not a page',
)
check(!(await page.locator('main .setup').count()), 'no wizard page route remains — the view was removed wholesale')

// --- The rail is the record's five steps, in order ----------------------
const LEDGER_TITLES = ['The router', 'Mint the token', 'Paste once', 'Tag firewall rules', 'Where setup stands']
const stepTitles = await page.locator('.wiz .rail .step-title').allTextContents()
check(
  JSON.stringify(stepTitles) === JSON.stringify(LEDGER_TITLES),
  `the rail reads the record's five steps in order (${JSON.stringify(stepTitles)})`,
)

const row = (n) => page.locator(`.wiz .rail li:nth-child(${n}) .step-row`)
await page.locator('.wiz .body h3').waitFor({ state: 'visible' })
check((await row(1).getAttribute('aria-current')) === 'step', 'the run opens on The router')

// --- Gated: a step ahead of the furthest reached is locked --------------
// DESIGN.md, "The model": "a step ahead of the furthest reached is locked
// -- dashed number, dim, not clickable, titled 'After the step before
// it'." On a fresh run that is rows 2-5; rows 3-5 are checked here
// because the wizard's own router-mint scenario already pins row 2's own
// lock-then-unlock across the real mint. What that scenario does not
// check is that the lock actually holds against a click forced through
// it -- disabled and aria-disabled are claims, not proof.
for (const n of [3, 4, 5]) {
  check(
    (await row(n).getAttribute('aria-disabled')) === 'true' && (await row(n).getAttribute('title')) === 'After the step before it',
    `row ${n} is locked, titled with the reason`,
  )
}

const titleBefore = await page.locator('.wiz .body h3').textContent()
// force: true skips Playwright's own actionability check (which would
// otherwise refuse the click here for the same reason a real pointer
// could not reach it), so this is the one click that actually tests the
// HTML disabled attribute rather than Playwright's opinion of it.
await row(4).click({ force: true })
const titleAfter = await page.locator('.wiz .body h3').textContent()
check(titleAfter === titleBefore, `forcing a click through a locked row does not change the current step (${JSON.stringify(titleBefore)})`)
check((await row(1).getAttribute('aria-current')) === 'step', 'the run is still on The router')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
