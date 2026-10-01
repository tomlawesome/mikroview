// SPDX-License-Identifier: AGPL-3.0-only
//
// Drive the built journey (#1386) in the real app, frame by frame, the
// way round-15/drive.mjs drove the prototype: Playwright's clock is
// paused at Enter (and again at Finish), so each frame is shot at its
// true time on the journey's own clock and can be laid beside
// round-15/shots/an-neon-in-*.png and an-neon-out-*.png.
//
// Two phases, because the two journeys need two different instances:
//
//   MV_PHASE=in   the way in. Needs a brand-new install: an admin and no
//                 router on record (live-env.sh always declares
//                 live-router, so README.md restarts the staged server
//                 with that line dropped from cfg.yaml).
//   MV_PHASE=out  the way out, the tour's offer, the account menu. Needs
//                 the declared router back: a syslog line from it and a
//                 certificate fetch place a bare "Run setup…" at the
//                 watch step, from which Next and Skip this step reach
//                 Finish (the route frontend/scripts/live-journey.mjs
//                 takes).
//
// Run from frontend/ with live-env.sh's exports in the environment:
//   node ../docs/design/screens/wizard/review-1386/drive.mjs
// MV_REDUCED=1 does the same under prefers-reduced-motion: reduce and
// records what replaces each journey (DESIGN.md: a short crossfade).
//
// The clock is paused *before* the second-factor Enter, so the sign-in
// request, the shell's loads and the journey's own two frames all run
// under the stepped clock; the journey's t0 is read when the riding box
// first takes a transform (journey() places it before its first frame),
// to within one 4ms step. CSS transitions and keyframes run on real
// time in both the prototype and the build, so what a shot shows of a
// *strike* (the 420ms keyframe) is wherever real time had got to.
import { createHmac } from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { launchBrowser, feedSyslog, goTo, openAccountMenu } from '../../../../../frontend/scripts/live-browser.mjs'

const here = path.dirname(fileURLToPath(import.meta.url))
const SHOTS = path.join(here, 'shots')
fs.mkdirSync(SHOTS, { recursive: true })
const URL_BASE = process.env.MV_URL
const USER = process.env.MV_USER
const PASS = process.env.MV_PASS
const SECRET = process.env.MV_TOTP_SECRET
const REDUCED = process.env.MV_REDUCED === '1'
const PHASE = process.env.MV_PHASE
if (PHASE !== 'in' && PHASE !== 'out') { console.error('MV_PHASE must be in or out'); process.exit(2) }
const PREFIX = REDUCED ? 'reduced' : 'live'

// --- the authenticator code, as live-browser.mjs computes it (not exported there) ---
function base32Decode(s) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = 0, value = 0
  const out = []
  for (const ch of s.replace(/=+$/, '').toUpperCase()) {
    const idx = alphabet.indexOf(ch)
    if (idx === -1) throw new Error('MV_TOTP_SECRET is not base32')
    value = (value << 5) | idx
    bits += 5
    if (bits >= 8) { bits -= 8; out.push((value >> bits) & 0xff) }
  }
  return Buffer.from(out)
}
function totpCode(secret, counter) {
  const buf = Buffer.alloc(8)
  buf.writeBigUInt64BE(BigInt(counter))
  const mac = createHmac('sha1', base32Decode(secret)).update(buf).digest()
  const offset = mac[mac.length - 1] & 0x0f
  return String((mac.readUInt32BE(offset) & 0x7fffffff) % 1000000).padStart(6, '0')
}
const COUNTER_FILE = path.join(process.env.MV_DIR, 'totp-last-counter')
async function freshTotpCode() {
  for (;;) {
    const counter = Math.floor(Date.now() / 1000 / 30)
    let spent = -1
    try { spent = Number.parseInt(fs.readFileSync(COUNTER_FILE, 'utf8').trim(), 10) } catch { spent = -1 }
    if (!Number.isFinite(spent) || counter > spent) { fs.writeFileSync(COUNTER_FILE, String(counter)); return totpCode(SECRET, counter) }
    await new Promise((r) => setTimeout(r, 30000 - (Date.now() % 30000) + 1000))
  }
}

