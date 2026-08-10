// SPDX-License-Identifier: AGPL-3.0-only
//
// The scenario that found the #181 defect. Committed rather than thrown
// away, because it is the only thing that catches this class: a rule
// pattern that is cheap against the events already buffered and
// catastrophic against ones that arrive afterwards.
//
// The unit tests around RuleMatcher all pass with that bug present. They
// use a fake Worker, so they can prove the timeout fires and a superseded
// reply is ignored, but they cannot produce the sequence where a real
// pattern changes cost mid-stream.
//
// TEST-DATA GOTCHAS -- three attempts were wasted on these, and each one
// produced a green run that proved nothing:
//
//   1. A rule label of pure 'a's MATCHES (a+)+$ immediately -- the greedy
//      quantifier consumes everything and $ succeeds with no backtracking
//      at all. matchingIds also short-circuits on `||`, so the raw line
//      is never even tested. Perfectly fast, tests nothing.
//   2. The run must be followed by a non-'a' so the $ anchor fails and
//      the engine explores every partition.
//   3. Length matters more than it looks: 30 a's finishes inside the
//      500ms budget, so the Worker returns normally and nothing is
//      refused. 45 does not finish in 20 seconds. Anything below ~40 is
//      a test that silently passes.
import { session, feedSyslog, check, responsive, done } from './live-browser.mjs'

const CHEAP = 'a'.repeat(38)             // matches instantly, see (1)
const EXPENSIVE = 'a'.repeat(45) + 'b'   // does not finish in 20s, see (2)/(3)

feedSyslog(200, CHEAP)
const { page, consoleErrors } = await session({ waitForEvents: 100 })

// #183: the initial fetch and the WebSocket stream overlap, so an event
// arriving in both used to land in the buffer twice and give LiveTable's
// keyed each duplicate keys. This scenario is where that was found --
// asserted here rather than only in live-smoke because this one keeps a
// filter active while the stream runs, which is when it showed up.
check(
  !consoleErrors.some((e) => e.includes('each_key_duplicate')),
  `no duplicate keys in the event buffer (${consoleErrors.filter((e) => e.includes('each_key_duplicate')).length} seen)`,
)

// Regex mode on, with a pattern that is cheap against what is buffered.
await page.click('button.regex-toggle')
await page.fill('input.rule', '(a+)+$')
await page.waitForTimeout(1500)

const refusedEarly = (await page.getAttribute('button.regex-toggle', 'class')).includes('refused')
check(!refusedEarly, 'a pattern that evaluates fine is not refused')

// Now the cost changes underneath it.
feedSyslog(20, EXPENSIVE)

check(await responsive(page, 4000), 'main thread stays responsive while the pattern is unevaluable')

await page.waitForTimeout(1500)
const cls = await page.getAttribute('button.regex-toggle', 'class')
const title = await page.getAttribute('button.regex-toggle', 'title')
check(cls.includes('refused'), 'the toggle reports the pattern was refused')
check(
  /too long|not a valid/.test(title),
  `the tooltip explains why rather than only colouring: "${title.slice(0, 50)}..."`,
)

// #184: clearing the pattern must clear the refused state. The filter is
// inactive either way, so this is about the indicator telling the truth
// -- a toggle still reading "refused" against an empty input is exactly
// the misleading state the indicator exists to avoid.
await page.fill('input.rule', '')
await page.waitForTimeout(800)
const clsAfterClear = await page.getAttribute('button.regex-toggle', 'class')
check(
  !clsAfterClear.includes('refused'),
  'clearing the pattern clears the refused state',
)

// And the recovery path all the way through: a fresh, cheap pattern after
// a refusal must evaluate normally rather than inheriting the dead state.
await page.fill('input.rule', 'live-test')
await page.waitForTimeout(1200)
const clsAfterRetype = await page.getAttribute('button.regex-toggle', 'class')
check(
  !clsAfterRetype.includes('refused'),
  'a fresh pattern after a refusal evaluates normally',
)

done()
