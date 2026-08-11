// SPDX-License-Identifier: AGPL-3.0-only
//
// Drives a real mikroview in a real browser. Companion to live-env.sh.
//
// Import the helpers from a per-change scenario script rather than
// editing this file: the point is that each PR gets its own short
// scenario, not that this grows into a second test suite.
//
//   import { session, feedSyslog, check, done } from './live-browser.mjs'
//   const { page } = await session()
//   ...
//   done()

import { chromium } from 'playwright'
import { execFileSync } from 'child_process'
import { setGlobalDispatcher, Agent } from 'undici'
import { fileURLToPath } from 'url'
import path from 'path'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..')

const URL_BASE = process.env.MV_URL
const USER = process.env.MV_USER
const PASS = process.env.MV_PASS
if (!URL_BASE) {
  console.error('MV_URL unset -- run: eval "$(scripts/live-env.sh up)"')
  process.exit(2)
}

let failed = false
let browser

/** check records a failure without aborting, so one run reports everything. */
export function check(ok, message) {
  console.log(`  ${ok ? 'ok  ' : 'FAIL'} ${message}`)
  if (!ok) failed = true
}

/**
 * ENV_SCRIPT is the harness driving the instance under test.
 *
 * live-env.sh (a locally built binary) is the default and stays the
 * everyday path. MV_ENV_SCRIPT points the same scenarios at
 * live-container.sh instead, which runs the image as it ships, under the
 * hardening it ships with, optionally against Postgres (#273 slice 1).
 *
 * Selecting the environment here rather than in each scenario is the
 * whole point: a scenario that has to know which environment it is in
 * would drift between the two, and the value of running them against the
 * container is that they are *the same scenarios*.
 */
const ENV_SCRIPT = path.join(REPO, process.env.MV_ENV_SCRIPT || 'scripts/live-env.sh')

// Node's own fetch has to accept the self-signed certificate too.
//
// Scenarios reach the API two ways: through Playwright's page.request
// (which inherits the browser context's ignoreHTTPSErrors) and through
// bare fetch (which does not, and failed with
// UNABLE_TO_VERIFY_LEAF_SIGNATURE against the container). Setting the
// dispatcher once here covers every scenario rather than making five of
// them each remember; doing it per scenario is how one gets missed.
//
// Deliberately not NODE_TLS_REJECT_UNAUTHORIZED=0, which would disable
// verification for the whole process including anything else it talks
// to. This is scoped to fetch, in a harness, against a certificate the
// server under test generated for itself moments earlier. Under
// live-env.sh (plain HTTP on loopback) it changes nothing.
setGlobalDispatcher(new Agent({ connect: { rejectUnauthorized: false } }))

/** feedSyslog pushes synthetic events into the running instance. */
export function feedSyslog(n, label = 'live-test-rule') {
  execFileSync(ENV_SCRIPT, ['syslog', String(n), label], {
    stdio: 'ignore',
    cwd: REPO,
  })
}

/**
 * feedRaw delivers one exact syslog line, for a scenario needing a
 * specific event shape rather than feedSyslog's bulk pattern.
 *
 * Go through this rather than opening a socket in the scenario: syslog
 * TLS is the only listener since #189 removed the plaintext ones, so a
 * hand-rolled UDP send delivers nothing at all -- silently, since there
 * is no longer anything bound to refuse it.
 */
export function feedRaw(line) {
  execFileSync(ENV_SCRIPT, ['raw', line], {
    stdio: 'ignore',
    cwd: REPO,
  })
}

/**
 * feedPortScan delivers n distinct destination ports from one source
 * inside the port-scan window, so a real port_scan flag is *raised* by
 * the detector rather than synthesized by the test.
 *
 * Lives here rather than being copied per scenario: six scenarios had
 * their own identical version, each with the harness path hardcoded, so
 * pointing them at a different environment meant editing six files and
 * missing one was silent.
 */
