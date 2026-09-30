// SPDX-License-Identifier: AGPL-3.0-only
//
// The tour's way in and way out (#1386, keeping #646's tour) against a
// real running mikroview: the wizard's Finish lands on the fall and,
// a beat later, offers the tour once; "not now" spends the offer for
// this account, so a second Finish never brings it back; and the
// account menu's "Take the tour" starts it any time and "leave the
// tour" ends it back where it was started.
//
// What this scenario cannot show, and why: the harness's own shared
// instance starts "with real syslog listeners and a real admin
// account" (the live-check skill's own words), so the *first-run*
// way in (AuthSetup's Continue straight into the wizard) never plays
// here -- the same reason the wizard's own scenarios test the relaunch
// door rather than its first auto-launch. The offer itself does not
// depend on that: wizardRun.finish() makes it after any Finish that
// lands on the fall, once per account (lib/tour.svelte.ts), and Run
// setup… is the door that lands there (wizardState.launch() sets
// finishTo to 'fall'; Add a router and Re-enrol… lead to the fleet,
// which gets no offer -- pinned in lib/tour.svelte.test.ts).
//
// Reaching Finish needs the run at the 'done' stage, and wizardRun.ts's
// footSpec offers it nowhere else. wizardRun.begin() reads the ledger
// from evidence alone (the same evidence a real walk leaves), so this
// reaches 'done' the way *reopening* an already-provisioned instance
// would: a syslog source on record and a certificate fetch on record
// are the only two things a bare "Run setup…" reads for the fleet as a
// whole (wizardRun.svelte.ts's evidence getter, the `!id` branch).
//
// MV_SHOT=<path> saves a screenshot of the offer over the fall, for
// looking at it against the fall's design.
import { session, check, done, goTo, feedSyslog, openAccountMenu } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const { page, consoleErrors } = await session({ dismissSetup: false, landing: 'fall' })

// --- No tour chrome on an ordinary, already-provisioned session --------
check(
  (await page.locator('.offerwrap, .tour .bar').count()) === 0,
  'neither the offer nor the tour renders itself on a session that never asked',
)

// --- The wizard's finish leads back to the fall, whichever door opened it ---
// Driven directly rather than assumed from the suite's baseline feed
// (run-scenarios.sh's `live-baseline`), which does not run when this
// script is invoked on its own (the live-check skill's `node
// scripts/live-smoke.mjs`-style usage).
feedSyslog(1, 'live-journey-finish')
await page.request.get(`${URL_BASE}/ca.crt`)

await goTo(page, 'Run setup…')
const wizard = page.locator('.page.wiz')
await wizard.waitFor({ state: 'visible' })

const finish = page.locator('.wiz .foot button.primary:text-is("Finish")')
await finish.waitFor({ state: 'visible', timeout: 15000 })
await finish.click()
await wizard.waitFor({ state: 'detached' })

const fallCentred = () => {
  const deck = document.querySelector('.deck')
  const el = deck?.querySelector('.card[data-card="fall"]')
  if (!el) return false
  return Math.abs(el.getBoundingClientRect().top - deck.getBoundingClientRect().top) < 2
}
await page.waitForFunction(fallCentred, { timeout: 10000 })
check(true, 'the finish lands on the fall, centred in the deck')

// --- The offer rises once the way out has landed -------------------------
// The way out plays ~6.5s at three-quarters speed, then a beat
// (OFFER_DELAY_MS) before the panel; generous, since the host is shared.
const offer = page.locator('.offerwrap')
await offer.waitFor({ state: 'visible', timeout: 20000 })
check(await page.locator('.offer .big').innerText() === 'Take the tour?', 'the offer asks in the menu row\'s own words')
check(
  /^Eight cards\. About three minutes\. It ends back here, on the fall\.$/.test(await page.locator('.offer .story').innerText()),
  'the offer sizes the promise to the admin\'s eight cards',
)
check(fallCentred, 'the fall is still under it')
if (process.env.MV_SHOT) await page.screenshot({ path: process.env.MV_SHOT })

await page.locator('.offer button.later').click()
await offer.waitFor({ state: 'detached' })
check(true, '"not now" takes the offer down')

// --- Once: a second Finish lands on the fall with no offer ---------------
await goTo(page, 'Run setup…')
await wizard.waitFor({ state: 'visible' })
await finish.waitFor({ state: 'visible', timeout: 15000 })
await finish.click()
await wizard.waitFor({ state: 'detached' })
await page.waitForFunction(fallCentred, { timeout: 10000 })
// Past the way out and the offer's own delay, with margin.
await page.waitForTimeout(12000)
check((await offer.count()) === 0, 'the offer is not made again once it has been answered')

// --- The menu row starts the tour any time; it ends where it started ----
await goTo(page, 'Metrics')
await openAccountMenu(page)
await page.locator('.account .menu button.row:text-is("Take the tour")').click()
const bar = page.locator('.tour .bar')
await bar.waitFor({ state: 'visible', timeout: 5000 })
check(/THE FALL · 1 OF 8/.test(await bar.locator('.progress').innerText()), 'the tour opens on the fall, 1 of the admin\'s 8')
await bar.locator('button.leave').click()
await bar.waitFor({ state: 'detached' })
await page.waitForFunction(() => {
  const deck = document.querySelector('.deck')
  const el = deck?.querySelector('.card[data-card="metrics"]')
  if (!el) return false
  return Math.abs(el.getBoundingClientRect().top - deck.getBoundingClientRect().top) < 2
}, { timeout: 10000 })
check(true, 'leaving the tour rolls back to the card it was started from')

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
