// Screenshot round 41's scenes: the metrics hourline after a warm restart
// (absolute and relative wording), after a cold start, and the docket's
// chip. Run from frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-41/capture.mjs
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
await page.goto('file://' + path.join(here, 'warm-restart.html'));
await page.waitForTimeout(1400);

async function scene(id, name) {
  const el = page.locator('#' + id);
  await el.scrollIntoViewIfNeeded();
  await page.waitForTimeout(500);
  await el.screenshot({ path: shot(name) });
  console.log(name + '.png');
}

await scene('s4', 'metrics-restored');
await scene('s4-cold', 'metrics-cold');
await scene('s4-rel', 'metrics-restored-relative');
await scene('s7', 'docket-restored');

await browser.close();
