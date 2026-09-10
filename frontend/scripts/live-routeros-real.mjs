// SPDX-License-Identifier: AGPL-3.0-only
//
// #273 slice 2, the RouterOS half: every #243 feature driven by data a
// real router actually produced, rather than by lines this repository
// wrote to look like a router's.
//
// Every other scenario feeds synthetic syslog. That is the right trade
// for them -- they are about mikroview's own behaviour, and a booted VM
// per run would make the default loop slow and fragile. But it means the
// bytes under test were written by the same people who wrote the parser,
// so the two agree with each other by construction. #243's whole
// identity model rests on what RouterOS puts in a log line, and this is
// where that gets checked against RouterOS.
//
// It found the MAC-case defect on its first run: a real RouterOS 7.23.3
// emits src-mac uppercase (52:55:0A:00:02:02) while every synthetic line
// in this repository, the docs, and the watchlist form's own free-text
// field use the conventional lowercase. Matching compared them byte for
// byte, so an entry typed the ordinary way silently never fired. See
// matchlog.Identity.identityKey.
//
// Excluded from the default loop by scripts/run-scenarios.sh, because it
// needs a booted CHR: `make live-routeros-container`.

import { execFileSync } from 'child_process'
import path from 'path'
import { fileURLToPath } from 'url'
import { session, check, done } from './live-browser.mjs'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const URL_BASE = process.env.MV_URL

// router drives the CHR through the same script the Makefile target
// uses, so there is one description of how to talk to the router rather
// than a second one embedded here.
function router(...args) {
  return execFileSync(path.join(REPO, 'scripts/live-routeros.sh'), args, {
    cwd: REPO,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'inherit'],
  })
}

const { page } = await session()

async function api(method, path_, body) {
  const res = await page.request.fetch(`${URL_BASE}${path_}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

async function events(query = '') {
  return (await api('GET', `/api/events?limit=500${query}`)).body?.events ?? []
}

// waitFor polls until predicate finds something, rather than sleeping a
// fixed amount. Ingest here crosses a VM boundary, a syslog TLS
// connection and a detector worker, so any constant would be either
// flaky or slow.
async function waitFor(fn, timeoutMs = 30000) {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const got = await fn()
    if (got !== undefined && got !== null && !(Array.isArray(got) && got.length === 0)) return got
    if (Date.now() > deadline) return null
    await new Promise((r) => setTimeout(r, 500))
  }
}

// summariseEvents prints, at a named checkpoint, the event population
// mikroview actually holds: total count and a breakdown by chain and
// destination port. #614 needed exactly this evidence -- traffic on
// the entry's own port had arrived, but no stored event ever showed
// that port as its dstPort, which is what a TCP framing defect looked
// like from the outside -- and finding it took a first, separately
// instrumented run. Left in place, slimmed to counts only, so a future
// failure of these checks starts from that same evidence rather than
// from zero.
async function summariseEvents(label) {
  const all = await events()
  const byChain = {}
  const byDstPort = {}
  for (const e of all) {
    const chainKey = e.chain || '(none)'
    const portKey = e.dstPort === undefined || e.dstPort === null ? '(none)' : String(e.dstPort)
    byChain[chainKey] = (byChain[chainKey] ?? 0) + 1
    byDstPort[portKey] = (byDstPort[portKey] ?? 0) + 1
  }
  console.log(
    `#614 checkpoint -- ${label}: total=${all.length} byChain=${JSON.stringify(byChain)} byDstPort=${JSON.stringify(byDstPort)} at ${new Date().toISOString()}`,
  )
}

// --- Real firewall traffic, in every chain the fixture can reach ---------

router('traffic', '6')

const arrived = await waitFor(async () => {
  const all = await events()
  return all.length >= 3 ? all : null
})
check(!!arrived, `real router syslog reached mikroview (${arrived?.length ?? 0} events)`)

