// Screenshot each round-1 navigation direction's five scenes.
// Run from frontend/ (playwright lives there):
//   node ../docs/design/screens/navigation/round-1/capture.mjs
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(process.cwd(), 'package.json'));
const { chromium } = require('playwright');

const files = [
  ['direction-p-masthead.html', 'p'],
  ['direction-q-rail.html', 'q'],
  ['direction-r-places.html', 'r'],
];
const scenes = ['s1', 's2', 's3', 's4', 's5'];

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 });

for (const [file, tag] of files) {
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
