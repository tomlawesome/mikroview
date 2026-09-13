// Screenshot round 57's scenes.
// Run from frontend/ (playwright lives in this worktree's own checkout):
//   node ../docs/design/concepts/round-57/capture.mjs
// Shots land in shots/<scene>.png.
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(here, '../../../../frontend/package.json'));
const { chromium } = require('playwright');

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1300, height: 1000 }, deviceScaleFactor: 2 });
page.on('pageerror', (e) => console.log('PAGE ERROR:', e.message));
await page.goto('file://' + path.join(here, 'index.html'));
await page.waitForTimeout(500);

const ids = await page.evaluate(() => Array.from(document.querySelectorAll('section.scene')).map((s) => s.id));
for (const id of ids) {
  const el = page.locator('#' + id);
  await el.scrollIntoViewIfNeeded();
  await page.waitForTimeout(300);
  await el.screenshot({ path: path.join(here, 'shots', id + '.png') });
  console.log(id + '.png');
}
await browser.close();
