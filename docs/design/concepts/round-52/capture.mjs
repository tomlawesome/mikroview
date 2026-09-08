// Screenshot round 50's three scenes, plus the fold opened.
// Run from frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-52/capture.mjs
// Shots land in shots/<scene>.png, with shots/<scene>-crop.png the
// 900x700 top-right quarter: the column and the router's shoulder.
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(process.env.HOME, 'projects/mikroview/frontend/package.json'));
const { chromium } = require('playwright');

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 });
page.on('pageerror', (e) => console.log('PAGE ERROR:', e.message));
await page.goto('file://' + path.join(here, 'index.html'));
await page.waitForTimeout(1400);

async function shoot(id, name) {
  await page.evaluate((i) => {
    document.getElementById(i).scrollIntoView({ block: 'start' });
    window.scrollBy(0, -20);
  }, id);
  await page.waitForTimeout(400);
  const el = page.locator('#' + id);
  await el.screenshot({ path: path.join(here, 'shots', name + '.png') });
  const b = await el.boundingBox();
  await page.screenshot({ path: path.join(here, 'shots', name + '-crop.png'),
    clip: { x: b.x + b.width - 900, y: b.y, width: 900, height: 700 } });
  console.log(name + '.png · ' + name + '-crop.png');
}

const ids = await page.evaluate(() => Array.from(document.querySelectorAll('section.scene')).map((s) => s.id));
for (const id of ids) {
  await shoot(id, id);
}
await browser.close();
