// Screenshot every state round 33 adds to the docket's watchlist tab.
// Run from frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-34/capture.mjs
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
await page.goto('file://' + path.join(here, 'verdicts-in-row.html'));
await page.waitForTimeout(800);

async function take(name, sel = '#s7') {
  await page.mouse.move(0, 0); // no hover state in a shot
  await page.waitForTimeout(450);
  await page.locator(sel).screenshot({ path: shot(name) });
  console.log(name + '.png');
}

// every shot is the flags tab
await page.locator('#dtabs span[data-p="flags"]').click();

// 1. resting: round 30's flags tab, every row carrying its trio before the caret; the first drawer open as it ships
await take('flags');

// 2. called noise from the row, drawer closed: the port scan dims in place, undo where the trio was
await page.locator('tr[data-d="d2"] .v[data-v="noise"]').click();
await take('called-noise');

// 3. called expected from the row, then undone: the trio comes back
await page.locator('tr[data-d="d3"] .v[data-v="expected"]').click();
await take('called-expected');
await page.locator('tr[data-d="d3"] .vdone a').click();
await take('undone');

// 4. called real: stays open, says so
await page.locator('tr[data-d="d1"] .v[data-v="real"]').click();
await take('called-real');

// 5. never again: armed, then done — the pair joins the list below
await page.locator('tr[data-d="d4"]').click();
await page.locator('#d4 .never').click();
await take('never-again-armed');
await page.locator('#d4 .never').click();
await take('never-again-done');

// 6. the exclusions body: a pair's drawer, then let it fire again
await page.locator('tr[data-d="x1"]').click();
await take('exclusion-drawer');
await page.locator('#x1 .again').click();
await take('let-it-fire-again');

await browser.close();
