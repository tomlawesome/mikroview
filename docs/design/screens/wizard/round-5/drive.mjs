// Drive the round-5 prototype end to end and screenshot each stage.
// Run from frontend/:  node ../docs/design/screens/wizard/round-5/drive.mjs
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
const here = path.dirname(fileURLToPath(import.meta.url))
const require = createRequire(path.join(process.cwd(), 'package.json'))
const { chromium } = require('playwright')
let executablePath; try { chromium.executablePath() } catch { executablePath = '/opt/pw-browsers/chromium' }
const browser = await chromium.launch(executablePath ? { executablePath } : {})
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 1.5 })
const errors = []
page.on('pageerror', e => errors.push('pageerror: ' + e.message))
page.on('console', m => { if (m.type() === 'error' || m.type() === 'warning') errors.push(m.type() + ': ' + m.text()) })
page.on('dialog', d => d.accept())
const shot = n => page.screenshot({ path: path.join(here, 'shots', n + '.png') })
const url = 'file://' + path.join(here, 'first-light.html')

await page.goto(url); await page.waitForTimeout(600)
await shot('01-tell-empty')
await page.fill('#f-name', 'rb5009'); await page.fill('#f-addr', '192.168.13.1'); await page.focus('#f-pass'); await page.fill('#f-pass', 'correct horse')
await page.waitForTimeout(300); await shot('02-tell-filled')
await page.selectOption('#speed', '3')
await page.click('#go-paste'); await page.waitForTimeout(500); await shot('03-paste')
await page.click('[data-act=fold]'); await page.waitForTimeout(400); await shot('04-paste-open')
await page.click('[data-act=copy]'); await page.waitForTimeout(300); await shot('05-copied')
await page.waitForTimeout(1200); await shot('06-watch-waiting')
await page.waitForTimeout(1800); await shot('07-watch-cert')
await page.waitForTimeout(2000); await shot('08-watch-firstlight')
await page.waitForTimeout(4000); await shot('09-watch-pushed')
await page.click('[data-act=to-tune]'); await page.waitForTimeout(500); await shot('10-tune')
await page.click('[data-act=toggle][data-i="3"]'); await page.waitForTimeout(300)
await page.click('[data-act=tune-copy]'); await page.waitForTimeout(400); await shot('11-tune-waiting')
await page.waitForTimeout(2000); await shot('12-tune-tagged')
await page.click('[data-act=to-done]'); await page.waitForTimeout(500); await shot('13-done')
await page.click('[data-act=undo][data-u=connect]'); await page.waitForTimeout(400); await shot('14-done-undo')

// wrong-address scenario
await page.goto(url + '?demo'); await page.waitForTimeout(400)
await page.selectOption('#scenario', 'wrongaddr'); await page.selectOption('#speed', '3')
await page.fill('#f-name', 'rb5009'); await page.fill('#f-addr', '192.168.13.1'); await page.fill('#f-pass', 'x')
await page.click('#go-paste'); await page.click('[data-act=copy]'); await page.waitForTimeout(5000); await shot('15-wrongaddr-refused')
await page.click('[data-act=use-other]'); await page.waitForTimeout(4000); await shot('16-wrongaddr-recovered')

// ahead-of-review scenario
await page.goto(url); await page.waitForTimeout(400)
await page.selectOption('#scenario', 'ahead'); await page.selectOption('#speed', '3')
await page.fill('#f-name', 'rb5009'); await page.fill('#f-addr', '192.168.13.1'); await page.fill('#f-pass', 'x')
await page.click('#go-paste'); await page.click('[data-act=copy]'); await page.waitForTimeout(7500)
await page.click('[data-act=warn]'); await page.waitForTimeout(300); await shot('17-ahead-warning')
console.log(errors.length ? errors.join('\n') : 'no console errors')
await browser.close()