export function feedPortScan(n, sourceIp) {
  const args = ['portscan', String(n)]
  if (sourceIp) args.push(sourceIp)
  execFileSync(ENV_SCRIPT, args, { stdio: 'ignore', cwd: REPO })
}

/**
 * isUntrustedCertServiceWorkerError filters out the one console error a
 * browser always produces against a self-signed certificate it has not
 * been told to trust.
 *
 * ignoreHTTPSErrors suppresses the interstitial for navigation and
 * fetches, but service-worker registration is deliberately stricter:
 * Chromium refuses to register one over a certificate outside its trust
 * store regardless, so mikroview's PWA registration fails with
 * `SecurityError: Failed to register a ServiceWorker ... An SSL
 * certificate error occurred when fetching the script.`
 *
 * That is browser policy, not a mikroview defect, and it is narrowly
 * matched here (both the SSL and service-worker halves must be present)
 * so a genuine service-worker error is still reported.
 *
 * It has an operator-facing consequence, which is documented in
 * docs/configuration.md rather than only filtered away here: with
 * mikroview's generated certificate, the install-as-an-app and offline
 * behaviour do not work until the CA it serves at /ca.crt is trusted by
 * the browser. Everything else works with the usual click-through.
 */
// Two messages, not one: the SecurityError from the registration call,
// and a bare resource-load error for the script fetch that does not
// mention the service worker at all. Both are matched, and both are
// specific to a *script* fetch failing certificate validation, so an
// ordinary failed request still surfaces.
function isUntrustedCertServiceWorkerError(text) {
  if (!/SSL certificate error/i.test(text)) return false
  return /ServiceWorker/i.test(text) || /fetching the script/i.test(text)
}

/** session launches a browser and signs in, returning a live page. */
export async function session({ waitForEvents = 0 } = {}) {
  browser = await chromium.launch()
  // ignoreHTTPSErrors, because the certificate under test is one
  // mikroview generated for itself seconds ago -- self-signed, with no
  // chain to verify against.
  //
  // Needed only since the container environment arrived: live-env.sh
  // serves plain HTTP on loopback, so this never came up, and all
  // fifteen scenarios failed at page.goto with ERR_CERT_AUTHORITY_INVALID
  // the first time they were pointed at the image, which serves HTTPS as
  // it ships. Accepting the certificate is right here and is not a hole:
  // whether a *router* should trust it is a different question, and one
  // live-routeros.sh's `trust` step covers properly against real
  // RouterOS rather than by waving it through.
  const page = await browser.newPage({ ignoreHTTPSErrors: true })
  const consoleErrors = []
  const record = (text) => {
    if (isUntrustedCertServiceWorkerError(text)) return
    consoleErrors.push(text)
  }
  page.on('pageerror', (e) => record(String(e)))
  page.on('console', (m) => {
    if (m.type() === 'error') record(m.text())
  })

  await page.goto(URL_BASE, { waitUntil: 'networkidle' })
  await page.fill('input[autocomplete="username"]', USER)
  await page.fill('input[autocomplete="current-password"]', PASS)
  await page.click('button[type="submit"]')
  await page.waitForSelector('input.rule', { timeout: 15000 })

  if (waitForEvents > 0) {
    await page.waitForFunction(
      (n) => document.querySelectorAll('.row').length >= n,
      waitForEvents,
      { timeout: 20000 },
    )
  }
  return { page, consoleErrors }
}

/**
 * responsive asserts the main thread still answers -- a hung tab cannot
 * run an evaluate at all, so a timeout here is the failure.
 */
export async function responsive(page, forMs = 2000) {
  const deadline = Date.now() + forMs
  while (Date.now() < deadline) {
    try {
      await page.evaluate(() => document.title, { timeout: 1500 })
    } catch {
      return false
    }
    await page.waitForTimeout(200)
  }
  return true
}

export function done() {
  browser?.close()
  console.log(failed ? 'RESULT: FAIL' : 'RESULT: PASS')
  process.exit(failed ? 1 : 0)
}
