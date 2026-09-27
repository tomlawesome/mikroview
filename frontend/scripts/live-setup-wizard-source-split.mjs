// SPDX-License-Identifier: AGPL-3.0-only
//
// #442: a router declared under one address whose logs arrive from
// another. RouterOS is multi-homed by definition, and syslog is stamped
// with whichever interface faces mikroview -- so the declared device
// sits silent, the real stream auto-discovers as a second device, and a
// token minted for the declared identity enriches nothing. The wizard's
// Send logs step reads this as partial, states both facts, and prints
// the remedy with the operator's values; the router cards carry a
// one-line echo pointing back at it.
//
// #1284 inserted a Name your router step ahead of it in the first-run
// ledger (SETUP_STEPS is now ['ca', 'name', 'syslog', ...]), so Send
// logs is the ledger's third pane now, not its second -- this drives
// into `li:nth-child(3)` below for that reason. The card's own echo
// named "step 2" until that same insertion made it point at the wrong
// step; it names Send logs now, which cannot drift again.
//
// live-env.sh's harness declares one router (live-router, 127.0.0.1)
// and feeds it from that same address, so the real registry never
// pairs anything: there is no second loopback source to declare, and
// declaring one would make devices[0] nondeterministic for every other
// scenario. So, as live-setup-wizard-tls-off-cert-mismatch.mjs does for
// its own unreachable shape, this drives the real bundled components
// against the real server's actual /api/devices response with only the
// shape under test overridden: the declared router made silent and
// paired, and one undeclared device streaming. The server's own half --
// that it emits multihomedCandidates exactly so -- is pinned by
// TestHandleDevicesReportsMultihomedCandidates in internal/api.

import { session, check, done, goTo } from './live-browser.mjs'

const { page, consoleErrors } = await session({ mocksApi: true })

const real = await page.request.get(`${process.env.MV_URL}/api/devices`).then((r) => r.json())
const declared = real.devices.find((d) => d.configured)
check(!!declared, `the harness declares a router (${JSON.stringify(real.devices.map((d) => d.id))})`)

await page.route('**/api/devices', async (route) => {
  const body = {
    devices: [
      {
        ...declared,
        sourceIp: '192.168.88.1',
        eventCount: 0,
        lastSeen: '0001-01-01T00:00:00Z',
        status: 'never_seen',
        multihomedCandidates: ['10.0.20.1'],
      },
      {
        id: '10.0.20.1',
        name: '10.0.20.1',
        sourceIp: '10.0.20.1',
        configured: false,
        firstSeen: new Date(Date.now() - 60_000).toISOString(),
        lastSeen: new Date().toISOString(),
        eventCount: 40,
        status: 'live',
      },
    ],
  }
  await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
})

// --- The wizard: Send logs (ledger position 3) -----------------------------
await goTo(page, 'Run setup…')
const wizard = page.locator('.setup-wizard')
await wizard.waitFor({ state: 'visible' })
await page.locator('.setup-wizard .steps li:nth-child(3) .step-row').click()

// The split reads as partial: evidence arrived, composed wrongly. That
// is the arrived voice, never attention (nothing on mikroview's side
// is wrong) and never waiting (something did arrive). Two boxes since
// #1132 -- what arrived, and the declared address that is silent
// beside it in the warning register.
const observation = page.locator('.setup-wizard .observation:not(.shortfall)')
const shortfall = page.locator('.setup-wizard .observation.shortfall')
await shortfall.waitFor({ state: 'visible', timeout: 15000 })
const detail = ((await observation.textContent()) ?? '').replace(/\s+/g, ' ').trim()
const silent = ((await shortfall.textContent()) ?? '').replace(/\s+/g, ' ').trim()
check(
  detail === "Connected — but from 10.0.20.1, an address you haven't declared.",
  `the Send logs step states what arrived, no diagnosis (${detail})`,
)
check(
  silent === '192.168.88.1, which you declared in config.yaml, has sent nothing.',
  `and what was declared but silent, in its own box (${silent})`,
)
check(await observation.evaluate((el) => el.classList.contains('arrived')), 'the split reads in the arrived voice')
check(
  (await page.locator('.setup-wizard .observation.attention').count()) === 0,
  "a split is not a problem on mikroview's own side, so it never reads as attention",
)

const body = ((await page.locator('.setup-wizard .split').textContent()) ?? '').replace(/\s+/g, ' ')
check(/MikroView can't tell whether these are the same router/.test(body), 'the body hands the operator the one fact only they hold')
check(/tell MikroView the address it's actually using/.test(body), 'the remedy is telling mikroview the arriving address, not pinning the router (#1370)')
check(/Change sourceIp to 10\.0\.20\.1 in config\.yaml and restart/.test(body), 'changing sourceIp to the arriving address is the one path offered')
check(/If they are two different routers, nothing is wrong\./.test(body), 'two routers is a non-error')

// No src-address command is printed any more (#1370): the Send logs block
// always sets src-address=0.0.0.0, so a pin would not survive a re-paste.
check((await page.locator('.setup-wizard .split pre').count()) === 0, 'no src-address command is printed')
check((await page.locator('.setup-wizard .split button.copy').count()) === 0, 'and so there is nothing to copy')

// The step list carries the split as the receipt.
const receipt = ((await page.locator('.setup-wizard .steps li:nth-child(3) .step-receipt').textContent()) ?? '').trim()
check(receipt === 'syslog from 10.0.20.1 · declared 192.168.88.1 silent', `the receipt names both sides (${receipt})`)

// Partial is evidence, so Next proceeds without the heavy warning.
await page.click('.setup-wizard footer button.primary')
check((await page.locator('.setup-wizard .heavy').count()) === 0, 'Next proceeds: the split is evidence, not silence')
await page.click('.setup-wizard header button.close')
await wizard.waitFor({ state: 'detached' })

// --- The router cards: one echo, pointing back at the ledger ---------------
await goTo(page, 'Entities')
const echo = page.locator('.fcard', { hasText: 'Declared as 192.168.88.1, nothing arrived.' })
await echo.waitFor({ state: 'visible', timeout: 15000 })
const echoText = ((await echo.textContent()) ?? '').replace(/\s+/g, ' ')
check(
  /If 10\.0\.20\.1 below is the same router on another of its addresses, Run setup… ▸ Send logs shows the one-line fix\./.test(echoText),
  `the configured-silent card points back at the ledger (${echoText})`,
)
check(
  (await page.locator('.fcard', { hasText: 'Declared as' }).count()) === 1,
  'only the configured-silent card carries the echo',
)
check(
  (await page.locator('.fcard pre').count()) === 0,
  'the card prints no command: the wizard owns the remedy',
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
