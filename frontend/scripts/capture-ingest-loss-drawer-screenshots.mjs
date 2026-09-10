// SPDX-License-Identifier: AGPL-3.0-only
//
// One-off dev tool, not a live-check scenario -- deliberately named
// outside the `live-*.mjs` glob so scripts/run-scenarios.sh never picks
// it up, the same reasoning capture-live-view-screenshots.mjs states
// for itself.
//
// Renders IngestLossDrawer.svelte at one row and at five, open and
// closed, for the owner to ratify from (#1015: "the owner ratifies from
// renders, not markup"). live-ingest-loss-drawer.mjs deliberately raises
// only one real condition (`oversized`) rather than all five at once --
// four of the five loss counters need genuinely different, sometimes
// racy real-world conditions (the ingest queue actually overrunning, the
// per-source connection limit actually hit, ...), which a live-check
// scenario should not gamble on simultaneously without risking a flake
// that has nothing to do with the thing under test. This script instead
// mocks GET /api/stats' response (Playwright route interception, not a
// code change) to add the `loss` field the drawer reads, and injects one
// extra WebSocket frame the same way a real server's hub would report a
// wsDropped increase -- so all five rows can be shown together on demand,
// deterministically, for a design screenshot. Nothing in src/ is touched
// to make this possible.
//
// Also measures, for every deck scene, whether the closed handle's
// 72x24 hit area (IngestLossDrawer.svelte's `.handle`) overlaps any
// other clickable element -- the check issue #1015 asks the builder to
// make by hand ("check every scene bar for content at its horizontal
// centre").
//
// Usage:
//   eval "$(scripts/live-env.sh up)"
//   cd frontend && node scripts/capture-ingest-loss-drawer-screenshots.mjs
//   scripts/live-env.sh down   (from the repo root)

import { launchBrowser, dismissSetupWizard, goTo } from './live-browser.mjs'
import { mkdirSync } from 'fs'
import path from 'path'

const URL_BASE = process.env.MV_URL
const USER = process.env.MV_USER
const PASS = process.env.MV_PASS
if (!URL_BASE || !USER || !PASS) {
  console.error('MV_URL/MV_USER/MV_PASS unset -- run: eval "$(scripts/live-env.sh up)"')
  process.exit(2)
}

const OUT_DIR = process.env.OUT_DIR || '/tmp/ingest-loss-drawer-screenshots'
mkdirSync(OUT_DIR, { recursive: true })

const NOW = () => new Date().toISOString()

// The four server-side rows the mocked `loss` block can carry. wsDropped
// (the fifth, info-tier row) is not part of this block -- it comes from
// the WS injection below, the same as a real client would compute it.
function lossBlockFor(profile) {
  if (profile === 'one') {
    return {
      dropped: { recent: 0, lastAt: null, active: false },
      rejectedConfigured: { recent: 0, lastAt: null, active: false, hosts: [] },
      rejected: { recent: 0, lastAt: null, active: false },
      oversized: { recent: 7, lastAt: NOW(), active: true, host: '10.20.3.9' },
    }
  }
  if (profile === 'four') {
    return {
      dropped: { recent: 812, lastAt: NOW(), active: true },
      rejectedConfigured: { recent: 43, lastAt: NOW(), active: true, hosts: ['branch-e4a1'] },
      rejected: { recent: 2867, lastAt: NOW(), active: true },
      oversized: { recent: 7, lastAt: NOW(), active: true, host: '10.20.3.9' },
    }
  }
  return undefined
}

let lossProfile = 'none'

const browser = await launchBrowser()
const page = await browser.newPage({ ignoreHTTPSErrors: true })

// --- mock GET /api/stats' `loss` field --------------------------------
await page.route('**/api/stats', async (route) => {
  const response = await route.fetch()
  const body = await response.json()
  const loss = lossBlockFor(lossProfile)
  if (loss && body.syslog) body.syslog = { ...body.syslog, loss }
  await route.fulfill({ response, json: body })
})

// --- pass the real WebSocket through, keeping a handle to inject one
// extra synthetic frame on demand (the wsDropped row) --------------------
let clientWs = null
await page.routeWebSocket(/\/api\/ws$/, (ws) => {
  clientWs = ws
  ws.connectToServer()
})

await page.goto(URL_BASE, { waitUntil: 'networkidle' })
await page.fill('input[autocomplete="username"]', USER)
await page.fill('input[autocomplete="current-password"]', PASS)
await page.click('button[type="submit"]')
await page.waitForSelector('#main-content', { timeout: 15000 })
await dismissSetupWizard(page)
await goTo(page, 'Stream')

async function shoot(name) {
  const file = path.join(OUT_DIR, `${name}.png`)
  await page.screenshot({ path: file })
  console.log(`wrote ${file}`)
}

async function reloadWithProfile(profile) {
  lossProfile = profile
  clientWs = null
  await page.reload({ waitUntil: 'networkidle' })
  await page.waitForSelector('#main-content', { timeout: 15000 })
}

// --- one row, open and closed -------------------------------------------
await reloadWithProfile('one')
await page.waitForSelector('#ingest-loss-drawer')
await page.waitForFunction(() => document.querySelectorAll('#ingest-loss-drawer .banner').length === 1)
await shoot('one-row-open')

