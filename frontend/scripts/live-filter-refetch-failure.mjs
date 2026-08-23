// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #373: a failed filter refetch (or initial load) used to be
// swallowed -- App.svelte's handleApiError only acts on a 401, so a
// rejected fetchEvents() (a 503, a dropped connection) left `events`
// exactly as it was, with nothing recording that the query never
// completed. The live view then read that untouched buffer as a
// definite, confirmed-empty answer: "No events match the current
// filters." The operator was told no matching traffic occurred when the
// truth was that mikroview could not ask.
//
// Not unit-testable end to end in the same way: the component tests
// (LiveTable.svelte.test.ts) drive appState.fetchFailed directly, which
// proves the rendering logic but not that a real fetch failure actually
// sets it, propagates through a real debounce, and reaches a real
// browser's DOM. This scenario reproduces the reach lens's own repro
// from the issue: route-intercept /api/events to fail, then narrow a
// filter so refetchWithFilters() is what has to run to find a genuine
// server-side match.
import { session, feedSyslog, check, done } from './live-browser.mjs'

const MATCHED_RULE = 'refetch-failure-373'

// Buffered locally so the client-side filter layer alone could, in
// principle, find these -- the point of the scenario is that the
// *refetch* fails, not that the buffer is empty to start with.
feedSyslog(30, MATCHED_RULE)

const { page, consoleErrors } = await session({ waitForEvents: 30 })

// The WebSocket stream must stay healthy throughout -- this is
// specifically the dual-channel failure the issue describes (the query
// endpoint fails while the transport that would otherwise announce
// trouble stays up), not a full outage that ConnectionBanner would
// already catch.
await page.route('**/api/events*', (route) => route.fulfill({ status: 503, body: 'simulated outage' }))

// A filter value that matches nothing already in the client buffer, so
// the only way to get a real answer is the server-side refetch -- which
// is exactly what is now failing.
await page.fill('input.rule', 'no-such-rule-in-the-local-buffer')
// FILTER_DEBOUNCE_MS (300ms, App.svelte) plus headroom for the rejected
// request to actually resolve.
await page.waitForTimeout(1500)

const emptyText = await page.textContent('.body .empty')
check(!!emptyText, 'the empty-state message is shown once the filter narrows to nothing')
check(
  !/No events match the current filters\./.test(emptyText ?? ''),
  `a failed refetch is not presented as a confirmed empty result (saw: "${emptyText}")`,
)
check(
  /could not load|failed|error/i.test(emptyText ?? ''),
  `the message names the failure honestly (saw: "${emptyText}")`,
)

// The dual-channel claim from the issue: the failure is real, but
// nothing else on screen incorrectly claims health is compromised either
// -- the WebSocket transport is untouched by this scenario, so the
// connection banner should still read healthy. Asserted for completeness
// -- the fix under test is the empty-state message above, not this.
const banner = await page.$('.banner-closed, .banner-connecting')
check(!banner, 'the WebSocket connection itself is unaffected by the API-only outage')

// Un-break the endpoint and confirm the honest state clears on the next
// successful refetch, rather than latching forever.
await page.unroute('**/api/events*')
await page.fill('input.rule', MATCHED_RULE)
await page.waitForTimeout(1500)

const recoveredText = await page.textContent('.body .empty').catch(() => null)
check(
  recoveredText === null || !/could not load/i.test(recoveredText),
  `the failure message clears once a refetch actually succeeds (saw: "${recoveredText}")`,
)

// The 503s above are this scenario's own fault injection, not a defect --
// filtered out here rather than by broadening the shared helper in
// live-browser.mjs, which every other scenario also uses and which
// should keep flagging a genuine failed resource load.
const unexpectedErrors = consoleErrors.filter((e) => !/503 \(Service Unavailable\)/.test(e))
check(unexpectedErrors.length === 0, `no unexpected console errors (${unexpectedErrors.join('; ')})`)
done()
