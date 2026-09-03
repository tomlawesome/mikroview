// SPDX-License-Identifier: AGPL-3.0-only
//
// #445: the NAT popup's two modes, driven end to end in a real browser
// against a NAT table genuinely pushed through POST /api/ingest/routeros
// with a real ingest token.
//
// The feature exists to keep two claims apart, and this scenario exists
// because that separation is not visible from either side of the wire.
// The parser's tests can show an event carries a log-prefix; the
// partition's unit tests can show which rules a set of facts rules out.
// Neither can show what the operator is told -- that a *logged*
// translation names its rule and says "logged", that an *unlogged* one
// says "not logged" and offers subtraction instead, and that neither
// rendering ever leaks into the other. A popup that quietly showed the
// could-have list under the logged heading would pass every test in the
// repo and be the exact dishonesty this issue was opened about.
//
// Also covered: hold-while-open (#413's decision, shared here), because
// an anchored popover whose row slides out from under it while you read
// it is only observable by watching it happen.
//
// #644's squared columns moved the *untagged* translation's entry point:
// the NAT cell is gone from the rows, so its trigger now lives in the
// detail sheet each row opens (EventDetailSheet's natlookup) -- same
// store, same two modes, same wording, rendered by the same
// RouterNatLookup the popover embeds. A tagged event keeps its trigger
// in the rule cell, and with it the anchored popover.
//
// Every line and address below is synthetic, using documentation address
// space (RFC 5737 / RFC 1918). Nothing here comes from a real deployment.

import { session, check, done, feedRaw, feedSyslog } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

// Every address below is unique to this scenario, checked against the
// other live-*.mjs files rather than picked for looking plausible. They
// all run against one shared instance, so an address two scenarios both
// use is a coupling: this one first used 198.51.100.9, which is the
// port-scan target live-routeros-ingest.mjs waits for a flag on, so a
// failure there could no longer be told apart from the known #450 flag
// race. Keep them unique, and keep the translated addresses outside
// 203.0.113.0-249, which is the synthetic feeder's own source range.
//
// UNLOGGED_SRC also has to be distinctive enough to filter the live view
// down to this scenario's own row on an instance other scenarios have
// already pushed hundreds of events through.
const UNLOGGED_SRC = '192.0.2.145'
const LOGGED_SLUG = 'mv445-nat'
const HOLD_SLUG = 'mv445-hold'

const SOURCE_BOX = 'input[aria-label="Source — name, IP or CIDR"]'
const SHEET_NAT_TRIGGER = '.sheet .natlookup'
const LOGGED_TRIGGER = `button[aria-label="Look up the NAT rule logged as ${LOGGED_SLUG}"]`

const { page, consoleErrors } = await session()

// Which device the events arrive from is discovered, never assumed --
// see live-before-router-lookup.mjs for the incident that lesson came from.
feedSyslog(2, 'mv445-device-probe')
let DEVICE
for (let i = 0; i < 40 && !DEVICE; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const res = await page.request.get(`${URL_BASE}/api/devices`)
  if (res.ok()) DEVICE = (await res.json()).devices?.[0]?.id
}
check(!!DEVICE, `the instance reports the device events arrive from (${DEVICE})`)

const tokenRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-nat-popup', kind: 'ingest', device: DEVICE },
})
check(tokenRes.status() === 201, `an ingest token is issued (${tokenRes.status()})`)
const token = (await tokenRes.json()).value

