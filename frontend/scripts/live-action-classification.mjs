// SPDX-License-Identifier: AGPL-3.0-only
//
// #437: mangle marks, NAT translations and log-only rules reaching the
// live view as what they are, instead of all landing in "unknown".
//
// The parser's own tests pin the classification, and they cannot fail
// the way this can. Every one of them compares the parser against lines
// written next to it, in the same package -- what they cannot show is
// the value surviving the store's byAction slots, JSON serialisation,
// the WebSocket stream and the browser's Action union, and then being
// *selectable*: an action that renders a badge but is missing from the
// filter pick-list is a category an operator can see and cannot isolate,
// and nothing on either side of the wire fails when that happens.
//
// Every line below is synthetic and uses documentation address space
// (RFC 5737 / RFC 1918). Nothing here comes from a real deployment.

import { session, feedRaw, check, done } from './live-browser.mjs'

// One distinctive slug per class, so the rule filter can isolate this
// scenario's own events on an instance other scenarios have already
// pushed hundreds of events through.
const MARK = 'mv437-mark'
const NAT = 'mv437-nat'
const LOGONLY = 'mv437-logonly'

// Tagged: the operator declares the rule kind in the log-prefix.
feedRaw(
  `firewall,info M|${MARK}| prerouting: in:bridge1 out:(unknown 0), connection-state:new, ` +
    `proto TCP (SYN), 192.168.88.20:51512->203.0.113.44:443, len 60`,
)
feedRaw(
  `firewall,info N|${NAT}| srcnat: in:bridge1 out:ether1, proto UDP, ` +
    `192.168.88.20:51258->198.51.100.53:53, NAT (203.0.113.10:51258->198.51.100.53:53), len 73`,
)
feedRaw(
  `firewall,info L|${LOGONLY}| forward: in:bridge1 out:ether1, connection-state:new, ` +
    `proto TCP (SYN), 192.168.88.30:44100->198.51.100.80:80, len 60`,
)
// Untagged, and inferable from the line alone: a dstnat chain carrying
// RouterOS's translated-address annotation.
feedRaw(
  `firewall,info dstnat: in:ether1 out:bridge1, proto TCP (SYN), ` +
    `198.51.100.7:41000->203.0.113.10:8080, NAT 198.51.100.7:41000->(192.168.88.10:8080), len 60`,
)
// Untagged and NOT inferable: a mangle-shaped line with nothing in it
// that says so. This one must stay unknown -- it is the control.
feedRaw(
  `firewall,info postrouting: in:(unknown 0) out:ether1, proto TCP (ACK), ` +
    `192.168.88.20:51512->203.0.113.44:443, len 52`,
)

const { page, consoleErrors } = await session({ waitForEvents: 1 })

/** Sets the rule filter and returns the action badges left on screen. */
async function badgesForRule(rule) {
  await page.fill('input.rule', rule)
  await page.waitForTimeout(900)
  return page.$$eval('.grid .row .badge', (els) => els.map((e) => e.textContent.trim()))
}

// --- The tagged classes reach the browser -------------------------------
const markBadges = await badgesForRule(MARK)
check(
  markBadges.length > 0 && markBadges.every((b) => b === 'MARKED'),
  `an M-tagged mangle rule renders as MARKED (${markBadges.join(', ') || 'no rows'})`,
)

const natBadges = await badgesForRule(NAT)
check(
  natBadges.length > 0 && natBadges.every((b) => b === 'NATTED'),
  `an N-tagged NAT rule renders as NATTED (${natBadges.join(', ') || 'no rows'})`,
)

const logBadges = await badgesForRule(LOGONLY)
check(
  logBadges.length > 0 && logBadges.every((b) => b === 'LOG'),
  `an L-tagged log-only rule still renders as LOG (${logBadges.join(', ') || 'no rows'})`,
)

// --- The action filter can actually isolate them ------------------------
//
// The half a unit test cannot reach: the value the badge shows has to be
// the value the pick-list offers and the API filters on. A label used as
// a filter value, or an option missing entirely, both look fine until
// someone tries to narrow to it.
await page.fill('input.rule', '')
await page.selectOption('select[aria-label="Action"]', 'marked')
await page.waitForTimeout(900)
const underMarkedFilter = await page.$$eval('.grid .row .badge', (els) =>
  els.map((e) => e.textContent.trim()),
)
check(
  underMarkedFilter.length > 0 && underMarkedFilter.every((b) => b === 'MARKED'),
  `filtering to "marked" returns marked events and nothing else (${[...new Set(underMarkedFilter)].join(', ') || 'no rows'})`,
)

await page.selectOption('select[aria-label="Action"]', 'natted')
await page.waitForTimeout(900)
const underNattedFilter = await page.$$eval('.grid .row .badge', (els) =>
  els.map((e) => e.textContent.trim()),
)
check(
  underNattedFilter.length > 0 && underNattedFilter.every((b) => b === 'NATTED'),
  `filtering to "natted" returns natted events and nothing else (${[...new Set(underNattedFilter)].join(', ') || 'no rows'})`,
)

// The untagged dstnat line has no rule label to search by, so it is
// found here: under the natted filter, on the chain the inference keys
// off. Two natted sources -- one declared, one inferred -- and this
// asserts the inferred one arrived.
const nattedChains = await page.$$eval('.grid .row .cell.chain', (els) =>
  els.map((e) => e.textContent.trim()),
)
check(
  nattedChains.includes('dstnat'),
  `an untagged dstnat line carrying a translation is classified natted without any prefix (chains seen: ${[...new Set(nattedChains)].join(', ') || 'none'})`,
)

// --- The control: what the parser cannot tell stays unknown -------------
//
// This is the assertion the whole change rests on. Shrinking "unknown"
// is only worth anything if it still means "unknown" -- a postrouting
// line is certainly a mangle rule and still says nothing about which
// mangle action ran, so it must not have been given a confident label on
// the way past.
await page.selectOption('select[aria-label="Action"]', 'unknown')
await page.waitForTimeout(900)
const unknownChains = await page.$$eval('.grid .row .cell.chain', (els) =>
  els.map((e) => e.textContent.trim()),
)
check(
  unknownChains.includes('postrouting'),
  `an untagged postrouting line is still honestly unknown rather than guessed as marked (chains seen: ${[...new Set(unknownChains)].join(', ') || 'none'})`,
)

await page.selectOption('select[aria-label="Action"]', '')
await page.waitForTimeout(500)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
