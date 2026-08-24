// SPDX-License-Identifier: AGPL-3.0-only
//
// #326: minting an ingest token entirely through the UI, then using it
// for a real push. The gap this guards against was found by the owner
// live: the dialog could only create read-only tokens while the docs
// said otherwise, and a read-only token named "routeros-ingest" pasted
// into a router script produced nothing but a 404. The API layer is
// covered by live-ingest-token.mjs; this scenario is the operator's
// path -- menu, dialog, dropdowns, copy banner -- ending in the one
// thing that proves the minted token is the right kind: a push that
// lands.

import { session, check, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

// #548: Tokens is a page now, not a modal -- TokensOverlay retired
// wholesale. Waiting on the page header (rather than reusing
// `.create-form`, which existed under both names) is what actually
// proves the navigation landed on the new page rather than merely that
// *some* form rendered somewhere.
await page.click('.rail .item:has-text("Tokens")')
await page.waitForSelector('.page-header h2')
check(
  (await page.textContent('.page-header h2'))?.trim() === 'Tokens',
  'the rail\'s Tokens row opens the Tokens page',
)
check((await page.$$('.modal')).length === 0, 'no modal renders -- the overlay is gone, this is a page')

// --- Ingest token, entirely through the dialog --------------------------
await page.fill('.create-form input[type="text"]', 'ui-ingest')
await page.selectOption('.kind-select', 'ingest')

// The pick-list must offer every device mikroview knows about --
// configured (live-env.sh declares one) or discovered from its own
// syslog (which is all the container harness has). Whichever this
// instance has, the list has to match /api/devices exactly: free text
// was the typo trap this list exists to remove.
const known = await page.request
  .get(`${URL_BASE}/api/devices`)
  .then(async (r) => ((await r.json()).devices ?? []).map((d) => d.id).sort())
check(known.length > 0, `the instance knows at least one device (${known})`)

await page.waitForSelector('.device-select')
const options = await page.$$eval('.device-select option:not([disabled])', (els) =>
  els.map((e) => e.value).sort(),
)
check(
  JSON.stringify(options) === JSON.stringify(known),
  `the pick-list offers exactly the known devices (list=${options} api=${known})`,
)
const DEVICE = known[0]
await page.selectOption('.device-select', DEVICE)

await page.click('.create-form .save')
await page.waitForSelector('.created-value')
const token = (await page.textContent('.created-value'))?.trim() ?? ''
check(token.length > 0, 'the one-time value banner shows the new token')

check(
  await page.isVisible(`.row:has-text("ui-ingest") .kind-badge:has-text("ingest: ${DEVICE}")`),
  'the list row says what the token is and which device it speaks for',
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
await page.fill('.create-form input[type="text"]', 'ui-readonly')
await page.selectOption('.kind-select', 'api')
await page.click('.create-form .save')
await page.waitForSelector('.created-value')
const roToken = (await page.textContent('.created-value'))?.trim() ?? ''
check(
  await page.isVisible('.row:has-text("ui-readonly") .kind-badge:has-text("read-only")'),
  'a default-kind token is labelled read-only in the list',
)
const events = await fetch(`${URL_BASE}/api/events`, {
  headers: { Authorization: `Bearer ${roToken}` },
})
check(events.status === 200, `the read-only token reads /api/events (${events.status})`)

// --- Tidy up, via the dialog's own revoke (confirm() included) ----------
page.on('dialog', (d) => d.accept())
for (const name of ['ui-ingest', 'ui-readonly']) {
  await page.click(`.row:has-text("${name}") .revoke`)
  await page.waitForSelector(`.row:has-text("${name}")`, { state: 'detached' })
}
const afterRevoke = await fetch(`${URL_BASE}/api/ingest/routeros`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
  body: JSON.stringify(payload),
})
check(afterRevoke.status === 401, `a revoked token stops working (${afterRevoke.status})`)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
