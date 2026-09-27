// Drive round 11's three directions: the door, the way in frame by frame,
// the wizard through to Finish, the way out frame by frame, the fall.
// Run from frontend/:  node ../docs/design/screens/wizard/round-11/drive.mjs [ah|ai|aj]
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
const here = path.dirname(fileURLToPath(import.meta.url))
const require = createRequire(path.join(process.cwd(), 'package.json'))
const { chromium } = require('playwright')
let executablePath; try { chromium.executablePath() } catch { executablePath = '/opt/pw-browsers/chromium' }
const browser = await chromium.launch(executablePath ? { executablePath } : {})
const errors = []; const times = []
const only = process.argv[2]
const DIRS = { ah: 'ah-slide', ai: 'ai-swell', aj: 'aj-storm' }
for (const [k, d] of Object.entries(DIRS)) {
  if (only && only !== k) continue
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
  await at(350, '02-in-350'); await at(800, '03-in-800'); await at(1300, '04-in-1300'); await at(1800, '05-in-1800'); await at(2300, '06-in-2300'); await at(2900, '07-in-2900'); await at(3800, '08-landed')
  await page.clock.runFor(1500)
  // the wizard, quickly, to Finish
  await page.selectOption('#speed', '12'); await w(100)
  await page.fill('[data-field=name]', 'rb5009'); await page.fill('[data-field=addr]', '192.168.13.1')
  await page.click('[data-act=set][data-q=push][data-v=yes]'); await page.click('[data-act=set][data-q=backup][data-v=yes]'); await w(150); await shot('09-router-filled')
  await page.click('[data-act=router-next]'); await w(250); await page.fill('[data-field=pass]', 'correct horse'); await page.press('[data-field=pass]', 'Enter'); await w(400)
  await page.click('[data-act=copy]'); await w(4600); await shot('10-arrived')
  await page.click('[data-act=to-tune]'); await w(400); await page.click('[data-act=toggle][data-i="3"]'); await w(150); await page.click('[data-act=tune-copy]'); await w(1500)
  await page.click('[data-act=to-done]'); await w(600); await shot('11-done')
  // the way out, frame by frame
  await page.evaluate(() => { window.__t0 = performance.now() }); await page.click('[data-act=finish]')
  clockAt = 0; const at1 = at
  await at1(300, '12-out-300'); await at1(800, '13-out-800'); await at1(1300, '14-out-1300'); await at1(1900, '15-out-1900'); await at1(2600, '16-out-2600'); await at1(3600, '17-fall')
  // the gate still holds
  await page.goto(url + '?skipdoor'); await page.clock.pauseAt(Date.now() + 90000); await page.clock.runFor(400)
  const lockedDisabled = await page.$eval('#steps li:nth-child(3) .step-row', b => b.disabled)
  if (!lockedDisabled) errors.push(`${d}: step 3 was clickable before step 2`)
  await shot('18-skipdoor')
  await page.close()
}
import('node:fs').then(fs => fs.writeFileSync(path.join(here, 'shots', 'times.txt'), times.join('\n') + '\n'))
console.log(errors.length ? errors.join('\n') : 'no console errors')
await browser.close()
