// SPDX-License-Identifier: AGPL-3.0-only
//
// #1354: deleting what an off history keeps on disk, driven for real.
// Turning history off keeps the day files; the admin is told so in the
// config-problem banner and the disk card, and can delete them only by
// two clicks and their password.
//
// What this proves that a component test over a mocked API cannot:
//
//  - Off really keeps the files: the day files are still in the history
//    directory after the switch moves.
//  - The banner entry is live: it appears when history goes off with
//    files on disk and goes when they are deleted, without a reload.
//  - The delete really empties the directory, only after the password,
//    and lands in the audit log as history.delete.
//  - The route refuses while history is on (409), whatever the password.
//
// Scenarios share one instance and run in filename order. This one runs
// after live-history-control.mjs, which leaves history on with a day
// file, and before live-history.mjs, which expects the same -- so
// history is turned back on and written to again before it finishes.

import { readdirSync } from 'node:fs'
import { join } from 'node:path'
import {
  adminPassword,
  session,
  check,
  done,
  goTo,
  feedSyslog,
  responsive,
  waitForStreamRows,
} from './live-browser.mjs'

const DISKG = '#diskg'
const DELETE = `${DISKG} button.delete`
const PASSWORD = `${DISKG} input[type="password"]`
const WARN = '.banner .warn'

const { page, consoleErrors } = await session()

feedSyslog(60, 'live-history-delete')
await waitForStreamRows(page, 30)

const dir = process.env.MV_DIR
check(Boolean(dir), `the harness exported MV_DIR -- got ${dir ?? 'nothing'}`)
const historyDir = join(dir, 'data', 'history')

async function history(p) {
  const res = await p.request.get(new URL('/api/settings/history', p.url()).toString())
  check(res.ok(), `GET /api/settings/history answers an admin -- status ${res.status()}`)
  return res.json()
}

async function heldOnServer(p, timeoutMs = 30000) {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const s = await history(p)
    if (s.held) return s
    if (Date.now() > deadline) return s
    await new Promise((r) => setTimeout(r, 250))
  }
}

function dayFiles() {
  try {
    return readdirSync(historyDir).filter((n) => n.startsWith('events-') && n.endsWith('.mvevt'))
  } catch {
    return []
  }
}

async function dayFilesGone(timeoutMs = 20000) {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    if (dayFiles().length === 0) return true
    if (Date.now() > deadline) return false
    await new Promise((r) => setTimeout(r, 250))
  }
}

async function onDiskRow(p) {
  return p.$$eval(`${DISKG} .orow`, (els) => {
    for (const el of els) {
      const [k, v] = el.querySelectorAll(':scope > span')
      if (k.textContent.trim() === 'on disk') return v.textContent.trim().replace(/\s+/g, ' ')
    }
    return null
  })
}

const plural = (n, what) => `${n} ${what}${n === 1 ? '' : 's'}`

async function deleteRoute(p, password) {
  return p.request.fetch(new URL('/api/settings/history/files', p.url()).toString(), {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: JSON.stringify({ password }),
  })
}

// --- history on, with something on disk ------------------------------------

await goTo(page, 'Settings')
await page.waitForSelector(DISKG)

const start = await heldOnServer(page)
check(start.enabled === true, 'history is on when this scenario begins')
check(start.held !== null, `the server holds a window -- got ${JSON.stringify(start.held)}`)
check(dayFiles().length > 0, `${historyDir} holds a day file to begin with`)
check((await page.locator(WARN).count()) === 0, 'no history banner entry while history is on')

const refusedWhileOn = await deleteRoute(page, adminPassword)
check(refusedWhileOn.status() === 409, `the delete route refuses while history is on -- got ${refusedWhileOn.status()}`)
check(dayFiles().length > 0, 'a refused delete left the day files alone')

// --- turn off: the files stay, and the admin is told ------------------------

await page.click(`${DISKG} button:has-text("turn off")`)
await page.locator(`${DISKG} button:has-text("turn on")`).waitFor({ timeout: 30000 })
const off = await history(page)
check(off.enabled === false, 'the server has history off')
check(off.held !== null, `the server still reports the held window -- got ${JSON.stringify(off.held)}`)
check(dayFiles().length > 0, `${historyDir} still holds its day files after off`)
const kept = off.held?.days ?? 0

const row = (await onDiskRow(page)) ?? ''
check(
  row.startsWith(`${plural(kept, 'day')} ·`) && row.endsWith('— kept on disk; turn history on with the same key to use them'),
  `the on-disk row reads the kept window -- got "${row}"`,
)

await page.locator(WARN).first().waitFor({ timeout: 10000 }).catch(() => {})
const warn = ((await page.locator(WARN).first().textContent().catch(() => '')) ?? '').replace(/\s+/g, ' ')
check(
  new RegExp(`History is off, but ${plural(kept, 'day')} \\(.+\\) (is|are) still on disk`).test(warn),
  `the banner says history is off with files on disk, without a reload -- got "${warn}"`,
)

