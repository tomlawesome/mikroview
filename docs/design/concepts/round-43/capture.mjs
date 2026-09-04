// Screenshot round 43: the settings screen, then the memory and disk
// groups together in each state the on-restart row reads. Run from
// frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-43/capture.mjs
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
await page.goto('file://' + path.join(here, 'restart.html'));
await page.waitForTimeout(1400);

const set = page.locator('#set');
await set.scrollIntoViewIfNeeded();
await page.waitForTimeout(500);
await set.screenshot({ path: shot('settings') });
console.log('settings.png');

// memory and disk together: the pair whose rows have to agree
async function pair(name) {
  const a = await page.locator('#memg').boundingBox();
  const b = await page.locator('#diskg').boundingBox();
  const x = Math.min(a.x, b.x), y = Math.min(a.y, b.y);
  const clip = { x, y, width: Math.max(a.x + a.width, b.x + b.width) - x, height: Math.max(a.y + a.height, b.y + b.height) - y };
  await page.screenshot({ path: shot(name), clip });
  console.log(name + '.png');
}
await pair('pair-on');
for (const st of ['dstopped', 'dnokey', 'dfail']) {
  await page.evaluate((c) => document.getElementById('set').classList.add(c), st);
  await page.waitForTimeout(300);
  await pair('pair-' + st.slice(1));
  await page.evaluate((c) => document.getElementById('set').classList.remove(c), st);
}

await browser.close();
