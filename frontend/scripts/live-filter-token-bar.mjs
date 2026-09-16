// SPDX-License-Identifier: AGPL-3.0-only
//
// #1246 (round 57): the stream filter box's third face -- a field menu on
// focus, that field's own values under it, and a committed token that is
// the chip the box already drew, over the same appState.filters. The unit
// tests (lib/tokenBar.test.ts, FilterBar.svelte.test.ts) pin the rules;
// what they cannot see is the real menu over the real stream: a real
// device list served by a real instance, a token that really narrows the
// table, and a Backspace whose meaning depends on which box the browser
// says holds the caret -- jsdom has no caret and no focus of its own.
//
// Every address below is documentation space (RFC 5737 TEST-NET-3,
// 203.0.113.0/24), and both rule names are deliberately odd so this run's
// own rows cannot be confused with what a sibling scenario pushed through
// the same shared instance.

import { session, feedRaw, check, done, waitForStreamRows } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const RULE = 'mv1246-tokenbar'
const CHAIN = 'mv1246chain'
const OTHER_RULE = 'mv1246-other'

const TOKEN_FIELDS = ['device', 'action', 'chain', 'proto', 'interface', 'port', 'source', 'destination']

// The active card -- the deck keeps its neighbours mounted, so a bare
// `.row` count would include rows another card rendered.
const CARD = '.card[aria-hidden="false"]'
const BOX = `${CARD} .filterline .fbox`
const TERM = `${BOX} input.fbtype`

// unfoldFilter: false -- the first check below is that the field menu
// opens on focus, and session()'s own unfold clicks that very input
// (#667), which would have opened it before the scenario started.
const { page, consoleErrors } = await session({ unfoldFilter: false })

async function api(path) {
  const res = await page.request.fetch(`${URL_BASE}${path}`)
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

// Same wait-and-refeed pattern live-filter-parity.mjs uses: under the
// full suite's load a single fed line can be dropped by a saturated
// ingest queue, and a bare page.waitForSelector would turn that into an
// uncaught timeout with no RESULT line.
async function waitForArrival(rule, line, timeoutMs = 25000) {
  feedRaw(line)
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    const { body } = await api(`/api/events?rule=${rule}&limit=5`)
    if ((body?.events?.length ?? 0) > 0) return true
    await new Promise((r) => setTimeout(r, 2000))
    feedRaw(line)
  }
  return false
}

/** waitUntil polls fn (never throws) until it returns truthy or times out. */
async function waitUntil(fn, timeoutMs = 8000, intervalMs = 250) {
  const deadline = Date.now() + timeoutMs
  let last
  while (Date.now() < deadline) {
    last = await fn().catch(() => undefined)
    if (last) return last
    await page.waitForTimeout(intervalMs)
  }
  return last
}

// A custom chain nothing else in the suite uses, so the chain token below
// narrows the table to exactly this scenario's own rows.
const line =
  `firewall,info A|${RULE}| ${CHAIN}: in:ether1246 out:bridge1246, connection-state:new, ` +
  `proto TCP (SYN), 203.0.113.246:41246->203.0.113.247:8291, len 60`
const otherLine =
  `firewall,info D|${OTHER_RULE}| forward: in:ether1246 out:bridge1246, connection-state:new, ` +
  `proto TCP (SYN), 203.0.113.248:41248->203.0.113.249:445, len 60`

const arrived = (await waitForArrival(RULE, line)) && (await waitForArrival(OTHER_RULE, otherLine))
check(arrived, `the ${RULE} and ${OTHER_RULE} test events reached the server`)
if (!arrived) {
  check(true, 'skipped the rest -- the token menu cannot be exercised over rows that never arrived')
  done()
}
await waitForStreamRows(page, 2)

const rowCount = () => page.locator(`${CARD} .grid .row`).count()
const chips = () => page.locator(`${BOX} .chip:not(.pending)`)
const menuNames = () => page.$$eval('.token-menu .tm-name', (els) => els.map((e) => e.textContent.trim()))

// Opens the field menu the way a pointer does. Focus alone opens it, but
// a click arriving at an already-focused box fires no focus event, so the
// box's own click handler is the second way in -- both land here.
async function openFieldMenu() {
  await page.click(TERM)
  return waitUntil(() => page.isVisible('.token-menu'))
}

async function pickMenuItem(label) {
  await page.click(`.token-menu .tm-item:has(.tm-name:text-is("${label}"))`)
}

// --- The field menu opens on focus, with the eight token fields ---------

const menuOpened = await openFieldMenu()
check(!!menuOpened, 'the field menu opens when the filter box takes focus')
const fields = await menuNames()
check(
  JSON.stringify(fields) === JSON.stringify(TOKEN_FIELDS),
  `the field menu lists the eight token fields (saw: ${fields.join(', ')})`,
)

// --- Picking a field shows a pending token, then its own values ---------

await pickMenuItem('device')
const pendingText = await waitUntil(() => page.textContent(`${BOX} .chip.pending`))
check(pendingText?.trim().startsWith('device:'), `a pending "device:" token appears before any value is chosen (saw "${pendingText?.trim()}")`)

const deviceNames = await menuNames()
check(deviceNames.length > 0, `the value menu lists this instance's own devices (saw: ${deviceNames.join(', ')})`)

