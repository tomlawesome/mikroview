// Screenshot round 14's two scenes, and round 13's repaired metrics
// views (register/table switchers were dead when first served).
// Run from frontend/ (playwright lives there):
//   node ../docs/design/concepts/round-14/capture.mjs
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
// Playwright lives in the main checkout's frontend, not this worktree's.
const require = createRequire(path.join(process.env.HOME, 'projects/mikroview/frontend/package.json'));
const { chromium } = require('playwright');

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 });

// Round 14: candidate C, purple and cyan.
await page.goto('file://' + path.join(here, 'aggregate-style-c.html'));
await page.waitForTimeout(1200);
for (const scene of ['Cp', 'Cc']) {
  const el = page.locator('#' + scene);
  await el.scrollIntoViewIfNeeded();
  await page.waitForTimeout(400);
  await el.screenshot({ path: path.join(here, 'shots', `${scene}.png`) });
  console.log(`${scene}.png`);
}

// Round 13: the deck's metrics scene, all three views.
await page.goto('file://' + path.join(here, '../round-13/the-deck.html'));
await page.waitForTimeout(1200);
const s4 = page.locator('#s4');
await s4.scrollIntoViewIfNeeded();
await page.waitForTimeout(600);
for (const [view, label] of [['seis', 'seismograph'], ['reg', 'register'], ['tab', 'table']]) {
  await page.locator(`#mviews span[data-v="${view}"]`).click();
  await page.waitForTimeout(300);
  await s4.screenshot({ path: path.join(here, '../round-13/shots', `metrics-${label}.png`) });
  console.log(`metrics-${label}.png`);
}
await browser.close();
