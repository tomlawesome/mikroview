// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #213: a live abuse-check button on flag cards, reusing the
// existing IpInvestigateButton/IpLookupPopover path wholesale.
//
// The point of this button is reachability when the live view has
// nothing left to click into (raw events aren't persisted), so the
// thing worth proving is that it actually opens the real popover for
// the real target IP from a real flag card -- not the lookup result's
// content, which depends on live third-party data this scenario doesn't
// control and shouldn't assert on.

import { execFileSync } from 'child_process'
import { fileURLToPath } from 'url'
import path from 'path'
import { session, check, done } from './live-browser.mjs'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..')

function portscan(n, sourceIp) {
  execFileSync(path.join(REPO, 'scripts/live-env.sh'), ['portscan', String(n), sourceIp], {
    stdio: 'ignore',
    cwd: REPO,
  })
}

const TARGET_IP = '198.51.100.95'
portscan(20, TARGET_IP)

const { page } = await session()

async function openMenuView(label) {
  await page.click('.nav-menu .trigger')
  await page.click(`.nav-menu button:has-text("${label}")`)
}

await openMenuView('Flags')
await page.waitForSelector('.card .type', { timeout: 15000 })

const card = page.locator('section[aria-labelledby="active-heading"] .card', { hasText: TARGET_IP })
check(await card.isVisible(), 'the port scan raised a card with a real, filterable target IP')
check(await card.locator('.investigate').isVisible(), 'a port_scan flag (IP-shaped, public target) gets the investigate button')

// The two invariants #213 states explicitly: additive, not a
// replacement for the target chip's own click-to-filter behaviour.
check(await card.locator('button.target').isVisible(), 'the target chip is still there, unchanged, next to the new button')

await card.locator('.investigate').click()
await page.waitForTimeout(600)

check(await page.isVisible('.popover'), 'clicking it opens the real lookup popover')
check(
  (await page.textContent('.popover .ip'))?.trim() === TARGET_IP,
  `the popover is for the card's own IP (${TARGET_IP}), not a stale or wrong one`,
)

await page.click('.popover .close')
await page.waitForTimeout(200)
check(!(await page.isVisible('.popover')), 'closing the popover leaves the flag card untouched underneath')
check(await card.isVisible(), 'the card is still there after closing the popover -- opening a lookup is not destructive')

// The chip still filters exactly as before -- the new button is
// additive, per #213's own "additive only" note. Clicking it navigates
// to the live view and applies the IP filter, same as pre-#213.
await card.locator('button.target').click()
await page.waitForTimeout(300)
check(await page.isVisible('input.rule'), 'clicking the target chip still navigates to the live view, unaffected by the new button')
check(
  (await page.inputValue('input[aria-label="IP address or CIDR"]')) === TARGET_IP,
  "the live view is filtered to the flag's IP, same as before this change",
)

done()
