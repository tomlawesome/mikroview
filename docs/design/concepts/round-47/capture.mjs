// Screenshot every state round 47 adds to the docket's flags tab.
// Run from frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-47/capture.mjs
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
await page.goto('file://' + path.join(here, 'campaigns.html'));
await page.waitForTimeout(800);

async function take(name, sel = '#s7') {
  await page.mouse.move(0, 0); // no hover state in a shot
  await page.waitForTimeout(450);
  await page.locator(sel).screenshot({ path: shot(name) });
  console.log(name + '.png');
}

// every shot is the flags tab
await page.locator('#dtabs span[data-p="flags"]').click();

// 1. resting: the strip across the top, the campaign collapsed, seven open flags in five rows
await take('flags');

// 2. the campaign opened: its three flags one step in, and UNPLANNED's drawer as round 34 drew it
await page.locator('tr[data-c="c1"]').click();
await page.locator('tr[data-d="d1"]').click();
await take('campaign-open');
await page.locator('tr[data-d="d1"]').click();
await page.locator('tr[data-c="c1"]').click();

// 3. a scored flag's drawer: the number beside the type, and the line saying where it came from
await page.locator('tr[data-d="d7"]').click();
await take('scored');
await page.locator('tr[data-d="d7"]').click();

// 4. the strip as a filter: repeated drops picked, the campaign opened to just that flag
await page.locator('#btcells .btc[data-t="repeated drops"]').click();
await take('filtered-by-type');
await page.locator('#btcells .btc[data-t="repeated drops"]').click();

// 5. the campaign called noise from its own row: all three dim, the strip counts down
await page.locator('tr[data-c="c1"] .v[data-v="noise"]').click();
await take('campaign-called-noise');
await page.locator('tr[data-c="c1"]').click();
await take('campaign-called-noise-open');
await page.locator('tr[data-c="c1"] .vdone a').click();
await take('campaign-undone');

await browser.close();
