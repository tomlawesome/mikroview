// Screenshot round 61: each forced-enrolment direction, every scene, at
// desktop and phone width. Run from anywhere (playwright lives in the
// main checkout's frontend/):
//   node docs/design/concepts/round-61/capture.mjs
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(process.env.HOME, 'projects/mikroview/frontend/package.json'));
const { chromium } = require('playwright');

const shot = (n) => path.join(here, 'shots', n + '.png');
const browser = await chromium.launch();

const scenes = ['first', 'totp', 'passkey', 'codes', 'no-passkey'];
for (const name of ['same-door', 'porch', 'two-keys']) {
  for (const scene of scenes) {
    for (const [w, h, suffix] of [[1440, 900, ''], [400, 820, '-phone']]) {
      const page = await browser.newPage({ viewport: { width: w, height: h }, deviceScaleFactor: 1 });
      page.on('pageerror', (e) => console.log('PAGE ERROR:', name, scene, e.message));
      await page.goto('file://' + path.join(here, name + '.html') + '#' + scene);
      await page.waitForTimeout(6000);
      await page.screenshot({ path: shot(`${name}-${scene}${suffix}`), fullPage: false });
      await page.close();
    }
  }
  console.log(name);
}
await browser.close();
