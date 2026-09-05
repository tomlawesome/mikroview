// Screenshot round 45: the wizard step in each state.
// group in each of its states. Run from frontend/ (playwright lives in
// the main checkout):
//   node ../docs/design/concepts/round-45/capture.mjs
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
await page.goto('file://' + path.join(here, 'wizard.html'));
await page.waitForTimeout(1400);

const wiz = page.locator('#wiz');
await wiz.scrollIntoViewIfNeeded();
await page.waitForTimeout(500);
await wiz.screenshot({ path: shot('step6-rest') });
console.log('step6-rest.png');
for (const st of ['warrived', 'wnokey', 'wlost']) {
  await page.evaluate((c) => document.getElementById('wiz').classList.add(c), st);
  await page.waitForTimeout(300);
  await wiz.screenshot({ path: shot('step6-' + st.slice(1)) });
  console.log('step6-' + st.slice(1) + '.png');
  await page.evaluate((c) => document.getElementById('wiz').classList.remove(c), st);
}

await browser.close();
