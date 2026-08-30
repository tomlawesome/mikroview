// SPDX-License-Identifier: AGPL-3.0-only
//
// #438: "if the view presents it, the bar can consume it" -- and the
// reverse, "if a filter is active, the bar shows it and can clear it".
// Unit tests (addressMatch.test.ts, portMatch.test.ts, countryMatch.test.ts,
// state.svelte.test.ts) pin the matching logic in isolation; this drives
// the real bar against a real browser to prove the worked-example bug the
// issue names is actually fixed (a chain cell's click-to-filter used to set
// a filter FilterBar.svelte had no control to show), and that the new
// controls (Source/Destination boxes, the swap button, the NAT and
// interface tokens, the country select) are wired end to end -- not just
// individually correct.
//
// Every address below is documentation space (RFC 5737 TEST-NET-3
// 203.0.113.0/24, RFC 1918 192.168.50.0/24) except the CIDR-containment
// fixtures, which deliberately use 198.51.100.0/24 (TEST-NET-2) instead of
// this suite's default 203.0.113.0/24 -- feedSyslog's generator (and
// several other scenarios) constantly emits traffic across
// 203.0.113.0-249, so a CIDR check there would be counting rows other
// scenarios are also contributing to. 198.51.100.0/24 is otherwise
// unclaimed here (live-router-lookup.mjs's wireguard peer only overrides
// *names* for 192.0.2.0/24 and 198.51.100.0/24, which doesn't matter for
// this check -- it's about the raw address, not the label).

import { session, feedRaw, feedSyslog, check, done, goTo } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const SRCNAT_RULE = 'mv438-srcnat'
const DSTNAT_RULE = 'mv438-dstnat'
const CHAIN_RULE = 'mv438-customchain'
const CIDR_IN_RULE = 'mv438-cidr-in'
const CIDR_OUT_RULE = 'mv438-cidr-out'

const fixtures = [
  [
    SRCNAT_RULE,
    // A srcnat (masquerade) line: the log's own tuple is the
    // pre-translation pair (192.168.50.20 -> 198.51.100.53); the NAT
    // annotation's other token, 203.0.113.230, is the translated source
    // RouterOS actually put on the wire -- see
    // internal/routeros/parser.go's parseNAT for why the parser has to
    // work this out by elimination rather than position.
    `firewall,info N|${SRCNAT_RULE}| srcnat: in:bridge1 out:ether1, proto UDP, ` +
      `192.168.50.20:51258->198.51.100.53:53, NAT (203.0.113.230:51258->198.51.100.53:53), len 73`,
  ],
  [
    DSTNAT_RULE,
    // A dstnat (port-forward) line: the log's tuple is the
    // pre-translation pair (a public client hitting the router's own
    // public address); 192.168.50.99 is the internal host the
    // connection actually reaches.
    `firewall,info A|${DSTNAT_RULE}| dstnat: in:ether1 out:bridge1, proto TCP (SYN), ` +
      `203.0.113.231:41000->203.0.113.10:8080, NAT 203.0.113.231:41000->(192.168.50.99:8080), len 60`,
  ],
  [
    CHAIN_RULE,
    // A custom (non-built-in) chain, and dstPort 443 -- doubles as the
    // Port text-search fixture (typing "https" should find it) so this
    // scenario doesn't need a sixth synthetic event just for that.
    `firewall,info A|${CHAIN_RULE}| ${CHAIN_RULE.replace('mv438-', '')}: in:bridge1 out:ether1, ` +
      `connection-state:new, proto TCP (SYN), 192.168.50.30:41111->198.51.100.53:443, len 60`,
  ],
  [
    CIDR_IN_RULE,
    `firewall,info A|${CIDR_IN_RULE}| forward: in:bridge1 out:ether1, connection-state:new, ` +
      `proto TCP (SYN), 198.51.100.242:5000->192.168.1.10:53, len 60`,
  ],
  [
    CIDR_OUT_RULE,
    `firewall,info A|${CIDR_OUT_RULE}| forward: in:bridge1 out:ether1, connection-state:new, ` +
      `proto TCP (SYN), 198.51.100.9:5000->192.168.1.10:53, len 60`,
  ],
]

const { page, consoleErrors } = await session()

