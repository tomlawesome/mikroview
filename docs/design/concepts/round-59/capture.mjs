// Screenshot round 59: ACTIVITY SPIKE's drawer with the note box empty,
// DROP SURGE's with last time's note read back and a draft in the box.
// Run from frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-59/capture.mjs
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

// the page opens itself onto the flags tab with d7's drawer out
await page.goto('file://' + path.join(here, 'combined.html'));
await page.waitForTimeout(1200);
await take('combined-spike-empty');

await page.locator('tr[data-d="d7"]').click();
await page.locator('tr[data-d="d5"]').click();
await take('combined-surge-readback');

await browser.close();
