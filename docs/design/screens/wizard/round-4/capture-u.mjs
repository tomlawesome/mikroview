// Screenshot direction U's nine scenes, dark only (the app has no light
// theme, #708). Shape copied from ../round-2/capture.mjs. Run from
// frontend/:
//   node ../docs/design/screens/wizard/round-4/capture-u.mjs
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import fs from 'node:fs';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(process.cwd(), 'package.json'));
const { chromium } = require('playwright');

const shotsDir = path.join(here, 'shots');
fs.mkdirSync(shotsDir, { recursive: true });

const launchOpts = {};
if (fs.existsSync('/opt/pw-browsers/chromium')) {
  launchOpts.executablePath = '/opt/pw-browsers/chromium';
}

const browser = await chromium.launch(launchOpts);
// Each scene is its own fixed 1440x900 app screen (no modal frame), so
// the viewport matches that exactly -- no scrolling into a taller page.
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
await page.goto('file://' + path.join(here, 'direction-u-wizard.html'));
await page.waitForTimeout(300);

for (const scene of ['u1', 'u2', 'u3', 'u4', 'u5', 'u6', 'u7', 'u8', 'u9']) {
  const el = page.locator('#' + scene);
  await el.scrollIntoViewIfNeeded();
  await page.waitForTimeout(150);
  await el.screenshot({ path: path.join(shotsDir, `u-${scene}-dark.png`) });
  console.log(`u-${scene}-dark.png`);
}
await browser.close();