// Every line here came off the router, so this is the parser's real
// input rather than its test fixtures.
//
// Checks for the scenario's own log-prefix marker rather than the
// literal 'firewall,info ' topic text: setup() (#614) now asks for
// remote-log-format=syslog, so that topic/severity travels in the
// numeric PRI RouterOS puts at the front of the line, not as text in
// the body -- ParseEnvelope strips the PRI off before Raw is stored.
// 'A|' is what every rule this scenario creates actually logs with, so
// this still proves the same thing the old check did (the body is
// RouterOS's own, not something rebuilt from it). Do not "fix" this
// back to 'firewall,info ' -- that prefix no longer exists on the wire.
check(
  arrived?.every((e) => e.raw?.startsWith('A|')),
  'every event carries the raw line RouterOS itself sent',
)

const forward = arrived?.find((e) => e.chain === 'forward')
check(!!forward, 'the forward chain produced events')
check(
  !!forward?.srcMac && /^[0-9A-Fa-f:]{17}$/.test(forward.srcMac),
  `the forward chain carries src-mac, as internal/routeros/parser.go relies on (got ${forward?.srcMac})`,
)
// A real dst-nat annotation, whose layout RouterOS does not document --
// the parser diffs it against the known address pair rather than
// assuming a fixed shape, and this is that shape as actually emitted.
check(
  !!forward?.natRaw && forward.natIp === '10.0.2.15' && forward.natPort === 15903,
  `the dst-nat annotation parsed into the pre-translation address (got ${JSON.stringify({ natIp: forward?.natIp, natPort: forward?.natPort })})`,
)

const input = arrived?.find((e) => e.chain === 'input')
check(!!input, 'the input chain produced events')
// Worth asserting rather than assuming: #243 and parser.go both say the
// input chain often omits src-mac and only forward reliably carries it.
// On this firmware input carries it too. That is a weaker claim holding
// than expected, so it is recorded here rather than left as folklore --
// nothing depends on input *lacking* it.
check(
  !!input?.srcMac,
  `this firmware's input chain also carries src-mac (got ${input?.srcMac}) -- weaker than parser.go assumes, so nothing breaks`,
)

// ICMP: named in parser.go as a shape with no ports at all.
const icmp = await waitFor(async () => (await events()).find((e) => e.protocol === 'ICMP'))
check(!!icmp, 'the output chain produced real ICMP events')
// !!icmp is part of the condition, not just the message. Written as
// `icmp?.srcPort === undefined`, a missing ICMP event satisfies it --
// the check passes precisely when there was nothing to check, which is
// worse than no assertion at all.
check(
  !!icmp && icmp.srcPort === undefined && icmp.dstPort === undefined,
  `a real ICMP line parses with no ports rather than zeroes or junk (got ${JSON.stringify({ srcPort: icmp?.srcPort, dstPort: icmp?.dstPort })})`,
)

// --- #243: a watchlist entry keyed on the router's real MAC -------------

const realMac = forward?.srcMac ?? ''
// Typed the way an operator would, which is the way every example in
// this repository writes a MAC -- and not the way this router reports
// one. That difference is the whole point of this assertion.
const typedMac = realMac.toLowerCase()
check(typedMac !== realMac, `the router reports MACs in a different case to the conventional form (${realMac} vs ${typedMac})`)

// #407 folded the entry surface into /api/definitions: an entry is an
// expectation definition, created with its matching data in an
// `expectation` block and read back under the same key. The routes it
// replaced are gone outright, so nothing here can fall back to them.
const watched = await api('POST', '/api/definitions', {
  name: 'real router input',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { ports: [15902], source: { mac: typedMac } },
})
check(watched.status === 201, `an entry scoped to the router's real device is created (${watched.status})`)
await summariseEvents('after the watched entry was created')

router('traffic', '3')

const matches = await waitFor(async () => {
  const got = await api('GET', `/api/matches?mac=${encodeURIComponent(typedMac)}`)
  return got.body?.matches?.length ? got.body.matches : null
})
await summariseEvents('before: entry matches the device')
check(
  !!matches,
  'an entry whose MAC was typed in the conventional case matches the same device as the router reports it',
)
check(
  matches?.[0]?.tuple?.port === 15902,
  `the recorded match is the real traffic (got ${JSON.stringify(matches?.[0]?.tuple)})`,
)
// The embedded event is the evidence #243 promises: the whole event, not
// a summary rebuilt from it.
check(
  matches?.[0]?.event?.raw?.includes('src-mac'),
  'the match embeds the full original event, raw line included',
)