async function push(payload) {
  const res = await fetch(`${URL_BASE}/api/ingest/routeros`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  return res.status
}

/** Narrows the live view to one address and opens that row's detail
 * sheet, where the untagged lookup's trigger lives (#644). */
async function openRowSheet(query) {
  await page.fill(SOURCE_BOX, query)
  const row = `.grid .row:has-text("${query}")`
  await page.waitForSelector(`${row} .time-btn`, { timeout: 20000 })
  await page.click(`${row} .time-btn`)
  await page.waitForSelector(SHEET_NAT_TRIGGER, { timeout: 5000 })
}

/** Opens the sheet's NAT lookup section and returns the sheet's text.
 *
 * The sheet's mode chip renders with the section itself, before the
 * lookup resolves -- unlike the popover's, which is the resolved signal
 * openPopover waits on -- so this waits for the section body to move
 * past Loading… instead.
 */
async function openSheetLookup() {
  await page.click(SHEET_NAT_TRIGGER)
  await page.waitForFunction(
    () => {
      const t = document.querySelector('.sheet .natsection')?.textContent ?? ''
      return t !== '' && !t.includes('Loading…')
    },
    null,
    { timeout: 15000 },
  )
  return page.textContent('.sheet')
}

async function closeSheet() {
  await page.keyboard.press('Escape')
  await page.locator('.sheet').waitFor({ state: 'hidden', timeout: 10000 })
}

/**
 * Opens a popover from `trigger` and returns its text.
 *
 * waitForSelector, not isVisible: isVisible answers about *this instant*
 * and does not wait, so it reports false on a popover that is about to
 * render and turns a passing feature into a failing scenario.
 */
async function openPopover(trigger) {
  await page.click(`${trigger} >> nth=0`)
  // The mode chip renders only once the lookup has resolved, so waiting
  // for it is also how this waits out "Loading…" -- reading the body
  // before then would assert against a popover that has not decided what
  // it is showing yet.
  await page.locator('.popover .chip').waitFor({ state: 'visible', timeout: 15000 })
  return page.textContent('.popover')
}

async function closePopover() {
  await page.keyboard.press('Escape')
  // 'hidden' covers detached too, which is what {#if st.anchor} does.
  await page.locator('.popover').waitFor({ state: 'hidden', timeout: 10000 })
}

// --- The two NAT events -------------------------------------------------
// One tagged with mikroview's N| log-prefix convention, one not. They are
// otherwise ordinary RouterOS NAT lines, annotation and all.

const unloggedLine =
  `firewall,info srcnat: in:bridge1 out:ether1, proto UDP, ` +
  `${UNLOGGED_SRC}:51258->198.51.100.223:53, NAT (203.0.113.251:51258->198.51.100.223:53), len 73`

const loggedLine =
  `firewall,info N|${LOGGED_SLUG}| dstnat: in:ether1 out:(unknown 0), proto TCP (SYN), ` +
  `198.51.100.221:41000->203.0.113.252:8443, NAT 198.51.100.221:41000->(192.0.2.146:8443), len 60`

feedRaw(unloggedLine)
feedRaw(loggedLine)

// --- State 0: nothing pushed yet ---------------------------------------
// Asserted only when it is genuinely true. Scenarios share one instance
// and one device, so an earlier one may already have pushed a NAT table;
// asking the instance is the difference between a real check and one
// that passes or fails on alphabetical filename order.
{
  const before = await (
    await page.request.get(`${URL_BASE}/api/routeros/${encodeURIComponent(DEVICE)}/nat`)
  ).json()
  if (!before.available) {
    await openRowSheet(UNLOGGED_SRC)
    const text = await openSheetLookup()
    check(
      text.includes('No NAT table pushed'),
      'before any push, the lookup says no table has been pushed -- not an empty table',
    )
    await closeSheet()
  }
}

// --- The pushed table ---------------------------------------------------
// Seven rules, chosen so every branch of the partition is exercised by
// the one unlogged event above: two survive, and the other five are ruled
// out for five different, stated reasons. Rule 6 carries the log-prefix
// the tagged event was logged with.

check(
  (await push({
    kind: 'nat-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      {
        ordinal: 0,
        comment: 'masquerade LAN out',
        chain: 'srcnat',
        action: 'masquerade',
        outInterface: 'ether1',
        srcAddress: '192.0.2.0/24',
      },
      {
        ordinal: 1,
        comment: 'web to the DMZ host',
        chain: 'dstnat',
        action: 'dst-nat',
        toAddresses: '192.0.2.10',
        toPorts: 8080,
        dstPort: 443,
        protocol: 'tcp',
        inInterface: 'ether1',
      },
      {
        ordinal: 2,
        comment: 'source-nat the tcp uplink',
        chain: 'srcnat',
        action: 'src-nat',
        protocol: 'tcp',
        toAddresses: '198.51.100.2',
      },
      {
        ordinal: 3,
        comment: 'old masquerade, kept but off',
        chain: 'srcnat',
        action: 'masquerade',
        outInterface: 'ether1',
        disabled: true,
      },
      {
        // The unknown-never-excludes case: src-address is an address-list
        // name, which mikroview holds as a separate pushed table and
        // deliberately does not join in here.
        ordinal: 4,
        comment: 'guest hosts by list',
        chain: 'srcnat',
        action: 'src-nat',
        protocol: 'udp',
        srcAddress: 'guest-hosts',
        toAddresses: '198.51.100.3',
      },
      {
        ordinal: 5,
        comment: 'wifi guests out',
        chain: 'srcnat',
        action: 'masquerade',
        outInterface: 'wlan1',
      },
      {
        ordinal: 6,
        comment: 'port forward, logged',
        chain: 'dstnat',
        action: 'dst-nat',
        logPrefix: `N|${LOGGED_SLUG}|`,
        toAddresses: '192.0.2.146',
        toPorts: 8443,
        dstPort: 8443,
        protocol: 'tcp',
        inInterface: 'ether1',
      },
    ],
  })) === 200,
  'a NAT table carrying a log-prefix is accepted through the real ingest endpoint',
)

{
  const table = await (
    await page.request.get(`${URL_BASE}/api/routeros/${encodeURIComponent(DEVICE)}/nat`)
  ).json()
  const logged = table.rules?.find((r) => r.ordinal === 6)
  check(
    logged?.logPrefix === `N|${LOGGED_SLUG}|`,
    `the NAT endpoint serves the pushed log-prefix (${logged?.logPrefix}) -- the join the logged mode resolves through`,
  )
}

