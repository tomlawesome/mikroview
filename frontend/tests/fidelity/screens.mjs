// SPDX-License-Identifier: AGPL-3.0-only
//
// The fidelity gate (#658): photograph the built app and its ratified
// mockup at the same viewport and compare per pixel.
//
// Why this exists rather than a reviewer's eye: the door's wrong box
// proportions, wrong button and misplaced label were each caught by eye
// across three review rounds, and a gate would have caught all three
// before the first. The failure this is really for is larger — six
// surfaces were reported built and merged while three of them were the
// old page moved into a new card, which no unit test and no live-check
// scenario could see, because both ask "does it work", never "is it the
// thing that was ratified".
//
// Ported from orbit/web/tests/fidelity/screens.spec.js, whose two-stage
// shape this keeps:
//
//   porting — the surface is compared against its mockup. This is where
//             every mikroview surface starts.
//   owned   — the owner has seen it and deliberately moved it on, so the
//             comparison becomes the approved baseline PNG instead.
//             Baselines are written only with UPDATE_BASELINE=1.
//
// Run: make fidelity     (FIDELITY_APP / FIDELITY_MOCKUPS override the hosts)
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { firefox } from 'playwright'
import pixelmatch from 'pixelmatch'
import { PNG } from 'pngjs'

const here = dirname(fileURLToPath(import.meta.url))
const baselines = resolve(here, 'baselines')
const artifacts = resolve(here, '../../test-results/fidelity')

const APP = process.env.FIDELITY_APP ?? 'https://192.168.11.30:19892/'
const MOCKUPS = process.env.FIDELITY_MOCKUPS ?? 'http://192.168.11.30:8311/'
const CREDS = process.env.FIDELITY_CREDS ?? '/tmp/mikroview-atlas-demo/credentials.txt'

// Orbit's numbers, unchanged: a per-pixel threshold loose enough to ignore
// antialiasing, and a total budget tight enough that a missing control or a
// restructured layout cannot hide inside it.
const PIXEL_THRESHOLD = 0.1
const MAX_DIFF_RATIO = 0.001

const VIEWPORT = { width: 1600, height: 1000 }

// Furniture that exists only in a mockup: the round ribbon and the amber
// scene notes are apparatus, never interface. Trimmed out of the mockup's
// own document BEFORE it paints, so nothing they occupy seeds a deviation
// -- Orbit's pitfall, carried over.
const MOCKUP_FURNITURE = ['.concept', '.scene-tag', '.outboard']

const SCREENS = [
  { name: 'docket-flags', scene: 's7', rail: 'The docket', tab: 'flags', stage: 'porting' },
  { name: 'entities', scene: 'ent', rail: 'Entities', stage: 'porting' },
  { name: 'stream', scene: 's5', rail: 'Stream', stage: 'porting' },
  { name: 'settings', scene: 'set', rail: 'Settings', stage: 'porting' },
  { name: 'fall', scene: 's2', rail: 'The fall', stage: 'porting' },
  { name: 'topography', scene: 's3', rail: 'Topography', stage: 'porting' },
  { name: 'metrics', scene: 's4', rail: 'Metrics', stage: 'porting' },
]

