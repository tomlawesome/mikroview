// SPDX-License-Identifier: AGPL-3.0-only
//
// The two things #1231 and #1232 added to the flag drawer, driven for
// real in one scenario because they share one composition (the owner's
// layout call, 2026-09-13): the confidence rating under the sparkline in
// the right column, and the note band full width beneath both columns.
//
// #1231: the detector's 0-100 left the row -- beside the type it read as
// a count of events -- and became a coloured rating with its band word.
// The band word is checked against the number the server actually put on
// the flag rather than against a figure written in here, so this asserts
// the mapping (low 0-39 / moderate 40-69 / high 70-100) rather than the
// feeder's arithmetic.
//
// #1232: write first, judge second. The note is typed while the flag is
// being looked at, carried by the verdict clicked next, read back on
// reopening, editable afterwards, and discarded when the verdict is
// undone -- the owner's four rulings, each driven through the UI and
// confirmed against /api/flags, since the note having *reached the
// server* is the whole point of writing one.
//
// Two independent port-scan sources, because the two halves need
// different verdicts to be reachable at all. An investigate verdict
// leaves the flag open, so its drawer can be reopened to read and edit
// the note; a clearing verdict makes the row inert (round 35's close(r)
// drops the caret), so the undo leg has to be the one that judges,
// undoes, and only then reopens.

import { session, check, done, feedPortScan, waitForFlag, goTo, DESKTOP_VIEWPORT } from './live-browser.mjs'

// Unused by every other scenario here (checked against every
// 198.51.100.* literal in this directory before picking these).
const RATED_IP = '198.51.100.112'
const UNDO_IP = '198.51.100.113'

// 40 distinct ports against a threshold of 15: well past it, so the
// overshoot score lands high rather than hugging a band edge where a
// tuning change would flip the word. Nothing below depends on that --
// the expected word is derived from the score the server reports.
const SCAN_PORTS = 40

// A desktop docket for the same #1150 reason live-verdicts.mjs asks for
// one: below 1300px the verdict chips move into the drawer, and this
// scenario clicks them on the row.
const { page, consoleErrors } = await session({ viewport: DESKTOP_VIEWPORT })

feedPortScan(SCAN_PORTS, RATED_IP)
feedPortScan(SCAN_PORTS, UNDO_IP)

async function openFlags() {
  await goTo(page, 'Flags')
  await page.waitForSelector('table.ftable', { timeout: 10000 })
}

function rowFor(ip) {
  return page.locator(`tr.frow:has-text("${ip}")`)
}

function drawerFor(ip) {
  return page.locator(`tr.frow:has-text("${ip}") + tr.drawer`)
}

async function flagByTarget(target) {
  const res = await page.request.get(`${process.env.MV_URL}/api/flags`)
  const body = await res.json()
  return (body.flags ?? []).find((f) => f.target === target)
}

// Polls rather than reading once: an awaited click resolves before the
// POST it triggered has landed, so a single immediate read would as
// often catch the instant before the write as prove anything about it.
async function waitForFlagState(target, predicate, { timeoutMs = 5000 } = {}) {
  const deadline = Date.now() + timeoutMs
  let last
  while (Date.now() < deadline) {
    last = await flagByTarget(target)
    if (last && predicate(last)) return { ok: true, flag: last }
    await page.waitForTimeout(300)
  }
  return { ok: false, flag: last }
}

// The drawing's three bands (frontend/src/lib/confidenceBand.ts). Kept
// here as its own copy on purpose: a scenario that imported the app's
// own function would agree with it however wrong both were.
function bandFor(n) {
  if (n >= 70) return 'high'
  if (n >= 40) return 'moderate'
  return 'low'
}

async function openDrawer(ip) {
  await rowFor(ip).locator('button.openc').click()
  const drawer = drawerFor(ip)
  await drawer.waitFor({ timeout: 5000 })
  return drawer
}

// Server-side first (#354): a locator timeout on its own cannot say
// whether the scan raised no flag at all or the row just has not
// rendered yet.
const raised = []
for (const ip of [RATED_IP, UNDO_IP]) {
  const r = await waitForFlag(page, ip)
  check(r.ok, r.message)
  raised.push(r)
}

