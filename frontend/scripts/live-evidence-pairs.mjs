// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #654: flags.Evidence keeps what the triggering events actually
// knew, instead of throwing most of it away.
//
// Two claims, both real-data-or-nothing:
//
//  - Evidence.Pairs is the (host, port) combinations *actually seen
//    together*, not Ports x Hosts. The commit's own example is a single
//    external source hitting several critical ports across several
//    internal hosts -- a case where the cross-product would invent
//    connections the source never made (up to 1000 of them, per the
//    commit message). This scenario feeds exactly five such connections
//    and asserts the recorded pairs are exactly those five, not the
//    3x3=9 a cross-product would produce.
//  - port_scan (ports, no destination that means anything) never carries
//    Pairs -- a pair is meaningless where only one side of it was ever
//    recorded. dest_spread's internal_recon is the opposite of what this
//    scenario originally asserted here: #641 (ee80537, after this
//    scenario's #654) deliberately added a port to each of its recorded
//    destinations, precisely so an "expected" verdict or a drafted
//    watcher can name the exact (host, port) pairs a source reached,
//    not just the bare host list. Evidence.SrcMAC is carried only for a
//    local source (matchlog.Identity is MAC-preferred so a device
//    survives a DHCP lease change) and never for an external one, even
//    when the event itself hands over a MAC.
//
// Every source IP below is unique in this directory (checked against
// every other scenario's IPs -- #590 is about exactly this collision).
// Flags raised here are left in place afterward, same as every other
// port_scan/critical_port scenario: nothing else in the suite reads
// these targets, so there is nothing to clean up.

import { session, feedRaw, check, done, goTo } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

// --- critical_port: real pairs, not a cross-product -----------------------

const CP_SRC = '198.51.100.130' // external -- critical_port requires it
const CP_HOST_A = '192.168.1.30'
const CP_HOST_B = '192.168.1.31'
const CP_HOST_C = '192.168.1.32'
// All three are in DefaultDetectorDefaults' CriticalPorts list.
const CP_PORT_SSH = 22
const CP_PORT_RDP = 3389
const CP_PORT_SMB = 445

// --- port_scan: MAC recorded for a local source, ports but no pairs -------

const PS_LOCAL_SRC = '192.168.1.40'
const PS_LOCAL_MAC = 'aa:bb:cc:dd:ee:40'

// --- port_scan again: MAC withheld for an external source, even though
// this event genuinely carries one on the wire -- proves recordEvidence's
// locality gate, not just "nobody sent a MAC".

const PS_EXT_SRC = '198.51.100.131'
const PS_EXT_MAC = 'aa:bb:cc:dd:ee:41'

// --- internal_recon (dest_spread's internal half): hosts, no pairs --------

const IR_SRC = '192.168.1.41'
const IR_DEST_PREFIX = '192.168.2.'

function line({ src, sport, dst, dport, mac, label = 'ev-pairs', action = 'D' }) {
  const state = mac ? `connection-state:new src-mac ${mac}` : 'connection-state:new'
  return `${action}|${label}| forward: in:ether1 out:bridge1, ${state}, proto TCP (SYN), ${src}:${sport}->${dst}:${dport}, len 60`
}

// The exact five (host, port) connections critical_port's threshold (5,
// the default) is fed with -- named up front so both the feed loop and
// the assertions below read off the same list rather than two that have
// to be kept in sync by hand.
const CP_CONNECTIONS = [
  [CP_HOST_A, CP_PORT_SSH],
  [CP_HOST_A, CP_PORT_RDP],
  [CP_HOST_B, CP_PORT_SSH],
  [CP_HOST_B, CP_PORT_SMB],
  [CP_HOST_C, CP_PORT_RDP],
]
CP_CONNECTIONS.forEach(([host, port], i) => {
  feedRaw(line({ src: CP_SRC, sport: 40000 + i, dst: host, dport: port }))
})

