// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #640: an expected verdict is a *sized* expectation -- "this much
// of this, from this host, is normal" -- and a resolved one is
// deliberately not a suppression at all. Both halves are only true end
// to end, so both are driven here against a real detector, a real store
// and the real page:
//
//   1. Expected on a 20-port scan absorbs a later 25-port one (inside
//      1.5x) -- the flag does not come back, and nothing appears in the
//      inbox.
//   2. A 40-port scan is past that ceiling, so the flag returns reading
//      "expected up to 20, saw 40" -- the two numbers off the store, not
//      a restatement of either.
//   3. Resolved clears, and the same circumstances recurring bring the
//      flag straight back, saying when it was resolved.
//
// The numbers are read off the API rather than hardcoded: the recorded
// size is whatever the firing the operator judged actually measured, and
// asserting the card repeats *that* is the real claim. Only the
// absorbed/returned decision is asserted as a fixed outcome, and the
// port counts below are chosen so it holds whether or not the detector's
// 60s window has rolled between feeds (25 and 40 distinct ports are both
// the same verdict either way).
//
// **State this leaves behind.** Step 1 records a real, permanent
// expectation for (port_scan, 198.51.100.110). `make live-check` starts
// from a wiped data directory, so a full gate run is unaffected. Running
// this scenario twice against one long-lived instance (the driving lane)
// will find the pair already expected and the first flag absorbed before
// it is ever raised -- the first check below says so by name rather than
// timing out with a message about detector flakiness. There is no API to
// prune an expectation until #640 part C lands the ledger; when it does,
// this scenario should reset itself the way live-flags-clearing.mjs used
// to reset its exclusion.

import { session, check, done, feedPortScan, waitForFlag, goTo } from './live-browser.mjs'

// Unused by every other scenario in this directory -- checked against
// every 198.51.100.* literal already in use here before picking these.
const EXPECT_IP = '198.51.100.110'
const RESOLVE_IP = '198.51.100.111'

feedPortScan(20, EXPECT_IP)
feedPortScan(20, RESOLVE_IP)

const { page, consoleErrors } = await session()

async function openFlags() {
  await goTo(page, 'Flags')
  await page.waitForSelector('table.ftable', { timeout: 10000 })
}

function rowFor(ip) {
  return page.locator(`tr.frow:has-text("${ip}")`)
}

async function flagByTarget(target) {
  const res = await page.request.get(`${process.env.MV_URL}/api/flags`)
  const body = await res.json()
  return (body.flags ?? []).find((f) => f.target === target)
}

async function waitForApiFlag(target, predicate, { timeoutMs = 8000 } = {}) {
  const deadline = Date.now() + timeoutMs
  let last
  while (Date.now() < deadline) {
    last = await flagByTarget(target)
    if (last && predicate(last)) return { ok: true, flag: last }
    await page.waitForTimeout(300)
  }
  return { ok: false, flag: last }
}

// Server-side first (#354): a locator timeout cannot say whether the
// scan raised nothing or merely had not rendered yet.
const first = await waitForFlag(page, EXPECT_IP)
check(
  first.ok,
  `${first.message}${first.ok ? '' : ' -- if this run is a repeat against a long-lived instance, an expectation from the previous run is absorbing it (see this file\'s header)'}`,
)
const resolveRaised = await waitForFlag(page, RESOLVE_IP)
check(resolveRaised.ok, resolveRaised.message)

