// Screenshot round 46's marks.html — direction C, the only direction
// the page draws now (A and B were rejected by the owner and removed).
// Run from frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-46/capture.mjs
// Shots land in shots/paint-<scene>.png, plus a close crop of cam-porch
// and of pihole (the 3-flag demonstration host). cam-porch lives in
// IoT, which round 40's #street camera (centred on LAN) never frames,
// so its crop is taken from #alarm instead, where cam-porch is the
// scene's own subject.
//
// #981: cam-porch and pihole also carry an activity-spike pulse (a 2s
// breathing rim + glow on their alarm mark) — a still can't show motion,
// so paint-spike-low.png and paint-spike-high.png freeze the same
// cam-porch crop at the pulse's dim and bright points, and
// paint-spike-reduced.png shows the steady state prefers-reduced-motion
// draws instead.
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(process.env.HOME, 'projects/mikroview/frontend/package.json'));
const { chromium } = require('playwright');

const shot = (n) => path.join(here, 'shots', 'paint-' + n + '.png');
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 });
page.on('pageerror', (e) => console.log('PAGE ERROR:', e.message));
await page.goto('file://' + path.join(here, 'marks.html'));
await page.waitForTimeout(1400);

for (const id of ['survey', 'street', 'alarm']) {
  const el = page.locator('#' + id);
  await el.scrollIntoViewIfNeeded();
  await page.waitForTimeout(500);
  await el.screenshot({ path: shot(id) });
  console.log('paint-' + id + '.png');
}

// a close crop of a building, from whichever scene has it on stage
async function closeCrop(sceneId, ariaPrefix, name, clipW, clipH) {
  const scene = page.locator('#' + sceneId);
  await scene.scrollIntoViewIfNeeded();
  await page.waitForTimeout(300);
  const box = await scene.boundingBox();
  const center = await page.evaluate((sel) => {
    const g = document.querySelector(sel);
    if (!g) return null;
    const r = g.getBoundingClientRect();
    return { x: r.x + r.width / 2, y: r.y + r.height / 2 };
  }, '#' + sceneId + ' [aria-label^="' + ariaPrefix + '"]');
  if (center && box) {
    const clipX = Math.max(box.x, Math.min(box.x + box.width - clipW, center.x - clipW / 2));
    const clipY = Math.max(box.y, Math.min(box.y + box.height - clipH, center.y - clipH / 2));
    await page.screenshot({ path: shot(name), clip: { x: clipX, y: clipY, width: clipW, height: clipH } });
    console.log('paint-' + name + '.png');
  } else {
    console.log(name + ' not found in #' + sceneId);
  }
}

// two flags and watched, the busiest single building in the set (see
// the file header for why #alarm)
await closeCrop('alarm', 'cam-porch', 'cam-porch', 420, 300);
// the 3-flag demonstration host, close enough to read the opacity step
await closeCrop('survey', 'pihole', '3flags', 260, 220);

// #981: freeze cam-porch's activity-spike pulse at its dim (t=0) and
// bright (t=1, the 2s cycle's midpoint) points, by giving every pulsing
// element a negative animation-delay and then pausing it — the standard
// way to park a CSS animation at an arbitrary frame without waiting.
async function setSpikeFrame(t) {
  await page.evaluate((t) => {
    document.querySelectorAll('.mk-spike-rim, .mk-spike-glow').forEach((el) => {
      el.style.animationPlayState = 'running';
      el.style.animationDelay = (-t) + 's';
    });
    document.body.getBoundingClientRect();   // force layout so the delay lands before pausing
    document.querySelectorAll('.mk-spike-rim, .mk-spike-glow').forEach((el) => {
      el.style.animationPlayState = 'paused';
    });
  }, t);
}
await page.locator('#alarm').scrollIntoViewIfNeeded();
await setSpikeFrame(0);
await closeCrop('alarm', 'cam-porch', 'spike-low', 420, 300);
await setSpikeFrame(1);
await closeCrop('alarm', 'cam-porch', 'spike-high', 420, 300);

// the steady state prefers-reduced-motion draws instead of the pulse
await page.emulateMedia({ reducedMotion: 'reduce' });
await page.waitForTimeout(200);
await closeCrop('alarm', 'cam-porch', 'spike-reduced', 420, 300);

await browser.close();
