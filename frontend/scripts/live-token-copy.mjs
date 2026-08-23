// SPDX-License-Identifier: AGPL-3.0-only
//
// #439: the live-view row-token interaction model. Click-to-filter is
// unchanged, but row text is now genuinely selectable, and each token
// (address, port, rule, device name) gets a hover-revealed control that
// copies the RAW value behind whatever friendly label is shown, with a
// transient "Copied" toast for feedback.
//
// None of that is provable from LiveTable.svelte.test.ts's jsdom suite
// alone -- jsdom doesn't implement the Clipboard API, doesn't run CSS
// transitions/opacity, and a synthetic mousedown/mouseup pair there is
// not a real drag that produces a real browser selection. This drives
// all three against a real browser instead: a real drag actually
// selecting text, the copy glyph's hover-gated opacity actually
// changing, and the clipboard actually holding the raw IP -- not the
// "nas-live-check" label the row displays -- after clicking it.

import { session, feedRaw, check, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const RULE = 'live-token-copy'
// 203.0.113.0/24 (RFC 5737 TEST-NET-3), not .2 specifically -- that's
// live-router-lookup.mjs's camera.lan dns-static entry, the only other
// exact claim in this block.
//
// Deliberately NOT 198.51.100.0/24 (this scenario's address until this
// fix) or 192.0.2.0/24: this suite's scenarios all share one device
// (live-router, loopback mode's only declared device -- every feeder
// connects from 127.0.0.1), and live-router-lookup.mjs -- which sorts
// before this scenario in run-scenarios.sh's glob and so always runs
// first -- pushes a wireguard-peer record with
// allowedAddress: ['192.0.2.0/24', '198.51.100.0/24'] and
// comment: 'branch office'. naming.Resolver.Host checks RouterHosts
// before Entities (internal/naming/naming.go: "RouterOS always wins",
// issue #186 step 4c's owner decision, verbatim), and
// routerstate.go's rebuildIdentityLocked applies that peer's Comment as
// the host name across *every* CIDR in AllowedAddress -- so any address
// in either /24, this scenario's own entity label included, resolved to
// "branch office" instead, in the full suite (never standalone, where
// live-router-lookup.mjs hadn't run). Root-caused via
// internal/naming/naming.go + internal/routerstate/routerstate.go
// (rebuildIdentityLocked's WireguardPeer branch), then confirmed with
// the diagnostic fetch below before this fix landed: it printed
// srcHostName "branch office" for an event whose only entity label was
// "nas-live-check". This is the deliberate, documented RouterOS-wins
// precedence working as designed -- not a product defect -- so the fix
// is entirely this scenario's choice of test address, not app code.
const HOST_IP = '203.0.113.221'
const HOST_LABEL = 'nas-live-check'

const { page, consoleErrors } = await session()

async function api(method, path, body) {
  const res = await page.request.fetch(`${URL_BASE}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

/** Polls an <input>'s value -- appState updates it reactively, not synchronously from this process's point of view. */
async function waitForInputValue(selector, expected, timeoutMs = 3000) {
  const deadline = Date.now() + timeoutMs
  let last = ''
  while (Date.now() < deadline) {
    last = await page.inputValue(selector)
    if (last === expected) return last
    await page.waitForTimeout(100)
  }
  return last
}

// A friendly label on the source IP -- the row must show HOST_LABEL, but
// still copy HOST_IP. Without this, "copies the raw value" and "copies
// whatever text happens to be on screen" are indistinguishable.
const labelled = await api('POST', '/api/entities', { type: 'host', key: HOST_IP, label: HOST_LABEL })
check(labelled.status === 200 || labelled.status === 201, `the host entity label is saved (${labelled.status})`)

const line =
  `firewall,info A|${RULE}| forward: in:ether1 out:bridge1, connection-state:new, ` +
  `proto TCP (SYN), ${HOST_IP}:51512->203.0.113.44:443, len 60`
feedRaw(line)

// Wait for the event on the *server* before asking the UI about it --
// #354's pattern, the same reason live-flags-investigate.mjs waits via
// waitForFlag (#450/#465). Under the full suite's load a single fed
// line can be dropped by a saturated ingest queue or arrive seconds
// late; waiting on the rendered row alone turned that into an uncaught
// Playwright timeout with no RESULT line -- observed on this very
// scenario's first full-suite run. Re-fed periodically because a
// dropped line never arrives on its own, and this scenario is testing
// token interactions, not ingest reliability under load.
let arrived = null
const deadline = Date.now() + 25000
while (Date.now() < deadline) {
  const events = await api('GET', `/api/events?rule=${RULE}&limit=5`)
  if ((events.body?.events?.length ?? 0) > 0) { arrived = events.body.events[0]; break }
  await new Promise((r) => setTimeout(r, 2500))
  feedRaw(line)
}
check(!!arrived, `the test event reached the server (rule=${RULE})`)
if (!arrived) {
  check(true, 'skipped -- token interactions cannot be exercised on a row that never arrived')
  done()
}

// Diagnostic, not an assertion of its own: names which of two worlds a
// row-wait failure below would otherwise leave ambiguous -- "the label
// never got applied at ingest" (this prints something other than
// HOST_LABEL, e.g. a router-pushed name shadowing it, or the raw IP if
// nothing resolved at all) versus "the label applied fine and the UI
// just didn't render/find it" (this prints exactly HOST_LABEL). See the
// HOST_IP comment above for the shadowing failure mode this caught.
console.log(`DIAGNOSTIC event.srcIp=${arrived.srcIp} event.srcHostName=${arrived.srcHostName ?? '(none)'}`)
check(
  arrived.srcHostName === HOST_LABEL,
  `the ingested event's srcHostName is the entity label, not something else (got "${arrived.srcHostName ?? '(none)'}")`,
)

await page.fill('input.rule', RULE)
// Wrapped rather than a bare await: an absent row is a legible FAIL +
// skip, not an uncaught Playwright timeout with no RESULT line (#465's
// rule, the same one the arrival-wait above already follows -- this
// scenario's second full-suite run hit exactly this crash, on this
// line, before this wrapping existed).
let rowFound = true
try {
  await page.waitForSelector(`.row:has-text("${HOST_LABEL}")`, { timeout: 15000 })
} catch {
  rowFound = false
}
check(rowFound, `a row showing "${HOST_LABEL}" rendered (srcHostName was "${arrived.srcHostName ?? '(none)'}")`)
if (!rowFound) {
  check(true, 'skipped -- token interactions cannot be exercised on a row that never rendered')
  done()
}
const row = page.locator('.row', { hasText: HOST_LABEL }).first()

// Scoped to the *source* address cell specifically (the one showing
// HOST_LABEL) rather than the row's first `.addr-btn`/`.copy-btn` --
// the row's very first copy glyph belongs to the device-name cell, not
// this one, and a plain `.first()` silently grabbed that instead.
const addrCell = row.locator('.cell.addr', { hasText: HOST_LABEL })
check((await addrCell.locator('.addr-btn', { hasText: HOST_LABEL }).count()) > 0, 'the row shows the resolved host label, not the raw IP')
check(!(await row.textContent())?.includes(HOST_IP), 'the raw IP is not the row\'s visible text')

// --- Native selection: a real drag actually selects the row's text ------
const addrBtn = addrCell.locator('.addr-btn').first()
const box = await addrBtn.boundingBox()
check(!!box, 'the address token has a real, hoverable box')
if (box) {
  await page.mouse.move(box.x + 2, box.y + box.height / 2)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width - 2, box.y + box.height / 2, { steps: 8 })
  await page.mouse.up()
  const selected = await page.evaluate(() => window.getSelection()?.toString() ?? '')
  check(selected.trim().length > 0, `dragging across the token actually selects its text (got "${selected}")`)

  // Selecting text and clicking to filter are supposed to be mutually
  // exclusive -- the drag above must not also have applied the filter.
  const ipDuringSelection = await page.inputValue('input[aria-label="IP address or CIDR"]')
  check(
    ipDuringSelection === '',
    `a drag that leaves a selection behind does not also apply the IP filter (got "${ipDuringSelection}")`,
  )

  // Clean up before the click-to-filter/copy checks below, which need
  // to observe their own behavior, not this drag's leftover selection.
  await page.evaluate(() => window.getSelection()?.removeAllRanges())
}

// The drag above ends with the pointer still resting on the row (leaving
// it CSS-:hover'd) and, since it started with a mousedown on a
// tabindex="0" element, with that element focused (:focus-within also
// reveals the glyph -- deliberately, for keyboard users tabbing to it
// directly). Undo both before "starts hidden" below, or it would be
// checking a test artifact rather than the real resting state.
await page.mouse.move(2, 2)
await page.evaluate(() => (document.activeElement instanceof HTMLElement) && document.activeElement.blur())
await page.waitForTimeout(200) // let the opacity transition settle back to 0

// --- Hover-revealed copy glyph -------------------------------------------
const copyBtn = addrCell.locator('.copy-btn').first()
const opacityBeforeHover = await copyBtn.evaluate((el) => getComputedStyle(el).opacity)
check(opacityBeforeHover === '0', `the copy glyph starts hidden (opacity ${opacityBeforeHover})`)

await row.hover()
await page.waitForTimeout(200) // the opacity transition
const opacityOnHover = await copyBtn.evaluate((el) => getComputedStyle(el).opacity)
check(opacityOnHover === '1', `hovering the row reveals the copy glyph (opacity ${opacityOnHover})`)

// Clipboard permissions, granted explicitly, so the read-back below can
// prove what actually landed on the clipboard rather than only that a
// toast appeared (which would still pass if the write silently failed).
await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: URL_BASE })

