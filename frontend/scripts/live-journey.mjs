// SPDX-License-Identifier: AGPL-3.0-only
//
// The journey (#646) against a real running mikroview.
//
// What this scenario cannot show, and why: the harness's own shared
// instance starts "with real syslog listeners and a real admin
// account" (the live-check skill's own words) -- exactly the state
// journeyState.begin() (lib/journey.svelte.ts) never fires into. A
// brand-new, zero-account instance is what Attach, Connecting, the
// glass and the tour choreograph across, and every scenario in this
// suite runs against one already-provisioned instance shared with
// everything that ran before it -- the same reason live-setup-wizard.mjs
// tests the modal's *relaunch* door rather than its first auto-launch.
// Those beats, and the tour's advance/skip/leave mechanics, are covered
// by component tests instead (lib/journey.svelte.test.ts,
// JourneyAttach/JourneyGlass/JourneyTour.svelte.test.ts).
//
// What a real browser against a real server *can* still prove here: the
// one piece of #646 that is not gated on instance freshness -- the full
// wizard ends by taking the operator back to the fall (SetupWizard.svelte's
// leaveToLanding), on this instance exactly as it would on a fresh one.
// And that the journey's own chrome never leaks into an ordinary,
// already-provisioned session.

import { session, check, done, goTo } from './live-browser.mjs'

const { page, consoleErrors } = await session({ dismissSetup: false, landing: 'fall' })

// --- No journey chrome on an ordinary, already-provisioned session -----
check(
  (await page.locator('.attach-screen, .glasswrap, .tour .bar').count()) === 0,
  'the journey never renders itself on a session that was never its trigger',
)

// --- The wizard's finish leads back to the fall, whichever door opened it ---
const modal = page.locator('.setup-wizard')
if (await modal.count()) {
  await page.keyboard.press('Escape')
  await modal.waitFor({ state: 'detached' })
}

await goTo(page, 'Run setup…')
await modal.waitFor({ state: 'visible' })

await page.locator('.setup-wizard .steps .finish-row').click()
await page.locator('.setup-wizard .readback').waitFor({ state: 'visible' })

await page.click('.setup-wizard footer button:text-is("Take me to the fall")')
await modal.waitFor({ state: 'detached' })

await page.waitForFunction(() => {
  const deck = document.querySelector('.deck')
  const el = deck?.querySelector('.card[data-card="fall"]')
  if (!el) return false
  return Math.abs(el.getBoundingClientRect().top - deck.getBoundingClientRect().top) < 2
}, { timeout: 10000 })
check(true, 'the finish lands on the fall, centred in the deck')

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
