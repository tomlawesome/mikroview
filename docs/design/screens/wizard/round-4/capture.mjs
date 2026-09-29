// Screenshot round 4's nine scenes for both directions, dark only (the
// app has no light theme). Run from frontend/:
//   node ../docs/design/screens/wizard/round-4/capture.mjs
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import fs from 'node:fs'

const here = path.dirname(fileURLToPath(import.meta.url))
const require = createRequire(path.join(process.cwd(), 'package.json'))
const { chromium } = require('playwright')

const shotsDir = path.join(here, 'shots')
fs.mkdirSync(shotsDir, { recursive: true })

let executablePath
try {
  chromium.executablePath()
} catch {
  executablePath = '/opt/pw-browsers/chromium'
}

const browser = await chromium.launch(executablePath ? { executablePath } : {})
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 })

const directions = [
  { file: 'direction-t-wizard.html', prefix: 't' },
  { file: 'direction-u-wizard.html', prefix: 'u' },
]

for (const { file, prefix } of directions) {
  await page.goto('file://' + path.join(here, file))
  // sticky bars ride mid-scene when a scene is taller than the viewport
  await page.addStyleTag({ content: '.concept, .scene-tag { position: static !important; }' })
  await page.waitForTimeout(1200)
  for (let n = 1; n <= 9; n++) {
    const scene = `${prefix}${n}`
    const el = page.locator('#' + scene)
    await el.scrollIntoViewIfNeeded()
    await page.waitForTimeout(400)
    await el.screenshot({ path: path.join(shotsDir, `${prefix}-${scene}-dark.png`) })
    console.log(`${prefix}-${scene}-dark.png`)
  }
}
await browser.close()
