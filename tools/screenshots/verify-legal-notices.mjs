// SPDX-License-Identifier: AGPL-3.0-only
//
// Compliance check, not a screenshot tool.
//
// AGPL section 0 defines "Appropriate Legal Notices" as displaying a
// copyright notice, that there is no warranty, that licensees may convey
// the work under the License, and how to view a copy of it. Section 5(d)
// requires an interactive interface to display them; section 13 requires
// that network users are offered the Corresponding Source.
//
// All of that lives in AboutOverlay, reached from the nav menu. This
// asserts it is genuinely reachable and genuinely rendering -- a unit
// test can prove the component exists, but not that a user can get to it.
//
// Usage: node verify-legal-notices.mjs [url] [outDir]
import { chromium } from 'playwright'
import { mkdirSync } from 'node:fs'

const url = process.argv[2] ?? 'http://localhost:18083'
const outDir = process.argv[3] ?? '.'
mkdirSync(outDir, { recursive: true })

// Each entry is one thing the AGPL requires the notice to display.
const REQUIRED = [
  { what: 'copyright notice (s0 a)', re: /copyright\s+©?\s*\d{4}/i },
  { what: 'no warranty (s0 b)', re: /without any warranty/i },
  { what: 'right to convey (s0 c)', re: /redistribute/i },
  { what: 'how to view the licence (s0 d)', re: /gnu affero general public license/i },
  { what: 'source offer (s13)', re: /github\.com\/tomlawesome\/mikroview/i },
]

const browser = await chromium.launch()
let failed = 0

for (const scheme of ['dark', 'light']) {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 860 },
    colorScheme: scheme,
  })
  const page = await context.newPage()
  await page.goto(url, { waitUntil: 'networkidle' })

  // Open the nav menu, then the About entry. Located by accessible name
  // rather than a CSS class, so a restyle doesn't silently break the
  // check while leaving it passing.
  const menu = page.getByRole('button', { name: /menu/i }).first()
  await menu.click()
  await page.getByRole('button', { name: /about & licence/i }).click()

  const dialog = page.getByRole('dialog', { name: /about mikroview/i })
  await dialog.waitFor({ state: 'visible', timeout: 5000 })
  const text = await dialog.innerText()

  for (const { what, re } of REQUIRED) {
    if (!re.test(text)) {
      console.error(`  FAIL [${scheme}] missing ${what}`)
      failed++
    }
  }

  // A dialog that renders empty would pass a naive "is visible" check.
  if (text.trim().length < 200) {
    console.error(`  FAIL [${scheme}] notice text is only ${text.trim().length} chars -- looks empty`)
    failed++
  }

  await page.screenshot({ path: `${outDir}/about-${scheme}.png` })
  console.log(`captured about-${scheme}.png (${text.trim().length} chars of notice text)`)
  await context.close()
}

await browser.close()

if (failed > 0) {
  console.error(`\n${failed} legal-notice check(s) failed -- this is an AGPL compliance regression.`)
  process.exit(1)
}
console.log('\nAll AGPL legal notices present in both colour schemes.')
