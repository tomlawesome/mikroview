// SPDX-License-Identifier: AGPL-3.0-only
//
// #910: the disk group in Settings, driven for real. Round 42's `#diskg`
// -- the bar of days held, the track of days allowed, the cap figure and
// the off link -- against a running instance with a key mounted, the
// way scripts/live-env.sh brings one up.
//
// Three things are proved here that a component test over a mocked API
// cannot prove:
//
//  - Shrinking the days is a proposal: the link names what would go, in
//    the server's own figures, and nothing changes until it is taken.
//  - `turn off` names every day on disk, and taking the link really
//    empties the history directory -- the deletion the sentence promised.
//  - `turn on` is immediate, and the row reads a held window again once
//    the writer has flushed.
//
// What this scenario cannot do: a gate instance has been on for minutes,
// so it holds exactly one day. A shrink that would delete something
// needs more days on disk than the figure proposed, and there is no
// yesterday to delete. The expected wording is therefore derived from
// the server's `held.days` rather than asserted as `delete N days`
// outright -- on a gate it comes out as the plain `apply` branch, and on
// an instance that has been up for days the delete branch is checked
// with the same code. Neither branch is skipped, and neither is faked.
//
// The read-only rendering is not exercised: GET /api/settings/history is
// admin-only (the contract on #910), so a `user` gets no group at all
// rather than a still copy. See the PR.
//
// Scenarios share one instance and run in filename order. This one sorts
// before live-history.mjs, which expects a day file to exist, so history
// is left ON with events flowing and the days put back where they were.

import { readdirSync } from 'node:fs'
import { join } from 'node:path'
import { session, check, done, goTo, feedSyslog, responsive } from './live-browser.mjs'

const DISKG = '#diskg'
const SLIDER = `${DISKG} svg[role="slider"]`
const NOTE = `${DISKG} .memnote`

// Something on disk before anything is asserted about it: the group at
// rest is a bar of held days, and an instance that has written nothing
// yet has no bar.
feedSyslog(60, 'live-history-control')
const { page, consoleErrors } = await session({ waitForEvents: 30 })

const dir = process.env.MV_DIR
check(Boolean(dir), `the harness exported MV_DIR -- got ${dir ?? 'nothing'}`)
const historyDir = join(dir, 'data', 'history')

/** The server's own answer, which is what every row below restates. */
async function history(p) {
  const res = await p.request.get(new URL('/api/settings/history', p.url()).toString())
  check(res.ok(), `GET /api/settings/history answers an admin -- status ${res.status()}`)
  return res.json()
}

/** Polls the server until something is held on disk (the writer flushes every few seconds). */
async function heldOnServer(p, timeoutMs = 30000) {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const s = await history(p)
    if (s.held) return s
    if (Date.now() > deadline) return s
    await new Promise((r) => setTimeout(r, 1000))
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
    const names = dayFiles()
    if (names.length === 0) return true
    if (Date.now() > deadline) return false
    await new Promise((r) => setTimeout(r, 500))
  }
}

/** The group's rows, as rendered: { "on disk": "...", allowed: "...", key: "..." }. */
async function rows(p) {
  return p.$$eval(`${DISKG} .orow`, (els) =>
    Object.fromEntries(
      els.map((el) => {
        const [k, v] = el.querySelectorAll(':scope > span')
        return [k.textContent.trim(), v.textContent.trim().replace(/\s+/g, ' ')]
      }),
    ),
  )
}

const noteText = async (p) => ((await p.locator(NOTE).textContent()) ?? '').replace(/\s+/g, ' ').trim()
const figureNow = (p) => p.getAttribute(SLIDER, 'aria-valuenow').then(Number)
const plural = (n, what) => `${n} ${what}${n === 1 ? '' : 's'}`

/**
 * Moves the handle to an exact day count by keyboard, as the memory
 * scenario does: Home, doublings while the next still fits, then single
 * days. Bounded, so a control that stops moving fails rather than spins.
 */
async function moveTo(p, targetDays) {
  await p.focus(SLIDER)
  await p.keyboard.press('Home')
  for (let i = 0; i < 10; i++) {
    if ((await figureNow(p)) * 2 > targetDays) break
    await p.keyboard.press('PageUp')
  }
  for (let i = 0; i < 400; i++) {
    if ((await figureNow(p)) >= targetDays) break
    await p.keyboard.press('ArrowRight')
  }
  return figureNow(p)
}

/**
 * Remounts Settings. The group refreshes itself once a minute; going
 * away and back reads the server again now, without a reload (which
 * would put the first-run setup modal back over the shell).
 */
async function remount(p) {
  await goTo(p, 'Stream')
  await goTo(p, 'Settings')
  await p.waitForSelector(DISKG)
}

// --- at rest -------------------------------------------------------------

await goTo(page, 'Settings')
await page.waitForSelector(DISKG)

