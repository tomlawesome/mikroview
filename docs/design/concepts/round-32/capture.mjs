// Screenshot every state round 32 adds to the Settings card.
// Run from frontend/ (playwright lives in the main checkout):
//   node ../docs/design/concepts/round-32/capture.mjs
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(path.join(process.env.HOME, 'projects/mikroview/frontend/package.json'));
const { chromium } = require('playwright');

const shot = (n) => path.join(here, 'shots', n + '.png');
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1600, height: 1760 }, deviceScaleFactor: 2 });
page.on('pageerror', (e) => console.log('PAGE ERROR:', e.message));
await page.goto('file://' + path.join(here, 'settings-doors.html'));
await page.waitForTimeout(800);

async function take(name, sel = '#set') {
  await page.waitForTimeout(450);
  await page.locator(sel).screenshot({ path: shot(name) });
  console.log(name + '.png');
}

// 1. resting: round 30's settings plus the two doors
await take('settings');

// 2. people: the form, then a viewer let in, then remove armed
await page.locator('#addperson').click();
await page.locator('#p-name').fill('sam');
await page.locator('#p-pass').fill('correct-horse-battery');
await page.locator('#p-role b[data-v="viewer"]').click();
await take('people-form', '#people');
await page.locator('#p-let').click();
await take('people-let-in', '#people');
await page.locator('[data-u="mia"] .remove').click();
await take('people-remove-armed', '#people');
await page.locator('body').click({ position: { x: 5, y: 5 } });

// 3. keys: the form for an ingest key, the once-only reveal, then a read-only key's reveal, then revoke armed
await page.locator('#addkey').click();
await page.locator('#k-kind b[data-v="ingest"]').click();
await page.locator('#k-dev b[data-v="hap-ax2"]').click();
await page.locator('#k-name').fill('hap-ax2 (replacement)');
await take('keys-form-ingest', '#keys');
await page.locator('#k-mint').click();
await take('keys-reveal-ingest', '#keys');
await page.locator('#kr-done').click();
await page.locator('#addkey').click();
await page.locator('#k-name').fill('birdcage');
await page.locator('#k-mint').click();
await take('keys-reveal-readonly', '#keys');
await page.locator('#kr-copy').click();
await page.locator('#kr-done').click();
await page.locator('[data-k="grafana"] .revoke').click();
await take('keys-revoke-armed', '#keys');

await browser.close();
