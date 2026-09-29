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
// everything that ran before it -- the same reason the wizard's own
// scenarios test the *relaunch* door rather than its first auto-launch.
// Those beats, and the tour's advance/skip/leave mechanics, are covered
// by component tests instead (lib/journey.svelte.test.ts,
// JourneyAttach/JourneyGlass/JourneyTour.svelte.test.ts). #750 B1's
// arrival gate is in that same set: beat 3 now waits on the first event
// reaching appState.events rather than on a 3.2s timer, and this
// instance has been pouring events since long before the page loaded --
// so the one state a shared instance could ever show here is the state
// after the gate, which is indistinguishable from the old timer having
// run. The gate is pinned in JourneyGlass.svelte.test.ts, which can
// hold the buffer empty and watch the clock not move the beat on.
//
// What a real browser against a real server *can* still prove here: the
// one piece of #646 that is not gated on instance freshness -- the wizard
// ends by taking the operator back to the fall, on this instance exactly
// as it would on a fresh one. And that the journey's own chrome never
// leaks into an ordinary, already-provisioned session.
//
// The full-screen wizard (#1381) moved that finish from SetupWizard.svelte's
// leaveToLanding to wizardRun.finish() (wizardRun.svelte.ts):
// `appState.view = wizardState.finishTo === 'fleet' ? 'fleet' : 'fall'`,
// then `wizardState.close()`. Run setup… is the door that sets finishTo
// to 'fall' (wizardState.launch()) -- Add a router and Re-enrol… lead to
// the fleet instead, which is not what this scenario is proving.
//
// Reaching Finish needs the run at the 'done' stage, and wizardRun.ts's
// footSpec offers it nowhere else. Paste once and Tag firewall rules
// (StepPaste.svelte, StepTune.svelte) are still stubs under #1382/#1384,
// with no Copy button yet to drive a walk through them -- but
// wizardRun.begin() reads the ledger from evidence alone (the same
// evidence a real walk leaves), so this reaches 'done' the way *reopening*
// an already-provisioned instance would, without needing that UI: a
// syslog source on record and a certificate fetch on record are the only
// two things a bare "Run setup…" reads for the fleet as a whole
// (wizardRun.svelte.ts's evidence getter, the `!id` branch -- there is no
// specific router named, so this cannot also check a receipt naming one;
// the wizard's own router-mint scenario proves that for a walk that does).
import { session, check, done, goTo, feedSyslog } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const { page, consoleErrors } = await session({ dismissSetup: false, landing: 'fall' })

// --- No journey chrome on an ordinary, already-provisioned session -----
check(
  (await page.locator('.attach-screen, .glasswrap, .tour .bar').count()) === 0,
  'the journey never renders itself on a session that was never its trigger',
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

await page.waitForFunction(() => {
  const deck = document.querySelector('.deck')
  const el = deck?.querySelector('.card[data-card="fall"]')
  if (!el) return false
  return Math.abs(el.getBoundingClientRect().top - deck.getBoundingClientRect().top) < 2
}, { timeout: 10000 })
check(true, 'the finish lands on the fall, centred in the deck')

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
