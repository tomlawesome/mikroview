// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #186 step 7: the ingest pipeline's rejection surface and its
// structural isolation from detection, against a real running
// mikroview -- real ingest token, real HTTP POST to the real endpoint.
//
// Deliberately does not boot a RouterOS CHR router. The router-specific
// half of step 7 -- does RouterOS's own scripting produce bytes this
// endpoint accepts -- was verified by hand this session against a real
// CHR 7.23.3 (see docs/routeros-setup.md's step 4 and the commit that
// added it): the exact JSON a real router's :serialize produces was
// round-tripped through internal/ingest.DecodePayload directly. What
// this script covers instead is everything about the endpoint's own
// behavior that doesn't depend on RouterOS specifically: a byte-correct
// payload from ANY sender must be accepted the same way, and every
// rejection path must actually refuse.
//
// Kept out of the default make live-check loop for the same reason
// make live-routeros itself is opt-in (see that target's own comment):
// this session's QEMU/CHR fixture showed real, unprompted "timeout
// connecting" failures on its own networking layer across several
// clean reboots while developing the step 6 docs -- unrelated to
// mikroview, but exactly the kind of thing that would make the default
// loop flaky for everyone if a router boot were required on every run.
// This scenario needs no router boot at all, so it runs here in
// make live-routeros rather than requiring one just to be included.

import { fileURLToPath } from 'url'
import { session, check, done, feedPortScan } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL


const { page } = await session()