// 20 distinct destination ports each -- comfortably over port_scan's
// default 15-port threshold, same margin live-browser.mjs's own
// feedPortScan uses elsewhere in this directory.
for (let i = 0; i < 20; i++) {
  feedRaw(line({ src: PS_LOCAL_SRC, sport: 50000 + i, dst: '192.168.1.10', dport: 20000 + i, mac: PS_LOCAL_MAC }))
}
for (let i = 0; i < 20; i++) {
  feedRaw(line({ src: PS_EXT_SRC, sport: 51000 + i, dst: '192.168.1.10', dport: 21000 + i, mac: PS_EXT_MAC }))
}

// 12 distinct internal destinations -- over internal_recon's default
// threshold of 10.
for (let i = 1; i <= 12; i++) {
  feedRaw(line({ src: IR_SRC, sport: 52000 + i, dst: `${IR_DEST_PREFIX}${i}`, dport: 80 }))
}

const { page } = await session()

async function flagsList() {
  const res = await page.request.get(`${URL_BASE}/api/flags`)
  const body = await res.json()
  return body.flags ?? []
}

async function findFlag(type, target) {
  return (await flagsList()).find((f) => f.type === type && f.target === target)
}

// Polls the server for a flag of a specific type + target -- waitForFlag
// (live-browser.mjs) only keys on target, and every detector here uses a
// different source IP as its target, but being explicit about the type
// too is what #354's own reasoning argues for: a locator/predicate
// timeout that also names which detector never fired is more useful than
// one that can't.
async function waitForTypedFlag(type, target, timeoutMs = 20000) {
  const deadline = Date.now() + timeoutMs
  let last
  while (Date.now() < deadline) {
    last = await findFlag(type, target)
    if (last && !last.cleared) return { ok: true, flag: last }
    await page.waitForTimeout(400)
  }
  return { ok: false, flag: last }
}

function pairKey(p) {
  return `${p.host}:${p.port}`
}

// --- critical_port: assertions 1 and 2 -------------------------------------

const cp = await waitForTypedFlag('critical_port', CP_SRC)
check(cp.ok, `critical_port raised for ${CP_SRC} (${cp.ok ? 'ok' : JSON.stringify(cp.flag)})`)

if (cp.ok) {
  const gotPairs = (cp.flag.evidence?.pairs ?? []).map(pairKey).sort()
  const wantPairs = CP_CONNECTIONS.map(([host, port]) => `${host}:${port}`).sort()
  check(
    gotPairs.length === wantPairs.length && gotPairs.every((p, i) => p === wantPairs[i]),
    `critical_port's evidence.pairs is exactly the 5 connections fed, not the 3 hosts x 3 ports = 9 a cross-product would invent (got: ${JSON.stringify(gotPairs)}, want: ${JSON.stringify(wantPairs)})`,
  )
  // The cross-product bug this issue exists to end, stated explicitly:
  // 9 distinct combinations were never fed, only 5 were.
  check(gotPairs.length < 9, `fewer pairs (${gotPairs.length}) than the full 3 hosts x 3 ports cross-product (9) would produce`)

  const gotPorts = (cp.flag.evidence?.ports ?? []).slice().sort((a, b) => a - b)
  check(
    JSON.stringify(gotPorts) === JSON.stringify([22, 445, 3389]),
    `critical_port's evidence.ports is the 3 distinct ports actually touched (got: ${JSON.stringify(gotPorts)})`,
  )
}

// --- port_scan / internal_recon: assertion 3, both halves ------------------

const ps = await waitForTypedFlag('port_scan', PS_LOCAL_SRC)
check(ps.ok, `port_scan raised for ${PS_LOCAL_SRC} (${ps.ok ? 'ok' : JSON.stringify(ps.flag)})`)
if (ps.ok) {
  check((ps.flag.evidence?.ports ?? []).length > 0, 'port_scan evidence carries ports')
  check(
    !ps.flag.evidence?.pairs || ps.flag.evidence.pairs.length === 0,
    'port_scan evidence carries no pairs -- a port alone names no meaningful destination to pair it with',
  )
}