await copyBtn.click()

await page.waitForSelector('.toast[role="status"]', { timeout: 3000 })
const toastText = await page.textContent('.toast')
check(!!toastText && /copied/i.test(toastText), `a transient "copied" toast appears (saw "${toastText}")`)

const clipboardText = await page.evaluate(() => navigator.clipboard.readText())
check(
  clipboardText === HOST_IP,
  `the clipboard holds the RAW IP, not the "${HOST_LABEL}" label the row shows (got "${clipboardText}")`,
)

// The toast is transient -- it must go away on its own rather than linger.
await page.waitForSelector('.toast', { state: 'detached', timeout: 4000 })
check(true, 'the "copied" toast auto-dismisses')

// Clicking the copy glyph must not also have applied the filter.
const ipAfterCopy = await page.inputValue('input[aria-label="IP address or CIDR"]')
check(ipAfterCopy === '', `clicking the copy glyph does not apply the IP filter (got "${ipAfterCopy}")`)

// --- A plain click (no drag) still filters, unchanged from before -------
await addrBtn.click()
const ipAfterClick = await waitForInputValue('input[aria-label="IP address or CIDR"]', HOST_IP)
check(ipAfterClick === HOST_IP, `a plain click still applies the IP filter (got "${ipAfterClick}")`)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