if (raised.every((r) => r.ok)) {
  await openFlags()
  await rowFor(RATED_IP).waitFor({ timeout: 15000 })

  // --- #1231: the row carries the type alone ------------------------

  check(
    (await page.locator('tr.frow .fmark .conf').count()) === 0,
    'no flag row carries a bare number beside its type any more',
  )
  check(
    !/\d/.test((await rowFor(RATED_IP).locator('td.fmark').textContent()) ?? ''),
    'a scored flag\'s FLAG cell reads as its type and nothing numeric',
  )

  // --- #1231: the rating in the drawer, under the sparkline ---------

  const ratedFlag = await flagByTarget(RATED_IP)
  const score = ratedFlag?.confidence
  if (typeof score === 'number') {
    const drawer = await openDrawer(RATED_IP)
    const block = drawer.locator('.side .conf')
    await block.waitFor({ timeout: 5000 })

    const word = bandFor(score)
    check(
      (await block.locator('.cval b').textContent())?.trim() === String(score),
      `the drawer shows the detector's own number, ${score}`,
    )
    check(
      (await block.locator('.cval em').textContent())?.trim() === word,
      `and the band word that number falls in, "${word}"`,
    )
    check(
      ((await block.getAttribute('class')) ?? '').split(/\s+/).includes(`c-${word}`),
      `the block wears the ${word} ink, so number, word and colour agree`,
    )
    check(
      (await block.getAttribute('aria-label')) === `confidence ${score} of 100, ${word}`,
      'the block names itself as a confidence rating out of 100, band included',
    )
    // Normalised: the sentence is wrapped across lines in the markup, so
    // textContent carries the source's own newline and indentation where
    // the rendered line has one space.
    const why = ((await block.locator('.cwhy').textContent()) ?? '').replace(/\s+/g, ' ')
    check(
      why.includes("The detector's number, not a verdict."),
      `and says in words that it is the detector's number, not a verdict (got: ${JSON.stringify(why)})`,
    )
    // The rating is a caption to the sparkline, so it sits under it
    // rather than anywhere else in the column (the owner's composition
    // call: "it is the sparkline's number").
    check(
      await drawer.evaluate((el) => {
        const side = el.querySelector('.side')
        const span = side?.querySelector('.span')
        const conf = side?.querySelector('.conf')
        if (!span || !conf) return false
        return conf.getBoundingClientRect().top >= span.getBoundingClientRect().bottom
      }),
      'and it sits under the episode sparkline and its caption, in the right column',
    )
    check(
      !((await drawer.locator('.side .span').textContent()) ?? '').includes('scored'),
      'the episode caption no longer repeats the score',
    )
    check(
      (await drawer.locator('.story .scored').count()) === 0,
      'and the story no longer carries its own "Scored N." sentence',
    )
  } else {
    check(false, `no confidence on the ${RATED_IP} flag, so the rating cannot be driven: ${JSON.stringify(ratedFlag)}`)
  }

  // --- #1232: write first, judge second ------------------------------

  const ratedDrawer = drawerFor(RATED_IP)
  if ((await ratedDrawer.count()) === 0) await openDrawer(RATED_IP)
  const box = ratedDrawer.locator('.note textarea')
  await box.waitFor({ timeout: 5000 })

  check(
    ((await ratedDrawer.locator('.note .nhint').textContent()) ?? '').includes('goes if the verdict is undone'),
    'the box says what happens to what you write: kept with the verdict, editable, gone on undo',
  )
  // Full width across the bottom, below both columns and above the
  // buttons -- the owner's composition call, and what makes growing the
  // box push the buttons down rather than squeeze the rating beside it.
  check(
    await ratedDrawer.evaluate((el) => {
      const inner = el.querySelector('.dwr-in')
      const note = el.querySelector('.note')
      const side = el.querySelector('.side')
      const acts = el.querySelector('.dwr-acts')
      if (!inner || !note || !side || !acts) return false
      const n = note.getBoundingClientRect()
      const spansBothColumns = n.width > side.getBoundingClientRect().width * 1.5
      const belowTheColumns = n.top >= side.getBoundingClientRect().bottom
      const aboveTheButtons = acts.getBoundingClientRect().top >= n.bottom
      return spansBothColumns && belowTheColumns && aboveTheButtons
    }),
    'the note is a full-width band below both columns and above the buttons',
  )

  // Growth is the ruling this has to prove -- "the drawer just gets
  // taller vertically, everything just moves down naturally with it" --
  // so it is measured across a short note and a much longer one rather
  // than asserted about one fill that might fit on a single line anyway.
  const shortNote = 'Left it alone.'
  const firstNote =
    'Every source already on the upstream block list, same as the August set.\n' +
    'Nothing inside answered anything, and the source count is flat rather than climbing.\n' +
    'Leaving it alone for now; worth another look if the count moves.'
  await box.fill(shortNote)
  await page.waitForTimeout(100)
  const shortHeight = await box.evaluate((el) => el.clientHeight)
  const actsBefore = await ratedDrawer.locator('.dwr-acts').evaluate((el) => el.getBoundingClientRect().top)
  await box.fill(firstNote)
  await page.waitForTimeout(100)
  const tallHeight = await box.evaluate((el) => el.clientHeight)
  check(tallHeight > shortHeight, `the box grows with what is typed (${shortHeight}px -> ${tallHeight}px)`)
  check(
    await box.evaluate((el) => {
      const s = getComputedStyle(el)
      return s.overflowY === 'hidden' && s.resize === 'none'
    }),
    'and grows rather than scrolling inside itself or offering a drag handle',
  )
  check(
    (await ratedDrawer.locator('.dwr-acts').evaluate((el) => el.getBoundingClientRect().top)) > actsBefore,
    'so the buttons below it move down, which is the drawer getting taller',
  )

  await rowFor(RATED_IP).locator('button.v.investigate').click()
  const carried = await waitForFlagState(RATED_IP, (f) => f.verdict === 'investigate' && f.note === firstNote)
  check(carried.ok, `what was typed reached the server with the verdict clicked next (got: ${JSON.stringify(carried.flag)})`)

  // --- #1232: reopen the flag and read it back ----------------------

  await rowFor(RATED_IP).locator('button.openc').click()
  await drawerFor(RATED_IP).waitFor({ state: 'detached', timeout: 5000 }).catch(() => {})
  const reopened = await openDrawer(RATED_IP)
  check(
    (await reopened.locator('.note textarea').inputValue()) === firstNote,
    'closing and reopening the flag reads the note back in the box',
  )

  // --- #1232: edit it, saved on the way out of the box ---------------

  const editedNote = `${firstNote} Checked again the next morning: unchanged.`
  await reopened.locator('.note textarea').fill(editedNote)
  await page.keyboard.press('Tab')
  const edited = await waitForFlagState(RATED_IP, (f) => f.note === editedNote)
  check(edited.ok, `the edit saved on leaving the box (got: ${JSON.stringify(edited.flag)})`)

  // --- #1232: undo takes the note with the verdict -------------------
  //
  // Its own flag, because undo is only offered once a verdict has
  // cleared the row -- and a cleared row is inert, so the drawer this
  // leg has to type into must be opened before the call and reopened
  // only after the undo.

  const undoRow = rowFor(UNDO_IP)
  await undoRow.waitFor({ timeout: 15000 })
  const undoDrawer = await openDrawer(UNDO_IP)
  const undoNote = 'Same source as last week. Fixed the rule; calling it checked.'
  await undoDrawer.locator('.note textarea').fill(undoNote)
  await undoRow.locator('button.v.checked').click()
  await undoRow.locator('.stamp.checked').waitFor({ timeout: 5000 })

  const judged = await waitForFlagState(UNDO_IP, (f) => f.cleared && f.verdict === 'checked' && f.note === undoNote)
  check(judged.ok, `a clearing verdict carries its note too (got: ${JSON.stringify(judged.flag)})`)

  await undoRow.locator('.olink', { hasText: 'undo' }).click()
  await undoRow.locator('button.v.checked').waitFor({ timeout: 5000 })
  const undone = await waitForFlagState(UNDO_IP, (f) => !f.verdict && !f.note)
  check(
    undone.ok,
    `undoing the verdict discarded the note with it -- the flag holds the only copy (got: ${JSON.stringify(undone.flag)})`,
  )

  const afterUndo = await openDrawer(UNDO_IP)
  check(
    (await afterUndo.locator('.note textarea').inputValue()) === '',
    'and the box is empty when the reopened flag is looked at again',
  )
} else {
  check(true, 'skipped -- the drawer cannot be driven without its two port-scan flags')
}

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
