// SPDX-License-Identifier: AGPL-3.0-only
//
// One-off dev tool, not a live-check scenario (see capture-city-devices.mjs
// for the same reasoning) -- deliberately named outside the `live-*.mjs`
// glob. Shoots dev/topography-preview.html at a chosen altitude stop
// against a small hand-built estate (#979's default-stop review). Needs
// no backend: the page drives zonesState/appState directly.
//
// Usage (from frontend/):
//   node scripts/capture-topography-preview.mjs <out.png> [stop]
import { createServer } from 'vite'
import { chromium } from 'playwright'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const here = path.dirname(fileURLToPath(import.meta.url))
const PORT = 5197

const [, , outArg, stopArg] = process.argv
if (!outArg) {
  console.error('usage: node scripts/capture-topography-preview.mjs <out.png> [stop]')
  process.exit(2)
}
const out = path.resolve(process.cwd(), outArg)
const stop = stopArg || 'zones'

const server = await createServer({
  configFile: path.resolve(here, '../vite.config.ts'),
  root: path.resolve(here, '..'),
  logLevel: 'warn',
  server: { port: PORT, strictPort: true },
})
await server.listen()

const browser = await chromium.launch()
const page = await browser.newPage({
  viewport: { width: 1600, height: 900 },
  deviceScaleFactor: 2,
})
page.on('pageerror', (e) => console.log('PAGE ERROR:', e.message))
page.on('console', (m) => m.type() === 'error' && console.log('CONSOLE:', m.text()))

await page.goto(`http://localhost:${PORT}/dev/topography-preview.html?stop=${encodeURIComponent(stop)}`)
await page.waitForSelector('#stage svg', { timeout: 20000 })
await page.waitForTimeout(900) // let the camera's own opacity/move transition settle

await page.locator('#stage').screenshot({ path: out })
console.log(path.relative(process.cwd(), out))

await browser.close()
await server.close()