async function api(path) {
  const res = await page.request.fetch(`${URL_BASE}${path}`)
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

// Waits for a rule's event to reach the server, re-feeding it periodically
// -- #465's pattern (see live-token-copy.mjs): under the full suite's
// load a single fed line can be dropped by a saturated ingest queue, and
// waiting on the rendered row alone turns that into an uncaught
// Playwright timeout with no RESULT line.
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

let allArrived = true
for (const [rule, line] of fixtures) {
  const arrived = await waitForArrival(rule, line)
  check(arrived, `the ${rule} test event reached the server`)
  allArrived = allArrived && arrived
}
if (!allArrived) {
  check(true, 'skipped the rest -- token interactions cannot be exercised on rows that never arrived')
  done()
}

// A page-load failure or a selector that never appears must still print a
// RESULT: line (see AGENTS.md's live-check trap notes) -- every wait below
// is either non-throwing (waitUntil, page.isVisible) or explicitly
// try/caught, never a bare page.waitForSelector/locator.waitFor that could
// throw uncaught mid-scenario.

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

/** waitForInputValue polls an input/select's value, returning what it last saw either way -- so a FAIL message shows the real mismatch, not a stale boolean. */
async function waitForInputValue(selector, expected, timeoutMs = 8000, intervalMs = 250) {
  const deadline = Date.now() + timeoutMs
  let last = ''
  while (Date.now() < deadline) {
    last = await page.inputValue(selector).catch(() => '')
    if (last === expected) return last
    await page.waitForTimeout(intervalMs)
  }
  return last
}

let ready = true
try {
  await page.waitForSelector('input.rule', { timeout: 15000 })
} catch {
  ready = false
}
check(ready, 'the live view loaded (filter bar rendered)')
if (!ready) {
  check(true, 'skipped -- the bar never rendered')
  done()
}

async function clearFilters() {
  if (await page.isVisible('.bar .clear').catch(() => false)) {
    await page.click('.bar .clear').catch(() => {})
    await page.waitForTimeout(300)
  }
}

function rowFor(rule) {
  return page.locator('.row', { hasText: rule }).first()
}

// --- Direction 2, the issue's own worked example: the chain filter was --
// --- already settable (EventRow's click handler) but had no bar control -
await clearFilters()
const chainRowVisible = await waitUntil(() => rowFor(CHAIN_RULE).isVisible())
check(!!chainRowVisible, `the ${CHAIN_RULE} row rendered`)
if (chainRowVisible) {
  const chainOptionValues = await page.$$eval('select[aria-label="Chain"] option', (opts) => opts.map((o) => o.value))
  check(
    chainOptionValues.includes('customchain'),
    `the Chain select includes the custom chain observed in the buffer (saw: ${chainOptionValues.join(', ')})`,
  )

  await rowFor(CHAIN_RULE).locator('.chain.cell-btn').click().catch(() => {})
  const chainSelectValue = await waitForInputValue('select[aria-label="Chain"]', 'customchain')
  check(
    chainSelectValue === 'customchain',
    `clicking the chain cell is reflected in the (previously nonexistent) Chain select -- the bidirectional-contract bug #438 names as its worked example (got "${chainSelectValue}")`,
  )

  await page.selectOption('select[aria-label="Chain"]', '').catch(() => {})
  const chainCleared = await waitForInputValue('select[aria-label="Chain"]', '')
  check(chainCleared === '', `the chain filter can be cleared from the select too (got "${chainCleared}")`)

  // --- Interface tokens: both in and out are independently click-to-filter
  await clearFilters()
  const ifaceButtons = rowFor(CHAIN_RULE).locator('.iface-btn')
  await ifaceButtons.nth(0).click().catch(() => {})
  const ifaceAfterIn = await waitForInputValue('input[aria-label="Interface"]', 'bridge1')
  check(ifaceAfterIn === 'bridge1', `clicking the "in" interface token filters to it (got "${ifaceAfterIn}")`)

  await ifaceButtons.nth(1).click().catch(() => {})
  const ifaceAfterOut = await waitForInputValue('input[aria-label="Interface"]', 'ether1')
  check(ifaceAfterOut === 'ether1', `clicking the "out" interface token filters to it (got "${ifaceAfterOut}")`)
} else {
  check(true, 'skipped the chain-select and interface-token checks -- their row never rendered')
}

// --- NAT parity: the translated address on each side is click-to-filter -
await clearFilters()
const srcnatRowVisible = await waitUntil(() => rowFor(SRCNAT_RULE).isVisible())
if (srcnatRowVisible) {
  await rowFor(SRCNAT_RULE).locator('.nat-value').click().catch(() => {})
  const srcAfterNatClick = await waitForInputValue('input[aria-label="Source — name, IP or CIDR"]', '203.0.113.230')
  check(
    srcAfterNatClick === '203.0.113.230',
    `clicking a srcnat row's NAT token sets the Source box to the translated source (got "${srcAfterNatClick}")`,
  )
} else {
  check(false, `the ${SRCNAT_RULE} row never rendered -- cannot exercise its NAT token`)
}

await clearFilters()
const dstnatRowVisible = await waitUntil(() => rowFor(DSTNAT_RULE).isVisible())
if (dstnatRowVisible) {
  await rowFor(DSTNAT_RULE).locator('.nat-value').click().catch(() => {})
  const dstAfterNatClick = await waitForInputValue('input[aria-label="Destination — name, IP or CIDR"]', '192.168.50.99')
  check(
    dstAfterNatClick === '192.168.50.99',
    `clicking a dstnat row's NAT token sets the Destination box to the translated destination (got "${dstAfterNatClick}")`,
  )
} else {
  check(false, `the ${DSTNAT_RULE} row never rendered -- cannot exercise its NAT token`)
}

// --- Source box: CIDR containment, typed directly -------------------------
await clearFilters()
await page.fill('input[aria-label="Source — name, IP or CIDR"]', '198.51.100.240/29')
const cidrInShown = await waitUntil(() => rowFor(CIDR_IN_RULE).isVisible())
check(!!cidrInShown, 'a source address inside the typed CIDR is shown')
await page.waitForTimeout(600) // let a (would-be) refetch/re-render settle before the negative check
check(!(await rowFor(CIDR_OUT_RULE).isVisible().catch(() => false)), 'a source address outside the typed CIDR is not shown')

// --- Port box: a well-known service name, not just a bare number ----------
await clearFilters()
await page.fill('input[aria-label="Port — number or service"]', 'https')
const httpsRowShown = await waitUntil(() => rowFor(CHAIN_RULE).isVisible())
check(!!httpsRowShown, 'typing a well-known service name matches its port (443/https)')
await page.waitForTimeout(600)
check(
  !(await rowFor(SRCNAT_RULE).isVisible().catch(() => false)),
  'a row on an unrelated port (53/DNS) is excluded by the same text search',
)

// --- The swap control: query, scope and country move together -------------
await clearFilters()
await page.fill('input[aria-label="Source — name, IP or CIDR"]', 'swap-src-value')
await page.fill('input[aria-label="Destination — name, IP or CIDR"]', 'swap-dst-value')
await page.selectOption('select[aria-label="Source scope"]', 'internal').catch(() => {})
await page.click('button[aria-label="Swap source and destination filters"]').catch(() => {})
const srcAfterSwap = await waitForInputValue('input[aria-label="Source — name, IP or CIDR"]', 'swap-dst-value')
check(srcAfterSwap === 'swap-dst-value', `swap moves the destination query into the source box (got "${srcAfterSwap}")`)
const dstAfterSwap = await page.inputValue('input[aria-label="Destination — name, IP or CIDR"]').catch(() => '')
check(dstAfterSwap === 'swap-src-value', `swap moves the source query into the destination box (got "${dstAfterSwap}")`)
const dstScopeAfterSwap = await page.inputValue('select[aria-label="Destination scope"]').catch(() => '')
check(dstScopeAfterSwap === 'internal', `swap carries the scope along with the query (got "${dstScopeAfterSwap}")`)

// --- Country: the Unknown bucket is reachable, not silently omitted -------
// This environment has no geoip feed configured, so every address here is
// genuinely undetermined -- the deterministic case the owner-ratified
// section calls for ("say so rather than silently omitting the row").
await clearFilters()
const srcCountryLabels = await page
  .$$eval('select[aria-label="Source country"] option', (opts) => opts.map((o) => o.textContent?.trim()))
  .catch(() => [])
const hasUnknownOption = srcCountryLabels.includes('Unknown')
check(
  hasUnknownOption,
  `the Source country select offers "Unknown" once an addressed event has no resolved country (options: ${srcCountryLabels.join(', ')})`,
)
if (hasUnknownOption) {
  await page.selectOption('select[aria-label="Source country"]', { label: 'Unknown' }).catch(() => {})
  const unknownFilterShowsRow = await waitUntil(() => rowFor(CHAIN_RULE).isVisible())
  check(
    !!unknownFilterShowsRow,
    'a row with a source address but an undetermined country still shows under the "Unknown" country filter',
  )
} else {
  check(true, 'skipped the Unknown-country selection check -- the option was never offered')
}

// --- Regression guard: an address whose only traffic is older than an ---
// --- unfiltered fetch's top-500 window must still be found once filtered -
//
// This is the exact bug that slipped through review once already:
// without forwarding a parseable address as `ip` to GET /api/events (see
// lib/api.ts's buildQuery doc comment), refetchWithFilters() falls back
// to "the 500 most recent events, address unfiltered" -- silently
// starving out a selective address whenever its only traffic is older
// than that, even though internal/store/query.go's own scan would have
// found it easily. A flood of fresher noise pushes the target below the
// unfiltered top-500 boundary server-side; a genuine page reload (not
// just clearing the bar) then reproduces the exact vulnerable starting
// point -- App.svelte's mount effect does the same unfiltered
// limit:500 fetch a first-ever load would, which is what
// refetchWithFilters replaces the instant a filter is typed.
const OLD_TRAFFIC_RULE = 'mv438-oldtraffic'
const OLD_TRAFFIC_IP = '198.51.100.222'
const NOISE_RULE = 'mv438-noiseflood'

const oldTrafficArrived = await waitForArrival(
  OLD_TRAFFIC_RULE,
  `firewall,info A|${OLD_TRAFFIC_RULE}| forward: in:bridge1 out:ether1, connection-state:new, ` +
    `proto TCP (SYN), ${OLD_TRAFFIC_IP}:5000->192.168.1.10:443, len 60`,
)
check(oldTrafficArrived, `the ${OLD_TRAFFIC_RULE} test event (the one the flood below must bury) reached the server`)

if (oldTrafficArrived) {
  feedSyslog(600, NOISE_RULE)

  // Polls for actual arrival, not just the socket write feedSyslog's
  // execFileSync call returning -- same reasoning as waitForArrival above.
  const noiseLanded = await waitUntil(
    async () => {
      const { body } = await api(`/api/events?rule=${NOISE_RULE}&limit=500`)
      return (body?.events?.length ?? 0) >= 500
    },
    40000,
    1000,
  )
  check(
    !!noiseLanded,
    `at least 500 events of fresher noise reached the server (enough to push ${OLD_TRAFFIC_RULE} below an unfiltered top-500 fetch)`,
  )

  let reloaded = true
  try {
    await page.goto(URL_BASE, { waitUntil: 'networkidle' })
    // A fresh load lands on the fall (#616's landing default), not
    // Stream -- this check is specifically about App.svelte's mount
    // fetch on the live view, so navigate there explicitly rather than
    // assume what a fresh load opens on.
    await page.waitForSelector('#main-content', { timeout: 15000 })
    await goTo(page, 'Stream')
    await page.waitForSelector('input.rule', { timeout: 15000 })
  } catch {
    reloaded = false
  }
  check(reloaded, 'the page reloaded back to a fresh, unfiltered live view')

  if (reloaded) {
    const rowsAfterReload = await waitUntil(() => page.locator('.row').count().then((n) => n > 0))
    check(!!rowsAfterReload, 'the fresh load rendered some rows')

    // Diagnostic, not a hard assertion -- on an already-busy shared
    // instance (the full suite) other scenarios' own traffic could
    // coincidentally also bury the target, or an unusually quiet run
    // could leave it just inside reach. Either way what actually matters,
    // asserted below, is that filtering finds it regardless.
    const oldRowVisibleUnfiltered = await rowFor(OLD_TRAFFIC_RULE).isVisible().catch(() => false)
    console.log(`DIAGNOSTIC: ${OLD_TRAFFIC_RULE} visible in the fresh unfiltered load: ${oldRowVisibleUnfiltered}`)

    await page.fill('input[aria-label="Source — name, IP or CIDR"]', OLD_TRAFFIC_IP)
    const foundAfterFilter = await waitUntil(() => rowFor(OLD_TRAFFIC_RULE).isVisible(), 15000)
    check(
      !!foundAfterFilter,
      `filtering to an address whose only traffic is older than the unfiltered top-500 window still finds it -- server-side narrowing via ip= (unfiltered-visible was ${oldRowVisibleUnfiltered})`,
    )
  } else {
    check(false, 'skipped the server-side-narrowing regression check -- the reload never completed')
  }
} else {
  check(false, `skipped the server-side-narrowing regression check -- ${OLD_TRAFFIC_RULE} never arrived`)
}

await clearFilters()

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
