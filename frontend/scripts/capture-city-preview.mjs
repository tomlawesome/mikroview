// SPDX-License-Identifier: AGPL-3.0-only
//
// One-off dev tool, not a live-check scenario (see capture-city-devices.mjs
// for the same reasoning) -- deliberately named outside the `live-*.mjs`
// glob. Shoots dev/city-preview.html at a chosen stop against the mockup
// estate (#978's minimap move, #979's default-stop review). Needs no
// backend: the page draws from the city fixture alone.
//
// Usage (from frontend/):
//   node scripts/capture-city-preview.mjs <out.png> [stop] [selector]
//
// The optional selector shoots one element instead of the whole stage
// (e.g. '.mini' for the estate minimap, #978/#1000 review shots).
import { createServer } from 'vite'
import { chromium } from 'playwright'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const here = path.dirname(fileURLToPath(import.meta.url))
const PORT = 5198

const [, , outArg, stopArg, selectorArg] = process.argv
if (!outArg) {
  console.error('usage: node scripts/capture-city-preview.mjs <out.png> [stop]')
  process.exit(2)
}
const out = path.resolve(process.cwd(), outArg)
const stop = stopArg || 'city'

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

await page.goto(`http://localhost:${PORT}/dev/city-preview.html?stop=${encodeURIComponent(stop)}`)
await page.waitForSelector('.city svg', { timeout: 20000 })
await page.waitForTimeout(700) // let the camera's own opacity/move transition settle

await page.locator(selectorArg || '#stage').screenshot({ path: out })
console.log(path.relative(process.cwd(), out))

await browser.close()
await server.close()
