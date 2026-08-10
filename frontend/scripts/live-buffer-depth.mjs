// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #244: the toolbar now shows how full the server's event ring is,
// and (once full) roughly how far back it reaches at the current rate --
// previously invisible anywhere, including the one real instance whose
// buffer had wrapped in under three minutes against a configured 24h
// retention with nothing on screen to say so.
//
// The shared live-check server runs the compiled-in default capacity
// (200,000, `internal/config/config.go`), which a scenario feeding a few
// hundred events has no business trying to fill -- that would mean either
// flooding the shared instance every other scenario also runs against, or
// reconfiguring it out from under them. So this checks the fractional
// state end to end against a real /api/stats response, and leaves the
// full-ring "holding last Xs" text to format.test.ts's unit coverage,
// which can assert every branch precisely without a 200,000-event feed.

import { session, feedSyslog, check, done } from './live-browser.mjs'

feedSyslog(100)
const { page } = await session({ waitForEvents: 50 })

const indicator = page.locator('.buffer-depth')
check(await indicator.isVisible(), 'the buffer-depth indicator is visible in the toolbar')

const text = (await indicator.textContent())?.trim() ?? ''
check(
  /^\d+% of buffer used$/.test(text),
  `shows a percentage while the ring is nowhere near full (got "${text}")`,
)

const title = await indicator.getAttribute('title')
check(
  (title ?? '').includes('overwrites the oldest'),
  'the tooltip explains what happens once the ring fills, not just the current number',
)

done()