await page.click('[data-testid="ingest-loss-handle"]')
await page.waitForSelector('#ingest-loss-drawer.closed')
await page.waitForTimeout(300) // let the 180ms close transition finish before shooting
await shoot('one-row-closed')

// --- five rows (four mocked server rows + one injected wsDropped frame),
// open and closed ----------------------------------------------------------
await reloadWithProfile('four')
// Wait for the fresh WS connection this reload opened, then inject the
// dropped frame the way ws.go's own hub would -- see ws.ts's onmessage,
// which reads msg.dropped as this connection's cumulative total.
for (let i = 0; i < 50 && !clientWs; i++) await page.waitForTimeout(100)
if (!clientWs) throw new Error('WebSocket route never connected after reload')
clientWs.send(JSON.stringify({ type: 'events', events: [], dropped: 156 }))

await page.waitForSelector('#ingest-loss-drawer')
await page.waitForFunction(() => document.querySelectorAll('#ingest-loss-drawer .banner').length === 5)
await shoot('five-rows-open')

await page.click('[data-testid="ingest-loss-handle"]')
await page.waitForSelector('#ingest-loss-drawer.closed')
await page.waitForTimeout(300) // let the 180ms close transition finish before shooting
await shoot('five-rows-closed')

// --- scene-bar hit-area overlap check (#1015: "check every scene bar
// for content at its horizontal centre and report any the hit area
// would cover") -------------------------------------------------------
const SCENES = ['The fall', 'Topography', 'Metrics', 'Stream', 'Flags', 'Watchlist', 'Audit log', 'Entities', 'Settings']

async function checkOverlaps(label) {
  const overlaps = await page.evaluate(() => {
    const handle = document.querySelector('[data-testid="ingest-loss-handle"]')
    if (!handle) return { error: 'no handle -- drawer not closed or not present' }
    const h = handle.getBoundingClientRect()
    const candidates = document.querySelectorAll(
      'button, a, input, select, textarea, [role="button"], [role="tab"], [role="group"]',
    )
    const hits = []
    for (const el of candidates) {
      if (el === handle || el.closest('#ingest-loss-drawer')) continue
      const r = el.getBoundingClientRect()
      if (r.width === 0 || r.height === 0) continue
      const overlapsX = r.left < h.right && r.right > h.left
      const overlapsY = r.top < h.bottom && r.bottom > h.top
      if (overlapsX && overlapsY) {
        hits.push({
          tag: el.tagName,
          cls: el.className?.toString().slice(0, 60),
          text: (el.textContent || '').trim().slice(0, 40),
          rect: { left: Math.round(r.left), top: Math.round(r.top), right: Math.round(r.right), bottom: Math.round(r.bottom) },
        })
      }
    }
    return { handle: { left: Math.round(h.left), top: Math.round(h.top), right: Math.round(h.right), bottom: Math.round(h.bottom) }, hits }
  })
  console.log(`${label}:`, JSON.stringify(overlaps))
}

console.log('\n--- closed-handle hit-area overlap, per scene, desktop (1280x720) ---')
for (const label of SCENES) {
  await goTo(page, label)
  await checkOverlaps(label)
}

// SceneBar.svelte's `.scene-bar` has flex-wrap:wrap, and the spec calls
// out phone widths as the one place this drawer's own layout changes
// (rows wrap to two lines) -- worth checking the hit area against a
// narrow viewport too. goTo()'s roll-rail click does not exist at
// mobile widths (App.svelte swaps it for BottomBar), so this only
// re-checks whichever scene the desktop loop above left current
// (Settings, the last SCENES entry) plus the fall (App's own landing,
// reloaded fresh) rather than looping through BottomBar's own nav.
console.log('\n--- closed-handle hit-area overlap, phone width (390x844) ---')
await page.setViewportSize({ width: 390, height: 844 })
await checkOverlaps('Settings (already current)')
await page.reload({ waitUntil: 'networkidle' })
await page.waitForSelector('#main-content', { timeout: 15000 })
// Not persisted (no localStorage key -- owner ruling): a fresh load
// always opens the drawer again, so close it by hand before checking
// the closed-state hit area.
await page.waitForSelector('#ingest-loss-drawer')
await page.click('[data-testid="ingest-loss-handle"]')
await page.waitForSelector('#ingest-loss-drawer.closed', { timeout: 15000 })
await checkOverlaps('The fall (fresh load, mobile landing)')

// --- phone-width shots ---------------------------------------------------
// Added for the closed-state rebuild (#1015, owner verdict 2026-09-07):
// `Clear all` moved off the sill and into the top row, which takes its
// room out of that row's padding. At 390px that padding is a large share
// of the row, so the narrow width has to be looked at, not assumed. The
// viewport is already 390x844 here and the drawer is already closed --
// but only just: the first shot taken here caught the rows halfway
// through the 180ms collapse, showing a half-height row above the line.
// Wait it out, as the desktop closed shots above already do.
await page.waitForTimeout(300)
await shoot('phone-closed')
await page.click('[data-testid="ingest-loss-handle"]')
await page.waitForSelector('#ingest-loss-drawer:not(.closed)', { timeout: 15000 })
await page.waitForTimeout(300)
await shoot('phone-open')

await browser.close()
console.log(`\nScreenshots written to ${OUT_DIR}`)
