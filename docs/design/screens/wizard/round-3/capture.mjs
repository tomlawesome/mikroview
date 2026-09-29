// SPDX-License-Identifier: AGPL-3.0-only
//
// Screenshots round 3's scenes: the real SetupWizard.svelte component,
// rendered in Chromium against fake-api.mjs. Run from frontend/:
//   node ../docs/design/screens/wizard/round-3/capture.mjs
//
// The app is dark-only since #708 (light/system themes were removed
// wholesale) -- see this round's README for the record. There is no
// light variant to capture, so unlike round 1/2's mockup captures this
// only ever produces one theme.
//
// Ported in shape from round-2's capture.mjs (this same tree), but
// driving a live app over http instead of a static file://, because the
// component being screenshotted fetches its own data.

import { fileURLToPath } from 'node:url'
import path from 'node:path'
import { spawn } from 'node:child_process'
import fs from 'node:fs'
import { createRequire } from 'node:module'

const here = path.dirname(fileURLToPath(import.meta.url))
// playwright lives in frontend/node_modules, not in this directory's own
// (nonexistent) node_modules -- same reason round-2's capture.mjs next
// door resolves it off process.cwd() rather than a plain import.
const require = createRequire(path.join(process.cwd(), 'package.json'))
const { chromium } = require('playwright')
const PORT = process.env.FAKE_API_PORT || '8791'
const BASE = `http://127.0.0.1:${PORT}`
const SHOTS = path.join(here, 'shots')
fs.mkdirSync(SHOTS, { recursive: true })

function waitForServer(url, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs
  return new Promise((resolve, reject) => {
    const tryOnce = () => {
      fetch(url)
        .then(() => resolve())
        .catch((err) => {
          if (Date.now() > deadline) reject(err)
          else setTimeout(tryOnce, 150)
        })
    }
    tryOnce()
  })
}

const server = spawn(process.execPath, [path.join(here, 'fake-api.mjs')], {
  env: { ...process.env, FAKE_API_PORT: PORT },
  stdio: ['ignore', 'pipe', 'pipe'],
})
server.stdout.on('data', (d) => process.stdout.write(`[fake-api] ${d}`))
server.stderr.on('data', (d) => process.stderr.write(`[fake-api] ${d}`))
await waitForServer(BASE + '/')

const browser = await chromium.launch()

async function newPage() {
  const context = await browser.newContext({
    viewport: { width: 1600, height: 1000 },
    deviceScaleFactor: 2,
    colorScheme: 'dark',
  })
  const page = await context.newPage()
  return { context, page }
}

// visitScenario sets the scenario cookie (also resetting that
// scenario's server-side state fresh) and loads the app.
async function visitScenario(page, name) {
  await page.goto(`${BASE}/scenario/${name}`)
  await page.waitForLoadState('networkidle')
}

// ensureWizardOpen opens the modal via the account menu's "Run setup…"
// row -- unless the record's own auto-launch (a fresh admin session
// with no router yet and no marks) has already opened it, which is
// itself one of the real states this round exists to show.
async function ensureWizardOpen(page) {
  const dialog = page.locator('.modal.setup-wizard')
  if (await dialog.count() > 0 && (await dialog.isVisible())) return
  await page.locator('button.chip:visible').first().click()
  await page.getByRole('menuitem', { name: 'Run setup…' }).click()
  await dialog.waitFor({ state: 'visible' })
}

// jumpTo clicks a step's row in the wizard's own left-hand step list --
// an ordinary user action (the ledger's rows are never gated on the
// previous step being done), not a shortcut around the UI.
async function jumpTo(page, title) {
  const row = page.locator('nav[aria-label="Setup steps"] .step-row', { hasText: title }).first()
  await row.click()
  await page.waitForTimeout(150)
}

async function jumpToFinish(page) {
  await page.locator('nav[aria-label="Setup steps"] .finish-row').click()
  await page.waitForTimeout(150)
}

async function shoot(page, name) {
  const dialog = page.locator('.modal.setup-wizard')
  await dialog.waitFor({ state: 'visible' })
  // Let one poll tick's worth of settling happen (fonts, a just-landed
  // fetch) before the pixels are taken -- cheap insurance against a
  // half-rendered frame, not a wait on POLL_MS's 5s interval.
  await page.waitForTimeout(200)
  // A step's own text can grow once its data lands (e.g. "nothing has
  // arrived yet" -> a two-line receipt), which reflows every row below
  // it -- the mouse stays where the last click left it, and a stray
  // :hover box then lands on whatever row drifted underneath. Park the
  // pointer off the modal before every shot so hover state never rides
  // into a screenshot by accident.
  await page.mouse.move(0, 0)
  await page.waitForTimeout(50)
  await dialog.screenshot({ path: path.join(SHOTS, `${name}-dark.png`) })
  console.log(name)
}

