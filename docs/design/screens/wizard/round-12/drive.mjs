// Drive round 12's direction: the door, the way in frame by frame, the
// wizard through to Finish, the way out frame by frame, the fall.
// Run from frontend/:  node ../docs/design/screens/wizard/round-12/drive.mjs
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
const here = path.dirname(fileURLToPath(import.meta.url))
const require = createRequire(path.join(process.cwd(), 'package.json'))
const { chromium } = require('playwright')
let executablePath; try { chromium.executablePath() } catch { executablePath = '/opt/pw-browsers/chromium' }
const browser = await chromium.launch(executablePath ? { executablePath } : {})
const errors = []; const times = []
const d = 'ak-catch'
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 1.5 })
page.on('pageerror', e => errors.push(`${d} pageerror: ${e.message}`))
page.on('console', m => { if (m.type() === 'error') errors.push(`${d} console: ${m.text()}`) })
page.on('dialog', x => x.accept())
const shot = async n => { const t = await page.evaluate(() => Math.round(performance.now() - (window.__t0 || 0))); await page.screenshot({ path: path.join(here, 'shots', `${d}-${n}.png`) }); times.push(`${d}-${n}: ${t}ms`); }
const url = 'file://' + path.join(here, d + '.html')
const w = ms => page.clock.runFor(ms)
await page.clock.install(); await page.goto(url); await page.clock.pauseAt(Date.now() + 5000); await page.clock.runFor(700); await shot('01-door')
// the way in, frame by frame: the clock advances only when told to, so each frame is at its true time
await page.evaluate(() => { window.__t0 = performance.now() }); await page.click('#enter')
let clockAt = 0; const at = async (ms, n) => { await page.clock.runFor(ms - clockAt); clockAt = ms; await shot(n) }
const IN = [400, 900, 1400, 1900, 2400, 2700, 3000, 3300, 3600, 3900, 4200, 4500, 4800, 5200, 5700, 6400]
for (const ms of IN) await at(ms, `in-${String(ms).padStart(4, '0')}`)
await page.clock.runFor(1500)
// the wizard, quickly, to Finish
await page.selectOption('#speed', '12'); await w(100)
await page.fill('[data-field=name]', 'rb5009'); await page.fill('[data-field=addr]', '192.168.13.1')
await page.click('[data-act=set][data-q=push][data-v=yes]'); await page.click('[data-act=set][data-q=backup][data-v=yes]'); await w(150); await shot('20-router-filled')
await page.click('[data-act=router-next]'); await w(250); await page.fill('[data-field=pass]', 'correct horse'); await page.press('[data-field=pass]', 'Enter'); await w(400)
await page.click('[data-act=copy]'); await w(4600); await shot('21-arrived')
await page.click('[data-act=to-tune]'); await w(400); await page.click('[data-act=toggle][data-i="3"]'); await w(150); await page.click('[data-act=tune-copy]'); await w(1500)
await page.click('[data-act=to-done]'); await w(600); await shot('22-done')
// the way out, frame by frame
await page.evaluate(() => { window.__t0 = performance.now() }); await page.click('[data-act=finish]')
clockAt = 0
const OUT = [300, 700, 1100, 1500, 1900, 2200, 2500, 2800, 3100, 3400, 3700, 4100, 4600, 5300]
for (const ms of OUT) await at(ms, `out-${String(ms).padStart(4, '0')}`)
// the gate still holds
await page.goto(url + '?skipdoor'); await page.clock.pauseAt(Date.now() + 90000); await page.clock.runFor(400)
const lockedDisabled = await page.$eval('#steps li:nth-child(3) .step-row', b => b.disabled)
if (!lockedDisabled) errors.push(`${d}: step 3 was clickable before step 2`)
await shot('30-skipdoor')
await page.close()
import('node:fs').then(fs => fs.writeFileSync(path.join(here, 'shots', 'times.txt'), times.join('\n') + '\n'))
console.log(errors.length ? errors.join('\n') : 'no console errors')
await browser.close()
