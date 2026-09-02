// SPDX-License-Identifier: AGPL-3.0-only
//
// Where the stream's verbs live, and where they do not.
//
// #616 moved them from the retired toolbar onto the Stream card's own
// scene bar. Round 30 then retired that placement too and homed them
// nowhere at all, which is how Autoscroll became a one-way trapdoor
// (#749) and why Pause, Group and Clear had no surface for a whole
// release. Rounds 36-38 settle it: they ride the whisper's own line, as
// `following · pause · group` and `wipe`, with `csv ↓` beside the wipe.
// The scene bar keeps the chrome -- wordmark, live reading, flag and
// watcher marks, account chip -- and no stream controls at all.
//
// The claim is still what it always was: the control renders where
// claimed, does what it did, and is absent from the scenes it does not
// belong to. Only the claimed place changed.

import { session, feedSyslog, check, goTo, done } from './live-browser.mjs'

// Events in the buffer, so the controls have something to act on and
// csv has something to be enabled for.
feedSyslog(100)
const { page, consoleErrors } = await session({ waitForEvents: 50 })

// The active card -- the deck mounts the neighbouring cards too, each
// with a scene bar of its own.
const CARD = '.card[aria-hidden="false"]'
const HAND = `${CARD} .whisper .hand`
const hand = (cls) => `${HAND} .hand-btn${cls}`
const pill = (label) => `${CARD} .whisper .wpill:text-is("${label}")`

// --- The stream's verbs are on the whisper's line --------------------------
check(await page.isVisible(hand('.follow')), 'following is on the whisper line')
check(await page.isVisible(hand('.held')), 'pause is on the whisper line')
check((await page.$$(hand(''))).length === 3, 'three toggles, in the span pills\' segmented idiom')
check(await page.isVisible(pill('wipe')), 'wipe sits beside them as a quiet pill')
check(await page.isVisible(pill('csv ↓')), 'and csv ↓ beside wipe, per round 37')

// The order is the drawn order: the three toggles, then wipe, then csv.
const handText = (await page.textContent(HAND)).replace(/\s+/g, ' ').trim()
check(handText === 'following pause group', `the hand reads "following pause group" -- got "${handText}"`)

// --- And they do what the retired toolbar's did ----------------------------
// Following defaults on; one click turns it off, one turns it back. The
// label changes with it, which is the half #749 had no way to reach.
check(
  await page.$eval(hand('.follow'), (el) => el.classList.contains('on')),
  'following starts on',
)
await page.click(hand('.follow'))
check(
  !(await page.$eval(hand('.follow'), (el) => el.classList.contains('on'))),
  'clicking it stops the stream following',
)
check(
  (await page.textContent(hand('.follow')))?.trim() === 'follow',
  'and the pill reads "follow" -- the way back, in the now ink',
)
await page.click(hand('.follow'))
check(
  await page.$eval(hand('.follow'), (el) => el.classList.contains('on')),
  'and clicking again follows once more (#749)',
)
check(
  (await page.textContent(hand('.follow')))?.trim() === 'following',
  'reading "following" again',
)

// Pause renames itself to the state it is in, then resumes.
await page.click(hand('.held'))
check((await page.textContent(hand('.held')))?.trim() === 'paused', 'pause reads "paused" while holding')
await page.click(hand('.held'))
check((await page.textContent(hand('.held')))?.trim() === 'pause', 'and goes back to "pause"')

// --- csv ↓ downloads the lines held here ----------------------------------
const csvTitle = await page.getAttribute(pill('csv ↓'), 'title')
check(
  /\d+ rows, every column/.test(csvTitle ?? ''),
  `csv states what it gives before giving it (${csvTitle})`,
)
const [download] = await Promise.all([
  page.waitForEvent('download', { timeout: 10000 }),
  page.click(pill('csv ↓')),
])
check(
  (download.suggestedFilename() ?? '').endsWith('.csv'),
  `csv ↓ downloads a CSV (${download.suggestedFilename()})`,
)

// --- The scene bar carries chrome only ------------------------------------
check(
  (await page.$$(`${CARD} .scene-bar .hand-btn`)).length === 0,
  'no stream toggle rode back onto the scene bar',
)
check(
  await page.isVisible(`${CARD} .scene-bar .account button.chip`),
  'the account chip is still on the bar -- the chrome stays where it was',
)

// --- Metrics carries a bare bar and no hand at all ------------------------
await goTo(page, 'Metrics')
check((await page.$$(HAND)).length === 0, 'the hand is absent from Metrics -- it belongs to the stream')
check(
  (await page.$$(`${CARD} .whisper`)).length === 0,
  'and so is the whisper it rides on',
)
check(
  await page.isVisible(`${CARD} .scene-bar .account button.chip`),
  'while the account chip is still there -- the chrome travels, the controls do not',
)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors.slice(0, 3))}`)
done()