// --- Mode 1: not logged, so subtraction ---------------------------------

await openRowSheet(UNLOGGED_SRC)
{
  const text = await openSheetLookup()

  check(
    text.includes(`NAT table — ${DEVICE}`),
    'the unlogged mode is announced in the header, not left to be inferred',
  )
  check(
    (await page.textContent('.sheet .chip')).trim() === 'not logged',
    'the mode chip reads "not logged" -- text, so it survives any colour scheme',
  )
  check(
    text.includes('This translation was not logged, so no rule can be named'),
    'the first thing in the body states what cannot be known',
  )
  check(
    text.includes('Could have performed it — 2 of 7'),
    'the could-have count is stated against the whole table, never as an answer',
  )
  check(text.includes('Ruled out by this event — 5'), 'the ruled-out half is counted too')

  for (const reason of [
    'ruled out: chain dstnat, event is srcnat',
    'ruled out: protocol tcp ≠ udp',
    'ruled out: disabled',
    'ruled out: out-interface wlan1, event out ether1',
  ]) {
    check(text.includes(reason), `every exclusion shows its work: "${reason}"`)
  }

  check(
    text.includes('src-address=guest-hosts — not evaluable here'),
    'a condition mikroview cannot evaluate keeps its rule and is displayed -- unknown never excludes',
  )
  check(
    text.includes('“Could have” is not “did”'),
    'the footnote refuses the upgrade from could-have to did',
  )
  check(
    text.includes('log=yes log-prefix=') && text.includes('never touches the router'),
    'the footnote prints the command for the operator to run, per the observe-only rule',
  )
  check(
    text.includes('Evaluated against this row’s event.'),
    'the popup names the evidence it was evaluated against',
  )
  check(
    !text.includes('NAT rule — logged'),
    'the logged rendering never appears in the unlogged mode',
  )

  // The ruled-out entries are de-emphasised, not removed and not hidden:
  // dimming is what lets the operator audit the partition. Asserted as
  // real visibility rather than by counting nodes, because an author
  // `display:` declaration outranks the UA stylesheet's rule for
  // `hidden`, so a "hidden" element can render and a rendered one can be
  // marked hidden.
  const out = page.locator('.sheet .entry.out')
  check((await out.count()) === 5, `all five ruled-out rules are still rendered (${await out.count()})`)
  await out.first().waitFor({ state: 'visible', timeout: 5000 })
  check(true, 'ruled-out entries stay visible and readable rather than being dropped')

  // The row itself carries no rule decoration: a guess must never sit on
  // the row wearing the clothes of an exact resolution.
  const ruleCell = (await page.textContent('.grid .row .cell.rule')).trim()
  check(
    ruleCell === '—',
    `an unlogged NAT row gets no inline rule decoration (rule cell reads "${ruleCell}")`,
  )
}
await closeSheet()

// --- Mode 2: logged, so the rule is named -------------------------------

await page.fill(SOURCE_BOX, '')
await page.fill('input.rule', LOGGED_SLUG)
await page.waitForSelector(LOGGED_TRIGGER, { timeout: 20000 })
{
  const text = await openPopover(LOGGED_TRIGGER)

  check(text.includes('NAT rule — logged'), 'the logged mode is announced in the header')
  check(
    (await page.textContent('.popover .chip')).trim() === 'logged',
    'the mode chip reads "logged"',
  )
  check(
    text.includes('#6') && text.includes('port forward, logged'),
    'the logged translation resolves to the rule its prefix names, with its RouterOS ordinal',
  )
  check(
    text.includes('go look at rule 6 in RouterOS'),
    'the footnote keeps the RouterOS numbering line',
  )
  check(
    !text.includes('Could have performed it') && !text.includes('ruled out:'),
    'the subtraction rendering never appears in the logged mode -- the two never share a rendering',
  )
}
await closePopover()

// --- Hold while open ----------------------------------------------------
// Newest-at-top pushes rows down as events arrive, so a popover anchored
// to a row it is about would slide away from under itself.

await page.fill('input.rule', '')
await page.fill(SOURCE_BOX, '')
// The hold belongs to the anchored popover (#413) -- the sheet is modal
// and takes no hold -- and since #644 removed the untagged row trigger,
// the tagged row's rule-cell trigger is the popover's entry point here.
feedRaw(loggedLine)
await page.waitForSelector(LOGGED_TRIGGER, { timeout: 20000 })
await openPopover(LOGGED_TRIGGER)

feedSyslog(30, HOLD_SLUG)
await page.waitForTimeout(4000)
const heldRows = await page.locator(`.grid .row:has-text("${HOLD_SLUG}")`).count()
check(heldRows === 0, `the stream holds while the popup is open (${heldRows} new rows appeared)`)

await closePopover()
await page.locator(`.grid .row:has-text("${HOLD_SLUG}")`).first().waitFor({
  state: 'visible',
  timeout: 20000,
})
check(true, 'closing the popup releases the hold and the held events appear')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)

done()
