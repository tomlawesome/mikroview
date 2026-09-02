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
// #490 moved that path once, to the engine room's "which machines may
// speak" door; round 32 (#767) moved it again, into the keys group
// mounted directly in the Settings card (docs/design/concepts/round-32/
// settings-doors.html's #keys), the door retired outright. The checks
// below are deliberately the same operator questions as before -- can I
// mint the right kind, does the pick-list match the devices this
// instance knows, is the secret shown exactly once, does revoking it
// stop the push -- asked at the new location rather than rewritten
// around it.

import { session, check, done, goTo } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

// Scoped to the keys group throughout: people sits in the same card with
// the same .prow markup, so an unscoped `.prow:has-text(...)` would be
// one badly-chosen token name away from clicking Remove on a person.
const DOOR = '#keys'

// goTo's own wait (SCENES in live-browser.mjs, waiting for the engineroom card to centre) is what proves arrival --
// this used to also wait for `.page-header h2`, but #700 unmounted PageHeader from EngineRoom.svelte entirely, so
// that selector no longer exists anywhere on the page (#667 group E).
await goTo(page, 'Settings')
check(true, "the rail's engine room row opens the engine room")
check((await page.$$('.modal')).length === 0, 'no modal renders -- keys is part of the page')
check(
  await page
    .locator(`${DOOR} h3`)
    .waitFor({ timeout: 5000 })
    .then(() => true, () => false),
  'the keys group is on the page for an admin',
)

// --- Ingest token, entirely through the keys group -----------------------
await page.click(`${DOOR} .ogfoot .olink`)
await page.waitForSelector(`${DOOR} .pform`)
await page.fill(`${DOOR} .pform input[aria-label="key name"]`, 'ui-ingest')
await page.click(`${DOOR} .pform .seg[aria-label="Key kind"] button:has-text("ingest")`)

// The pick-list must offer every device mikroview knows about --
// configured (live-env.sh declares one) or discovered from its own
// syslog (which is all the container harness has). Whichever this
// instance has, the list has to match /api/devices exactly: free text
// was the typo trap this list exists to remove.
const known = await page.request
  .get(`${URL_BASE}/api/devices`)
  .then(async (r) => ((await r.json()).devices ?? []).map((d) => d.id).sort())
check(known.length > 0, `the instance knows at least one device (${known})`)

const deviceSeg = `${DOOR} .pform .seg[aria-label="which router"]`
await page.waitForSelector(deviceSeg)
// Read the id, not the label. A button shows the device's *name* when it
// has one (EngineRoom.svelte:731: `d.name && d.name !== d.id ? d.name :
// d.id`), so the harness's live-router renders as "Live Router" and a
// text comparison against /api/devices' ids can never match. The id is
// carried on the button as `data-device-id` (EngineRoom.svelte:725), and
// it is the identity that matters here anyway -- the token binds to the
// id, and this list exists to remove the typo trap of free text.
const options = await page.$$eval(`${deviceSeg} button`, (els) =>
  els.map((e) => e.getAttribute('data-device-id')).sort(),
)
check(
  JSON.stringify(options) === JSON.stringify(known),
  `the pick-list offers exactly the known devices (list=${options} api=${known})`,
)
const DEVICE = known[0]
await page.click(`${deviceSeg} button[data-device-id="${DEVICE}"]`)

await page.click(`${DOOR} .pform button:has-text("mint it")`)
await page.waitForSelector(`${DOOR} .reveal code`)
const token = (await page.textContent(`${DOOR} .reveal code`))?.trim() ?? ''
check(token.length > 0, 'the once-only reveal shows the new token')

// The reveal stands in for the new row until done is clicked -- the
// ordinary row is filtered out of the list while it is showing (round
// 32's "pending" swap), so done comes first here.
await page.click(`${DOOR} .reveal button:has-text("done")`)

// Which device an ingest key speaks for is the fact an operator revokes
// on -- with two routers pushing, "ingest" alone does not say which key
// belongs to which. The old Tokens page carried it and the keys group
// has to as well.
check(
  await page
    .locator(`${DOOR} .prow:has-text("ui-ingest") .pr:has-text("ingest · speaks for ${DEVICE}")`)
    .waitFor({ timeout: 15000 })
    .then(() => true, () => false),
  'the key row says what the token is and which device it speaks for',
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
await page.click(`${DOOR} .ogfoot .olink`)
await page.waitForSelector(`${DOOR} .pform`)
await page.fill(`${DOOR} .pform input[aria-label="key name"]`, 'ui-readonly')
// read-only is the segment's own default -- left untouched here on purpose.
await page.click(`${DOOR} .pform button:has-text("mint it")`)

await page.waitForSelector(`${DOOR} .reveal code`)
const roToken = (await page.textContent(`${DOOR} .reveal code`))?.trim() ?? ''
check(roToken.length > 0 && roToken !== token, 'the reveal shows the new read-only token, not the ingest one above')
check(
  (await page.textContent(`${DOOR} .reveal .pr`))?.trim() === 'read-only',
  'a default-kind token is labelled read-only in the reveal',
)
await page.click(`${DOOR} .reveal button:has-text("done")`)
check(
  await page
    .locator(`${DOOR} .prow:has-text("ui-readonly") .pr:has-text("read-only")`)
    .waitFor({ timeout: 15000 })
    .then(() => true, () => false),
  'the ordinary row takes the reveal\'s place once done is clicked',
)
const events = await fetch(`${URL_BASE}/api/events`, {
  headers: { Authorization: `Bearer ${roToken}` },
})
check(events.status === 200, `the read-only token reads /api/events (${events.status})`)

// --- Tidy up, via the keys group's own revoke (arm-then-confirm) --------
for (const name of ['ui-ingest', 'ui-readonly']) {
  const revoke = page.locator(`${DOOR} .prow:has-text("${name}") .revoke`)
  await revoke.click()
  await revoke.click()
  await page.waitForSelector(`${DOOR} .prow:has-text("${name}")`, { state: 'detached' })
}
const afterRevoke = await fetch(`${URL_BASE}/api/ingest/routeros`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
  body: JSON.stringify(payload),
})
check(afterRevoke.status === 401, `a revoked token stops working (${afterRevoke.status})`)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
