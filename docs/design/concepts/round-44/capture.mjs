// Screenshot round 44: the settings screen, then the router backups
// group in each of its states. Run from frontend/ (playwright lives in
// the main checkout):
//   node ../docs/design/concepts/round-44/capture.mjs
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
await page.goto('file://' + path.join(here, 'backups.html'));
await page.waitForTimeout(1400);

const set = page.locator('#set');
await set.scrollIntoViewIfNeeded();
await page.waitForTimeout(500);
await set.screenshot({ path: shot('settings') });
console.log('settings.png');

async function group(name) {
  await page.locator('#bakg').screenshot({ path: shot(name) });
  console.log(name + '.png');
}
await group('backups-rest');
for (const st of ['brecv', 'brefused', 'bquota', 'bnone', 'bnokey', 'bfail']) {
  await page.evaluate((c) => document.getElementById('set').classList.add(c), st);
  await page.waitForTimeout(300);
  await group('backups-' + st.slice(1));
  await page.evaluate((c) => document.getElementById('set').classList.remove(c), st);
}
// no key is one fact told twice: the disk group and the backups group agree
await page.evaluate(() => document.getElementById('set').classList.add('dnokey', 'bnokey'));
await page.waitForTimeout(300);
await set.screenshot({ path: shot('settings-nokey') });
console.log('settings-nokey.png');

await browser.close();
