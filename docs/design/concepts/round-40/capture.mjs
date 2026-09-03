// Screenshot round 40's scenes for one direction.
// Run from frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-40/capture.mjs isometric
//   node ../docs/design/concepts/round-40/capture.mjs relief
// Shots land in shots/<direction>-<scene>.png. A direction may add extra
// states by exporting `window.round40States` = [{name, apply(), undo()}].
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const dir = process.argv[2];
if (!dir) { console.error('usage: capture.mjs <direction>'); process.exit(2); }
const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(process.env.HOME, 'projects/mikroview/frontend/package.json'));
const { chromium } = require('playwright');

const shot = (n) => path.join(here, 'shots', dir + '-' + n + '.png');
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 });
page.on('pageerror', (e) => console.log('PAGE ERROR:', e.message));
await page.goto('file://' + path.join(here, dir + '.html'));
await page.waitForTimeout(1400);

for (const id of ['survey', 'street', 'estate', 'alarm']) {
  const el = page.locator('#' + id);
  await el.scrollIntoViewIfNeeded();
  await page.waitForTimeout(500);
  await el.screenshot({ path: shot(id) });
  console.log(dir + '-' + id + '.png');
}

// importance toggled to "watched" on the survey
const watched = page.locator('#survey [data-importance="watched"]');
if (await watched.count()) {
  await page.locator('#survey').scrollIntoViewIfNeeded();
  await watched.first().click();
  await page.waitForTimeout(600);
  await page.locator('#survey').screenshot({ path: shot('survey-watched') });
  console.log(dir + '-survey-watched.png');
}

const extras = await page.evaluate(() => (window.round40States || []).map((s) => s.name));
for (const name of extras) {
  await page.evaluate((n) => { const s = window.round40States.find((x) => x.name === n); s.apply(); }, name);
  await page.waitForTimeout(500);
  const sel = await page.evaluate((n) => window.round40States.find((x) => x.name === n).scene || 'survey', name);
  await page.locator('#' + sel).scrollIntoViewIfNeeded();
  await page.locator('#' + sel).screenshot({ path: shot(name) });
  console.log(dir + '-' + name + '.png');
  await page.evaluate((n) => { const s = window.round40States.find((x) => x.name === n); s.undo && s.undo(); }, name);
}

await browser.close();