const start = await heldOnServer(page)
check(start.keyed === true, 'the harness mounted a key, so the group has a control')
check(start.enabled === true, 'history is on when this scenario begins')
check(start.held !== null, `the server holds a window -- got ${JSON.stringify(start.held)}`)
check(
  start.days >= 1 && start.days <= 365,
  `the days allowed are a whole number of days between 1 and 365 (got ${start.days})`,
)
await remount(page)

const atRest = await rows(page)
// KiB too: a gate instance up for minutes holds a few KiB, not a MiB (#932).
const heldRow = /^(\d+) days? · since .+ · \d+(\.\d+)? [KGM]iB( — (filling|full))?$/
check(heldRow.test(atRest['on disk'] ?? ''), `the on-disk row reads a held window -- got "${atRest['on disk']}"`)
check(
  (atRest['on disk'] ?? '').startsWith(`${plural(start.held.days, 'day')} ·`),
  `the row leads with the server's ${start.held.days} held days -- got "${atRest['on disk']}"`,
)
const wantSuffix = start.capped ? ' — full' : start.held.days < start.days ? ' — filling' : ''
check(
  wantSuffix === '' ? !/ — (filling|full)$/.test(atRest['on disk']) : atRest['on disk'].endsWith(wantSuffix),
  `the row ends "${wantSuffix || '(nothing)'}", as the server's figures say -- got "${atRest['on disk']}"`,
)
check(
  (atRest.allowed ?? '').startsWith(`${start.days} days · at most `),
  `the allowed row leads with the days in effect -- got "${atRest.allowed}"`,
)
check((atRest.allowed ?? '').endsWith(' · turn off'), `the allowed row ends with the off link -- got "${atRest.allowed}"`)
check(atRest.key === 'mounted at start', `the key row says where the key came from -- got "${atRest.key}"`)
check((await page.locator(`${DISKG} svg.stmem`).count()) === 1, 'the bar of days held is drawn')
check(
  (await page.locator(`${DISKG} svg.stmem rect[fill="var(--accent)"]`).count()) === start.held.days,
  `the bar has one cell per held day (${start.held.days})`,
)
check((await figureNow(page)) === start.days, `the track's handle sits on the days in effect (${start.days})`)
check((await page.locator(NOTE).count()) === 0, 'nothing is proposed at rest')

// --- fewer days: a proposal, worded from the server's figures ------------

await page.focus(SLIDER)
await page.keyboard.press('Home')
check((await figureNow(page)) === 1, 'Home takes the handle to 1 day')
check((await page.locator(NOTE).count()) === 1, 'a consequence sentence appears while a shrink is proposed')

const wouldGo = start.held.days - 1
const wantApply = wouldGo > 0 ? `delete ${plural(wouldGo, 'day')}` : 'apply'
const wantKeep = wouldGo > 0 ? `keep all ${start.held.days}` : `keep ${start.days} days`
check(
  (await page.locator(`${NOTE} button:has-text("${wantApply}")`).count()) === 1,
  `the link is "${wantApply}": ${wouldGo} of ${start.held.days} held days would go`,
)
check(
  (await page.locator(`${NOTE} button:has-text("${wantKeep}")`).count()) === 1,
  `the other link is "${wantKeep}"`,
)
const shrinkNote = await noteText(page)
check(
  wouldGo > 0
    ? new RegExp(`the ${wouldGo === 1 ? 'day' : `${wouldGo} days`} before .+ lets? go`).test(shrinkNote)
    : / — nothing on disk lets go/.test(shrinkNote),
  `the sentence says what the shrink costs -- got "${shrinkNote}"`,
)
check(
  (await page.locator(`${DISKG}.dshrink`).count()) === 1,
  "the group is in round 42's dshrink state while a shrink is proposed",
)
check(
  (await page.locator(`${DISKG} g.dcut`).count()) === (wouldGo > 0 ? 1 : 0),
  wouldGo > 0 ? 'the days that would go are marked on the bar itself' : 'the bar marks nothing when nothing would go',
)
check((await page.locator(`${DISKG} circle.mghost`).count()) === 1, "the handle's old place stays as a ghost")
const midDrag = await history(page)
check(midDrag.days === start.days, `proposing changed nothing on the server (still ${midDrag.days} days)`)

await page.click(`${NOTE} button:has-text("${wantKeep}")`)
await page.waitForSelector(NOTE, { state: 'detached', timeout: 5000 })
check((await figureNow(page)) === start.days, 'keep puts the handle back where it was')

// Fewer days, applied: the row follows. Half the figure in effect, so
// there is a proposal to take whatever the instance was configured with.
const fewer = Math.max(1, Math.floor(start.days / 2))
check(fewer < start.days, `there is room below ${start.days} days to shrink into (to ${fewer})`)
const reached = await moveTo(page, fewer)
check(reached === fewer, `the handle reached ${fewer} days exactly (got ${reached})`)
await page.click(`${NOTE} button:has-text("${fewer < start.held.days ? 'delete' : 'apply'}")`)
await page.waitForSelector(NOTE, { state: 'detached', timeout: 30000 })
const afterFewer = await history(page)
check(afterFewer.days === fewer, `the server is keeping ${fewer} days (got ${afterFewer.days})`)
const fewerRows = await rows(page)
check(
  (fewerRows.allowed ?? '').startsWith(`${fewer} days · at most `),
  `the allowed row followed the change -- got "${fewerRows.allowed}"`,
)
check(
  (fewerRows['on disk'] ?? '').startsWith(`${plural(afterFewer.held?.days ?? 0, 'day')} ·`),
  `the on-disk row still reads the server's window (${afterFewer.held?.days} days) -- got "${fewerRows['on disk']}"`,
)

