// Screenshot each direction's three scenes for the README and the issue record.
// Run from frontend/ (playwright lives there): node ../docs/design/concepts/round-1/capture.mjs
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
// playwright is a devDependency of frontend/, which this script lives outside of
const require = createRequire(path.join(here, '../../../../frontend/package.json'));
const { chromium } = require('playwright');
const files = [
  ['direction-a-instrument.html', 'a'],
  ['direction-c-luminous.html', 'c'],
  ['direction-d-atlas.html', 'd'],
  ['direction-e-casefile.html', 'e'],
];

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 });

for (const [file, tag] of files) {
  await page.goto('file://' + path.join(here, file));
  // Let fonts settle and animations reach a mid-flight frame.
  await page.waitForTimeout(1200);
  for (const scene of ['s1', 's2', 's3']) {
    const el = page.locator('#' + scene);
    await el.scrollIntoViewIfNeeded();
    await page.waitForTimeout(400);
    await el.screenshot({ path: path.join(here, 'shots', `${tag}-${scene}.png`) });
    console.log(`${tag}-${scene}.png`);
  }
}
await browser.close();