if (first.ok && resolveRaised.ok) {
  await openFlags()

  // --- 1. Expected records what this host was doing ------------------

  const expectRow = rowFor(EXPECT_IP)
  await expectRow.waitFor({ timeout: 15000 })
  await expectRow.locator('button.v.expected').click()
  await expectRow.locator('.stamp.expected').waitFor({ timeout: 5000 })
  check(true, 'calling expected stamps the row and clears the flag')

  const judged = await waitForApiFlag(EXPECT_IP, (f) => f.cleared && f.verdict === 'expected')
  check(judged.ok, `the expected verdict reached the server (got: ${JSON.stringify(judged.flag)})`)
  const recorded = judged.flag?.size
  check(
    typeof recorded === 'number' && recorded > 0,
    `the judged firing carried a size for the expectation to record (got ${recorded})`,
  )

  // --- 2. A firing inside the tolerance never comes back -------------

  feedPortScan(25, EXPECT_IP)
  // Give the detector longer than it needs -- this assertion is waiting
  // for something *not* to happen, so it has to outlast the raise path
  // rather than race it.
  await page.waitForTimeout(6000)
  const absorbed = await flagByTarget(EXPECT_IP)
  check(
    absorbed?.cleared === true,
    `a 25-port scan is inside 1.5x the recorded ${recorded} and must be absorbed silently (got: ${JSON.stringify(absorbed)})`,
  )

  await openFlags()
  check(
    (await rowFor(EXPECT_IP).count()) === 0,
    'nothing about the absorbed firing reaches the inbox -- no row, not even a cleared one',
  )

  // --- 3. Past the ceiling it returns, carrying both numbers ---------

  feedPortScan(40, EXPECT_IP)
  const back = await waitForApiFlag(EXPECT_IP, (f) => !f.cleared, { timeoutMs: 20000 })
  check(back.ok, `a 40-port scan is past the ceiling and must raise the flag again (got: ${JSON.stringify(back.flag)})`)

  if (back.ok) {
    check(
      back.flag.expectedSize === recorded,
      `the returning flag carries the size the expectation recorded (${back.flag.expectedSize} vs ${recorded})`,
    )
    await openFlags()
    const returnedRow = rowFor(EXPECT_IP)
    await returnedRow.waitFor({ timeout: 15000 })
    const note = (await returnedRow.locator('.returned').textContent())?.trim() ?? ''
    check(
      note === `expected up to ${back.flag.expectedSize}, saw ${back.flag.size}`,
      `the card reads the two real numbers back: "${note}"`,
    )
  }

  // --- 4. Resolved is not a suppression ------------------------------

  await openFlags()
  const resolveRow = rowFor(RESOLVE_IP)
  await resolveRow.waitFor({ timeout: 15000 })
  await resolveRow.locator('button.v.investigate').click()
  await resolveRow.locator('button.v.resolved').waitFor({ timeout: 5000 })
  await resolveRow.locator('button.v.resolved').click()
  await resolveRow.locator('.stamp.resolved').waitFor({ timeout: 5000 })

  const resolved = await waitForApiFlag(RESOLVE_IP, (f) => f.cleared && f.verdict === 'resolved')
  check(resolved.ok, `the resolved verdict cleared the flag server-side (got: ${JSON.stringify(resolved.flag)})`)

  // The same circumstances recur. A resolved verdict records nothing
  // that suppresses, so this must come back -- the fix was not what was
  // intended, and the flag says so.
  feedPortScan(20, RESOLVE_IP)
  const recurred = await waitForApiFlag(RESOLVE_IP, (f) => !f.cleared, { timeoutMs: 20000 })
  check(recurred.ok, `a resolved pair recurring raises the flag again (got: ${JSON.stringify(recurred.flag)})`)

  if (recurred.ok) {
    check(
      recurred.flag.priorVerdict === 'resolved',
      `the returning flag remembers it was resolved (got ${recurred.flag.priorVerdict})`,
    )
    await openFlags()
    const recurredRow = rowFor(RESOLVE_IP)
    await recurredRow.waitFor({ timeout: 15000 })
    const note = (await recurredRow.locator('.returned').textContent())?.trim() ?? ''
    check(
      note.startsWith('resolved on ') && note.endsWith("it's back"),
      `the card says when it was resolved and that it is back: "${note}"`,
    )
  }
} else {
  check(true, 'skipped -- the expectation flow cannot run without both port-scan flags')
}

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