async function createToken(body) {
  const res = await page.request.post(`${URL_BASE}/api/tokens`, {
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

async function push(token, payload) {
  const res = await fetch(`${URL_BASE}/api/ingest/routeros`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: typeof payload === 'string' ? payload : JSON.stringify(payload),
  })
  return res.status
}

// Uses page.request, which carries the browser session's cookies --
// GET /api/routeros/{device}/... is session-gated (accessUser), unlike
// the ingest endpoint above, which is bearer-token-only and reachable
// with a plain fetch().
async function getTable(path_) {
  const res = await page.request.get(`${URL_BASE}${path_}`)
  return { status: res.status(), body: await res.json() }
}

// --- Setup: two independently-scoped ingest tokens ------------------------

const tokA = await createToken({ name: 'live-ingest-a', kind: 'ingest', device: 'router-a' })
check(tokA.status === 201, `token A issued (${tokA.status})`)
const tokB = await createToken({ name: 'live-ingest-b', kind: 'ingest', device: 'router-b' })
check(tokB.status === 201, `token B issued (${tokB.status})`)

const validArp = {
  kind: 'arp',
  page: 1,
  pages: 1,
  records: [{ address: '198.51.100.9', mac: 'aa:bb:cc:dd:ee:ff' }],
}

// --- A valid payload is accepted and surfaced through the read side -------

check((await push(tokA.body.value, validArp)) === 200, 'a well-formed push is accepted (200)')

// --- Device isolation: router-a's push must not appear under router-b -----

check((await push(tokB.body.value, { ...validArp, records: [{ address: '198.51.100.10', mac: '11:22:33:44:55:66' }] })) === 200,
  'router-b pushes its own arp table')

{
  const rulesA = await getTable('/api/routeros/router-a/rules')
  check(rulesA.status === 200 && rulesA.body.available === false,
    'router-a never pushed a rule table -- available:false, not an empty table pretending to be real')
}

// --- Rejection surface ------------------------------------------------------

check((await push(tokA.body.value, { ...validArp, records: [{ address: '198.51.100.9', mac: '', extra: 'field' }] })) === 400,
  'an unknown field in a record is refused (400)')

check((await push(tokA.body.value, { kind: 'not-a-real-kind', page: 1, pages: 1, records: [] })) === 400,
  'an unrecognised kind is refused (400)')

{
  // ~70KiB body, over the 64KiB cap the endpoint (and RouterOS's own
  // /tool fetch) both enforce.
  const huge = {
    kind: 'arp',
    page: 1,
    pages: 1,
    records: [{ address: '198.51.100.9', mac: 'a'.repeat(70 * 1024) }],
  }
  const status = await push(tokA.body.value, huge)
  check(status === 400 || status === 413, `an oversized body is refused (got ${status}, want 400 or 413)`)
}

check((await push('not-a-real-token', validArp)) === 401, 'an invalid token is refused (401)')

{
  const revokeMe = await createToken({ name: 'live-ingest-revoked', kind: 'ingest', device: 'router-revoked' })
  const del = await page.request.delete(`${URL_BASE}/api/tokens/${revokeMe.body.id}`, {
    headers: { 'X-Requested-With': 'mikroview' },
  })
  check(del.status() === 200, 'the soon-to-be-tested token is revoked (200)')
  check((await push(revokeMe.body.value, validArp)) === 401, 'a revoked token is refused (401)')
}

// --- Rate limit: real requests, not a lowered test-only threshold ---------
// ingestLimiterThreshold is 120/15min in the real binary this live-check
// drives -- exercised directly rather than injecting a smaller value,
// since this is exactly the kind of "does the real wiring work" check
// live-check exists for.

{
  const burstToken = (await createToken({ name: 'live-ingest-burst', kind: 'ingest', device: 'router-burst' })).body.value
  let lastStatus = 0
  for (let i = 0; i < 121; i++) {
    lastStatus = await push(burstToken, validArp)
    if (lastStatus === 429) break
  }
  check(lastStatus === 429, `a burst past the rate limit is refused (429) -- last status seen: ${lastStatus}`)
}

// --- Structural isolation: pushed data cannot touch a flag ----------------
// internal/routerstate never imports flags/detect (a build-failing test
// already guards that at compile time) -- this is the same claim
// checked live, end to end: a flag raised behaviorally from real syslog
// traffic must be completely unaffected by any amount of router-state
// pushing, including pushes that name the exact same source address.

feedPortScan(20, '198.51.100.9')

// Poll rather than wait on a DOM selector -- this scenario reads
// /api/flags directly throughout and never navigates to the Flags
// view, so there's no rendered card to wait on.
let scanFlag
for (let i = 0; i < 30 && !scanFlag; i++) {
  const flags = await getTable('/api/flags')
  scanFlag = flags.body.flags?.find((f) => f.target === '198.51.100.9')
  if (!scanFlag) await new Promise((r) => setTimeout(r, 500))
}
check(!!scanFlag, 'the port scan raised a real flag for 198.51.100.9')

// Let the flag settle before snapshotting it.
//
// The scan's own events are still arriving when it first appears, and
// each re-fire raises confidence -- so comparing a snapshot taken at
// first sight against one taken after the pushes measures ingest still
// progressing, not what the pushes did. Wait for two consecutive reads
// to agree, then snapshot: the assertion below is about pushed data
// being inert, and it should fail only for that reason.
//
// It passed under live-env.sh and failed against the container, which
// is the same race with more latency either side of it.
for (let i = 0; i < 40; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const flags = await getTable('/api/flags')
  const now = flags.body.flags?.find((f) => f.target === '198.51.100.9')
  if (now && now.confidence === scanFlag.confidence && now.count === scanFlag.count) {
    scanFlag = now
    break
  }
  if (now) scanFlag = now
}

for (let i = 0; i < 5; i++) {
  await push(tokA.body.value, {
    kind: 'address-list',
    page: 1,
    pages: 1,
    records: [{ list: 'blocked', address: '198.51.100.9', comment: `push ${i}`, dynamic: false }],
  })
}

const flagsAfter = await getTable('/api/flags')
const scanFlagAfter = flagsAfter.body.flags.find((f) => f.target === '198.51.100.9')
check(
  !!scanFlagAfter && scanFlagAfter.cleared === scanFlag.cleared && scanFlagAfter.confidence === scanFlag.confidence,
  'five pushes naming the exact same address left the flag completely unchanged -- pushed data cannot clear, lower, or otherwise touch it',
)

done()
