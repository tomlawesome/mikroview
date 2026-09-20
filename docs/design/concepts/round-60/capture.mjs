// Screenshot round 60: each landing-page direction, full page at desktop
// width and the first screen at phone width. Run from frontend/ (playwright
// lives in the main checkout):
//   node ../docs/design/concepts/round-60/capture.mjs
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(process.env.HOME, 'projects/mikroview/frontend/package.json'));
const { chromium } = require('playwright');

const shot = (n) => path.join(here, 'shots', n + '.png');
const browser = await chromium.launch();

for (const name of ['fall-opened', 'docket', 'tour']) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 });
  page.on('pageerror', (e) => console.log('PAGE ERROR:', name, e.message));
  await page.goto('file://' + path.join(here, name + '.html'));
  await page.waitForTimeout(800);
  await page.screenshot({ path: shot(name + '-top'), fullPage: false });
  await page.screenshot({ path: shot(name + '-full'), fullPage: true });
  await page.close();

  const phone = await browser.newPage({ viewport: { width: 400, height: 820 }, deviceScaleFactor: 1 });
  await phone.goto('file://' + path.join(here, name + '.html'));
  await phone.waitForTimeout(800);
  await phone.screenshot({ path: shot(name + '-phone'), fullPage: false });
  await phone.close();
  console.log(name);
}
await browser.close();
