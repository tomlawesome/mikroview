// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #207: permanent exclusions on their own page.
//
// Drives the whole path against a real instance: raise a real port_scan
// flag (not synthesized), permanently clear it from the Flags page,
// confirm it shows up -- filterable -- on the new Exclusions page, then
// remove it and confirm it's gone. A UI reorganisation like this one has
// no truth beyond "the thing that used to be there still works, in the
// new place", so that's what this checks.

import { execFileSync } from 'child_process'
import { fileURLToPath } from 'url'
import path from 'path'
import { session, check, done } from './live-browser.mjs'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..')

function portscan(n) {
  execFileSync(path.join(REPO, 'scripts/live-env.sh'), ['portscan', String(n)], {
    stdio: 'ignore',
    cwd: REPO,
  })
}

// A real port_scan flag: one source IP, 20 distinct destination ports
// inside the default 60s/15-port threshold.
portscan(20)

const { page } = await session()

async function openMenuView(label) {
  await page.click('.nav-menu .trigger')
  await page.click(`.nav-menu button:has-text("${label}")`)
}

await openMenuView('Flags')
await page.waitForSelector('.card .type', { timeout: 15000 })

// Split button + dropdown as of #198 -- the arrow segment opens
// "Permanently clear", replacing the old second inline button.
check(await page.isVisible('.split-arrow'), 'the port scan raised a real flag with the permanent-clear action visible')

// Permanently clear it -- this is what creates the exclusion under test.
await page.click('.split-arrow')
await page.click('.split-menu-item:has-text("Permanently clear")')
await page.waitForTimeout(500)

check(
  await page.isVisible('text=Permanently-excluded'),
  'Flags page shows the pointer to the Exclusions page once an exclusion exists',
)

await openMenuView('Exclusions')
await page.waitForSelector('.card .type', { timeout: 15000 })

check(
  await page.isVisible('.card:has-text("Port scan")'),
  'the permanently-cleared flag shows up on the Exclusions page',
)
check(
  await page.isVisible('.card:has-text("198.51.100.77")'),
  'the exclusion card shows the correct target',
)

// Filter by detector type -- selecting anything other than Port scan
// (or All) must hide the one exclusion under test.
await page.selectOption('.filter select', { label: 'Port scan' })
check(await page.isVisible('.card:has-text("198.51.100.77")'), 'the type filter set to a match still shows the card')

const typeOptions = await page.$$eval('.filter select option', (opts) => opts.map((o) => o.textContent))
const otherType = typeOptions.find((t) => t && t !== 'All' && t !== 'Port scan')
if (otherType) {
  await page.selectOption('.filter select', { label: otherType })
  check(!(await page.isVisible('.card:has-text("198.51.100.77")')), `the type filter set to "${otherType}" hides the non-matching card`)
} else {
  check(true, 'only one detector type has an exclusion -- the mismatch branch has nothing to test against')
}
await page.selectOption('.filter select', { label: 'All' })

// Filter by target text.
await page.fill('.filter input[type="search"]', '198.51.100.77')
check(await page.isVisible('.card:has-text("198.51.100.77")'), 'the target filter matches the excluded IP')
await page.fill('.filter input[type="search"]', 'no-such-target-xyz')
check(await page.isVisible('text=No exclusions match this filter'), 'a non-matching target filter shows the empty-filter message, not a blank list')
await page.fill('.filter input[type="search"]', '')

// Remove it, and confirm it's actually gone rather than just visually
// hidden -- reload the page and check again.
await page.click('button:has-text("Remove exclusion")')
await page.waitForTimeout(500)
check(
  !(await page.isVisible('.card:has-text("198.51.100.77")')),
  'removing the exclusion takes it off the page immediately',
)

await page.reload({ waitUntil: 'networkidle' })
await page.waitForTimeout(500)
check(
  !(await page.isVisible('.card:has-text("198.51.100.77")')),
  'the removal persisted -- the exclusion is gone after a reload, not just optimistically hidden',
)

done()