// --- the browser ---
const browser = await launchBrowser()
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 1.5, ...(REDUCED ? { reducedMotion: 'reduce' } : {}) })
const errors = [], times = [], notes = []
page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`))
page.on('console', (m) => { if (m.type() === 'error') errors.push(`console: ${m.text()}`) })
const note = (s) => { notes.push(s); console.log(s) }
const tNow = () => page.evaluate(() => Math.round(performance.now() - (window.__t0 || 0)))
const shot = async (n) => { const t = await tNow(); await page.screenshot({ path: path.join(SHOTS, `${PREFIX}-${n}.png`) }); times.push(`${PREFIX}-${n}: ${t}ms`) }
const bodyClass = () => page.evaluate(() => document.body.className)
const sig = () => page.evaluate(() => ({
  door: !!document.querySelector('.door-hold'),
  wiz: !!document.querySelector('.page.wiz'),
  wizLive: !!document.querySelector('.page.wiz.live'),
  fx: !!document.querySelector('.journey-fx'),
  ride: !!document.querySelector('.ride'),
  journey: document.body.classList.contains('journey'),
  fall: !!document.querySelector('.card[data-card="fall"]'),
  offer: !!document.querySelector('.offerwrap'),
  fade: document.body.classList.contains('journey-fade'),
  body: document.body.className,
}))

let clockAt = 0
// Step the paused clock until the riding box has been placed (journey()
// has been called), then mark t0 there. Steps of 4ms: the journey starts
// on a frame boundary, so t0 is read within 4ms of the true one.
async function awaitJourneyStart(limitMs) {
  for (let el = 0; el < limitMs; el += 4) {
    await page.clock.runFor(4)
    if (await page.evaluate(() => !!document.querySelector('.ride')?.style.transform)) {
      await page.evaluate(() => { window.__t0 = performance.now() })
      clockAt = 0
      return el
    }
  }
  return -1
}
const at = async (ms, n) => { await page.clock.runFor(ms - clockAt); clockAt = ms; await shot(n) }
// Step until a predicate holds; returns the journey-clock time it held at.
async function stepUntil(pred, stepMs, limitMs) {
  for (; clockAt < limitMs; ) { await page.clock.runFor(stepMs); clockAt += stepMs; if (await page.evaluate(pred)) return tNow() }
  return -1
}
// Under reduced motion there is no journey clock: step 50ms at a time,
// shoot whenever the DOM's shape changes, and keep the timeline.
async function reducedTimeline(name, stop) {
  const seen = []
  let last = ''
  let el = 0
  for (; el < 20000; el += 50) {
    await page.clock.runFor(50)
    const s = await sig()
    const key = JSON.stringify({ ...s, body: undefined })
    if (key !== last) { last = key; seen.push(`${el}ms ${JSON.stringify(s)}`); await page.screenshot({ path: path.join(SHOTS, `reduced-${name}-${String(el).padStart(5, '0')}.png`) }) }
    if (stop(s)) break
  }
  await page.clock.runFor(500)
  await page.screenshot({ path: path.join(SHOTS, `reduced-${name}-${String(el + 500).padStart(5, '0')}.png`) })
  note(`reduced ${name}, DOM timeline:\n  ` + seen.join('\n  '))
}

// ---------------------------------------------------------------- the door
await page.clock.install()
await page.goto(URL_BASE, { waitUntil: 'networkidle' })
await page.fill('input[autocomplete="username"]', USER)
await page.fill('input[autocomplete="current-password"]', PASS)
if (PHASE === 'in') await shot('00-door-password')
await page.click('button[type="submit"]')
await page.waitForSelector('input[autocomplete="one-time-code"]', { timeout: 20000 })
await page.fill('input[autocomplete="one-time-code"]', await freshTotpCode())
if (PHASE === 'in') await shot('01-door')

if (PHASE === 'in') {
  // -------------------------------------------------------------- the way in
  await page.clock.pauseAt(Date.now() + 100)
  await page.click('button[type="submit"]')
  if (REDUCED) {
    await reducedTimeline('in', (s) => s.wizLive && !s.door)
  } else {
    const started = await awaitJourneyStart(30000)
    if (started < 0) errors.push('the way in never started')
    else {
      note(`way in: journey() placed the box ${started}ms after Enter (stepped clock)`)
      const IN = [400, 900, 1400, 1900, 2400, 2700, 3000, 3300, 3600, 3900, 4200, 4500, 4800, 5200, 5700, 6400]
      for (const ms of IN) await at(ms, `in-${String(ms).padStart(4, '0')}`)
      const endedAt = await stepUntil(() => !document.body.classList.contains('journey'), 50, 12000)
      note(`way in: body lost 'journey' at ${endedAt}ms; body="${await bodyClass()}"`)
      await page.clock.runFor(300)
      await shot('10-wizard-landed')
    }
  }
  const focused = await page.evaluate(() => {
    const a = document.activeElement
    if (!a || !a.closest('.wiz .body')) return null
    const lab = a.closest('label')?.textContent?.trim() || a.getAttribute('aria-label') || a.getAttribute('placeholder') || a.id
    return `${a.tagName.toLowerCase()} "${lab}"`
  })
  note(`way in: focused ${focused}; ${JSON.stringify(await sig())}`)
  await page.clock.resume()
} else {
  // ------------------------------------------------------- reach Finish
  await page.click('button[type="submit"]')
  await page.waitForSelector('#main-content', { timeout: 15000 })
  await page.waitForTimeout(1500)
  if (await page.locator('.page.wiz').count()) throw new Error('the wizard auto-launched: is live-router declared again?')
  feedSyslog(1, 'review-1386')
  await page.request.get(`${URL_BASE}/ca.crt`)
  const wizard = page.locator('.page.wiz')
  const foot = wizard.locator('.foot')
  const finish = foot.locator('button.primary:text-is("Finish")')
  async function openToFinish() {
    await goTo(page, 'Run setup…')
    await wizard.waitFor({ state: 'visible' })
    await page.waitForTimeout(1200)
    if (!(await finish.count())) {
      await foot.locator('button.primary:text-is("Next")').click()
      await foot.locator('button:text-is("Skip this step")').click()
    }
    await finish.waitFor({ state: 'visible', timeout: 15000 })
    await page.waitForTimeout(800)
  }
  await openToFinish()
  await shot('22-done')

  // -------------------------------------------------------------- the way out
  await page.clock.pauseAt(Date.now() + 100)
  await finish.click()
  if (REDUCED) {
    await reducedTimeline('out', (s) => !s.wiz && s.fall && !s.journey)
  } else {
    const started = await awaitJourneyStart(30000)
    if (started < 0) errors.push('the way out never started')
    else {
      note(`way out: journey() placed the box ${started}ms after Finish (stepped clock)`)
      const OUT = [300, 700, 1100, 1500, 1900, 2200, 2500, 2800, 3100, 3400, 3700, 4100, 4600, 5300]
      for (const ms of OUT) await at(ms, `out-${String(ms).padStart(4, '0')}`)
      const endedAt = await stepUntil(() => !document.body.classList.contains('journey'), 50, 12000)
      note(`way out: body lost 'journey' at ${endedAt}ms; body="${await bodyClass()}"`)
      await shot('23-fall')
      const offerAt = await stepUntil(() => !!document.querySelector('.offerwrap'), 50, 15000)
      note(`way out: the tour's offer rose at ${offerAt}ms`)
      await page.clock.runFor(500)
      await shot('24-offer')
    }
  }
  note(`way out landed: ${JSON.stringify(await sig())}`)
  await page.clock.resume()

  if (!REDUCED) {
    // ------------------------------------------------------ the offer, once
    const offer = page.locator('.offerwrap')
    await offer.waitFor({ state: 'visible', timeout: 20000 })
    note(`offer text: ${JSON.stringify(await page.locator('.offer').innerText())}`)
    await page.locator('.offer button.later').click()
    await offer.waitFor({ state: 'detached' })
    await openToFinish()
    await finish.click()
    await wizard.waitFor({ state: 'detached', timeout: 20000 })
    await page.waitForTimeout(9000)
    note(`second Finish: offer present = ${(await offer.count()) > 0}`)

    // -------------------------------------------------------- the account menu
    await openAccountMenu(page)
    const row = page.locator('.account .menu button.row:text-is("Take the tour")')
    note(`account menu has "Take the tour": ${(await row.count()) > 0}`)
    await shot('25-menu')
    await row.click()
    const bar = page.locator('.tour .bar')
    await bar.waitFor({ state: 'visible', timeout: 5000 })
    await page.waitForTimeout(600)
    await shot('26-tour')
    note(`tour bar: ${JSON.stringify(await bar.locator('.progress').innerText())}`)
    await bar.locator('button.leave').click()
    await bar.waitFor({ state: 'detached' })
  }
}

fs.appendFileSync(path.join(SHOTS, `${PREFIX}-times.txt`), times.join('\n') + '\n')
fs.appendFileSync(path.join(SHOTS, `${PREFIX}-notes.txt`), notes.join('\n') + '\n')
console.log(errors.length ? 'ERRORS:\n' + errors.join('\n') : 'no console errors')
fs.appendFileSync(path.join(SHOTS, `${PREFIX}-notes.txt`), (errors.length ? 'ERRORS:\n' + errors.join('\n') : 'no console errors') + '\n')
await page.close()
await browser.close()
