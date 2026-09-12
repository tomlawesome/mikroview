// SPDX-License-Identifier: AGPL-3.0-only
//
// Log every rule (#435; "Tune logging" until #1134 renamed it) against
// a real running mikroview: the surface as a fresh gate instance
// actually sees it (well under 24 hours of observation), the
// ephemerality wording verbatim from the issue body, and the analyse
// endpoint's under-24h and secret-rejection paths driven the way the
// browser itself would -- page.request.post, using the session's own
// cookie.
//
// #1134 also put the page on the deck and made its input one drop zone,
// so what this drives now is the drop itself (a real DragEvent carrying
// a real File) and the deck's own roll rail as the way off the page.
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
import { session, feedSyslog, goTo, check, done, waitForStreamRows } from './live-browser.mjs'

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

const { page, consoleErrors } = await session()
// Its own traffic: the instance is reset before every scenario (#1064),
// so nothing a sibling fed is there to count.
feedSyslog(20, 'live-log-every-rule')
await waitForStreamRows(page, 20)

// --- Reach the page the way the wizard's finish screen offers it ------
// (#435 decision 2's other way in is a dark boundary's own card on the
// map -- round 49 made coverage always-on material rather than a lens,
// so `rules ▸` on that card is the other door. The wizard's link is
// used here because it needs no particular boundary state set up
// first).
await goTo(page, 'Run setup…')
const modal = page.locator('.setup-wizard')
await modal.waitFor({ state: 'visible' })

// The finish row ("Where setup stands") is nth-child(7): #394 added
// "Back up the router" as the ledger's sixth step, so the finish row --
// rendered after the six-item ledger loop -- shifted from position 6.
await page.locator('.setup-wizard .steps li:nth-child(7) .step-row').click()
const pageLink = page.locator('.setup-wizard button.link:text-is("Log every rule…")')
await pageLink.waitFor({ state: 'visible' })
await pageLink.click()
await modal.waitFor({ state: 'detached' })

await page.waitForSelector('.og h3:has-text("log every rule")')

// --- The page has the app's own navigation on it (#1134, fault 1) -----
// It used to render outside the deck, which is the app's navigation, so
// there was no way off it. The card and the roll rail's own entry are
// what the ruling ("same shell as every other page") means in the DOM.
check(
  (await page.locator('.deck .card[data-card="log-every-rule"]').count()) === 1,
  'the page is a deck card, not a shell of its own',
)
const railEntry = page.locator('.roll-rail button.rail-name:text-is("Log every rule")')
check((await railEntry.count()) === 1, 'the roll rail names it, so there is a way off the page')
check(
  (await railEntry.getAttribute('aria-current')) === 'page',
  'the rail marks it as the card you are on',
)

// And the way off really works: roll to Topography and back.
await goTo(page, 'Topography')
await goTo(page, 'Log every rule')
await page.waitForSelector('.og h3:has-text("log every rule")')

// --- The lead sentence, verbatim from #1134's ruling ------------------
// The owner's third fault was that the page never said what it was for.
const lead = ((await page.textContent('.lead')) ?? '').replace(/\s+/g, ' ').trim()
check(
  lead ===
    "Drop in your router's export (/export hide-sensitive). You get it back with logging switched on for every " +
      'firewall rule that is not logging yet, ready to paste into the router. Nothing you paste is stored.',
  `the lead sentence renders verbatim (${JSON.stringify(lead)})`,
)

// --- The ephemerality sentence, verbatim from the issue body ----------
// (#435 issue, "Never persisted": "your config is never stored -- it
// runs through memory, and once you leave this page it is gone.")
const ephemeral = ((await page.textContent('.note.ephemeral')) ?? '').trim()
check(
  ephemeral === 'Your config is never stored — it runs through memory, and once you leave this page it is gone.',
  `the ephemerality sentence renders verbatim (${JSON.stringify(ephemeral)})`,
)

// --- Under 24h: the waiting message, and nothing derived ---------------
// The picker is only drawn when there is more than one router to choose
// between (#1134's layout), so it is taken only if it is there.
const deviceSelect = page.locator('#ler-device')
if (await deviceSelect.count()) {
  await deviceSelect.selectOption({ index: 1 })
}

// --- The drop zone is the input (#1134, fault 2) ----------------------
// A real drop, not setInputFiles on the hidden control: the drop is the
// interaction the ruling names first, and it is the one no unit test
// can prove against a real browser's DataTransfer.
await page.evaluate((text) => {
  const transfer = new DataTransfer()
  transfer.items.add(new File([text], 'edge-1.rsc', { type: 'text/plain' }))
  document
    .querySelector('.card[data-card="log-every-rule"] .drop')
    .dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: transfer }))
}, fixtureExport)

const picked = page.locator('.drop.filled .drop-picked')
await picked.waitFor({ state: 'visible', timeout: 10000 })
check((await picked.textContent()) === 'edge-1.rsc', 'the zone names the file that was dropped on it')
check(
  ((await page.textContent('.drop-sub')) ?? '').includes('7 firewall rules in it'),
  `the zone counts the fixture's seven filter rules (${JSON.stringify(await page.textContent('.drop-sub'))})`,
)
const card = page.locator('.card[data-card="log-every-rule"]')
// The native control is kept and never shown (#1134): LogEveryRule's
// `.sr-only` is the 1px clip-path idiom, deliberately not display:none,
// so clicking the zone can still open the browser's file chooser and a
// screen reader still finds the input. `:visible` is the wrong question
// to ask of that -- Playwright counts a 1x1 clipped box as visible -- so
// this asserts what the operator actually gets: a control with no area
// to see or hit, and a zone that is the thing under the pointer.
const fileInput = card.locator('input[type="file"]')
check((await fileInput.count()) === 1, 'the native file control is kept, for the click-to-browse path')
const fileBox = await fileInput.boundingBox()
check(
  fileBox !== null && fileBox.width <= 1 && fileBox.height <= 1,
  `the file control occupies at most one pixel (${JSON.stringify(fileBox)})`,
)
const atZone = await page.evaluate(() => {
  const drop = document.querySelector('.card[data-card="log-every-rule"] .drop')
  const r = drop.getBoundingClientRect()
  const el = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2)
  return { inDrop: !!el?.closest('.drop'), isFileInput: el instanceof HTMLInputElement && el.type === 'file' }
})
check(
  atZone.inDrop && !atZone.isFileInput,
  `the drop zone is what the pointer lands on, not the file control (${JSON.stringify(atZone)})`,
)
check((await card.locator('textarea').count()) === 0, 'and there is no second control beside the zone')

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