// --- turn off: a proposal that names every day, then really deletes ----

await page.click(`${DISKG} button:has-text("turn off")`)
await page.waitForSelector(NOTE)
const heldNow = afterFewer.held.days
check(
  (await page.locator(`${NOTE} button:has-text("delete ${plural(heldNow, 'day')}")`).count()) === 1,
  `the link names every day on disk: "delete ${plural(heldNow, 'day')}"`,
)
check((await page.locator(`${NOTE} button:has-text("keep them")`).count()) === 1, 'the other link is "keep them"')
const offNote = await noteText(page)
check(
  (heldNow === 1
    ? /^off deletes the one day on disk, .+, and keeps nothing after/
    : new RegExp(`^off deletes all ${heldNow} days on disk, back to .+, and keeps nothing after`)
  ).test(offNote),
  `the sentence says what off costs -- got "${offNote}"`,
)
check((await page.locator(`${DISKG}.doff`).count()) === 1, "the group is in round 42's doff state")
const offRows = await rows(page)
check((offRows.allowed ?? '').endsWith(' · off'), `the allowed row shows the proposed off -- got "${offRows.allowed}"`)
check((await history(page)).enabled === true, 'proposing off changed nothing on the server')

await page.click(`${NOTE} button:has-text("delete ${plural(heldNow, 'day')}")`)
await page.waitForSelector(NOTE, { state: 'detached', timeout: 30000 })
const off = await history(page)
check(off.enabled === false, 'the server has history off')
check(off.held === null, `nothing is held after off -- got ${JSON.stringify(off.held)}`)
check(await dayFilesGone(), `${historyDir} holds no day files after off -- got ${dayFiles().join(', ') || 'none'}`)

const stoppedRows = await rows(page)
check(stoppedRows['on disk'] === 'nothing', `the on-disk row reads "nothing" -- got "${stoppedRows['on disk']}"`)
check((stoppedRows.allowed ?? '').endsWith(' · turn on'), `the allowed row ends with "turn on" -- got "${stoppedRows.allowed}"`)
check((await page.locator(`${DISKG} svg.stmem`).count()) === 0, 'there is no bar with nothing on disk')
check((await page.locator(SLIDER).count()) === 1, 'the track is still live while off')
check((await page.locator(`${DISKG}.dstopped`).count()) === 1, "the group is in round 42's dstopped state")
const hint = ((await page.locator(`${DISKG} .wleft .oghint`).first().textContent()) ?? '').replace(/\s+/g, ' ').trim()
check(
  /^nothing on disk — events live in memory only/.test(hint),
  `the hint says where events live now -- got "${hint}"`,
)

// --- turn on: immediate, and a held window comes back --------------------

await page.click(`${DISKG} button:has-text("turn on")`)
await page.locator(`${DISKG} button:has-text("turn on")`).waitFor({ state: 'detached', timeout: 30000 })
check((await page.locator(NOTE).count()) === 0, 'turning on asks nothing: no proposal, it deletes nothing')
const on = await history(page)
check(on.enabled === true, 'the server has history on again')
check(on.days === fewer, `the days survived the off/on (${on.days})`)

feedSyslog(60, 'live-history-control')
const back = await heldOnServer(page)
check(back.held !== null, `the writer flushed a day file again -- got ${JSON.stringify(back.held)}`)
check(dayFiles().length > 0, `${historyDir} holds a day file again`)
await remount(page)
const onRows = await rows(page)
check(heldRow.test(onRows['on disk'] ?? ''), `the on-disk row reads a held window again -- got "${onRows['on disk']}"`)
check((onRows.allowed ?? '').endsWith(' · turn off'), `the allowed row offers "turn off" again -- got "${onRows.allowed}"`)

// --- put it back -----------------------------------------------------------
// Days back where this scenario found them (a grow: plain apply) and
// history left on, for live-history.mjs and everything after it.

if (fewer !== start.days) {
  const restored = await moveTo(page, start.days)
  check(restored === start.days, `the handle reached ${start.days} days again (got ${restored})`)
  await page.click(`${NOTE} button:has-text("apply")`)
  await page.waitForSelector(NOTE, { state: 'detached', timeout: 30000 })
}
const end = await history(page)
check(
  end.enabled === true && end.days === start.days,
  `history is on and keeping ${start.days} days, as this scenario found it (got on=${end.enabled}, ${end.days} days)`,
)

check(await responsive(page), 'the main thread is still answering after the off and on')
check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)

done()