function creds() {
  return Object.fromEntries(
    readFileSync(CREDS, 'utf8').split('\n').filter(Boolean)
      .map((l) => l.replace(/^export\s+/, '').split('=').map((v) => v.replace(/^['"]|['"]$/g, ''))),
  )
}

async function shootMockup(ctx, scene) {
  const page = await ctx.newPage()
  // Trim before load, not after: furniture that never paints cannot leave
  // a ghost for pixelmatch to find.
  await page.addInitScript((sels) => {
    const strip = () => sels.forEach((s) => document.querySelectorAll(s).forEach((el) => el.remove()))
    document.addEventListener('DOMContentLoaded', strip)
    strip()
  }, MOCKUP_FURNITURE)
  await page.goto(`${MOCKUPS}concepts/round-29/the-whole.html#${scene}`, { waitUntil: 'load' })
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(700)
  const buf = await page.screenshot({ animations: 'disabled' })
  await page.close()
  return PNG.sync.read(buf)
}

async function shootApp(ctx, screen, c) {
  const page = await ctx.newPage()
  await page.goto(APP, { waitUntil: 'networkidle' })
  if (await page.locator('input[type="password"]').count()) {
    await page.fill('input[name="username"], input[type="text"]', c.MV_USER)
    await page.fill('input[type="password"]', c.MV_PASS)
    await page.click('button[type="submit"]')
    await page.waitForSelector('.roll-rail', { timeout: 20000 })
  }
  await page.click(`.roll-rail button.rail-name:text-is("${screen.rail}")`).catch(() => {})
  await page.waitForTimeout(1800)
  if (screen.tab) {
    await page.click(`.card[aria-hidden="false"] [role="tab"]:has(.tlabel:text-is("${screen.tab}"))`).catch(() => {})
    await page.waitForTimeout(1200)
  }
  await page.evaluate(() => document.fonts.ready)
  const buf = await page.screenshot({ animations: 'disabled' })
  await page.close()
  return PNG.sync.read(buf)
}

function compare(expected, actual, name) {
  const width = Math.min(expected.width, actual.width)
  const height = Math.min(expected.height, actual.height)
  const diff = new PNG({ width, height })
  const changed = pixelmatch(expected.data, actual.data, diff.data, width, height, {
    threshold: PIXEL_THRESHOLD,
  })
  const ratio = changed / (width * height)
  if (ratio > MAX_DIFF_RATIO) {
    mkdirSync(artifacts, { recursive: true })
    writeFileSync(resolve(artifacts, `${name}-expected.png`), PNG.sync.write(expected))
    writeFileSync(resolve(artifacts, `${name}-actual.png`), PNG.sync.write(actual))
    writeFileSync(resolve(artifacts, `${name}-diff.png`), PNG.sync.write(diff))
  }
  return { changed, ratio }
}

const browser = await firefox.launch()
// reducedMotion so raf-driven surfaces (the fall, the drum) hold still;
// animations:'disabled' cannot reach a canvas.
const ctx = await browser.newContext({
  ignoreHTTPSErrors: true, viewport: VIEWPORT, colorScheme: 'dark', reducedMotion: 'reduce',
})
const c = creds()
let failures = 0
console.log(`fidelity gate — ${SCREENS.length} screens, budget ${(MAX_DIFF_RATIO * 100).toFixed(2)}% of pixels\n`)
for (const s of SCREENS) {
  const baseline = resolve(baselines, `${s.name}.png`)
  const useBaseline = s.stage === 'owned' && existsSync(baseline)
  const expected = useBaseline ? PNG.sync.read(readFileSync(baseline)) : await shootMockup(ctx, s.scene)
  const actual = await shootApp(ctx, s, c)
  if (process.env.UPDATE_BASELINE === '1') {
    mkdirSync(baselines, { recursive: true })
    writeFileSync(baseline, PNG.sync.write(actual))
    console.log(`  ${s.name.padEnd(16)} baseline written`)
    continue
  }
  const { changed, ratio } = compare(expected, actual, s.name)
  const ok = ratio <= MAX_DIFF_RATIO
  if (!ok) failures++
  console.log(`  ${ok ? 'ok  ' : 'FAIL'} ${s.name.padEnd(16)} ${(ratio * 100).toFixed(2)}% of pixels differ ` +
              `(${changed.toLocaleString()}) vs ${useBaseline ? 'baseline' : 'round-29 #' + s.scene}`)
}
await browser.close()
console.log(`\n${failures} of ${SCREENS.length} screens outside the budget.`)
if (failures) console.log(`artifacts: ${artifacts}`)
process.exit(failures ? 1 : 0)
