// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #363 item 2: Group mode (#341) ships asserting only direction
// (grouping never produces MORE rows, live-group-mode.mjs) and honesty
// (the counts account for every event). Neither is a measurement of what
// it actually compresses. This scenario is that measurement.
//
// No real RouterOS capture is available in this environment, so this
// feeds three traffic *shapes* chosen to bracket the real answer rather
// than guess a single blended number:
//
//   - "hammer": one connection (same source, destination, port) hit
//     repeatedly -- a blocked host retrying, or a bot backing off and
//     retrying the exact same target. The case Group mode was built for.
//   - "sweep": one source, one destination, a different port every hit --
//     a port scan. Deliberately NOT collapsed, because the grouping key
//     is source+destination+port+protocol+action (the owner's "golden
//     group", #363/#341) -- rule and chain are excluded, but port is not.
//     This is the case the owner flagged as a real risk: "if repetition
//     is rare, the option is machinery nobody benefits from."
//   - "background": scripts/live-env.sh's own generic `syslog` generator
//     (also what live-smoke.mjs/live-autoscroll.mjs feed) -- a round-robin
//     of 250 distinct sources against one fixed destination:port, which
//     is what every *other* scenario in this suite treats as ordinary
//     traffic. Included so the number isn't only the two extremes.
//
// Each is measured on its own (filtered to its own rule label, so the
// three feeds -- and whatever earlier scenarios in the same live-check
// run already pushed -- can't contaminate each other's count) and then
// combined, so the write-up can report the real range rather than one
// number that hides which end of it any given deployment lands on.

import { session, feedRaw, feedSyslog, check, done } from './live-browser.mjs'

const { page, consoleErrors } = await session({ waitForEvents: 50 })

const rowCount = () => page.$$eval('.grid .row', (els) => els.length)

async function setGroupMode(desired) {
  const current = await page.evaluate(() => localStorage.getItem('mikroview:group') === '1')
  if (current !== desired) {
    await page.click('button:text-is("Group")')
    await page.waitForTimeout(400)
  }
}

async function filterTo(label) {
  await page.fill('input.rule', label)
  await page.waitForTimeout(400)
}

/** measure returns {normal, grouped, reduction} for whatever the current rule filter shows. */
async function measure(label) {
  await filterTo(label)
  await setGroupMode(false)
  const normal = await rowCount()
  await setGroupMode(true)
  const grouped = await rowCount()
  const reduction = normal > 0 ? (1 - grouped / normal) * 100 : 0
  return { normal, grouped, reduction }
}

// --- Feed the three shapes ------------------------------------------------

const HAMMER_N = 200
const hammerLines = Array.from(
  { length: HAMMER_N },
  (_, i) =>
    `firewall,info D|compress-hammer| forward: in:ether1 out:bridge1, ` +
    `connection-state:new, proto TCP (SYN), 198.51.100.50:${40000 + i}->192.0.2.10:3389, len 60`,
).join('\n')
feedRaw(hammerLines)

const SWEEP_N = 200
const sweepLines = Array.from(
  { length: SWEEP_N },
  (_, i) =>
    `firewall,info D|compress-sweep| forward: in:ether1 out:bridge1, ` +
    `connection-state:new, proto TCP (SYN), 198.51.100.60:${45000 + i}->192.0.2.20:${1000 + i}, len 60`,
).join('\n')
feedRaw(sweepLines)

const BACKGROUND_N = 300
feedSyslog(BACKGROUND_N, 'compress-background')

// The rule filter narrows what's rendered, so waiting on the combined
// label is enough to know all three feeds have landed -- no fixed
// MAX_RENDERED_ROWS=800 risk here since applyFilters runs before the
// slice (LiveTable.svelte), and the combined filtered set (700) is well
// under it.
await page.fill('input.rule', 'compress-')
await page.waitForFunction(
  (want) => document.querySelectorAll('.grid .row').length >= want,
  HAMMER_N + SWEEP_N, // background's own repeats mean its row count is < BACKGROUND_N; don't wait on the full 700
  { timeout: 20000 },
)
await page.waitForTimeout(500) // let the last of the background batch settle in too

// --- Measure each shape and the combination -------------------------------

const hammer = await measure('compress-hammer')
const sweep = await measure('compress-sweep')
const background = await measure('compress-background')
const combined = await measure('compress-')

console.log('')
console.log('Group mode compression, measured (issue #363 item 2):')
console.log(
  `  hammer      (repeated identical connection, n=${HAMMER_N}): ${hammer.normal} -> ${hammer.grouped} rows  (${hammer.reduction.toFixed(1)}% fewer rows)`,
)
console.log(
  `  sweep       (port scan, one src/dst, ${SWEEP_N} distinct ports): ${sweep.normal} -> ${sweep.grouped} rows  (${sweep.reduction.toFixed(1)}% fewer rows)`,
)
console.log(
  `  background  (live-env.sh's generic syslog feed, n=${BACKGROUND_N}): ${background.normal} -> ${background.grouped} rows  (${background.reduction.toFixed(1)}% fewer rows)`,
)
console.log(
  `  combined    (all three together, n=${HAMMER_N + SWEEP_N + BACKGROUND_N}): ${combined.normal} -> ${combined.grouped} rows  (${combined.reduction.toFixed(1)}% fewer rows)`,
)
console.log('')

// --- Sanity on the shapes themselves (direction, not the headline number) -

// The hammer case is what the feature was built for -- it should collapse
// to (close to) one row.
check(
  hammer.grouped <= 2 && hammer.reduction > 95,
  `a repeated identical connection collapses to (near) one row (${hammer.grouped} rows, ${hammer.reduction.toFixed(1)}% reduction)`,
)

// The sweep case is the one the owner named as the risk: strict grouping
// (port is part of the key) means a port scan does NOT collapse at all.
// This is a real, load-bearing finding, not a bug -- confirming it here
// rather than only asserting hammer's success would make this scenario
// blind to exactly the failure mode the measurement exists to catch.
check(
  sweep.grouped === sweep.normal,
  `a port scan is not collapsed by the current (source+dest+port+protocol+action) key -- ${sweep.grouped} of ${sweep.normal} rows remain (this is expected, not a defect)`,
)

// Grouping must never show more rows than ungrouped, for every shape --
// the same direction invariant live-group-mode.mjs asserts generically,
// repeated here because a shape-specific regression (e.g. background's
// round-robin suddenly not collapsing) would otherwise only show up as a
// changed number, not a failure.
for (const [name, m] of [['hammer', hammer], ['sweep', sweep], ['background', background], ['combined', combined]]) {
  check(m.grouped <= m.normal, `${name}: grouped rows (${m.grouped}) <= normal rows (${m.normal})`)
}

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)

// Leave the view as most scenarios expect to find it.
await page.fill('input.rule', '')
await setGroupMode(false)

done()
