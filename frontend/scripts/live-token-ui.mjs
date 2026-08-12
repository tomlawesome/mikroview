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

await page.click('.nav-menu .trigger')
await page.click('.nav-menu button:has-text("API tokens")')
await page.waitForSelector('.modal[aria-label="API tokens"]')

// --- Ingest token, entirely through the dialog --------------------------
await page.fill('.create-form input[type="text"]', 'ui-ingest')
await page.selectOption('.kind-select', 'ingest')

// The device pick-list only offers configured devices -- live-env.sh
// declares live-router in loopback mode, so it must be here. A typo'd
// free-text device was the failure mode this list exists to prevent.
await page.waitForSelector('.device-select')
const options = await page.$$eval('.device-select option:not([disabled])', (els) =>
  els.map((e) => e.value),
)
check(options.includes('live-router'), `the pick-list offers the configured device (${options})`)
await page.selectOption('.device-select', 'live-router')

await page.click('.create-form .save')
await page.waitForSelector('.created-value')
const token = (await page.textContent('.created-value'))?.trim() ?? ''
check(token.length > 0, 'the one-time value banner shows the new token')

check(
  await page.isVisible('.row:has-text("ui-ingest") .kind-badge:has-text("ingest: live-router")'),
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
