// SPDX-License-Identifier: AGPL-3.0-only
//
// Tune logging (#435) against a real running mikroview: the surface as
// a fresh gate instance actually sees it (well under 24 hours of
// observation), the ephemerality wording verbatim from the issue body,
// and the analyse endpoint's under-24h and secret-rejection paths
// driven the way the browser itself would -- page.request.post, using
// the session's own cookie.
//
// What this deliberately does not cover: the >=24h "ready" path (the
// rule list, counters, render) -- a fresh instance has been observing
// for minutes, not a day, and there is no lever here to move a device's
// FirstSeen back 24 hours. internal/api/tunelogging_test.go already
// exercises that path with a synthetic FirstSeen; this scenario proves
// the same server behaves the same way end to end for the one window a
// live run can actually reach.

import { readFileSync } from 'fs'
import { fileURLToPath } from 'url'
import path from 'path'
import { session, goTo, check, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..')

// The shared fixture: a real hide-sensitive export with six filter
// rules and no secret-shaped keys (internal/routeros/export's own
// parser test data, reused rather than a second copy of the same
// shape).
const fixtureExport = readFileSync(
  path.join(REPO, 'internal/routeros/export/testdata/hide-sensitive.rsc'),
  'utf8',
)

const { page, consoleErrors } = await session({ waitForEvents: 20 })

// --- Reach the page the way the wizard's finish screen offers it ------
// (#435 decision 2's other way in is a dark pair on the topography's
// coverage lens; the wizard's link is used here because it needs no
// particular boundary state to be set up first).
await goTo(page, 'Run setup…')
const modal = page.locator('.setup-wizard')
await modal.waitFor({ state: 'visible' })

await page.locator('.setup-wizard .steps li:nth-child(6) .step-row').click()
const tuneLink = page.locator('.setup-wizard button.link:text-is("Tune logging…")')
await tuneLink.waitFor({ state: 'visible' })
await tuneLink.click()
await modal.waitFor({ state: 'detached' })

await page.waitForSelector('.og h3:has-text("tune logging")')

// --- The ephemerality sentence, verbatim from the issue body ----------
// (#435 issue, "Never persisted": "your config is never stored -- it
// runs through memory, and once you leave this page it is gone.")
const ephemeral = ((await page.textContent('.note.ephemeral')) ?? '').trim()
check(
  ephemeral === 'Your config is never stored — it runs through memory, and once you leave this page it is gone.',
  `the ephemerality sentence renders verbatim (${JSON.stringify(ephemeral)})`,
)

// --- Under 24h: the waiting message, and nothing derived ---------------
// By the time this scenario runs, earlier scenarios (live-setup-wizard,
// among others) have already pushed at least one device, so the picker
// (if shown at all) has a real option to choose.
const deviceSelect = page.locator('#tl-device')
if (await deviceSelect.count()) {
  await deviceSelect.selectOption({ index: 1 })
}
await page.fill('#tl-export', fixtureExport)
await page.click('button.primary:has-text("Analyse")')

const waiting = page.locator('.observation.waiting')
await waiting.waitFor({ state: 'visible', timeout: 10000 })
const waitingText = ((await waiting.textContent()) ?? '').trim()
check(
  /^Watching for \d+ hours?; suggestions arrive at 24 hours\.$/.test(waitingText),
  `the under-24h waiting message renders in the component's own words (${JSON.stringify(waitingText)})`,
)
check((await page.locator('.rules').count()) === 0, 'no rule list renders before 24 hours of observation')
check((await page.locator('.load-error').count()) === 0, 'the fixture export is not rejected')

// --- The same endpoint, called directly ---------------------------------
// Point 3 of the finishing brief: prove the under-24h shape and the
// secret-rejection gate hold at the wire, not only as the component
// happens to render them.
const analyseRes = await page.request.post(`${URL_BASE}/api/tune-logging/analyse`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { device: 'edge-1', export: fixtureExport, darkBoundaries: [] },
})
check(analyseRes.status() === 200, `POST analyse with the fixture export answers 200 (${analyseRes.status()})`)
const analyseBody = await analyseRes.json()
check(analyseBody.ready === false, `ready is false well under 24 hours of observation (${analyseBody.ready})`)
check(
  Array.isArray(analyseBody.rules) && analyseBody.rules.length === 0,
  `rules is empty before 24 hours (${JSON.stringify(analyseBody.rules)})`,
)

// A plain (non-hide-sensitive) export with a live password must be
// refused outright -- the parser-level safety gate (internal/routeros/
// export's secretKeys), exercised end to end through the same endpoint.
const secretExport = [
  '# 2026/09/01 10:00:00 by RouterOS 7.24.1',
  '/ppp secret',
  'add name=vpn-user password=hunter2',
  '',
].join('\n')
const rejectRes = await page.request.post(`${URL_BASE}/api/tune-logging/analyse`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { device: 'edge-1', export: secretExport, darkBoundaries: [] },
})
check(rejectRes.status() === 400, `POST analyse with a live password is refused (${rejectRes.status()})`)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