// The banner's link lands on the disk card from elsewhere in the app.
await goTo(page, 'Stream')
const go = page.locator(`${WARN} button:has-text("Go to the disk card")`).first()
check((await go.count()) === 1, 'the banner entry links to the disk card')
if ((await go.count()) === 1) {
  await go.click()
  await page.waitForSelector(DISKG, { timeout: 10000 })
  check(await page.isVisible(DISKG), 'the link lands on the disk card')
} else {
  await goTo(page, 'Settings')
  await page.waitForSelector(DISKG)
}

// --- arm, disarm, arm again, then the password -----------------------------

check((await page.locator(DELETE).count()) === 1, 'the delete is offered')
check(((await page.textContent(DELETE)) ?? '').trim() === 'Delete history files', 'at rest it reads "Delete history files"')

await page.click(DELETE)
const armedText = ((await page.textContent(DELETE)) ?? '').trim()
check(armedText === `confirm — delete ${plural(kept, 'day')}`, `the first click arms it -- got "${armedText}"`)
check((await page.locator(PASSWORD).count()) === 0, 'no password field after one click')

await page.click(`${DISKG} h3`)
check(
  ((await page.textContent(DELETE)) ?? '').trim() === 'Delete history files',
  'a click elsewhere disarms it',
)

await page.click(DELETE)
await page.click(DELETE)
await page.waitForSelector(PASSWORD, { timeout: 5000 })
check(dayFiles().length > 0, 'two clicks alone deleted nothing')

// A wrong password: the server's words, the field still open.
await page.fill(PASSWORD, `${adminPassword}-wrong`)
await page.click(`${DISKG} button:has-text("submit")`)
await page.waitForSelector(`${DISKG} .oghint.err`, { timeout: 10000 })
const refusal = ((await page.textContent(`${DISKG} .oghint.err`)) ?? '').trim()
check(refusal === 'incorrect password', `a wrong password is refused in the server's words -- got "${refusal}"`)
check((await page.locator(PASSWORD).count()) === 1, 'the field stays open for another try')
check(dayFiles().length > 0, 'a wrong password deleted nothing')

await page.fill(PASSWORD, adminPassword)
await page.click(`${DISKG} button:has-text("submit")`)
await page.waitForSelector(PASSWORD, { state: 'detached', timeout: 30000 })

// --- gone: the files, the row, the banner entry; and audited ---------------

check(await dayFilesGone(), `${historyDir} holds no day files after the delete -- got ${dayFiles().join(', ') || 'none'}`)
const after = await history(page)
check(after.enabled === false && after.held === null, `the server reports off with nothing held -- got ${JSON.stringify(after)}`)
check((await onDiskRow(page)) === 'nothing', `the on-disk row reads "nothing" -- got "${await onDiskRow(page)}"`)
check((await page.locator(DELETE).count()) === 0, 'with nothing on disk there is nothing to delete')
await page.locator(WARN).first().waitFor({ state: 'detached', timeout: 10000 }).catch(() => {})
check((await page.locator(WARN).count()) === 0, 'the banner entry is gone without a reload')

const audit = await page.request.get(new URL('/api/audit', page.url()).toString()).then((r) => r.json())
const entry = (audit.entries ?? []).find((e) => e.action === 'history.delete')
check(
  Boolean(entry) && entry.target === 'history' && new RegExp(`^${kept} days?, \\d+ bytes, `).test(entry.detail ?? ''),
  `the delete is in the audit log as history.delete -- got ${JSON.stringify(entry)}`,
)

// --- put it back: on, and written to, for live-history.mjs -----------------

await page.click(`${DISKG} button:has-text("turn on")`)
await page.locator(`${DISKG} button:has-text("turn off")`).waitFor({ timeout: 30000 })
feedSyslog(60, 'live-history-delete')
const back = await heldOnServer(page)
check(back.enabled === true, 'the server has history on again')
check(back.held !== null, `the writer flushed a day file again -- got ${JSON.stringify(back.held)}`)
check(dayFiles().length > 0, `${historyDir} holds a day file again`)
check(back.days === start.days, `the days survived the off, delete and on (${back.days})`)

check(await responsive(page), 'the main thread is still answering after the delete')
// The one wrong password above is a 401 the browser engine itself logs
// ("Failed to load resource ... 401"); it is this scenario's own doing,
// so exactly one of those is allowed and nothing else is.
const refused401 = (t) => /the server responded with a status of 401/.test(t)
const others = consoleErrors.filter((t) => !refused401(t))
check(consoleErrors.filter(refused401).length <= 1, `at most the one expected 401 in the console -- got ${JSON.stringify(consoleErrors)}`)
check(others.length === 0, `no other console errors -- got ${JSON.stringify(others)}`)

done()
