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
// begin() only lands straight on 'done', though, if the browser's own
// cached evidence already carries that cert fetch and syslog arrival --
// true once some earlier scenario has already touched /ca.crt on this
// instance, but not on a freshly booted one where this script is the
// first to (a lone run, or this family's own slice under sharding,
// #1004: shards start on an empty instance beyond the baseline feed).
// There the reopened ledger lands on 'watch' instead, and reaching
// Finish needs walking forward the way every other door does -- see
// reachFinish() below, which takes the same route live-enrolment.mjs
// does through Tag firewall rules.
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
const foot = wizard.locator('.foot')
const finish = foot.locator('button.primary:text-is("Finish")')

// reachFinish walks the reopened ledger the rest of the way to Finish
// when it has not already landed there (see the header comment above):
// Next past Paste once -- arrivedAll's own wait, since the evidence is
// polled in rather than pushed -- then Skip this step at Tag firewall
// rules (this walk tags nothing), the same route live-enrolment.mjs
// takes to reach its own Finish.
async function reachFinish() {
  if (!(await finish.count())) {
    await foot.locator('button.primary:text-is("Next")').click()
    await foot.locator('button:text-is("Skip this step")').click()
  }
  await finish.waitFor({ state: 'visible', timeout: 15000 })
}

await reachFinish()
await finish.click()

// --- The way out's groups, as the box leaves (#1386) ----------------------
// Read the moment the journey starts: the bar's wordmark is the ride's
// alone from Finish until the ride lands (`.page.wiz.live` drops), and
// the chips and the strip stay as the record (ak-bar, ak-strip on
// <body> from the start, as an.js had them).
await page.waitForFunction(() => document.body.classList.contains('journey'), { timeout: 5000 })
const started = await page.evaluate(() => ({
  akBar: document.body.classList.contains('ak-bar'),
  akStrip: document.body.classList.contains('ak-strip'),
  barLive: !!document.querySelector('.page.wiz.live'),
}))
check(started.akBar && started.akStrip, 'the bar\'s chips and the strip stay as the record while the box leaves')
check(!started.barLive, 'the bar gives up its wordmark while the ride carries it')

await wizard.waitFor({ state: 'detached' })

const fallCentred = () => {
  const deck = document.querySelector('.deck')
  const el = deck?.querySelector('.card[data-card="fall"]')
  if (!el) return false
  return Math.abs(el.getBoundingClientRect().top - deck.getBoundingClientRect().top) < 2
}
await page.waitForFunction(fallCentred, { timeout: 10000 })
check(await page.evaluate(fallCentred), 'the finish lands on the fall, centred in the deck')

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
check(await page.evaluate(fallCentred), 'the fall is still under it')
if (process.env.MV_SHOT) await page.screenshot({ path: process.env.MV_SHOT })

await page.locator('.offer button.later').click()
await offer.waitFor({ state: 'detached' })
check((await offer.count()) === 0, '"not now" takes the offer down')

// --- What stands bare under the swell (#1386, DESIGN.md's sixth beat) ---
// Under the cover the fall's frame stands bare: its columns strike at
// 4.45s, the axis and the body fade in at 4.8s (<body> gains ak-fb),
// the side rail comes with the last beat (`journey` comes off). The
// rules are journey.css's, read off the real stylesheet with the
// journey's classes put on <body> by hand once the fall stands still --
// the journey's own clock is not a thing a shared host can be asked to
// sample at the right moment (wizardJourney.test.ts pins the clock; this
// pins what the classes do to the fall). jsdom does not cascade
// stylesheets, so this cannot live in vitest.
// The axis and the body fade over 600ms and the NOW line pulses, so
// each state is read after its transition has run, and "on" is read
// off the axis labels (the NOW line's pulse never sits at exactly 1).
const readFall = () =>
  page.evaluate(() => {
    const op = (sel) => {
      const el = document.querySelector(sel)
      return el ? getComputedStyle(el).opacity : 'missing'
    }
    return { axis: op('.fall .rig svg .tlab'), now: op('.fall .rig svg .nowline'), dot: op('.fall .rig svg .now-dot'), rail: op('.deck-shell .roll-rail') }
  })
const bodyClasses = (add, remove) => page.evaluate(([a, r]) => { document.body.classList.add(...a); document.body.classList.remove(...r) }, [add, remove])
const still = await readFall()
await bodyClasses(['journey', 'am'], [])
await page.waitForTimeout(800)
const underTheSwell = await readFall()
await bodyClasses(['ak-fb'], [])
await page.waitForTimeout(800)
const bodyBeat = await readFall()
await bodyClasses([], ['journey', 'am', 'ak-fb'])
await page.waitForTimeout(800)
const after = await readFall()
check(still.axis === '1' && still.rail === '1', `the fall stands before the check -- got ${JSON.stringify(still)}`)
check(
  underTheSwell.axis === '0' && underTheSwell.now === '0' && underTheSwell.dot === '0',
  `the fall's axis and NOW line stand bare under the swell -- got ${JSON.stringify(underTheSwell)}`,
)
check(underTheSwell.rail === '0', `the deck's roll rail waits for the last beat -- got ${underTheSwell.rail}`)
check(bodyBeat.axis === '1' && bodyBeat.rail === '0', `the body's beat brings the axis but not the rail -- got ${JSON.stringify(bodyBeat)}`)
check(after.axis === '1' && after.rail === '1', `the fall and the rail stand once the journey is off -- got ${JSON.stringify(after)}`)

// --- Once: a second Finish lands on the fall with no offer ---------------
await goTo(page, 'Run setup…')
await wizard.waitFor({ state: 'visible' })
await reachFinish()
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
const metricsCentred = () => {
  const deck = document.querySelector('.deck')
  const el = deck?.querySelector('.card[data-card="metrics"]')
  if (!el) return false
  return Math.abs(el.getBoundingClientRect().top - deck.getBoundingClientRect().top) < 2
}
await bar.locator('button.leave').click()
await bar.waitFor({ state: 'detached' })
await page.waitForFunction(metricsCentred, { timeout: 10000 })
check(await page.evaluate(metricsCentred), 'leaving the tour rolls back to the card it was started from')

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
