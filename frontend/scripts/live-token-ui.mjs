// SPDX-License-Identifier: AGPL-3.0-only
//
// #326: minting an ingest token entirely through the UI, then using it
// for a real push. The gap this guards against was found by the owner
// live: the dialog could only create read-only tokens while the docs
// said otherwise, and a read-only token named "routeros-ingest" pasted
// into a router script produced nothing but a 404. The API layer is
// covered by live-ingest-token.mjs; this scenario is the operator's
// path -- find the door, mint, read the one-time banner -- ending in the
// one thing that proves the minted token is the right kind: a push that
// lands.
//
// #490 moved that path. The Tokens page is gone; minting happens at the
// engine room's "which machines may speak" door. The checks below are
// deliberately the same operator questions as before -- can I mint the
// right kind, does the pick-list match the devices this instance knows,
// is the secret shown exactly once, does revoking it stop the push --
// asked at the new location rather than rewritten around it.

import { session, check, done, goTo } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

// Scoped to the tokens door throughout: the people door beside it uses
// the same .row/.verb markup, so an unscoped `.row:has-text(...)` would
// be one badly-chosen token name away from clicking Remove on a person.
const DOOR = '.door:has-text("Which machines may speak")'

// goTo's own wait (SCENES in live-browser.mjs, waiting for the engineroom card to centre) is what proves arrival --
// this used to also wait for `.page-header h2`, but #700 unmounted PageHeader from EngineRoom.svelte entirely, so
// that selector no longer exists anywhere on the page (#667 group E).
await goTo(page, 'Settings')
check(true, "the rail's engine room row opens the engine room")
check((await page.$$('.modal')).length === 0, 'no modal renders -- the doors are part of the page')
check(
  await page
    .locator(`${DOOR} .dname`)
    .waitFor({ timeout: 5000 })
    .then(() => true, () => false),
  'the machines door is on the page for an admin',
)

// --- Ingest token, entirely through the door ----------------------------
await page.click(`${DOOR} .footer-action`)
await page.waitForSelector(`${DOOR} .inline-form`)
await page.fill(`${DOOR} .inline-form input[type="text"]`, 'ui-ingest')
await page.selectOption(`${DOOR} select[aria-label="Key kind"]`, 'ingest')

// The pick-list must offer every device mikroview knows about --
// configured (live-env.sh declares one) or discovered from its own
// syslog (which is all the container harness has). Whichever this
// instance has, the list has to match /api/devices exactly: free text
// was the typo trap this list exists to remove.
const known = await page.request
  .get(`${URL_BASE}/api/devices`)
  .then(async (r) => ((await r.json()).devices ?? []).map((d) => d.id).sort())
check(known.length > 0, `the instance knows at least one device (${known})`)

const deviceSelect = `${DOOR} select[aria-label="Device this key speaks for"]`
await page.waitForSelector(deviceSelect)
const options = await page.$$eval(`${deviceSelect} option:not([disabled])`, (els) => els.map((e) => e.value).sort())
check(
  JSON.stringify(options) === JSON.stringify(known),
  `the pick-list offers exactly the known devices (list=${options} api=${known})`,
)
const DEVICE = known[0]
await page.selectOption(deviceSelect, DEVICE)

await page.click(`${DOOR} .inline-form .save`)
await page.waitForSelector(`${DOOR} .secretbanner .sk`)
const token = (await page.textContent(`${DOOR} .secretbanner .sk`))?.trim() ?? ''
check(token.length > 0, 'the one-time value banner shows the new token')

// Which device an ingest key speaks for is the fact an operator revokes
// on -- with two routers pushing, "ingest" alone does not say which key
// belongs to which. The old Tokens page carried it and the door has to
// as well.
check(
  await page
    .locator(`${DOOR} .row:has-text("ui-ingest") .chip:has-text("ingest: ${DEVICE}")`)
    .waitFor({ timeout: 15000 })
    .then(() => true, () => false),
  'the door row says what the token is and which device it speaks for',
)

// --- The proof: the UI-minted token actually ingests --------------------
// log stays false deliberately: scenarios share one instance, and
// live-watchlist-coverage's starting-state assertion is that no table
// pushed by an earlier scenario contains a logging rule. A non-logging
// rule proves the token's kind and scope just as well.
const payload = {
  kind: 'filter-rule',
  page: 1,
  pages: 1,
  records: [
    {
      ordinal: 0,
      comment: 'ui token check',
      chain: 'input',
      action: 'drop',
      srcAddressList: '',
      logPrefix: '',
      dstPort: '3389',
      protocol: 'tcp',
      log: false,
      dstAddress: '',
      srcAddress: '',
    },
  ],
}
const pushed = await fetch(`${URL_BASE}/api/ingest/routeros`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
  body: JSON.stringify(payload),
})
check(pushed.status === 200, `the UI-minted ingest token pushes a filter-rule payload (${pushed.status})`)

// --- Read-only stays the default and still works ------------------------
await page.click(`${DOOR} .footer-action`)
await page.waitForSelector(`${DOOR} .inline-form`)
await page.fill(`${DOOR} .inline-form input[type="text"]`, 'ui-readonly')
await page.selectOption(`${DOOR} select[aria-label="Key kind"]`, 'api')
await page.click(`${DOOR} .inline-form .save`)

// Both waits below look redundant and are not. The copy-once banner is
// already on screen from the ingest token above, so waiting for the
// banner returns instantly and proves nothing -- it has to be the *text*
// that is waited on, or this reads the ingest token back and the
// /api/events check gets the 404 an ingest token is meant to get.
// page.isVisible() has the same shape of problem: it answers
// immediately rather than waiting, and the new row lands a few tens of
// milliseconds after the click. Both passed only by luck until
// live-suggestions.mjs started running ahead of this scenario (#547)
// and put one more token in the list to fetch and render.
const roRow = page.locator(`${DOOR} .row:has-text("ui-readonly") .chip:has-text("read")`)
check(
  await roRow.waitFor({ timeout: 15000 }).then(() => true, () => false),
  'a default-kind token is labelled read in the door',
)
await page
  .waitForFunction(
    (previous) => document.querySelector('.door .secretbanner .sk')?.textContent?.trim() !== previous,
    token,
    { timeout: 15000 },
  )
  .catch(() => {})
const roToken = (await page.textContent(`${DOOR} .secretbanner .sk`))?.trim() ?? ''
check(roToken !== token, 'the banner shows the new read-only token, not the ingest one above')
const events = await fetch(`${URL_BASE}/api/events`, {
  headers: { Authorization: `Bearer ${roToken}` },
})
check(events.status === 200, `the read-only token reads /api/events (${events.status})`)

// --- Tidy up, via the door's own Revoke (confirm() included) ------------
page.on('dialog', (d) => d.accept())
for (const name of ['ui-ingest', 'ui-readonly']) {
  await page.click(`${DOOR} .row:has-text("${name}") .verb`)
  await page.waitForSelector(`${DOOR} .row:has-text("${name}")`, { state: 'detached' })
}
const afterRevoke = await fetch(`${URL_BASE}/api/ingest/routeros`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
  body: JSON.stringify(payload),
})
check(afterRevoke.status === 401, `a revoked token stops working (${afterRevoke.status})`)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
