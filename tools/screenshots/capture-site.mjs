import { chromium } from 'playwright'

const url = process.argv[2] ?? 'http://localhost:18099'
const outDir = process.argv[3] ?? '.'

const browser = await chromium.launch()
const errors = []

for (const scheme of ['dark', 'light']) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1200 }, colorScheme: scheme })
  const page = await context.newPage()
  page.on('pageerror', (e) => errors.push(`[${scheme}] pageerror: ${e.message}`))
  page.on('console', (m) => { if (m.type() === 'error') errors.push(`[${scheme}] console: ${m.text()}`) })
  await page.goto(url, { waitUntil: 'networkidle' })
  await page.waitForTimeout(1500)
  await page.screenshot({ path: `${outDir}/site-${scheme}-top.png` })
  await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight))
  await page.waitForTimeout(200)
  await page.screenshot({ path: `${outDir}/site-${scheme}-bottom.png` })
  await page.screenshot({ path: `${outDir}/site-${scheme}-full.png`, fullPage: true })
  await context.close()
}

await browser.close()
if (errors.length) {
  console.log('ERRORS:\n' + errors.join('\n'))
} else {
  console.log('no console/page errors')
}
