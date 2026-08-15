// SPDX-License-Identifier: AGPL-3.0-only
//
// The toolbar's uptime readout: present, plausible, and actually
// counting. The counting assertion matters more than presence -- a badge
// that renders once and never ticks would pass any static check while
// being exactly the stale readout the feature exists to avoid.

import { session, check, done } from './live-browser.mjs'

const { page, consoleErrors } = await session()

const badge = page.locator('.uptime')
await badge.waitFor({ timeout: 10000 })
const first = await badge.textContent()
check(/^up \d+[smhd]/.test(first.trim()), `uptime badge renders a duration (got "${first.trim()}")`)

// The server-side number and the badge must agree. healthz is unauthed,
// so read it through the page's own fetch.
const healthz = await page.evaluate(async () => {
  const res = await fetch('/api/healthz')
  return res.json()
})
check(
  Number.isInteger(healthz.uptimeSeconds) && healthz.uptimeSeconds >= 0,
  `healthz reports uptimeSeconds (got ${healthz.uptimeSeconds})`,
)

// A young server's badge shows seconds, so two reads 2.5s apart must
// differ -- this is the tick. (The live harness always starts a fresh
// instance, so uptime here is well under a minute.)
await page.waitForTimeout(2500)
const second = await badge.textContent()
check(second !== first, `the readout counts (was "${first.trim()}", now "${second.trim()}")`)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.slice(0, 2).join(' | ') || 'none'})`)

done()
