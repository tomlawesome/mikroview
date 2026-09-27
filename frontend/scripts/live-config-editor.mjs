// SPDX-License-Identifier: AGPL-3.0-only
//
// #1347: the config editor, driven for real. As the admin: open
// Settings' Config card, give the password again, see the editor holding
// the running config's own text, type a key MikroView does not know and
// watch a problem appear once typing pauses, press Carry forward, keep a
// snapshot, and close.
//
// What a component test over a mocked API cannot prove, and this does:
//
//  - the open call really re-checks the password and sends back the file
//    MikroView was started with (the harness's $MV_DIR/cfg.yaml), line
//    for line -- secrets masked in place, so the line count is the same;
//  - the Problems rail is fed by the real validator, the same one as
//    -validate-config, and names the line that was typed;
//  - Carry forward and Snapshot answer, and the server keeps a snapshot
//    of the old text before Carry forward changes anything.
//
// Nothing here downloads: the gate's browser has nowhere to put a file,
// and Download is the same POST the unit tests cover. Setup-only mode
// needs a refused config at start-up, which the shared instance cannot
// have, so it is not driven here either -- say so in the MR.
//
// Leaves up to two snapshots behind (the server keeps five); nothing
// else in the suite reads them.

import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { session, check, done, goTo, responsive, adminPassword } from './live-browser.mjs'

const CARD = '#engineroom-config'
const EDITOR = '.ce[role="dialog"]'
const TEXT = `${EDITOR} textarea[aria-label="config.yaml"]`
const UNKNOWN = 'notARealMikroViewSetting'

const { page, consoleErrors } = await session()

await goTo(page, 'Settings')
await page.waitForSelector(CARD)
check(
  await page.locator(`${CARD} .orow`).filter({ hasText: 'snapshots' }).isVisible(),
  'the Config card shows its snapshots row',
)

// --- open, with the password again -----------------------------------------

await page.click(`${CARD} button:text-is("Open the editor")`)
await page.fill(`${CARD} input[type="password"]`, adminPassword)
await page.click(`${CARD} button:text-is("Open")`)
await page.waitForSelector(TEXT, { timeout: 15000 })

const shown = await page.inputValue(TEXT)
let onDisk = ''
try {
  onDisk = readFileSync(join(process.env.MV_DIR ?? '', 'cfg.yaml'), 'utf8')
} catch (err) {
  check(false, `read the harness's config file -- ${err}`)
}
check(shown.length > 0, 'the editor holds some text')
check(
  shown.split('\n').length === onDisk.split('\n').length,
  `the editor holds the running config line for line (${shown.split('\n').length} lines shown, ${onDisk.split('\n').length} on disk)`,
)
check(shown.split('\n')[0] === onDisk.split('\n')[0], 'the first line is the file\'s own first line')
check(
  (await page.locator(`${EDITOR} .gutter .ln`).count()) === shown.split('\n').length,
  'the gutter numbers every line',
)

// --- a key MikroView does not know -------------------------------------------

const typed = `${shown.replace(/\n*$/, '')}\n${UNKNOWN}: 1\n`
const unknownLine = typed.split('\n').indexOf(`${UNKNOWN}: 1`) + 1
await page.fill(TEXT, typed)
const problem = page.locator(`${EDITOR} .plist button.row`).filter({ hasText: `line ${unknownLine}` })
await problem.first().waitFor({ timeout: 15000 }).catch(() => {})
check((await problem.count()) > 0, `a problem names line ${unknownLine}, where the unknown key was typed`)
if ((await problem.count()) > 0) {
  await problem.first().click()
  const caretLine = await page.$eval(TEXT, (el) => el.value.slice(0, el.selectionStart).split('\n').length)
  check(caretLine === unknownLine, `clicking the problem moves the caret to line ${unknownLine} (got ${caretLine})`)
}

// --- Carry forward ------------------------------------------------------------

await page.click(`${EDITOR} .actions button:text-is("Carry forward")`)
// A headerless file asks to confirm the guessed schema first.
const guess = page.locator(`${EDITOR} button`, { hasText: /^Carry forward from schema/ })
if (await guess.isVisible().catch(() => false)) await guess.click()
await page
  .waitForFunction(
    (sel) => {
      const btn = Array.from(document.querySelectorAll(`${sel} .actions button`)).find((b) =>
        (b.textContent ?? '').includes('Carry'),
      )
      return btn && btn.textContent.trim() === 'Carry forward'
    },
    EDITOR,
    { timeout: 15000 },
  )
  .catch(() => {})
const carryError = await page.locator(`${EDITOR} .msg.error`).textContent().catch(() => null)
check(!carryError, `Carry forward answers without an error -- ${carryError ?? 'none'}`)
check((await page.inputValue(TEXT)).length > 0, 'the editor still holds text after Carry forward')
await page
  .locator(`${EDITOR} .snap`, { hasText: 'before Carry forward' })
  .first()
  .waitFor({ timeout: 10000 })
  .catch(() => {})
check(
  (await page.locator(`${EDITOR} .snap`, { hasText: 'before Carry forward' }).count()) > 0,
  'the server kept a snapshot of the text before Carry forward',
)

// --- Snapshot ------------------------------------------------------------------

await page.click(`${EDITOR} .actions button:text-is("Snapshot")`)
await page.fill(`${EDITOR} input[type="text"]`, 'live-config-editor')
await page.click(`${EDITOR} button:text-is("Keep snapshot")`)
await page
  .locator(`${EDITOR} .msg`, { hasText: 'Snapshot kept.' })
  .waitFor({ timeout: 10000 })
  .catch(() => {})
check(
  (await page.locator(`${EDITOR} .msg`, { hasText: 'Snapshot kept.' }).count()) > 0,
  'Snapshot says it kept one',
)
check(
  (await page.locator(`${EDITOR} .snap`, { hasText: 'live-config-editor' }).count()) > 0,
  'the new snapshot is listed with its note',
)

// --- Close --------------------------------------------------------------------

await page.click(`${EDITOR} .actions button:text-is("Close")`)
const unsaved = page.locator(`${EDITOR} button:text-is("Close without saving")`)
if (await unsaved.isVisible().catch(() => false)) await unsaved.click()
await page.waitForSelector(EDITOR, { state: 'detached', timeout: 5000 }).catch(() => {})
check((await page.locator(EDITOR).count()) === 0, 'Close takes the editor away')
check(
  !(await page.locator(`${CARD} .orow`).filter({ hasText: 'file' }).first().textContent()).includes('shown once'),
  'the card now names the running file',
)

check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, `no console errors -- ${consoleErrors.join(' | ')}`)
done()
