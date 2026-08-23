// SPDX-License-Identifier: AGPL-3.0-only
//
// The guided setup wizard (#320) against a real running mikroview.
//
// The unit tests cover command generation and the step rules. What they
// cannot show is the part that matters most: that the wizard's claims
// track what the server actually observed. A wizard that says "step 3
// done" when it isn't is worse than no wizard -- so every assertion
// here goes through a real browser reading a real /api/setup/status.

import { session, check, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session({ waitForEvents: 20 })

await page.click('.nav-menu .trigger')
await page.click('.nav-menu button:has-text("Connect a router")')
await page.waitForSelector('.setup')

// --- Commands carry real values, never placeholders ---------------------
const blocks = await page.$$eval('.setup pre', (els) => els.map((e) => e.textContent ?? ''))
check(blocks.length > 0, 'the wizard renders command blocks')

const host = new URL(URL_BASE).host
const withPlaceholders = blocks.filter((b) => /<[a-z-]+>/.test(b) && !b.includes('<paste the script above>'))
check(
  withPlaceholders.length === 0,
  `no block still contains a placeholder (${withPlaceholders.length} did)`,
)
check(
  blocks.some((b) => b.includes(`https://${host}/ca.crt`)),
  `the CA fetch names this instance (${host})`,
)

// The syslog port comes from the running config, not an assumed 6514 --
// live-env.sh uses a non-default port, so this would fail if it were
// hard-coded.
const status = await page.request.get(`${URL_BASE}/api/setup/status`).then((r) => r.json())
const syslogPort = status.instance.syslogPort.split(':').pop()
check(
  blocks.some((b) => b.includes(`remote-port=${syslogPort}`)),
  `the syslog command uses this instance's port (${syslogPort})`,
)

// --- Step status reflects what the server observed ----------------------
// The harness has already fed events, so events-arriving must be true,
// and those events carry log-prefixes, so actions decode.
const deviceObs = status.devices.find((d) => d.events > 0)
check(!!deviceObs, `the server observed events (${JSON.stringify(status.devices)})`)
check(
  !!deviceObs && deviceObs.decodedActions > 0,
  'the server observed events carrying a decoded action',
)

const states = await page.$$eval('.setup .state', (els) =>
  els.map((e) => ({ cls: e.className, text: e.textContent ?? '' })),
)

// Which state the rule-tagging step must show is derived from what the
// server reports, not hard-coded to "done".
//
// Every scenario shares one instance, so whether *all* of its events
// happen to carry a log-prefix depends on which other scenarios have
// run -- live-action-classification.mjs deliberately feeds untagged
// lines, which is a state a real router produces and which correctly
// makes this step "partial". Hard-coding "done" was asserting a
// property of the harness rather than of the wizard.
//
// Deriving it is the stronger check anyway: it says the wizard agrees
// with /api/setup/status, rather than that the instance is fully tagged.
const withEvents = status.devices.filter((d) => d.events > 0)
const totalEvents = withEvents.reduce((n, d) => n + d.events, 0)
const totalDecoded = withEvents.reduce((n, d) => n + d.decodedActions, 0)
const expectedRuleState = totalDecoded === totalEvents ? 'done' : 'partial'
check(
  states.some((s) => s.cls.includes(expectedRuleState) && /events/.test(s.text)),
  `the rule-tagging step reports ${expectedRuleState}, matching ${totalDecoded}/${totalEvents} decoded ` +
    `(${JSON.stringify(states.map((s) => s.cls))})`,
)

// The push step must NOT claim success -- nothing has pushed yet in this
// scenario, and a wizard that reports a step it cannot see is the whole
// failure mode being guarded against.
const pushState = states.find((s) => /push|table/i.test(s.text))
check(
  !pushState || !pushState.cls.includes('done'),
  `the push step does not claim success before anything pushed (${pushState?.text})`,
)

// --- Minting a token produces a script that actually works --------------
await page.selectOption('.mint select', deviceObs.device)
await page.click('.mint button.primary')
await page.waitForSelector('pre.script')
const script = (await page.textContent('pre.script')) ?? ''

// Stop at the closing quote: \S+ swallows it, and a token with a
// trailing " authenticates as nothing (401) -- which looked like a
// product bug on first run and was this line.
const tokenMatch = script.match(/Bearer ([^"\s)]+)/)
check(!!tokenMatch, 'the generated script embeds a bearer token')
const token = tokenMatch?.[1] ?? ''

// Every kind the server declares must appear in the script.
for (const kind of status.pushKinds) {
  check(script.includes(`"kind"="${kind}"`), `the script pushes ${kind}`)
}

// The proof: the token the wizard minted, used the way the script uses
// it, is accepted.
const pushed = await fetch(`${URL_BASE}/api/ingest/routeros`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
  body: JSON.stringify({
    // arp, not filter-rule: scenarios share one instance and an earlier
    // one has already pushed filter rules, so waiting for those to
    // appear would pass whether or not this push worked at all.
    kind: 'arp',
    page: 1,
    pages: 1,
    records: [{ address: '192.0.2.77', mac: 'aa:bb:cc:dd:ee:77' }],
  }),
})
check(pushed.status === 200, `the wizard-minted token is accepted for a push (${pushed.status})`)

// And the wizard notices, without a reload.
await page.waitForFunction(
  () => {
    const els = Array.from(document.querySelectorAll('.setup .state'))
    return els.some((e) => /arp/.test(e.textContent ?? ''))
  },
  { timeout: 15000 },
)
check(true, 'the push step updates itself once a table arrives')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
