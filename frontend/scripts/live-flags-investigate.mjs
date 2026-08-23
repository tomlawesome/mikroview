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

import { fileURLToPath } from 'url'
import { session, check, done, feedPortScan, waitForFlag } from './live-browser.mjs'



const TARGET_IP = '198.51.100.95'
feedPortScan(20, TARGET_IP)

const { page } = await session()

async function openMenuView(label) {
  await page.click(`.rail .item:has-text("${label}")`)
}

// Wait for the flag on the *server* before asking the UI about it
// (#354's pattern, applied here per #450).
//
// This scenario used to wait on `.card .type` and go straight into its
// assertions. That selector only proves some card rendered, and every
// scenario shares one instance, so an earlier scenario's flag satisfies
// it -- which made the wait no wait at all for this target. When the
// port scan's own flag was still in flight the result was not one clean
// failure but four: three assertions reporting false, and then an
// uncaught Playwright timeout at the first .click() on a card that was
// never there, which reads as a broken scenario rather than a flag that
// did not arrive. Observed 2026-08-21 on a back-to-back live-check run,
// the load pattern #450 documents.
const raised = await waitForFlag(page, TARGET_IP)
check(raised.ok, raised.message)

if (!raised.ok) {
  // Nothing below can be evaluated without the card, so it is reported
  // as blocked rather than as a pile of independent failures it cannot
  // actually distinguish from real regressions (#361).
  check(true, `skipped -- the investigate button cannot be exercised on a flag card that never arrived (${raised.message})`)
  done()
}

await openMenuView('Flags')
// Scoped to this scenario's own target, not to any card at all.
await page.waitForSelector(`section[aria-labelledby="active-heading"] .card:has-text("${TARGET_IP}")`, {
  timeout: 15000,
})

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
