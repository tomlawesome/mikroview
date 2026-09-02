// SPDX-License-Identifier: AGPL-3.0-only
//
// Uptime, in the account menu's foot (#804, round 37).
//
// This scenario used to drive a scene-bar badge and assert that it
// counted -- two reads 2.5s apart had to differ. Both halves are now
// wrong, and neither was weakened to make this pass:
//
// - The badge had no home. Round 30 drew no scene-bar readout, so
//   UptimeBadge.svelte was left mounted nowhere; this scenario has been
//   failing on dev ever since, waiting on a selector nothing rendered.
//   Round 37 gives it one, beside the version in the menu's foot.
// - The tick was the point, and now it is the defect. Days and hours
//   only: "a ticking second is a clock, not a fact" (round-37 README,
//   accepted 2026-09-02). So the assertion inverts -- the readout must
//   hold still -- and the "it is real, not a frozen render" half moves
//   to agreeing with the server's own number, which is what the tick
//   was standing in for.
//
// live-account-menu.mjs checks the foot's shape and its agreement with
// healthz. What only this file can show is the holding still, which
// needs real elapsed time in a real browser.

import { session, check, openAccountMenu, done } from './live-browser.mjs'

const { page, consoleErrors } = await session()

await openAccountMenu(page)
const ver = page.locator('.account .menu .ver')
await ver.waitFor({ timeout: 10000 })

const first = (await ver.textContent())?.trim()
check(/ · up \d+ d \d+ h$/.test(first ?? ''), `the foot ends in days-and-hours uptime, spaced (got ${JSON.stringify(first)})`)

// The old shape must not come back: no minutes, no seconds, no brackets.
check(!/\[/.test(first ?? ''), `no brackets around the readout (got ${JSON.stringify(first)})`)
check(!/\d+m \d+s/.test(first ?? ''), `no minutes or seconds (got ${JSON.stringify(first)})`)

// The counter underneath advances every second (uptime.svelte.ts ticks
// at 1s and resyncs at 60s). Held open across several of those ticks,
// the rendered string must not move -- that is the whole difference
// between a fact and a clock. The live harness always starts a fresh
// instance, so this window is nowhere near the next hour boundary.
await page.waitForTimeout(3500)
const second = (await ver.textContent())?.trim()
check(second === first, `the readout holds still across the counter's ticks (was ${JSON.stringify(first)}, now ${JSON.stringify(second)})`)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.slice(0, 2).join(' | ') || 'none'})`)

done()
