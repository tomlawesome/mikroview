// Screenshot round 37's scenes and the states the round adds.
// Run from frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-38/capture.mjs
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

// the fall: the window chip, the empty band, the quieter count
await scene('s2', 'fall');

// the topography at rest, then degraded
await scene('s3', 'topography');
await page.evaluate(() => document.getElementById('s3').classList.add('degraded'));
await page.waitForTimeout(300);
await page.locator('#s3').screenshot({ path: shot('topography-degraded') });
console.log('topography-degraded.png');
await page.evaluate(() => document.getElementById('s3').classList.remove('degraded'));

// metrics: the hourline reading every series, and the table view with its ledger
await page.locator('#s4').scrollIntoViewIfNeeded();
await page.waitForTimeout(400);
await page.locator('#s4').screenshot({ path: shot('metrics-seismograph') });
console.log('metrics-seismograph.png');
await page.locator('#mviews span[data-v="tab"]').click();
await page.waitForTimeout(350);
await page.locator('#s4').screenshot({ path: shot('metrics-table-ledger') });
console.log('metrics-table-ledger.png');
await page.locator('#mviews span[data-v="seis"]').click();

// the stream: at rest, then a seek (following off), following back on,
// paused, grouped, a column boundary under the hand, and wiped
await scene('s5', 'stream');
const wsvg = page.locator('#wsvg');
const box = await wsvg.boundingBox();
await page.mouse.click(box.x + box.width * 0.62, box.y + box.height / 2);
await page.waitForTimeout(300);
await page.locator('#s5').screenshot({ path: shot('stream-seek-held') });
console.log('stream-seek-held.png');
await page.locator('#hfollow').click();
await page.waitForTimeout(300);
await page.locator('#s5').screenshot({ path: shot('stream-following-again') });
console.log('stream-following-again.png');
await page.locator('#hpause').click();
await page.waitForTimeout(300);
await page.locator('#s5 .whisper').screenshot({ path: shot('stream-paused-whisper') });
console.log('stream-paused-whisper.png');
await page.locator('#hpause').click();
await page.locator('#hgroup').click();
await page.waitForTimeout(300);
await page.locator('#s5').screenshot({ path: shot('stream-grouped') });
console.log('stream-grouped.png');
await page.locator('#hgroup').click();
await page.locator('table.stream th').nth(2).hover();
await page.waitForTimeout(300);
await page.locator('table.stream thead').screenshot({ path: shot('stream-head-resize-hover') });
console.log('stream-head-resize-hover.png');
// saved filters: the list open under the box (the top of the scene, so the list shows)
await page.locator('#fsaved').click();
await page.waitForTimeout(300);
{
  const b = await page.locator('#s5').boundingBox();
  await page.screenshot({ path: shot('stream-saved-filters'), clip: { x: b.x, y: b.y, width: b.width, height: 360 } });
  console.log('stream-saved-filters.png');
}
await page.mouse.click(10, 600);
await page.waitForTimeout(200);
await page.locator('#hwipe').click();
await page.waitForTimeout(300);
await page.locator('#s5').screenshot({ path: shot('stream-wiped') });
console.log('stream-wiped.png');

// entities: hosts, then rules, then ports; then as a viewer; then an unregistered router
await scene('ent', 'entities-hosts');
await page.locator('#eviews span[data-v="rules"]').click();
await page.waitForTimeout(300);
await page.locator('#ent').screenshot({ path: shot('entities-rules') });
console.log('entities-rules.png');
await page.locator('#eviews span[data-v="ports"]').click();
await page.waitForTimeout(300);
await page.locator('#ent').screenshot({ path: shot('entities-ports') });
console.log('entities-ports.png');
await page.locator('#eviews span[data-v="hosts"]').click();
await page.evaluate(() => document.getElementById('ent').classList.add('viewer'));
await page.waitForTimeout(300);
await page.locator('#ent').screenshot({ path: shot('entities-viewer') });
console.log('entities-viewer.png');
await page.evaluate(() => { const e = document.getElementById('ent'); e.classList.remove('viewer'); e.classList.add('unreg'); });
await page.waitForTimeout(300);
await page.locator('#ent .fcards').screenshot({ path: shot('entities-unregistered-router') });
console.log('entities-unregistered-router.png');
await page.evaluate(() => document.getElementById('ent').classList.remove('unreg'));

// the account menu, with uptime beside the version
await page.locator('#s7').scrollIntoViewIfNeeded();
await page.waitForTimeout(300);
await page.locator('#s7 button.who').click();
await page.waitForTimeout(400);
await page.screenshot({ path: shot('account-menu-uptime'), clip: { x: 1600 - 320, y: 0, width: 320, height: 300 } });
console.log('account-menu-uptime.png');

// settings: the whole screen, then the memory group at rest, proposing a grow, proposing a shrink
await scene('set', 'settings');
await page.locator('#memg').screenshot({ path: shot('settings-memory') });
console.log('settings-memory.png');
for (const st of ['mgrow', 'mshrink']) {
  await page.evaluate((c) => document.getElementById('set').classList.add(c), st);
  await page.waitForTimeout(300);
  await page.locator('#memg').screenshot({ path: shot('settings-memory-' + st.slice(1)) });
  console.log('settings-memory-' + st.slice(1) + '.png');
  await page.evaluate((c) => document.getElementById('set').classList.remove(c), st);
}

await browser.close();