async function closeWizard(page) {
  // Esc closes the modal; harmless if it is not open.
  await page.keyboard.press('Escape').catch(() => {})
}

const STEP_TITLES = {
  ca: 'Trust the certificate',
  name: 'Name your router',
  syslog: 'Send logs',
  rules: 'Tag firewall rules',
  push: 'Push router state',
  backup: 'Back up the router',
  register: 'Register the router',
}

// --- scenes ----------------------------------------------------------------

async function sceneFreshInstall() {
  const { context, page } = await newPage()
  await visitScenario(page, 'fresh-install')
  await ensureWizardOpen(page)

  await shoot(page, 'ca-waiting') // already on pane 1
  await jumpTo(page, STEP_TITLES.name)
  await shoot(page, 'name-quiet')
  await jumpTo(page, STEP_TITLES.rules)
  await shoot(page, 'rules-waiting')
  await jumpTo(page, STEP_TITLES.push)
  await shoot(page, 'push-no-token')
  await jumpTo(page, STEP_TITLES.backup)
  await shoot(page, 'backup-no-token')

  await jumpTo(page, STEP_TITLES.register)
  await page.getByRole('button', { name: 'Register this router' }).click()
  await page.waitForTimeout(200)
  await shoot(page, 'register-error')

  await jumpToFinish(page)
  await shoot(page, 'finish-fresh')

  await context.close()
}

async function sceneCaBlocked() {
  const { context, page } = await newPage()
  await visitScenario(page, 'ca-blocked')
  await ensureWizardOpen(page)
  await shoot(page, 'ca-blocked')
  await context.close()
}

async function sceneSourceSplit() {
  const { context, page } = await newPage()
  await visitScenario(page, 'source-split')
  await ensureWizardOpen(page)
  await jumpTo(page, STEP_TITLES.syslog)
  await shoot(page, 'syslog-partial-split')
  await context.close()
}

async function sceneRulesPartial(scenario, name) {
  const { context, page } = await newPage()
  await visitScenario(page, scenario)
  await ensureWizardOpen(page)
  await jumpTo(page, STEP_TITLES.rules)
  await shoot(page, name)
  await context.close()
}

async function sceneMostlyDone() {
  const { context, page } = await newPage()
  await visitScenario(page, 'mostly-done')
  await ensureWizardOpen(page)

  await shoot(page, 'ca-done') // pane 1
  await jumpTo(page, STEP_TITLES.name)
  await shoot(page, 'name-quiet-existing-router')
  await jumpTo(page, STEP_TITLES.syslog)
  await shoot(page, 'syslog-done')
  await jumpTo(page, STEP_TITLES.rules)
  await shoot(page, 'rules-done')
  await jumpTo(page, STEP_TITLES.push)
  await shoot(page, 'push-done')
  await jumpTo(page, STEP_TITLES.backup)
  await shoot(page, 'backup-done')
  await jumpTo(page, STEP_TITLES.register)
  await shoot(page, 'register-quiet-existing-router')
  await jumpToFinish(page)
  await shoot(page, 'finish-mostly-done')

  await context.close()
}

async function sceneBackupBlocked() {
  const { context, page } = await newPage()
  await visitScenario(page, 'backup-blocked')
  await ensureWizardOpen(page)
  await jumpTo(page, STEP_TITLES.backup)
  await shoot(page, 'backup-blocked')
  await context.close()
}

async function sceneLostRouter() {
  const { context, page } = await newPage()
  await visitScenario(page, 'lost-router')
  // This one door is Settings ▸ router backups' own "is it gone?" link
  // (EngineRoom.svelte/RouterBackups.svelte), not Run setup… -- see the
  // README's coverage notes.
  await page.locator('nav.roll-rail button', { hasText: 'Settings' }).click()
  await page.getByRole('button', { name: 'is it gone?' }).click()
  await shoot(page, 'backup-lost-router')
  await context.close()
}

