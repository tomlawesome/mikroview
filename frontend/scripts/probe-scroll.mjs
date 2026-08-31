// SPDX-License-Identifier: AGPL-3.0-only
//
// Diagnostic probe for #689 -- NOT a regression test, and not picked up
// by the scenario runner (deliberately not named live-*.mjs). Read-only:
// signs in and clicks the roll rail, never writes anything, so it is
// safe to run against a shared demo instance.
//
// Measures, per deck scene: document.scrollingElement's own scrollHeight
// against the viewport, the .deck's scrollHeight against the sum of its
// cards, and the active card's real content bottom -- so the actual
// culprit shows up in numbers rather than being guessed at.

import { chromium } from 'playwright'

const URL_BASE = process.env.MV_URL
const USER = process.env.MV_USER
const PASS = process.env.MV_PASS
if (!URL_BASE || !USER || !PASS) {
  console.error('MV_URL/MV_USER/MV_PASS unset -- source the demo credentials first')
  process.exit(2)
}

const browser = await chromium.launch()
const context = await browser.newContext({ colorScheme: 'dark', ignoreHTTPSErrors: true })
const page = await context.newPage()

await page.goto(URL_BASE, { waitUntil: 'networkidle' })
await page.fill('input[autocomplete="username"]', USER)
await page.fill('input[autocomplete="current-password"]', PASS)
await page.click('button[type="submit"]')
await page.waitForSelector('#main-content', { timeout: 15000 })
await page.setViewportSize({ width: 1280, height: 720 })

// Close the setup wizard if it auto-opened (first-run modal) -- harmless
// read of /api/devices, no writes.
const devices = await page.request.get(`${URL_BASE}/api/devices`).then((r) => r.json())
if (!(Array.isArray(devices) && devices.length > 0)) {
  const modal = page.locator('.setup-wizard')
  if (await modal.waitFor({ state: 'visible', timeout: 5000 }).then(() => true).catch(() => false)) {
    await page.keyboard.press('Escape')
    await modal.waitFor({ state: 'detached', timeout: 5000 }).catch(() => {})
  }
}

const rails = ['The fall', 'Topography', 'Metrics', 'Stream', 'The docket', 'Entities', 'Settings']

for (const name of rails) {
  const btn = page.locator(`.roll-rail .rail-name:text-is("${name}")`)
  if ((await btn.count()) === 0) {
    console.log(`${name}: SKIP (not in rail for this role)`)
    continue
  }
  await btn.click()
  // Let the smooth roll (~700ms) and any effects settle.
  await page.waitForTimeout(1000)

  const m = await page.evaluate(() => {
    const se = document.scrollingElement
    const deck = document.querySelector('.deck')
    const rail = document.querySelector('.roll-rail')
    const activeCard = document.querySelector('.card[aria-hidden="false"]')
    const cardRect = activeCard ? activeCard.getBoundingClientRect() : null
    const railRect = rail ? rail.getBoundingClientRect() : null
    return {
      doc: {
        scrollHeight: se.scrollHeight,
        clientHeight: se.clientHeight,
        scrollTop: se.scrollTop,
      },
      bodyOverflowY: getComputedStyle(document.body).overflowY,
      htmlOverflowY: getComputedStyle(document.documentElement).overflowY,
      deck: deck
        ? {
            scrollHeight: deck.scrollHeight,
            clientHeight: deck.clientHeight,
            scrollTop: deck.scrollTop,
            cardCount: deck.querySelectorAll(':scope > .card').length,
          }
        : null,
      cardBottom: cardRect ? cardRect.bottom : null,
      cardTop: cardRect ? cardRect.top : null,
      innerHeight: window.innerHeight,
      railPosition: rail ? getComputedStyle(rail).position : null,
      railTop: railRect ? railRect.top : null,
      railBottom: railRect ? railRect.bottom : null,
      railHeight: railRect ? railRect.height : null,
    }
  })
  console.log(`${name}: ${JSON.stringify(m)}`)
}

await browser.close()
