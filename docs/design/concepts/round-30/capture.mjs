// Screenshot every round-30 scene, and both states of the stream's filter.
// Run from frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-30/capture.mjs
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
await page.goto('file://' + path.join(here, 'the-whole.html'));
await page.waitForTimeout(1400);

async function scene(id, name) {
  const el = page.locator('#' + id);
  await el.scrollIntoViewIfNeeded();
  await page.waitForTimeout(500);
  await el.screenshot({ path: shot(name) });
  console.log(name + '.png');
}

await scene('s1', 'door');
await scene('firsthour', 'journey');
await scene('s2', 'fall');
await scene('s3', 'topography');

// metrics, all three views
await page.locator('#s4').scrollIntoViewIfNeeded();
await page.waitForTimeout(400);
for (const [v, label] of [['seis', 'seismograph'], ['reg', 'register'], ['tab', 'table']]) {
  await page.locator(`#mviews span[data-v="${v}"]`).click();
  await page.waitForTimeout(350);
  await page.locator('#s4').screenshot({ path: shot('metrics-' + label) });
  console.log('metrics-' + label + '.png');
}
await page.locator('#mviews span[data-v="seis"]').click();

// the stream: bar folded, bar out, and the box with every term cleared
await scene('s5', 'stream');
await page.locator('#fbopen').click();
await page.waitForTimeout(500);
await page.locator('#s5').screenshot({ path: shot('stream-bar-out') });
console.log('stream-bar-out.png');
await page.locator('#s5 .fb-clear').click();
await page.waitForTimeout(300);
await page.locator('#s5').screenshot({ path: shot('stream-no-filter') });
console.log('stream-no-filter.png');
await page.locator('#fbfold').click();
await page.waitForTimeout(400);

// the docket, all three tabs
await page.locator('#s7').scrollIntoViewIfNeeded();
await page.waitForTimeout(400);
for (const [p, label] of [['flags', 'flags'], ['watch', 'watchlist'], ['audit', 'audit']]) {
  await page.locator(`#dtabs span[data-p="${p}"]`).click();
  await page.waitForTimeout(350);
  await page.locator('#s7').screenshot({ path: shot('docket-' + label) });
  console.log('docket-' + label + '.png');
}
await page.locator('#dtabs span[data-p="flags"]').click();

await scene('ent', 'entities');
await scene('set', 'settings');

await browser.close();
