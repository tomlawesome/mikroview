// SPDX-License-Identifier: AGPL-3.0-only
//
// #369: internal/routeros.Parse's field clamp exempted Flags, the
// TCP-flags/ICMP-type string parseProto extracts from proto's
// parenthetical detail ("proto TCP (SYN)", "proto ICMP (type 8, code
// 0)"). Every other extracted field went through clampAll's safeField,
// but Flags did not -- so a crafted line could put tens of kilobytes
// into it alone, reopening the same overrun #285 finding 5 closed for
// the raw log line: retained in full in every event slot, and returned
// verbatim by GET /api/events, which anything holding a session cookie
// can call with limit up to 5000.
//
// Unit and fuzz coverage pin the parser's own output directly, including
// at the 65KB scale the finding was originally reproduced at
// (internal/routeros/clamp_test.go's TestFlagsFieldIsClamped,
// FuzzParse's assertFieldsClamped invariant in fuzz_test.go). This is
// the same claim checked end to end instead: a crafted line over the
// real syslog TLS listener, through the real store, read back from the
// real API response a client actually receives -- rather than trusting
// that nothing between the parser and the wire re-introduces the gap.
//
// The payload matches the unit tests' 65KB worst case. It used to stay
// at 4,000 bytes instead, as a workaround for a real but distinct bug
// in internal/syslog/tcp_listener.go's read loop (#415): a message
// under 64KB that arrived fragmented across multiple non-full reads
// wasn't recognised as one message, so each fragment was parsed as its
// own garbage event. #415 fixed the listener to reassemble by message
// framing rather than by whether a read happened to fill its buffer,
// so this can exercise the real worst case end to end again.
import { session, feedRaw, check, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const RULE = 'flags-clamp-live'

// Balanced parens, no top-level comma inside them -- the shape the
// finding was reproduced with (splitTopLevel's paren-aware path, not
// its naive unbalanced-parens fallback). 65,000 bytes matches the
// worst case the unit tests pin (internal/routeros/clamp_test.go's
// TestFlagsFieldIsClamped).
const oversized = 'a'.repeat(65000)
feedRaw(
  `A|${RULE}|forward: in:ether1 out:bridge1, proto TCP (${oversized}), ` +
    '198.51.100.77:1024->203.0.113.9:443, len 60',
)

const { page } = await session()

const res = await page.request.get(`${URL_BASE}/api/events?rule=${RULE}&limit=5`)
check(res.status() === 200, `the event query succeeds (${res.status()})`)

const body = await res.json()
const event = (body.events ?? []).find((e) => e.ruleLabel === RULE)
check(!!event, 'the crafted line was ingested rather than dropped or crashing the process')

if (event) {
  check(
    event.flags.length <= 256,
    `flags is clamped like every other extracted field, not left to grow unbounded (got ${event.flags.length} bytes)`,
  )
  check(
    event.raw.length <= 2048,
    `raw stays bounded by store.MaxRawBytes regardless of what Flags does (got ${event.raw.length} bytes)`,
  )
}

done()
