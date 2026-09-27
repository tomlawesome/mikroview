// Drive round 10's AG through the door, every stage, the way out, and
// the scenarios.  Run from frontend/:  node ../docs/design/screens/wizard/round-10/drive.mjs
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
const here = path.dirname(fileURLToPath(import.meta.url))
const require = createRequire(path.join(process.cwd(), 'package.json'))
const { chromium } = require('playwright')
let executablePath; try { chromium.executablePath() } catch { executablePath = '/opt/pw-browsers/chromium' }
const browser = await chromium.launch(executablePath ? { executablePath } : {})
const errors = []
const d = 'ag-rises'
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 1.5 })
page.on('pageerror', e => errors.push(`${d} pageerror: ${e.message}`))
page.on('console', m => { if (m.type() === 'error') errors.push(`${d} console: ${m.text()}`) })
page.on('dialog', x => x.accept())
const shot = n => page.screenshot({ path: path.join(here, 'shots', `${d}-${n}.png`) })
const url = 'file://' + path.join(here, d + '.html')
const w = ms => page.waitForTimeout(ms)
const router = async (push = 'yes', backup = 'yes') => {
  await page.fill('[data-field=name]', 'rb5009'); await page.fill('[data-field=addr]', '192.168.13.1')
  await page.click(`[data-act=set][data-q=push][data-v=${push}]`); await page.click(`[data-act=set][data-q=backup][data-v=${backup}]`); await w(150)
}
const mint = async () => { await page.click('[data-act=router-next]'); await w(250); await page.fill('[data-field=pass]', 'correct horse'); await page.press('[data-field=pass]', 'Enter'); await w(400) }
// the door and the way in
await page.goto(url); await w(600); await shot('01-door')
await page.click('#enter'); await w(450); await shot('02-way-in-a'); await w(500); await shot('03-way-in-b'); await w(1100); await shot('04-router-step')
await page.selectOption('#speed', '12'); await w(100)
await page.fill('[data-field=addr]', '192.168.13.1:514'); await w(150); await shot('05-addr-problem')
await router(); await shot('06-router-filled')
await mint(); await shot('07-paste')
await page.click('[data-act=copy]'); await w(900); await shot('08-watch-waiting')
await w(1500); await shot('09-watch-enrol')
await w(2200); await shot('10-watch-arrived')
await page.click('[data-act=to-tune]'); await w(500); await shot('11-tune')
await page.click('[data-act=toggle][data-i="3"]'); await w(200)
await page.click('[data-act=tune-copy]'); await w(1500); await shot('12-tune-tagged')
await page.click('[data-act=to-done]'); await w(600); await shot('13-done')
await page.click('[data-act=undo][data-u=push]'); await w(400); await shot('14-done-undo')
await page.click('[data-act=undo][data-u=push]'); await w(200)
// the way out
await page.click('[data-act=finish]'); await w(450); await shot('15-way-out-a'); await w(450); await shot('16-way-out-b'); await w(500); await shot('17-way-out-c'); await w(900); await shot('18-fall')
// not-now path
await page.goto(url + '?skipdoor'); await w(400); await page.selectOption('#speed', '12'); await router('no', 'no'); await mint(); await shot('19-paste-minimal')
await page.click('[data-act=copy]'); await w(3400); await shot('20-minimal-arrived')
await page.click('[data-act=to-tune]'); await w(500); await shot('21-minimal-done')
// wrong address
await page.goto(url + '?skipdoor'); await w(400); await page.selectOption('#scenario', 'wrongaddr'); await w(200); await page.selectOption('#speed', '12'); await router(); await mint()
await page.click('[data-act=copy]'); await w(2200); await shot('22-wrongaddr')
await page.click('[data-act=use-other]'); await w(3400); await shot('23-wrongaddr-recovered')
// ahead of review
await page.goto(url + '?skipdoor'); await w(400); await page.selectOption('#scenario', 'ahead'); await w(200); await page.selectOption('#speed', '12'); await router(); await mint()
await page.click('[data-act=copy]'); await w(4400); await shot('24-ahead')
// the gate: a locked step cannot be clicked
await page.goto(url + '?skipdoor'); await w(400)
const lockedDisabled = await page.$eval('#steps li:nth-child(3) .step-row', b => b.disabled)
if (!lockedDisabled) errors.push('step 3 was clickable before step 2')
await page.close()
console.log(errors.length ? errors.join('\n') : 'no console errors')
await browser.close()
