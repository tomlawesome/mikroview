// Screenshots for round 2 (#1360). Run from anywhere:
//   node docs/design/screens/blocklist/round-2/capture.mjs
// Playwright's chromium from the frontend's node_modules; the browser from /opt/pw-browsers.
import { chromium } from '/home/codex/projects/mikroview/frontend/node_modules/playwright/index.mjs';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const shots = path.join(here, 'shots');
fs.mkdirSync(shots, { recursive: true });
const candidates = ['/opt/pw-browsers/chromium', ...(fs.existsSync('/opt/pw-browsers') ? fs.readdirSync('/opt/pw-browsers').filter(d => d.startsWith('chromium-')).map(d => `/opt/pw-browsers/${d}/chrome-linux/chrome`) : [])];
const executablePath = candidates.find(p => fs.existsSync(p));

// file · query · shot name
const scenes = [
  ['builder.html', '', 'builder-01-7.24.4'],
  ['builder.html', '?ros=7.19.4', 'builder-02-7.19.4'],
  ['builder.html', '?ros=7.12.1', 'builder-03-below-floor'],
  ['builder.html', '?more=1', 'builder-04-left-out-open'],
  ['builder.html', '?undo=1', 'builder-05-undo-open'],
  ['tail.html', '?scene=stand', 'tail-01-where-setup-stands'],
  ['tail.html', '?scene=build', 'tail-02-the-stage'],
  ['tail.html', '?scene=after', 'tail-03-after-the-paste'],
];
const browser = await chromium.launch(executablePath ? { executablePath } : {});
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 1 });
for (const [file, query, name] of scenes) {
  const f = path.join(here, file);
  if (!fs.existsSync(f)) { console.log('skip', name, '(no', file + ')'); continue; }
  await page.goto('file://' + f + query);
  await page.waitForTimeout(300);
  // the considered-and-left-out record lives under the cards: show it where the operator would scroll to
  if (query.includes('more=1')) await page.evaluate(() => { const c = document.querySelector('.col.left'); if (c) c.scrollTop = c.scrollHeight; });
  await page.screenshot({ path: path.join(shots, name + '.png') });
  console.log('captured', name);
}
await browser.close();
console.log('browser:', executablePath ?? 'playwright default');
