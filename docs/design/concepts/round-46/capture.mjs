// Screenshot round 46's marks.html for both mark directions.
// Run from frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-46/capture.mjs
// Shots land in shots/<dir>-<scene>.png, plus a close crop of cam-porch
// in shots/<dir>-cam-porch.png. cam-porch lives in IoT, which round 40's
// #street camera (centred on LAN) never frames, so the crop is taken
// from #alarm instead, where cam-porch is the scene's own subject.
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(process.env.HOME, 'projects/mikroview/frontend/package.json'));
const { chromium } = require('playwright');

const shot = (dir, n) => path.join(here, 'shots', dir + '-' + n + '.png');
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 });
page.on('pageerror', (e) => console.log('PAGE ERROR:', e.message));
await page.goto('file://' + path.join(here, 'marks.html'));
await page.waitForTimeout(1400);

for (const dir of ['tint', 'ring']) {
  const btn = page.locator('.markstoggle .mtb[data-marks="' + dir + '"]');
  await btn.click();
  await page.waitForTimeout(500);

  for (const id of ['survey', 'street', 'alarm']) {
    const el = page.locator('#' + id);
    await el.scrollIntoViewIfNeeded();
    await page.waitForTimeout(500);
    await el.screenshot({ path: shot(dir, id) });
    console.log(dir + '-' + id + '.png');
  }

  // a close crop of cam-porch: two flags and watched, the busiest
  // single building in the set (see the file header for why #alarm)
  const alarm = page.locator('#alarm');
  await alarm.scrollIntoViewIfNeeded();
  const box = await alarm.boundingBox();
  const camPorch = await page.evaluate(() => {
    const g = document.querySelector('#alarm [aria-label^="cam-porch"]');
    if (!g) return null;
    const r = g.getBoundingClientRect();
    return { x: r.x + r.width / 2, y: r.y + r.height / 2 };
  });
  if (camPorch && box) {
    const clipW = 420, clipH = 300;
    const clipX = Math.max(box.x, Math.min(box.x + box.width - clipW, camPorch.x - clipW / 2));
    const clipY = Math.max(box.y, Math.min(box.y + box.height - clipH, camPorch.y - clipH / 2));
    await page.screenshot({ path: shot(dir, 'cam-porch'), clip: { x: clipX, y: clipY, width: clipW, height: clipH } });
    console.log(dir + '-cam-porch.png');
  } else {
    console.log('cam-porch not found in #alarm for direction ' + dir);
  }
}

await browser.close();
