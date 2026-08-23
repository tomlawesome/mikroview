// Screenshot round 3's scenes, dark and light. Run from frontend/:
//   node ../docs/design/screens/navigation/round-5/capture.mjs
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(process.cwd(), 'package.json'));
const { chromium } = require('playwright');

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 });
for (const theme of ['dark', 'light']) {
  const qs = theme === 'light' ? '?theme=light' : '';
  await page.goto('file://' + path.join(here, 'direction-q5-rail.html') + qs);
  await page.waitForTimeout(1200);
  for (const scene of ['s1', 's2', 's3', 's4']) {
    const el = page.locator('#' + scene);
    await el.scrollIntoViewIfNeeded();
    await page.waitForTimeout(400);
    await el.screenshot({ path: path.join(here, 'shots', `q5-${scene}-${theme}.png`) });
    console.log(`q5-${scene}-${theme}.png`);
  }
}
await browser.close();
