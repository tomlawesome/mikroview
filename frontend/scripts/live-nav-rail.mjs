// SPDX-License-Identifier: AGPL-3.0-only
//
// #544: the ratified left rail replaces the hamburger. Two things here
// cannot be proved in jsdom and are the reason this scenario exists.
//
// The reserved-slot rule ("no link renders for a surface that does not
// exist") is a claim about the whole rendered document, not about one
// component's props: a unit test asserting `groups` has no Map entry
// would pass while some other component happily rendered one. This walks
// the real DOM instead.
//
// And aria-current has to survive a real view switch. The rail sets it
// from appState.view, which only actually changes when the app mounts a
// different view component -- exactly the wiring a mocked store hides.

import { session, feedSyslog, check, responsive, done } from './live-browser.mjs'

feedSyslog(120, 'nav-rail')
const { page, consoleErrors } = await session({ waitForEvents: 100 })

// --- The geography, in the ratified order --------------------------------
// The live check signs in as an admin, so every group is visible; a
// viewer sees fewer, which is #490's grammar and not this scenario's job.
const headings = await page.$$eval('.rail .group-heading', (els) => els.map((e) => e.textContent.trim()))
check(
  JSON.stringify(headings) === JSON.stringify(['Live', 'Investigate', 'Detect', 'Expect', 'Admin']),
  `rail groups in the ratified order -- got ${JSON.stringify(headings)}`,
)

// Headings are labels, never controls. The record is explicit, and the
// obvious regression is someone making them collapse a group.
const headingTags = await page.$$eval('.rail .group-heading', (els) => els.map((e) => e.tagName))
check(
  headingTags.every((t) => t !== 'BUTTON' && t !== 'A'),
  `group headings are not controls -- got ${JSON.stringify(headingTags)}`,
)

// --- Reserved slots: absent from the DOM, not disabled in it -------------
const labels = await page.$$eval('.rail .item', (els) => els.map((e) => e.textContent.trim()))
for (const absent of ['Map', 'Lookback']) {
  check(
    !labels.includes(absent),
    `${absent} is reserved in the spec, not rendered -- rail shows ${JSON.stringify(labels)}`,
  )
}
// A disabled stub would satisfy the check above while breaking the rule,
// so prove nothing in the rail is disabled at all.
const disabled = await page.$$eval('.rail .item', (els) => els.filter((e) => e.disabled).map((e) => e.textContent.trim()))
check(disabled.length === 0, `no rail item is disabled (absent, never disabled) -- got ${JSON.stringify(disabled)}`)

// --- The hamburger is gone -----------------------------------------------
check((await page.$$('.nav-menu, .hamburger')).length === 0, 'the hamburger menu no longer renders')

// --- Navigation actually navigates, and aria-current follows it ----------
const current = async () =>
  (await page.$$eval('.rail .item[aria-current="page"]', (els) => els.map((e) => e.textContent.trim())))[0]

check((await current()) === 'Stream', `Stream is the landing -- got ${await current()}`)

await page.click('.rail .item:text-is("Metrics")')
await page.waitForFunction(
  () => document.querySelector('.rail .item[aria-current="page"]')?.textContent.trim() === 'Metrics',
  null,
  { timeout: 5000 },
)
check((await current()) === 'Metrics', 'clicking Metrics moves aria-current to it')
check(
  (await page.$$eval('.rail .item[aria-current="page"]', (els) => els.length)) === 1,
  'exactly one rail item is aria-current at a time',
)

await page.click('.rail .item:text-is("Stream")')
await page.waitForSelector('.grid .row', { timeout: 5000 })
check((await current()) === 'Stream', 'and back to Stream, with the live table rendered')

// --- The skip-link is real, and first ------------------------------------
// It is visually offscreen until focused, so "exists" is not enough --
// tab once from the document and it must be what lands.
//
// Reloading, not blurring. blur() moves document.activeElement to the
// body but leaves Chromium's sequential focus navigation starting point
// on the element that had focus, so the next Tab continues from the rail
// button clicked above and lands on the following rail item -- which
// reads exactly like a missing skip-link. A fresh document is the only
// reset that actually moves the starting point back to the top.
await page.reload()
await page.waitForSelector('.rail .item', { timeout: 10000 })
await page.keyboard.press('Tab')
const focused = await page.evaluate(() => {
  const el = document.activeElement
  return { cls: el?.className ?? '', text: el?.textContent?.trim() ?? '', visible: el?.getBoundingClientRect().left >= 0 }
})
check(focused.cls.includes('skip-link'), `first Tab lands on the skip-link -- got "${focused.text}"`)
check(focused.visible, 'the skip-link becomes visible once focused')

check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
