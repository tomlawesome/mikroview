// Screenshot every state round 31 adds to the docket's watchlist tab.
// Run from frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-31/capture.mjs
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
await page.goto('file://' + path.join(here, 'watchlist-managed.html'));
await page.waitForTimeout(800);

async function take(name) {
  await page.waitForTimeout(450);
  await page.locator('#s7').screenshot({ path: shot(name) });
  console.log(name + '.png');
}

// 1. resting: the tab as round 30 drew it, plus `+ watch`, a learning watch and a paused one
await take('watchlist');

// 2. a blank draft, from `+ watch`
await page.locator('#wbtn').click();
await take('draft-blank');
await page.locator('#f-mode .mode[data-m="fence"]').click();
await page.locator('#f-who').fill('printer-hall');
await page.locator('#f-seg b[data-w="between"]').click();
await take('draft-fence-window');
await page.locator('#f-discard').click();

// 3. a draft written by a flag: `watch this pathway` on the UNPLANNED flag
await page.locator('#dtabs span[data-p="flags"]').click();
await page.locator('#d1 .dwr-acts button', { hasText: 'watch this pathway' }).click();
await take('draft-from-flag');
await page.locator('#f-discard').click();

// 4. the learning watch: permit per place, permit all, fence now
await page.locator('tr[data-d="w3"]').click();
await take('learning');
await page.locator('#w3 .permit').first().click();
await take('learning-permit-one');
await page.locator('#fencenow').click();
await take('fenced');
await page.locator('tr[data-d="w3"]').click();

// 5. mend: the broken watch's drawer, then its form with the fix typed in
await page.locator('tr[data-d="w1"]').click();
await take('broken');
await page.locator('#w1 .mend').click();
await take('mend');
await page.locator('#w1 [data-save]').click();
await take('mended');
await page.locator('tr[data-d="w1"]').click();

// 6. remove, armed
await page.locator('tr[data-d="w2"]').click();
await page.locator('#w2 .remove').click();
await take('remove-armed');
await page.locator('body').click({ position: { x: 5, y: 5 } });
await page.locator('tr[data-d="w2"]').click();

// 7. paused
await page.locator('tr[data-d="w4"]').click();
await take('paused');

await browser.close();
