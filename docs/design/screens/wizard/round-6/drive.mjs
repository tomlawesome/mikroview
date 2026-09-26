// Drive the three round-6 prototypes through every stage and scenario.
// Run from frontend/:  node ../docs/design/screens/wizard/round-6/drive.mjs
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
const here = path.dirname(fileURLToPath(import.meta.url))
const require = createRequire(path.join(process.cwd(), 'package.json'))
const { chromium } = require('playwright')
let executablePath; try { chromium.executablePath() } catch { executablePath = '/opt/pw-browsers/chromium' }
const browser = await chromium.launch(executablePath ? { executablePath } : {})
const errors = []
for (const d of ['w-console', 'x-blueprint', 'y-analyzer']) {
  const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 1.5 })
  page.on('pageerror', e => errors.push(`${d} pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error') errors.push(`${d} console: ${m.text()}`) })
  page.on('dialog', x => x.accept())
  const shot = n => page.screenshot({ path: path.join(here, 'shots', `${d}-${n}.png`) })
  const url = 'file://' + path.join(here, d + '.html')
  const fill = async () => { await page.fill('[data-field=name]', 'rb5009'); await page.fill('[data-field=addr]', '192.168.13.1'); await page.fill('[data-field=pass]', 'correct horse') }
  await page.goto(url); await page.waitForTimeout(500); await shot('01-tell')
  await fill(); await page.selectOption('#speed', '12'); await page.waitForTimeout(200); await shot('02-tell-filled')
  await page.click('[data-act=go-paste]'); await page.waitForTimeout(500); await shot('03-paste')
  await page.click('[data-act=copy]'); await page.waitForTimeout(900); await shot('04-watch-waiting')
  await page.waitForTimeout(1500); await shot('05-watch-enrol')
  await page.waitForTimeout(2200); await shot('06-watch-pushed')
  await page.click('[data-act=to-tune]'); await page.waitForTimeout(500); await shot('07-tune')
  await page.click('[data-act=toggle][data-i="3"]'); await page.waitForTimeout(200)
  await page.click('[data-act=tune-copy]'); await page.waitForTimeout(1500); await shot('08-tune-tagged')
  await page.click('[data-act=to-done]'); await page.waitForTimeout(600); await shot('09-done')
  await page.click('[data-act=undo][data-u=connect]'); await page.waitForTimeout(400); await shot('10-done-undo')
  // wrong address
  await page.goto(url); await page.waitForTimeout(300); await page.selectOption('#scenario', 'wrongaddr'); await page.selectOption('#speed', '12'); await fill()
  await page.click('[data-act=go-paste]'); await page.click('[data-act=copy]'); await page.waitForTimeout(2200)
  if (!(await page.$('[data-act=use-other]'))) { const chk = await page.$('[data-act=checks]'); if (chk) { await chk.click(); await page.waitForTimeout(300) } }
  await shot('11-wrongaddr'); await page.click('[data-act=use-other]'); await page.waitForTimeout(3200); await shot('12-wrongaddr-recovered')
  // ahead of review
  await page.goto(url); await page.waitForTimeout(300); await page.selectOption('#scenario', 'ahead'); await page.selectOption('#speed', '12'); await fill()
  await page.click('[data-act=go-paste]'); await page.click('[data-act=copy]'); await page.waitForTimeout(4200); await shot('13-ahead')
  await page.close()
}
console.log(errors.length ? errors.join('\n') : 'no console errors')
await browser.close()
