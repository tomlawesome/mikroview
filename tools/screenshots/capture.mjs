// One-off dev tool: screenshots a running mikroview instance for docs/marketing.
// Usage: node capture.mjs <url> <outDir>
import { chromium } from 'playwright'
import { mkdirSync } from 'node:fs'

const url = process.argv[2] ?? 'http://localhost:18083'
const outDir = process.argv[3] ?? '.'
mkdirSync(outDir, { recursive: true })

const browser = await chromium.launch()

for (const scheme of ['dark', 'light']) {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 860 },
    colorScheme: scheme,
  })
  const page = await context.newPage()
  await page.goto(url, { waitUntil: 'networkidle' })
  await page.waitForSelector('.row', { timeout: 5000 }).catch(() => {})
  await page.waitForTimeout(600)
  await page.screenshot({ path: `${outDir}/live-view-${scheme}.png` })
  await context.close()
  console.log(`captured live-view-${scheme}.png`)
}

await browser.close()
