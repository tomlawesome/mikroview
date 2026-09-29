// Drive the round-8 prototypes through every stage and scenario.
// Run from frontend/:  node ../docs/design/screens/wizard/round-8/drive.mjs [z-ledger aa-block ab-sitting]
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
const here = path.dirname(fileURLToPath(import.meta.url))
const require = createRequire(path.join(process.cwd(), 'package.json'))
const { chromium } = require('playwright')
let executablePath; try { chromium.executablePath() } catch { executablePath = '/opt/pw-browsers/chromium' }
const browser = await chromium.launch(executablePath ? { executablePath } : {})
const errors = []
const dirs = process.argv.slice(2).length ? process.argv.slice(2) : ['ad-ledger', 'ac-alive']
for (const d of dirs) {
  const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 1.5 })
  page.on('pageerror', e => errors.push(`${d} pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error') errors.push(`${d} console: ${m.text()}`) })
  page.on('dialog', x => x.accept())
  const shot = n => page.screenshot({ path: path.join(here, 'shots', `${d}-${n}.png`) })
  const url = 'file://' + path.join(here, d + '.html')
  const w = ms => page.waitForTimeout(ms)
  const answer = async (push = 'yes', backup = 'yes', shots = false) => {
    await page.fill('[data-field=name]', 'rb5009'); if (shots) await shot('02-name'); await page.press('[data-field=name]', 'Enter'); await w(250)
    await page.fill('[data-field=addr]', '192.168.13.1'); await page.press('[data-field=addr]', 'Enter'); await w(250)
    if (shots) await shot('03-push-asked')
    await page.click(`[data-act=${push}]`); await w(250)
    await page.click(`[data-act=${backup}]`); await w(250)
    if (shots) await shot('04-pass-asked')
    await page.fill('[data-field=pass]', 'correct horse'); await page.press('[data-field=pass]', 'Enter'); await w(400)
  }
  await page.goto(url); await w(500); await shot('01-ask')
  await page.selectOption('#speed', '12'); await w(100)
  await answer('yes', 'yes', true); await shot('05-paste')
  await page.click('[data-act=copy]'); await w(900); await shot('06-watch-waiting')
  await w(1500); await shot('07-watch-enrol')
  await w(2200); await shot('08-watch-arrived')
  await page.click('[data-act=to-tune]'); await w(500); await shot('09-tune')
  await page.click('[data-act=toggle][data-i="3"]'); await w(200)
  await page.click('[data-act=tune-copy]'); await w(1500); await shot('10-tune-tagged')
  await page.click('[data-act=to-done]'); await w(600); await shot('11-done')
  await page.click('[data-act=undo][data-u=push]'); await w(400); await shot('12-done-undo')
  // not-now path: no push, no backup
  await page.goto(url); await w(300); await page.selectOption('#speed', '12'); await answer('no', 'no'); await shot('13-paste-minimal')
  await page.click('[data-act=copy]'); await w(3400); await shot('14-minimal-arrived')
  await page.click('[data-act=to-tune]'); await w(500); await shot('15-minimal-done')
  // wrong address
  await page.goto(url); await w(300); await page.selectOption('#scenario', 'wrongaddr'); await page.selectOption('#speed', '12'); await answer()
  await page.click('[data-act=copy]'); await w(2200); await shot('16-wrongaddr')
  await page.click('[data-act=use-other]'); await w(3400); await shot('17-wrongaddr-recovered')
  // ahead of review
  await page.goto(url); await w(300); await page.selectOption('#scenario', 'ahead'); await page.selectOption('#speed', '12'); await answer()
  await page.click('[data-act=copy]'); await w(4400); await shot('18-ahead')
  await page.close()
}
console.log(errors.length ? errors.join('\n') : 'no console errors')
await browser.close()
