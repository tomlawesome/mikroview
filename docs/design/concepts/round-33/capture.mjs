// Screenshot every state round 33 adds to the docket's watchlist tab.
// Run from frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-33/capture.mjs
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
await page.goto('file://' + path.join(here, 'suggestions-matches.html'));
await page.waitForTimeout(800);

async function take(name, sel = '#s7') {
  await page.waitForTimeout(450);
  await page.locator(sel).screenshot({ path: shot(name) });
  console.log(name + '.png');
}

// 1. resting: round 31's tab, with the suggestions body beneath the watches
await take('watchlist');

// 2. matches: the held watch's drawer, its last-match line grown into a list
await page.locator('tr[data-d="w2"]').click();
await take('matches-held');
await page.locator('tr[data-d="w2"]').click();
await page.locator('tr[data-d="w3"]').click();
await take('matches-learning');
await page.locator('tr[data-d="w3"]').click();

// 3. suggestions: a device from a lease, a port from a drop rule, a stale address list
await page.locator('tr[data-d="g1"]').click();
await take('suggestion-device');
await page.locator('tr[data-d="g1"]').click();
await page.locator('tr[data-d="g2"]').click();
await take('suggestion-port');
await page.locator('tr[data-d="g2"]').click();
await page.locator('tr[data-d="g3"]').click();
await take('suggestion-stale');
await page.locator('tr[data-d="g3"]').click();

// 4. watch it: the device suggestion becomes a learning watch, in place
await page.locator('tr[data-d="g1"]').click();
await page.locator('#g1 .accept').click();
await take('accepted');
await page.locator('tr[data-d="g1"]').click();

// 5. not this, then show them, then bring it back
await page.locator('tr[data-d="g3"]').click();
await page.locator('#g3 .aside').click();
await page.locator('#sugshow').click();
await take('set-aside-shown');
await page.locator('tr[data-d="h1"]').click();
await take('set-aside-drawer');
await page.locator('#h1 .back').click();
await take('brought-back');

// 6. start over: armed, then done
await page.locator('#sugreset').click();
await take('start-over-armed');
await page.locator('#sugreset').click();
await take('started-over');

await browser.close();
