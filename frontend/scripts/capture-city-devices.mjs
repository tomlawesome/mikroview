// SPDX-License-Identifier: AGPL-3.0-only
//
// Re-shoot the city device library's contact sheet (#864). Run from
// frontend/:
//
//   node scripts/capture-city-devices.mjs
//
// Shots land in docs/design/screens/city/devices-{city,district,street}.png.
// It starts its own Vite dev server on a spare port, so nothing needs to
// be running first, and it needs no backend: the gallery draws from the
// device library alone and touches no API.
import { createServer } from 'vite'
import { chromium } from 'playwright'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const here = path.dirname(fileURLToPath(import.meta.url))
const out = path.resolve(here, '../../docs/design/screens/city')
const PORT = 5199

const server = await createServer({
  configFile: path.resolve(here, '../vite.config.ts'),
  root: path.resolve(here, '..'),
  logLevel: 'warn',
  server: { port: PORT, strictPort: true },
})
await server.listen()

const browser = await chromium.launch()
const page = await browser.newPage({
  viewport: { width: 1500, height: 1100 },
  deviceScaleFactor: 2,
})
page.on('pageerror', (e) => console.log('PAGE ERROR:', e.message))
page.on('console', (m) => m.type() === 'error' && console.log('CONSOLE:', m.text()))

await page.goto(`http://localhost:${PORT}/dev/city-devices.html`)
await page.waitForSelector('[data-band="street"] svg', { timeout: 20000 })
await page.waitForTimeout(500)

for (const band of ['city', 'district', 'street']) {
  const file = path.join(out, `devices-${band}.png`)
  await page.locator(`[data-band="${band}"]`).screenshot({ path: file })
  console.log(path.relative(process.cwd(), file))
}

await browser.close()
await server.close()
