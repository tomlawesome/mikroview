// Screenshot round 40's one-layout page (the 2D map and the city sharing one
// ground plan, plus the registration overlay).
// Run from frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-40/capture-one-layout.mjs
// Shots land in shots/one-layout-<panel>.png.
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(process.env.HOME, 'projects/mikroview/frontend/package.json'));
const { chromium } = require('playwright');

const shot = (n) => path.join(here, 'shots', 'one-layout-' + n + '.png');
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 2100, height: 1100 }, deviceScaleFactor: 2 });
page.on('pageerror', (e) => console.log('PAGE ERROR:', e.message));
page.on('console', (m) => { if (m.type() === 'error') console.log('CONSOLE ERROR:', m.text()); });
await page.goto('file://' + path.join(here, 'one-layout.html'));
await page.waitForTimeout(1200);

for (const id of ['plan', 'city', 'overlay']) {
  const el = page.locator('#' + id);
  await el.scrollIntoViewIfNeeded();
  await page.waitForTimeout(300);
  await el.screenshot({ path: shot(id) });
  console.log('one-layout-' + id + '.png');
}

await browser.close();
