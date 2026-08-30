// SPDX-License-Identifier: AGPL-3.0-only
//
// The door (#645, round 5's v3): the fall rains across the whole screen
// behind the sign-in, masked out of the centre; the amber draws as a
// thin box framing the wordmark; sign-out plays the beat in reverse.
// What needs a real browser rather than a unit test:
//
//  1. The way-out is wiring across a real sign-out -- the one-shot flag
//     set by logout() and consumed at AuthLogin's next mount. A unit
//     test mocks one side or the other; only the real round trip shows
//     the reverse beat on an actual sign-out and not on a plain load.
//  2. The mask is CSS the DOM cannot vouch for -- a missing mask leaves
//     every assertion about elements green while the rain runs straight
//     through the credentials. Computed style is the only witness.
//  3. prefers-reduced-motion is a media feature: emulating it and
//     reading what actually renders is exactly what jsdom cannot do.
//
// Leaves nothing behind: the session it signs out of belonged to this
// scenario, and every later scenario signs in fresh.

import { chromium } from 'playwright'
import { session, check, done, openAccountMenu } from './live-browser.mjs'

const { page, consoleErrors } = await session()

// --- The way out: a real sign-out plays the reverse beat ----------------

await openAccountMenu(page)
await page.click('.account .menu button.row:text-is("Sign out")')
await page.waitForSelector('.screen', { timeout: 10000 })

check(
  (await page.locator('.screen.reverse').count()) === 1,
  'the door after a real sign-out wears the reverse beat',
)

// --- The door's own furniture, on the signed-out page -------------------

check(
  (await page.locator('.fullfall i').count()) >= 10,
  'the fall rains across the door -- a handful of strokes, present behind the form',
)

const mask = await page.$eval('.fullfall', (el) => {
  const s = getComputedStyle(el)
  return s.maskImage || s.webkitMaskImage || ''
})
check(
  mask.includes('radial-gradient'),
  `the rain is masked out of the centre -- it never crosses the login elements (mask: ${mask.slice(0, 40)}…)`,
)

const frame = await page.$eval('.wm-box', (el) => {
  const s = getComputedStyle(el)
  return { width: s.borderTopWidth, style: s.borderTopStyle }
})
check(
  frame.style === 'solid' && parseFloat(frame.width) > 0 && parseFloat(frame.width) <= 2,
  `the amber draws as a thin box framing the wordmark (${frame.width} ${frame.style})`,
)

// No footer on the door (owner, 2026-08-30): the standing promise came
// off it -- the door carries the brink, the fall and the way in, nothing
// else. The promise still lives where it works, the wizard.
check(
  (await page.locator('.promise').count()) === 0,
  'the door carries no footer',
)

// The wording rule (round 15): the product says password, never
// passphrase -- checked on the one page whose mockups said otherwise.
const doorText = (await page.textContent('.screen')) ?? ''
check(!/passphrase/i.test(doorText), 'no surface of the door says passphrase')
check(/password/i.test(doorText), 'the credential field is named password')

// A plain page load -- no sign-out behind it -- must NOT replay the
// reverse beat: the flag is one-shot and belongs to logout() alone.
await page.reload({ waitUntil: 'networkidle' })
await page.waitForSelector('.screen', { timeout: 10000 })
check(
  (await page.locator('.screen.reverse').count()) === 0,
  'a plain load of the door never replays the way-out -- the flag is one-shot',
)

// --- prefers-reduced-motion: the rain stays home ------------------------

const browser = await chromium.launch()
const ctx = await browser.newContext({ ignoreHTTPSErrors: true, reducedMotion: 'reduce' })
const still = await ctx.newPage()
await still.goto(process.env.MV_URL, { waitUntil: 'networkidle' })
await still.waitForSelector('.screen', { timeout: 10000 })
check(
  await still.$eval('.fullfall', (el) => getComputedStyle(el).display === 'none').catch(() => false),
  'under prefers-reduced-motion the rain does not render at all',
)
await browser.close()

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