async function sceneWalkthrough() {
  const { context, page } = await newPage()
  await visitScenario(page, 'walkthrough')
  await ensureWizardOpen(page)

  // Name the router -- Next on the name pane is what creates it.
  await jumpTo(page, STEP_TITLES.name)
  await page.locator('#router-name').fill('rb5009')
  await page.getByRole('button', { name: 'Next' }).click()
  await page.waitForTimeout(300)
  await shoot(page, 'name-done')

  // Mint the enrolment token for it -- the plain "waiting, nothing minted
  // yet" state first, since every later syslog shot has already used
  // the mint-ask form.
  await jumpTo(page, STEP_TITLES.syslog)
  await page.waitForTimeout(200)
  await shoot(page, 'syslog-waiting')
  await page.locator('#setup-wizard-enrol-address').fill('192.168.13.1')
  await page.locator('#setup-wizard-enrol-password').fill('correct-horse-battery-staple')
  await page.getByRole('button', { name: 'Mint the token' }).click()
  await page.waitForTimeout(300)
  await shoot(page, 'syslog-token-fresh')

  // Push: the one known router auto-mints a push token on arrival.
  await jumpTo(page, STEP_TITLES.push)
  await page.waitForTimeout(400)
  await shoot(page, 'push-with-token')

  // Clear the header address field to reach push's "no address" body,
  // then put the story's address back before moving on.
  const addressField = page.locator('#setup-wizard-address')
  await addressField.fill('')
  await page.waitForTimeout(500)
  await shoot(page, 'push-no-address')
  await addressField.fill('192.168.13.15:8080')
  await page.waitForTimeout(500)

  // Backup: same auto-mint, still waiting for a first push to arrive.
  await jumpTo(page, STEP_TITLES.backup)
  await page.waitForTimeout(400)
  await shoot(page, 'backup-normal-with-token')

  // Register.
  await jumpTo(page, STEP_TITLES.register)
  await page.getByRole('button', { name: 'Register this router' }).click()
  await page.waitForTimeout(300)
  await shoot(page, 'register-done')

  await context.close()
}

async function sceneWalkthroughExpired() {
  const { context, page } = await newPage()
  await visitScenario(page, 'walkthrough-expired-token')
  await ensureWizardOpen(page)
  await jumpTo(page, STEP_TITLES.name)
  await page.locator('#router-name').fill('rb5009')
  await page.getByRole('button', { name: 'Next' }).click()
  await page.waitForTimeout(300)
  await jumpTo(page, STEP_TITLES.syslog)
  await page.locator('#setup-wizard-enrol-address').fill('192.168.13.1')
  await page.locator('#setup-wizard-enrol-password').fill('correct-horse-battery-staple')
  await page.getByRole('button', { name: 'Mint the token' }).click()
  await page.waitForTimeout(300)
  await shoot(page, 'syslog-token-expired')
  await context.close()
}

async function sceneWalkthroughRefused() {
  const { context, page } = await newPage()
  await visitScenario(page, 'walkthrough-refused')
  await ensureWizardOpen(page)
  await jumpTo(page, STEP_TITLES.name)
  await page.locator('#router-name').fill('rb5009')
  await page.getByRole('button', { name: 'Next' }).click()
  await page.waitForTimeout(300)
  await jumpTo(page, STEP_TITLES.syslog)
  await page.locator('#setup-wizard-enrol-address').fill('192.168.13.1')
  await page.locator('#setup-wizard-enrol-password').fill('correct-horse-battery-staple')
  await page.getByRole('button', { name: 'Mint the token' }).click()
  await page.waitForTimeout(300)
  // refusedForThisWalk only ever reflects the *next* GET
  // /api/devices/refused after the mint (the pane's own poll re-fires
  // on an interval, not on wizardState.enrolmentMintedAt changing) --
  // see SetupWizard.svelte's own effect around POLL_MS. Wait past one
  // full tick so the box has something to filter in.
  await page.waitForTimeout(5200)
  await shoot(page, 'syslog-refused-senders')
  await context.close()
}

try {
  await sceneFreshInstall()
  await sceneCaBlocked()
  await sceneSourceSplit()
  await sceneRulesPartial('rules-partial-undecoded', 'rules-partial-undecoded')
  await sceneRulesPartial('rules-partial-some', 'rules-partial-some')
  await sceneMostlyDone()
  await sceneBackupBlocked()
  await sceneLostRouter()
  await sceneWalkthrough()
  await sceneWalkthroughExpired()
  await sceneWalkthroughRefused()
} finally {
  await browser.close()
  server.kill()
}
