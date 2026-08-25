// SPDX-License-Identifier: AGPL-3.0-only
//
// #374: tls.enabled=false only turns HTTPS off on the API port. The
// syslog TLS listener still presents a certificate whose SANs come
// from tls.hosts, so a router that reaches mikroview by an address the
// certificate does not name still fails its handshake -- exactly as it
// would with HTTP TLS on. Step 0 of the wizard exists to catch this
// before the router ever tries, and before this fix it silently
// reported no problem whenever tls.enabled was false, regardless of
// tls.hosts.
//
// live-env.sh's loopback harness already runs with tls.enabled=false
// and syslog TLS on (the exact deployment shape from the bug report),
// but its certificate falls back to covering localhost/127.0.0.1, and
// the browser reaches it at 127.0.0.1 -- so the address the harness
// exercises is, correctly, covered. There is no knob to make the
// harness generate a certificate that does NOT cover the address in
// use without standing up a second instance, so this scenario drives
// the real bundled wizard component against the real server's actual
// /api/setup/status response, with only the two fields the bug is
// about (tlsEnabled, hosts) overridden to the mismatched shape from the
// issue -- everything else (sources, devices, syslogPort, syslogEnabled,
// pushKinds, marks) is left exactly as the real server reported it.
//
// #487 moved the wizard into a modal without touching this check logic,
// which it inherits. What changed here is only where the answer is
// read: the step's observation line, in the flavour reserved for a
// problem on mikroview's own side. That flavour exists precisely so a
// mikroview-side misconfiguration never borrows the patient,
// nothing-is-wrong voice the waiting flavour is required to use.

import { session, check, done } from './live-browser.mjs'

const { page, consoleErrors } = await session({ waitForEvents: 20 })

// Capture what the real server actually reports before intercepting,
// so only the fields the bug depends on get overridden.
const real = await page.request.get(`${process.env.MV_URL}/api/setup/status`).then((r) => r.json())
check(real.instance.tlsEnabled === false, `harness runs with tls.enabled=false (${real.instance.tlsEnabled})`)
check(real.instance.syslogEnabled === true, `harness runs with syslog TLS on (${real.instance.syslogEnabled})`)

await page.route('**/api/setup/status', async (route) => {
  const body = {
    ...real,
    instance: { ...real.instance, hosts: ['unrelated.example.invalid'] },
  }
  await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
})

await page.click('.rail .item:has-text("Run setup…")')
const wizard = page.locator('.setup-wizard')
await wizard.waitFor({ state: 'visible' })

// Step 1 explicitly, rather than relying on where the ledger reopens:
// the modal lands on the first step still waiting, and a scenario that
// ran before this one could have decided step 1 already.
await page.locator('.setup-wizard .steps li:nth-child(1) .step-row').click()

// The mismatch must surface as a mikroview-side problem, not be waved
// through because HTTP TLS happens to be off -- and not dressed up as
// patient waiting either.
const observation = page.locator('.setup-wizard .observation.attention')
await observation.waitFor({ state: 'visible' })
const detail = await observation.textContent()
check(!!detail && /does not cover/.test(detail), `step 1 reports the mismatch (${detail})`)
check(!!detail && /name verification failed/.test(detail), 'the detail names the failure the router will show')
check(
  (await page.locator('.setup-wizard .observation.waiting').count()) === 0,
  "a problem on mikroview's own side never reads as waiting for the router",
)

// And the command block for step 1 is withheld while blocked -- a
// router told to run it would get a certificate it cannot verify.
check(
  (await page.locator('.setup-wizard .body pre').count()) === 0,
  'the CA-trust command is withheld while step 1 is blocked',
)

// Next must not walk past it either. There is nothing to force past
// here: the check could not run, so the record says that rather than
// claiming nothing has arrived.
await page.click('.setup-wizard footer button.primary')
const heavy = page.locator('.setup-wizard .heavy')
await heavy.waitFor({ state: 'visible' })
const quoted = ((await page.textContent('.setup-wizard .heavy .quote')) ?? '').trim()
check(
  /could not run/.test(quoted),
  `forcing past a blocked step records that the check could not run, not that nothing arrived (${quoted})`,
)
await page.click('.setup-wizard .heavy button.primary')
await heavy.waitFor({ state: 'detached' })

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
