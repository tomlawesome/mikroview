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
// The payload here is deliberately much smaller than the unit tests'
// (a few KB, not 65KB): this layer is proving the wiring, which the
// parser-level tests already establish is correct at any size, and a
// large single-line write is not guaranteed to reach the TCP listener
// in one Read() -- crossing that boundary hit an unrelated bug in
// internal/syslog/tcp_listener.go's read loop (its message-reassembly
// logic only recognises a read that exactly fills its 64KB buffer as
// "message continues"; a large-but-under-64KB message that still
// arrives fragmented across multiple non-full reads is not recognised,
// and each fragment gets parsed as its own garbage event). Real, found
// live while sizing this scenario, but a distinct defect from #369 and
// out of scope for it -- flagged separately rather than fixed here.
// Staying small avoids exercising that path at all.
import { session, feedRaw, check, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const RULE = 'flags-clamp-live'

// Balanced parens, no top-level comma inside them -- the shape the
// finding was reproduced with (splitTopLevel's paren-aware path, not
// its naive unbalanced-parens fallback). 4000 bytes is >15x maxFieldLen
// (256), comfortably enough to prove the clamp fires, while staying
// well under a single TLS record.
const oversized = 'a'.repeat(4000)
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