const ir = await waitForTypedFlag('internal_recon', IR_SRC)
check(ir.ok, `internal_recon (dest_spread) raised for ${IR_SRC} (${ir.ok ? 'ok' : JSON.stringify(ir.flag)})`)
if (ir.ok) {
  check((ir.flag.evidence?.hosts ?? []).length > 0, 'internal_recon evidence carries destinations')
  // #641 (ee80537): dest_spread records the port alongside each
  // destination it counts, so a watcher drafted from this flag can name
  // exact (host, port) pairs rather than only the bare host list. Every
  // fixture line above reaches port 80, so each pair's port must be 80
  // too, and the pair hosts must be exactly the recorded destinations.
  const irPairs = ir.flag.evidence?.pairs ?? []
  check(irPairs.length > 0, `internal_recon evidence carries pairs, one per destination it recorded (#641) (got: ${JSON.stringify(irPairs)})`)
  check(
    irPairs.every((p) => p.port === 80),
    `every internal_recon pair carries the port that destination was actually reached on (got: ${JSON.stringify(irPairs)})`,
  )
  const irPairHosts = irPairs.map((p) => p.host).sort()
  const irHosts = (ir.flag.evidence?.hosts ?? []).slice().sort()
  check(
    JSON.stringify(irPairHosts) === JSON.stringify(irHosts),
    `internal_recon's pairs name exactly the same hosts as its Hosts list, no more and no fewer (pairs: ${JSON.stringify(irPairHosts)}, hosts: ${JSON.stringify(irHosts)})`,
  )
}

// --- assertion 4: MAC recorded for a local source ---------------------------

if (ps.ok) {
  check(
    ps.flag.evidence?.srcMac === PS_LOCAL_MAC,
    `port_scan's evidence.srcMac is the local source's own MAC (got: ${JSON.stringify(ps.flag.evidence?.srcMac)})`,
  )
}

// --- assertion 5: MAC withheld for an external source, even though this
// event's own line carried one -------------------------------------------

const psExt = await waitForTypedFlag('port_scan', PS_EXT_SRC)
check(psExt.ok, `port_scan raised for external source ${PS_EXT_SRC} (${psExt.ok ? 'ok' : JSON.stringify(psExt.flag)})`)
if (psExt.ok) {
  check(
    !psExt.flag.evidence?.srcMac,
    `an external source's MAC is never recorded, even though this source's own events carried ${PS_EXT_MAC} on the wire (got: ${JSON.stringify(psExt.flag.evidence?.srcMac)})`,
  )
}

// --- assertion 6: the Flags panel groups pairs by host, on real data ------

if (cp.ok) {
  await goTo(page, 'Flags')
  await page.waitForSelector('.card .type', { timeout: 15000 })
  const card = page.locator('section[aria-labelledby="active-heading"] .card', { hasText: CP_SRC })
  await card.waitFor({ timeout: 15000 })
  await card.locator('.openc').click()
  await card.locator('.ev-pair-row').first().waitFor({ timeout: 10000 })

  const rows = card.locator('.ev-pair-row')
  const rowCount = await rows.count()
  // 3 rows -- one per distinct host -- not 5 (one per pair) and not 9
  // (the cross-product): this is what "grouped by host" means on screen.
  check(rowCount === 3, `the drawer shows one row per host (3), not one per pair (5) or per cross-product combination (9) -- got ${rowCount}`)

  const byHost = {}
  for (let i = 0; i < rowCount; i++) {
    const row = rows.nth(i)
    const host = (await row.locator('.ev-label').textContent())?.trim()
    const ports = (await row.locator('.ev-value').textContent())?.trim()
    byHost[host] = ports
  }
  check(
    byHost[CP_HOST_A] === '22, 3389' && byHost[CP_HOST_B] === '22, 445' && byHost[CP_HOST_C] === '3389',
    `each host row lists exactly the ports actually seen with it, sorted (got: ${JSON.stringify(byHost)})`,
  )

  check(
    (await card.locator('.ev-label:has-text("Source MAC")').count()) === 0,
    'critical_port -- an external-source-only detector -- never shows a Source MAC row at all',
  )
}

done()
