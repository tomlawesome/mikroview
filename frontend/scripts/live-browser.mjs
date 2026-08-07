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
 * knownIssue records an observation that is currently failing, without
 * failing the run.
 *
 * For a defect this scenario found that is filed but not yet fixed. The
 * alternative -- deleting the assertion until someone gets to it -- is
 * how the knowledge gets lost, which is the thing this whole fixture
 * exists to prevent. Always cite the issue.
 */
export function knownIssue(ok, message, issue) {
  console.log(`  ${ok ? 'ok  ' : 'KNOWN'} ${message}${ok ? ' (fixed -- promote to check() and close ' + issue + ')' : ' -- ' + issue}`)
}

/** feedSyslog pushes synthetic events into the running instance. */
export function feedSyslog(n, label = 'live-test-rule') {
  execFileSync(path.join(REPO, 'scripts/live-env.sh'), ['syslog', String(n), label], {
    stdio: 'ignore',
    cwd: REPO,
  })
}

/** session launches a browser and signs in, returning a live page. */
export async function session({ waitForEvents = 0 } = {}) {
  browser = await chromium.launch()
  const page = await browser.newPage()
  const consoleErrors = []
  page.on('pageerror', (e) => consoleErrors.push(String(e)))
  page.on('console', (m) => {
    if (m.type() === 'error') consoleErrors.push(m.text())
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
