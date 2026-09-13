// Screenshot round 58's scenes: the flags tab with no number in the row,
// and the two scored drawers open, for each direction; then bands.html.
// Run from frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-58/capture.mjs
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(process.env.HOME, 'projects/mikroview/frontend/package.json'));
const { chromium } = require('playwright');

const shot = (n) => path.join(here, 'shots', n + '.png');
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 });
page.on('pageerror', (e) => console.log('PAGE ERROR:', e.message));

async function take(name, sel = '#s7') {
  await page.mouse.move(0, 0);
  await page.waitForTimeout(450);
  await page.locator(sel).screenshot({ path: shot(name) });
  console.log(name + '.png');
}

for (const dir of ['one-hue', 'three-bands']) {
  await page.goto('file://' + path.join(here, dir + '.html'));
  await page.waitForTimeout(800);
  await page.locator('#dtabs span[data-p="flags"]').click();

  // 1. resting: the rows carry the type alone
  await take(dir + '-flags');

  // 2. the activity spike's drawer: scored 72, high
  await page.locator('tr[data-d="d7"]').click();
  await take(dir + '-spike-high');
  await page.locator('tr[data-d="d7"]').click();

  // 3. the drop surge's drawer: scored 46, moderate
  await page.locator('tr[data-d="d5"]').click();
  await take(dir + '-surge-moderate');
  await page.locator('tr[data-d="d5"]').click();
}

await page.goto('file://' + path.join(here, 'bands.html'));
await page.waitForTimeout(500);
await page.screenshot({ path: shot('bands'), fullPage: true });
console.log('bands.png');

await browser.close();
