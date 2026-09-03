// Screenshot round 42: the settings screen, then the disk group in each
// of its states. Run from frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-42/capture.mjs
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
await page.goto('file://' + path.join(here, 'disk.html'));
await page.waitForTimeout(1400);

const set = page.locator('#set');
await set.scrollIntoViewIfNeeded();
await page.waitForTimeout(500);
await set.screenshot({ path: shot('settings') });
console.log('settings.png');

// the memory and disk groups together: the pair the round is about
await page.locator('#memg').evaluate((el) => el.parentElement.classList.add('pair'));
await page.locator('#diskg').screenshot({ path: shot('settings-disk') });
console.log('settings-disk.png');
for (const st of ['dshrink', 'dgrow', 'dcap', 'doff', 'dcapped', 'dstopped', 'dnokey']) {
  await page.evaluate((c) => document.getElementById('set').classList.add(c), st);
  await page.waitForTimeout(300);
  await page.locator('#diskg').screenshot({ path: shot('settings-disk-' + st.slice(1)) });
  console.log('settings-disk-' + st.slice(1) + '.png');
  await page.evaluate((c) => document.getElementById('set').classList.remove(c), st);
}

await browser.close();
