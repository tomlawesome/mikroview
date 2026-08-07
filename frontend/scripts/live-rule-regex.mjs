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

// Two behaviours this scenario surfaced are not asserted here, because
// they do not work yet and a committed assertion that fails is not a
// test -- it is broken state recorded in the wrong place. They are on
// the tracker instead:
//
//   #183 -- duplicate event ids in the client buffer produce duplicate
//           Svelte keys, so a console-error assertion cannot pass here.
//   #184 -- the refused indicator does not clear when the pattern is
//           cleared.
//
// Add the assertions when the fixes land.

done()
