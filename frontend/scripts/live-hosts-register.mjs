// SPDX-License-Identifier: AGPL-3.0-only
//
// The host presence register (#1016) against a real running mikroview:
// events fed through the real syslog listener land in the register, a
// mark survives a round trip through the API, and a further event clears
// a dismissal by itself -- the issue's own "if it reappears in the feed
// it comes back by itself".
//
// API-driven rather than UI-driven, deliberately: this slice is the data
// model only. Nothing on the map reads the register yet -- that lands
// with the drawing round -- so there is no rendering here to assert, and
// a scenario clicking at a surface that does not exist would be a
// scenario asserting nothing.
//
// Self-provisioned: it feeds its own events on addresses no other
// scenario uses (10.77.0.0/24), so it behaves the same standalone as it
// does mid-suite, where a shared instance already carries thousands of
// hosts from everything that ran before it.

import { session, check, done, feedRaw } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const IFACE = 'bridge-hostreg'
const QUIET_IP = '10.77.0.11'
const BUSY_IP = '10.77.0.12'
const QUIET_KEY = `${IFACE}|${QUIET_IP}`
const BUSY_KEY = `${IFACE}|${BUSY_IP}`

// One inbound event: the shape zones.svelte.ts reads a host off, and so
// the shape internal/hosts.Registers accepts -- traffic *arriving* on an
// internal interface from a private source.
function inboundLine(srcIp, port) {
  return (
    `firewall,info A|hostreg| forward: in:${IFACE} out:ether1, connection-state:new, ` +
    `proto TCP (SYN), ${srcIp}:${port}->203.0.113.9:443, len 60`
  )
}

const { page, consoleErrors } = await session()

