// SPDX-License-Identifier: AGPL-3.0-only
//
// #442: a router declared under one address whose logs arrive from
// another. RouterOS is multi-homed by definition, and syslog is stamped
// with whichever interface faces mikroview -- so the declared device
// sits silent, the real stream auto-discovers as a second device, and a
// token minted for the declared identity enriches nothing. The wizard's
// step 2 reads this as partial, states both facts, and prints the
// remedy with the operator's values; the router cards carry a one-line
// echo pointing back at it.
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

const { page, consoleErrors } = await session()

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

// --- The wizard: step 2 ----------------------------------------------------
await goTo(page, 'Run setup…')
const wizard = page.locator('.setup-wizard')
await wizard.waitFor({ state: 'visible' })
await page.locator('.setup-wizard .steps li:nth-child(2) .step-row').click()

// The split reads as partial: evidence arrived, composed wrongly. That
// is the arrived voice, never attention (nothing on mikroview's side
// is wrong) and never waiting (something did arrive).
const observation = page.locator('.setup-wizard .observation')
await page.locator('.setup-wizard .observation', { hasText: 'you declared in config.yaml' }).waitFor({ state: 'visible', timeout: 15000 })
const detail = ((await observation.textContent()) ?? '').replace(/\s+/g, ' ').trim()
check(
  detail ===
    "Connected — but from 10.0.20.1, an address you haven't declared, while 192.168.88.1, which you declared in config.yaml, has sent nothing.",
  `step 2 states what was declared and what arrived, no diagnosis (${detail})`,
)
check(await observation.evaluate((el) => el.classList.contains('arrived')), 'the split reads in the arrived voice')
check(
  (await page.locator('.setup-wizard .observation.attention').count()) === 0,
  "a split is not a problem on mikroview's own side, so it never reads as attention",
)

const body = ((await page.locator('.setup-wizard .split').textContent()) ?? '').replace(/\s+/g, ' ')
check(/MikroView can't tell whether these are the same router/.test(body), 'the body hands the operator the one fact only they hold')
check(/Keep 192\.168\.88\.1 \(recommended\)/.test(body), 'keeping the declared address is the recommended remedy')
check(/Or keep 10\.0\.20\.1: change sourceIp to 10\.0\.20\.1/.test(body), 'changing sourceIp is offered as the alternative')
check(/If they are two different routers, nothing is wrong\./.test(body), 'two routers is a non-error')

// The command is printed, never run, with the declared address filled
// in -- the wizard never renders a placeholder.
const commands = await page.$$eval('.setup-wizard .split pre', (els) => els.map((e) => e.textContent.trim()))
check(
  commands.length === 1 && commands[0] === '/system logging action set mikroview src-address=192.168.88.1',
  `the src-address command carries the declared address (${JSON.stringify(commands)})`,
)
check((await page.locator('.setup-wizard .split button.copy').count()) === 1, 'the command has its Copy control')

// The step list carries the split as the receipt.
const receipt = ((await page.locator('.setup-wizard .steps li:nth-child(2) .step-receipt').textContent()) ?? '').trim()
check(receipt === 'syslog from 10.0.20.1 · declared 192.168.88.1 silent', `the receipt names both sides (${receipt})`)

// Partial is evidence, so Next proceeds without the heavy warning.
await page.click('.setup-wizard footer button.primary')
check((await page.locator('.setup-wizard .heavy').count()) === 0, 'Next proceeds: the split is evidence, not silence')
await page.click('.setup-wizard header button.close')
await wizard.waitFor({ state: 'detached' })

// --- The router cards: one echo, pointing at step 2 ------------------------
await goTo(page, 'Entities')
const echo = page.locator('.fcard', { hasText: 'Declared as 192.168.88.1, nothing arrived.' })
await echo.waitFor({ state: 'visible', timeout: 15000 })
const echoText = ((await echo.textContent()) ?? '').replace(/\s+/g, ' ')
check(
  /If 10\.0\.20\.1 below is the same router on another of its addresses, Run setup… step 2 shows the one-line fix\./.test(echoText),
  `the configured-silent card points at step 2 (${echoText})`,
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
