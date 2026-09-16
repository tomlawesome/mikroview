// SPDX-License-Identifier: AGPL-3.0-only
//
// #1226: the Stream filter bar's Protocol and Interface boxes used to be
// free text an operator typed and hoped -- "tcp" or "TCP", whichever the
// router happened to log. GET /api/seen-values (internal/api/seen.go)
// now answers with the values this instance has actually observed for
// the two fields, from internal/seen's register, and FilterBar.svelte
// wires them into a <datalist> combo per field
// (input[aria-label="Protocol"]/list="fb-seen-protos",
// input[aria-label="Interface"]/list="fb-seen-interfaces"). This drives
// the real endpoint and the real strip together: a value that just
// arrived is offered, and a value that never arrived is still accepted
// -- the register only ever adds suggestions, it never gates the filter.
//
// Unit tests cover the register's own retention and folding rules
// (internal/seen/register_test.go); this is the one thing they cannot
// see -- that a real event landing really does make it into the real
// menu a real browser renders, end to end.

import { session, feedRaw, check, done, goTo, unfoldStreamFilter } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

// Deliberately odd, so this run's own values cannot be confused with
// anything the shared feed or a sibling scenario already pushed --
// see the family note in scripts/run-scenarios.sh about scenarios
// sharing one instance and one ordering.
const RULE = 'mv1226-seenvalues'
const PROTO = 'mv1226proto'
const IN_IFACE = 'ether1226in'
const OUT_IFACE = 'bridge1226out'
const NEVER_SEEN_PROTO = 'mv1226-typed-unseen'
const NEVER_SEEN_IFACE = 'mv1226-iface-unseen'

const { page, consoleErrors } = await session()

async function api(path) {
  const res = await page.request.fetch(`${URL_BASE}${path}`)
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

// Same wait-and-refeed pattern live-filter-parity.mjs uses: under the
// full suite's load a single fed line can be dropped by a saturated
// ingest queue, and a bare page.waitForSelector would turn that into an
// uncaught timeout with no RESULT line.
async function waitForArrival(line, timeoutMs = 25000) {
  feedRaw(line)
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    const { body } = await api(`/api/events?rule=${RULE}&limit=5`)
    if ((body?.events?.length ?? 0) > 0) return true
    await new Promise((r) => setTimeout(r, 2000))
    feedRaw(line)
  }
  return false
}

const line =
  `firewall,info A|${RULE}| forward: in:${IN_IFACE} out:${OUT_IFACE}, connection-state:new, ` +
  `proto ${PROTO}, 203.0.113.226:41111->203.0.113.227:443, len 60`

const arrived = await waitForArrival(line)
check(arrived, `the ${RULE} test event reached the server`)
if (!arrived) {
  check(true, 'skipped the rest -- the seeding event never arrived')
  done()
}

// --- GET /api/seen-values reports what the feed actually used -----------

async function waitForSeenValues(field, value, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs
  let last = null
  while (Date.now() < deadline) {
    const { status, body } = await api('/api/seen-values')
    last = { status, body }
    const values = (body?.fields?.[field] ?? []).map((v) => v.value)
    if (status === 200 && values.includes(value)) return last
    await new Promise((r) => setTimeout(r, 500))
  }
  return last
}

const protoSeen = await waitForSeenValues('proto', PROTO)
check(protoSeen?.status === 200, `GET /api/seen-values answers 200 (${protoSeen?.status})`)
check(
  (protoSeen?.body?.fields?.proto ?? []).some((v) => v.value === PROTO),
  `the seen-values register lists the pushed protocol "${PROTO}" (got ${JSON.stringify(protoSeen?.body?.fields?.proto)})`,
)

const ifaceSeen = await waitForSeenValues('interface', IN_IFACE)
const ifaceValues = (ifaceSeen?.body?.fields?.interface ?? []).map((v) => v.value)
check(ifaceValues.includes(IN_IFACE), `the seen-values register lists the pushed "in" interface "${IN_IFACE}" (got ${JSON.stringify(ifaceValues)})`)
check(
  ifaceValues.includes(OUT_IFACE),
  `the seen-values register lists the pushed "out" interface "${OUT_IFACE}" in the same list (got ${JSON.stringify(ifaceValues)})`,
)

// --- The strip's own datalists offer the same values ---------------------
//
// seenValuesState fetches once per page load and caches (seenValues.svelte.ts),
// so a fresh load is needed for the values pushed above to reach the
// menu -- the same reasoning live-fall-composition.mjs gives for
// reloading on a width change.
let reloaded = true
try {
  await page.reload({ waitUntil: 'networkidle' })
  await page.waitForSelector('#main-content', { timeout: 15000 })
  await goTo(page, 'Stream')
  await page.waitForSelector('input.rule', { timeout: 15000 })
} catch {
  reloaded = false
}
check(reloaded, 'the page reloaded onto a fresh Stream view')

if (reloaded) {
  await unfoldStreamFilter(page)

  async function datalistOptions(id) {
    return page.$$eval(`#${id} option`, (opts) => opts.map((o) => o.value))
  }

  let protoOptions = []
  let ifaceOptions = []
  const deadline = Date.now() + 10000
  while (Date.now() < deadline) {
    protoOptions = await datalistOptions('fb-seen-protos')
    ifaceOptions = await datalistOptions('fb-seen-interfaces')
    if (protoOptions.includes(PROTO) && ifaceOptions.includes(IN_IFACE) && ifaceOptions.includes(OUT_IFACE)) break
    await page.waitForTimeout(500)
  }

  check(
    protoOptions.includes(PROTO),
    `#fb-seen-protos offers the observed protocol "${PROTO}" (got ${JSON.stringify(protoOptions)})`,
  )
  check(
    ifaceOptions.includes(IN_IFACE) && ifaceOptions.includes(OUT_IFACE),
    `#fb-seen-interfaces offers both observed interfaces (got ${JSON.stringify(ifaceOptions)})`,
  )

  // --- ...and a value that was never seen is still accepted, not refused -
  await page.fill('input[aria-label="Protocol"]', NEVER_SEEN_PROTO)
  const protoTyped = await page.inputValue('input[aria-label="Protocol"]')
  check(
    protoTyped === NEVER_SEEN_PROTO,
    `the Protocol box still accepts a value it has never seen (got "${protoTyped}")`,
  )

  await page.fill('input[aria-label="Interface"]', NEVER_SEEN_IFACE)
  const ifaceTyped = await page.inputValue('input[aria-label="Interface"]')
  check(
    ifaceTyped === NEVER_SEEN_IFACE,
    `the Interface box still accepts a value it has never seen (got "${ifaceTyped}")`,
  )

  // Cleanup: leave the strip's own filters empty for whatever runs next.
  await page.fill('input[aria-label="Protocol"]', '')
  await page.fill('input[aria-label="Interface"]', '')
}

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
