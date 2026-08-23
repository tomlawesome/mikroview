// Screenshot each round-3 direction's three scenes.
// Run from frontend/ (playwright lives there): node ../docs/design/concepts/round-3/capture.mjs
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(here, '../../../../frontend/package.json'));
const { chromium } = require('playwright');

const files = [
  ['direction-m-core.html', 'm', ['s1', 's2', 's3']],
  ['direction-n-score.html', 'n', ['s1', 's2', 's3']],
  ['direction-o-waterfall.html', 'o', ['s1', 's2', 's3']],
];

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 });

for (const [file, tag, scenes] of files) {
  await page.goto('file://' + path.join(here, file));
  await page.waitForTimeout(1200);
  for (const scene of scenes) {
    const el = page.locator('#' + scene);
    await el.scrollIntoViewIfNeeded();
    await page.waitForTimeout(400);
    await el.screenshot({ path: path.join(here, 'shots', `${tag}-${scene}.png`) });
    console.log(`${tag}-${scene}.png`);
  }
}
await browser.close();