// Collapsing (#243 section 4): the same tuple repeated is one record
// with a count, not a record per packet.
router('traffic', '4')
const collapsed = await waitFor(async () => {
  const got = await api('GET', `/api/matches?mac=${encodeURIComponent(typedMac)}`)
  const rec = got.body?.matches?.find((m) => m.tuple?.port === 15902)
  return rec && rec.count > 1 ? rec : null
})
await summariseEvents('before: repeated traffic collapsed into one record')
check(!!collapsed, `repeated identical real traffic collapsed into one record (count ${collapsed?.count})`)
check(
  (await api('GET', `/api/matches?mac=${encodeURIComponent(typedMac)}`)).body.matches.filter(
    (m) => m.tuple?.port === 15902,
  ).length === 1,
  'collapsing produced exactly one record for that tuple, not one per burst',
)

// --- #243: inverted entry, observe then promote, on real traffic --------

const inverted = await api('POST', '/api/definitions', {
  name: 'real router egress',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { invert: true, source: { mac: typedMac } },
})
check(
  inverted.status === 201 && inverted.body?.expectation?.observing === true,
  'an inverted entry on the real device starts observing',
)
await summariseEvents('after the inverted entry was created')

router('traffic', '3')

const observed = await waitFor(async () => {
  const got = await api('GET', '/api/definitions')
  const d = (got.body?.definitions ?? []).find((x) => x.id === inverted.body?.id)
  return d?.expectation?.observed?.length ? d.expectation.observed[0] : null
})
await summariseEvents('before: real traffic became an Observed candidate')
check(!!observed, 'real traffic from the device became an Observed candidate while observing')

const promoted = await api('POST', `/api/definitions/${inverted.body?.id}/promote`, {
  destinations: [{ destIp: observed?.destIp, port: observed?.port }],
})
check(
  promoted.status === 200 && (promoted.body?.expectation?.permitted ?? []).length === 1,
  `the real destination promoted into Permitted (${promoted.status})`,
)

// --- #243 slice 5: suggestions, from tables the router itself pushed ----

const device = (await api('GET', '/api/devices')).body?.devices?.[0]?.id
check(!!device, `the instance reports the device the router's events arrive from (${device})`)

const token = await api('POST', '/api/tokens', {
  name: 'live-routeros-real',
  kind: 'ingest',
  device,
})
check(token.status === 201, `an ingest token scoped to that device is issued (${token.status})`)

// The router does the pushing, over TLS it verifies against the CA it
// imported -- so this exercises the documented setup end to end, not a
// curl standing in for it.
router('push', URL_BASE, token.body.value)

const rules = await waitFor(async () => {
  const got = await api('GET', `/api/routeros/${device}/rules`)
  return got.body?.available ? got.body : null
})
check(!!rules, "the router's own filter table arrived and is marked available")
check(
  (rules?.rules ?? []).some((r) => r.logPrefix === 'A|lan-wan|'),
  'the pushed table carries the real tagged rules, renamed to mikroview\'s schema',
)

// The lease the router genuinely learned -- see live-routeros.sh setup
// for why the fixture makes the router a DHCP client of itself. A
// hand-typed static lease has no host-name at all (RouterOS refuses to
// let you assert one), and a lease with no host-name suggests nothing.
const suggestions = await api('POST', '/api/suggestions/reset', { confirm: true })
check(suggestions.status === 200, `suggestions regenerate from the pushed tables (${suggestions.status})`)
const fromLease = (suggestions.body?.candidates ?? []).find((c) => c.name === 'CHR')
check(
  !!fromLease,
  `a real learned DHCP lease produced a device suggestion (got ${JSON.stringify((suggestions.body?.candidates ?? []).map((c) => c.name))})`,
)
check(
  !!fromLease?.source?.mac,
  `the suggestion carries the device's real MAC (${fromLease?.source?.mac})`,
)

done()