const jsonHeaders = { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' }

async function readHosts() {
  const res = await page.request.get(`${URL_BASE}/api/hosts`)
  if (!res.ok()) return null
  return (await res.json()).hosts ?? []
}

// waitForHost polls until the register has caught up. Ingest is
// asynchronous by design (the syslog listener hands off to a single
// store-writer goroutine), so the alternative to polling is a fixed
// sleep long enough to be slow and short enough to be flaky.
async function waitForHost(key, predicate = () => true) {
  for (let i = 0; i < 40; i++) {
    const hosts = await readHosts()
    const found = hosts?.find((h) => h.key === key)
    if (found && predicate(found)) return found
    await new Promise((r) => setTimeout(r, 250))
  }
  return null
}

// --- Two hosts arrive on the feed ---------------------------------------
feedRaw(inboundLine(QUIET_IP, 51001))
feedRaw(inboundLine(BUSY_IP, 51002))

const quiet = await waitForHost(QUIET_KEY)
check(!!quiet, `the register holds the first host the feed showed (${QUIET_KEY})`)
check(
  quiet?.iface === IFACE && quiet?.ip === QUIET_IP,
  `the entry carries the boundary and address split out of its key (${quiet?.iface}, ${quiet?.ip})`,
)
check(!!quiet?.firstSeen && !!quiet?.lastSeen, 'the entry carries first- and last-seen times -- what lets the map tell how long a host has been quiet')
check(!quiet?.mark, 'a host nobody has said anything about carries no mark')

const busy = await waitForHost(BUSY_KEY)
check(!!busy, `the register holds the second host too (${BUSY_KEY})`)

// The far side of the boundary is not a host: a public source is what
// the internal host was talking *to*.
const hostsNow = await readHosts()
check(
  !hostsNow?.some((h) => h.ip === '203.0.113.9'),
  'the public destination never appears as a host -- the register mirrors the map rule, not a broader one',
)

// --- A mark survives a round trip ---------------------------------------
const markRes = await page.request.put(
  `${URL_BASE}/api/hosts/${encodeURIComponent(QUIET_KEY)}/mark`,
  { headers: jsonHeaders, data: { kind: 'intended', reason: 'the lab NAS, powered on twice a month' } },
)
check(markRes.status() === 200, `marking a host intended answers 200 (${markRes.status()})`)

const marked = await waitForHost(QUIET_KEY, (h) => !!h.mark)
check(marked?.mark?.kind === 'intended', `the mark reads back off GET /api/hosts (${marked?.mark?.kind})`)
check(
  marked?.mark?.reason === 'the lab NAS, powered on twice a month',
  `the reason reads back with it (${JSON.stringify(marked?.mark?.reason)})`,
)
check(
  !!marked?.mark?.by && !!marked?.mark?.at,
  `who marked it and when are set server-side (${marked?.mark?.by})`,
)

// An intended mark is a statement that stays said: the host speaking
// again must not withdraw it.
feedRaw(inboundLine(QUIET_IP, 51003))
const stillIntended = await waitForHost(QUIET_KEY, (h) => h.events > (marked?.events ?? 0))
check(
  stillIntended?.mark?.kind === 'intended',
  `an intended mark survives the host reappearing (${stillIntended?.mark?.kind})`,
)

// --- A dismissal is cleared by the host coming back ---------------------
const dismissRes = await page.request.put(
  `${URL_BASE}/api/hosts/${encodeURIComponent(BUSY_KEY)}/mark`,
  { headers: jsonHeaders, data: { kind: 'dismissed', reason: '' } },
)
check(dismissRes.status() === 200, `dismissing a host answers 200 with no reason (${dismissRes.status()})`)

const dismissed = await waitForHost(BUSY_KEY, (h) => h.mark?.kind === 'dismissed')
check(!!dismissed, 'the dismissal reads back')

feedRaw(inboundLine(BUSY_IP, 51004))
const back = await waitForHost(BUSY_KEY, (h) => !h.mark)
check(
  !!back,
  'a further event clears the dismissal by itself -- "if it reappears in the feed it comes back by itself"',
)

// --- Taking a statement back ---------------------------------------------
const unmarkRes = await page.request.delete(
  `${URL_BASE}/api/hosts/${encodeURIComponent(QUIET_KEY)}/mark`,
  { headers: { 'X-Requested-With': 'mikroview' } },
)
check(unmarkRes.status() === 204, `removing a mark answers 204 (${unmarkRes.status()})`)

const unmarked = await waitForHost(QUIET_KEY, (h) => !h.mark)
check(!!unmarked, 'the host is back to plain presence with the mark gone')

const secondUnmark = await page.request.delete(
  `${URL_BASE}/api/hosts/${encodeURIComponent(QUIET_KEY)}/mark`,
  { headers: { 'X-Requested-With': 'mikroview' } },
)
check(
  secondUnmark.status() === 404,
  `removing a mark that is not there answers 404 rather than pretending (${secondUnmark.status()})`,
)

// --- Refusals hold at the wire ------------------------------------------
const badKind = await page.request.put(`${URL_BASE}/api/hosts/${encodeURIComponent(QUIET_KEY)}/mark`, {
  headers: jsonHeaders,
  data: { kind: 'hidden', reason: 'x' },
})
check(badKind.status() === 400, `an unknown mark kind is refused (${badKind.status()})`)

const noReason = await page.request.put(`${URL_BASE}/api/hosts/${encodeURIComponent(QUIET_KEY)}/mark`, {
  headers: jsonHeaders,
  data: { kind: 'intended', reason: '' },
})
check(noReason.status() === 400, `an intended mark with no reason is refused -- the reason is the mark (${noReason.status()})`)

const unknownHost = await page.request.put(
  `${URL_BASE}/api/hosts/${encodeURIComponent(`${IFACE}|10.77.0.99`)}/mark`,
  { headers: jsonHeaders, data: { kind: 'dismissed', reason: '' } },
)
check(
  unknownHost.status() === 404,
  `marking a host the feed has never shown is refused -- the register records presence, it never invents it (${unknownHost.status()})`,
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
