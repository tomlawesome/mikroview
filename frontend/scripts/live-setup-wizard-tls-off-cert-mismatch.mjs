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
// pushKinds) is left exactly as the real server reported it.

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
await page.waitForSelector('.setup')

// The mismatch must surface as blocked, not be waved through because
// HTTP TLS happens to be off.
await page.waitForSelector('.setup section.blocked .state.blocked')
const detail = await page.textContent('.setup section.blocked .state.blocked')
check(!!detail && /does not cover/.test(detail), `step 1 reports the mismatch (${detail})`)
check(!!detail && /name verification failed/.test(detail), 'the detail names the failure the router will show')

// And the command block for step 1 is withheld while blocked -- a
// router told to run it would get a certificate it cannot verify.
const commandVisible = await page.$('.setup section.blocked pre')
check(!commandVisible, 'the CA-trust command is withheld while step 1 is blocked')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