const rowsBefore = await rowCount()
await pickMenuItem(deviceNames[0])
const deviceChip = await waitUntil(() => page.isVisible(`${BOX} [aria-label="Remove the device filter"]`))
check(!!deviceChip, `picking "${deviceNames[0]}" commits it as a removable device token`)
check(!(await page.isVisible(`${BOX} .chip.pending`)), 'the pending token is gone once the value commits')

// This instance has one router in the usual live environment, so a device
// token cannot narrow the table further than it already is -- what is
// checked here is that committing one filters rather than empties: the
// rows that remain are the ones that device produced.
const rowsAfterDevice = await waitUntil(async () => {
  const n = await rowCount()
  return n > 0 && n <= rowsBefore ? n : 0
})
check(!!rowsAfterDevice, `the stream still holds the picked device's rows (${rowsAfterDevice} of ${rowsBefore})`)

// --- A token really narrows the stream ---------------------------------

await openFieldMenu()
await pickMenuItem('chain')
const chainValues = await menuNames()
check(chainValues.includes(CHAIN), `the chain value menu offers the custom chain observed in the buffer (saw: ${chainValues.join(', ')})`)
await pickMenuItem(CHAIN)
const narrowed = await waitUntil(async () => {
  const n = await rowCount()
  return n > 0 && n < rowsBefore ? n : 0
})
check(!!narrowed, `the chain token narrows the stream to this scenario's own rows (${narrowed} of ${rowsBefore})`)
check(
  (await chips().count()) >= 2,
  'both tokens stand in the box at once, each with its own ⌫',
)

// --- Free text lives in the same box, beside the tokens -----------------

await page.fill(TERM, RULE)
const freeText = await waitUntil(async () => {
  const rows = await page.locator(`${CARD} .grid .row`, { hasText: RULE }).count()
  const total = await rowCount()
  return rows > 0 && rows === total ? rows : 0
})
check(!!freeText, `free text typed beside the tokens narrows by rule label (${freeText} rows, all ${RULE})`)
check(
  await page.isVisible(`${BOX} [aria-label="Remove the chain filter"]`),
  'typing free text does not disturb the tokens already committed',
)

// --- A saved filter expands into individually removable tokens ----------
//
// Saved through the app's own store rather than the `save this filter
// as…` row, which opens a window.prompt() a scenario cannot type into.
await page.evaluate(() => {
  const preset = {
    name: 'mv1246 scans',
    filters: { action: 'drop', chain: 'forward', srcScope: 'external' },
  }
  localStorage.setItem('mikroview-filter-presets', JSON.stringify([preset]))
})
await page.reload({ waitUntil: 'networkidle' })
await page.waitForSelector('#main-content', { timeout: 15000 })
await page.waitForSelector(TERM, { timeout: 15000 })
await page.click(`${BOX} .fsaved`)
await page.click(`${BOX} .fpname:text-is("mv1246 scans")`)

const expanded = await waitUntil(async () => {
  const action = await page.isVisible(`${BOX} [aria-label="Remove the action filter"]`)
  const chain = await page.isVisible(`${BOX} [aria-label="Remove the chain filter"]`)
  const source = await page.isVisible(`${BOX} [aria-label="Remove the source filter"]`)
  return action && chain && source
})
check(!!expanded, 'applying a saved filter expands it into its own separately removable tokens')
check(
  (await page.locator(`${BOX} .chip`, { hasText: 'mv1246 scans' }).count()) === 0,
  'a saved filter is never one opaque named token',
)

await page.click(`${BOX} [aria-label="Remove the chain filter"]`)
const oneGone = await waitUntil(async () => {
  const chain = await page.isVisible(`${BOX} [aria-label="Remove the chain filter"]`)
  const action = await page.isVisible(`${BOX} [aria-label="Remove the action filter"]`)
  return !chain && action
})
check(!!oneGone, 'removing one of the expanded tokens leaves the rest of the filter alone')

// --- Backspace: one key, one meaning, in whichever box holds the caret --

const beforeBackspace = await chips().count()
await page.click(TERM)
await page.fill(TERM, '')
await page.keyboard.press('Backspace')
const tokenGone = await waitUntil(async () => (await chips().count()) === beforeBackspace - 1)
check(!!tokenGone, `Backspace in an empty box deletes the last token (${beforeBackspace} → ${await chips().count()})`)

// ...and never from a value box with characters in it (owner verdict 3,
// "character you just typed"): the pending port stands, the tokens stand,
// and only the typed digit goes.
const beforeMidValue = await chips().count()
await openFieldMenu()
await pickMenuItem('port')
await page.type(TERM, '829')
await page.keyboard.press('Backspace')
const midValue = await waitUntil(async () => {
  const typed = await page.inputValue(TERM)
  const pending = await page.isVisible(`${BOX} .chip.pending`)
  return typed === '82' && pending ? { typed, pending } : null
})
check(!!midValue, `Backspace mid-value deletes the character, not the pending token (box reads "${midValue?.typed}")`)
check(
  (await chips().count()) === beforeMidValue,
  'and never reaches past a half-typed value to a committed token',
)

// Cleanup: leave the box empty and the saved list as it was found, for
// whatever runs next on this shared instance.
await page.evaluate(() => localStorage.removeItem('mikroview-filter-presets'))
await page.keyboard.press('Escape')
await page.reload({ waitUntil: 'networkidle' }).catch(() => {})

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
