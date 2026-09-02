// SPDX-License-Identifier: AGPL-3.0-only
//
// Drives a real mikroview in a real browser. Companion to live-env.sh.
//
// Import the helpers from a per-change scenario script rather than
// editing this file: the point is that each PR gets its own short
// scenario, not that this grows into a second test suite.
//
//   import { session, feedSyslog, check, done } from './live-browser.mjs'
//   const { page } = await session()
//   ...
//   done()

import { chromium, firefox, webkit } from 'playwright'
import { execFileSync } from 'child_process'
import { setGlobalDispatcher, Agent } from 'undici'
import { fileURLToPath } from 'url'
import path from 'path'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..')

const URL_BASE = process.env.MV_URL
const USER = process.env.MV_USER
const PASS = process.env.MV_PASS
if (!URL_BASE) {
  console.error('MV_URL unset -- run: eval "$(scripts/live-env.sh up)"')
  process.exit(2)
}

/**
 * MV_BROWSER picks the engine every scenario runs under: chromium (the
 * default, and the only engine the gate has ever driven), firefox, or
 * webkit.
 *
 * It exists because "every scenario is Chromium" was itself the gate's
 * biggest blind spot. #659 was a static style="..." attribute that
 * Chromium tolerates and this app's CSP-conscious Firefox refuses --
 * and it shipped green through live-check, vitest and every screenshot,
 * because nothing in the gate had ever asked a second engine. Scenarios
 * do not read this directly; resolving it once here, and handing every
 * browser launch (this file's own session() and the handful of
 * scenarios that open a second browser for a second signed-in tab)
 * through launchBrowser() below, is what makes "run the suite under
 * Firefox" mean the whole suite rather than most of it.
 *
 * An unrecognised value exits rather than falling back to chromium.
 * Silently substituting the default would produce a run that reports
 * PASS believing it exercised Firefox when it never left Chromium --
 * which is a worse outcome than the run simply refusing to start, since
 * a green result then gets cited as coverage that was never taken.
 */
const ENGINES = { chromium, firefox, webkit }
const BROWSER_NAME = process.env.MV_BROWSER || 'chromium'
if (!(BROWSER_NAME in ENGINES)) {
  console.error(
    `MV_BROWSER=${JSON.stringify(process.env.MV_BROWSER)} is not a recognised engine -- ` +
      `choose one of: ${Object.keys(ENGINES).join(', ')}`,
  )
  process.exit(2)
}

/**
 * launchBrowser is chromium.launch() (or firefox's, or webkit's)
 * with one difference: a missing browser binary says so in the one
 * sentence that matters -- which engine, and the exact command that
 * fixes it -- instead of Playwright's own multi-line "Looks like
 * Playwright was just installed or updated" block, which names a path
 * under ~/.cache and never says `npx playwright install <engine>`.
 *
 * Only that specific failure is rewritten. Anything else (a real crash,
 * missing system libraries, ...) is rethrown as Playwright reported it,
 * because guessing a friendlier message for a failure this function
 * does not understand risks hiding what actually went wrong.
 */
export async function launchBrowser() {
  try {
    return await ENGINES[BROWSER_NAME].launch()
  } catch (e) {
    const message = String(e?.message ?? e)
    if (/Executable doesn't exist/.test(message)) {
      console.error(`${BROWSER_NAME}'s browser binary is not installed -- run: npx playwright install ${BROWSER_NAME}`)
      process.exit(2)
    }
    throw e
  }
}

let failed = false
let browser

/** check records a failure without aborting, so one run reports everything. */
export function check(ok, message) {
  console.log(`  ${ok ? 'ok  ' : 'FAIL'} ${message}`)
  if (!ok) failed = true
}

/**
 * ENV_SCRIPT is the harness driving the instance under test.
 *
 * live-env.sh (a locally built binary) is the default and stays the
 * everyday path. MV_ENV_SCRIPT points the same scenarios at
 * live-container.sh instead, which runs the image as it ships, under the
 * hardening it ships with, optionally against Postgres (#273 slice 1).
 *
 * Selecting the environment here rather than in each scenario is the
 * whole point: a scenario that has to know which environment it is in
 * would drift between the two, and the value of running them against the
 * container is that they are *the same scenarios*.
 */
const ENV_SCRIPT = path.join(REPO, process.env.MV_ENV_SCRIPT || 'scripts/live-env.sh')

// Node's own fetch has to accept the self-signed certificate too.
//
// Scenarios reach the API two ways: through Playwright's page.request
// (which inherits the browser context's ignoreHTTPSErrors) and through
// bare fetch (which does not, and failed with
// UNABLE_TO_VERIFY_LEAF_SIGNATURE against the container). Setting the
// dispatcher once here covers every scenario rather than making five of
// them each remember; doing it per scenario is how one gets missed.
//
// Deliberately not NODE_TLS_REJECT_UNAUTHORIZED=0, which would disable
// verification for the whole process including anything else it talks
// to. This is scoped to fetch, in a harness, against a certificate the
// server under test generated for itself moments earlier. Under
// live-env.sh (plain HTTP on loopback) it changes nothing.
setGlobalDispatcher(new Agent({ connect: { rejectUnauthorized: false } }))

/** feedSyslog pushes synthetic events into the running instance. */
export function feedSyslog(n, label = 'live-test-rule') {
  execFileSync(ENV_SCRIPT, ['syslog', String(n), label], {
    stdio: 'ignore',
    cwd: REPO,
  })
}

/**
 * feedRaw delivers one exact syslog line, for a scenario needing a
 * specific event shape rather than feedSyslog's bulk pattern.
 *
 * Go through this rather than opening a socket in the scenario: syslog
 * TLS is the only listener since #189 removed the plaintext ones, so a
 * hand-rolled UDP send delivers nothing at all -- silently, since there
 * is no longer anything bound to refuse it.
 */
export function feedRaw(line) {
  execFileSync(ENV_SCRIPT, ['raw', line], {
    stdio: 'ignore',
    cwd: REPO,
  })
}

/**
 * feedPortScan delivers n distinct destination ports from one source
 * inside the port-scan window, so a real port_scan flag is *raised* by
 * the detector rather than synthesized by the test.
 *
 * Lives here rather than being copied per scenario: six scenarios had
 * their own identical version, each with the harness path hardcoded, so
 * pointing them at a different environment meant editing six files and
 * missing one was silent.
 */
export function feedPortScan(n, sourceIp) {
  const args = ['portscan', String(n)]
  if (sourceIp) args.push(sourceIp)
  execFileSync(ENV_SCRIPT, args, { stdio: 'ignore', cwd: REPO })
}

/**
 * feedInternalRecon delivers n distinct internal destinations from one
 * LAN source, each reached on `port`, so a real internal_recon flag is
 * raised carrying evidence pairs (#641) -- the shape an expected verdict
 * permits and a "watch for this" draft is built from.
 *
 * Same reasoning as feedPortScan above for living here rather than in
 * the one scenario that needs it today.
 */
export function feedInternalRecon(n, sourceIp, port) {
  const args = ['recon', String(n)]
  if (sourceIp) args.push(sourceIp)
  if (port) args.push(String(port))
  execFileSync(ENV_SCRIPT, args, { stdio: 'ignore', cwd: REPO })
}

/**
 * isUntrustedCertServiceWorkerError filters out the one console error a
 * browser always produces against a self-signed certificate it has not
 * been told to trust.
 *
 * ignoreHTTPSErrors suppresses the interstitial for navigation and
 * fetches, but service-worker registration is deliberately stricter:
 * Chromium refuses to register one over a certificate outside its trust
 * store regardless, so mikroview's PWA registration fails with
 * `SecurityError: Failed to register a ServiceWorker ... An SSL
 * certificate error occurred when fetching the script.`
 *
 * That is browser policy, not a mikroview defect, and it is narrowly
 * matched here (both the SSL and service-worker halves must be present)
 * so a genuine service-worker error is still reported.
 *
 * It has an operator-facing consequence, which is documented in
 * docs/configuration.md rather than only filtered away here: with
 * mikroview's generated certificate, the install-as-an-app and offline
 * behaviour do not work until the CA it serves at /ca.crt is trusted by
 * the browser. Everything else works with the usual click-through.
 */
// Two messages, not one: the SecurityError from the registration call,
// and a bare resource-load error for the script fetch that does not
// mention the service worker at all. Both are matched, and both are
// specific to a *script* fetch failing certificate validation, so an
// ordinary failed request still surfaces.
function isUntrustedCertServiceWorkerError(text) {
  if (!/SSL certificate error/i.test(text)) return false
  return /ServiceWorker/i.test(text) || /fetching the script/i.test(text)
}

/** session launches a browser and signs in, returning a live page. */
/**
 * dismissSetupWizard closes the setup modal if a fresh instance
 * auto-launched it (#487).
 *
 * The modal opens on first admin sign-in with no router sending, which
 * is exactly the state a freshly stood-up harness is in before any
 * scenario has fed anything. It is a real focus trap over a real veil,
 * so leaving it up makes every other scenario's first click fail an
 * actionability check for a reason that has nothing to do with what it
 * is testing.
 *
 * Gated on the server's own device list rather than a blind wait: with a
 * device already known the modal cannot auto-launch, so there is nothing
 * to wait for and no seconds to spend waiting for it. `waitFor` rather
 * than `isVisible`, because isVisible answers immediately about a modal
 * that is still one paint away.
 */
export async function dismissSetupWizard(page) {
  const devices = await page.request.get(`${URL_BASE}/api/devices`).then((r) => r.json())
  if (Array.isArray(devices) && devices.length > 0) return
  const modal = page.locator('.setup-wizard')
  await modal.waitFor({ state: 'visible', timeout: 10000 }).catch(() => {})
  if (await modal.count()) {
    await page.keyboard.press('Escape')
    await modal.waitFor({ state: 'detached', timeout: 5000 })
  }
}

/**
 * session's own landing default is 'stream' (#616 retired #544's interim
 * -- the fall is the real landing page now, not Stream) so that every
 * scenario written against the old landing keeps working unmodified:
 * session() signs in, then navigates to Stream itself before returning,
 * exactly where those scenarios already assume they start. Pass
 * `landing: 'fall'` (live-fall.mjs's own case) to stay on the fall
 * instead of being moved off it.
 */
/**
 * SCENES maps the deck's visible names to their view keys (Deck.svelte's
 * own table). Anything not in here is an operate page or account action,
 * reached through the account chip's menu instead.
 */
// Each entry names the card the rail rolls to (Deck.svelte's data-card
// key). The docket is one card whose tabs are the flags/watchlist/audit
// views (#633), so those labels roll its card and then click the tab.
//
// Entities and Settings are here because #647 made them the deck's last
// two cards. They used to be account-menu rows, and this table is what
// tells goTo which way to reach a label -- so a destination that moves
// into the deck without moving into this table is not a slow test, it
// is a scenario that dies at the menu with no RESULT line at all.
//
// `tab` is matched with text-is against the tab's own label, so it is
// the string Docket.svelte renders: 'audit log', not 'audit'.
const SCENES = {
  'The fall': { rail: 'The fall', card: 'fall' },
  Topography: { rail: 'Topography', card: 'topography' },
  Metrics: { rail: 'Metrics', card: 'metrics' },
  Stream: { rail: 'Stream', card: 'live' },
  'The docket': { rail: 'The docket', card: 'docket' },
  Flags: { rail: 'The docket', card: 'docket', tab: 'flags' },
  Watchlist: { rail: 'The docket', card: 'docket', tab: 'watchlist' },
  'Audit log': { rail: 'The docket', card: 'docket', tab: 'audit log' },
  Entities: { rail: 'Entities', card: 'entities' },
  Settings: { rail: 'Settings', card: 'engineroom' },
  // #657: a viewer's deck carries Fleet in place of Entities/Settings
  // (deckCards.ts's `fleet` key) -- the same standalone page the
  // phone-width bottom bar has always reached, now also on the roll rail.
  Fleet: { rail: 'Fleet', card: 'fleet' },
}

/**
 * openAccountMenu opens the scene bar's account chip menu -- where the
 * operate pages and account actions live since #616's deck retired the
 * atlas overlay.
 *
 * Scoped to the card that is actually centred: the deck mounts the
 * active card *and its neighbours*, each carrying its own scene bar, so
 * a bare `.chip` selector can resolve to an off-viewport neighbour --
 * and clicking that would scroll the deck to it. Outside the deck (the
 * operate pages) there is exactly one scene bar and no cards at all.
 */
export async function openAccountMenu(page) {
  if ((await page.locator('.account .menu').count()) > 0) return
  const inDeck = (await page.locator('.deck').count()) > 0
  const chip = inDeck
    ? page.locator('.card[aria-hidden="false"] .account button.chip')
    : page.locator('.account button.chip')
  await chip.click()
  await page.waitForSelector('.account .menu', { timeout: 5000 })
}

/**
 * unfoldStreamFilter opens the strip that holds the stream's filter
 * fields (input.rule and its neighbours).
 *
 * Round 30 (#697) changed how that happens. The box itself --
 * `.filterline .fbox` -- is always on screen and is the disclosure:
 * clicking anywhere inside it opens the strip. The standalone
 * "Filters ▸" trigger this used to click belonged to round 8's folded
 * box (#644) and is retired, not merely hidden: FilterBar.svelte gates
 * it on FILTERS_TRIGGER_ENABLED, which is `false`, so
 * `button.fold-trigger` renders nowhere and the old click was a silent
 * no-op -- the count guard swallowed it, the strip never opened, and
 * every scenario then timed out waiting for input.rule (#667).
 *
 * Idempotent, via the box's own `open` class rather than the trigger's
 * absence: clicking an already-open box would not close it, but nor is
 * there any reason to. The mobile drawer has its own trigger and no
 * .filterline, hence the count guard rather than a bare click.
 */
export async function unfoldStreamFilter(page) {
  const box = page.locator('.filterline .fbox')
  if (!(await box.count())) return
  if (await box.evaluate((el) => el.classList.contains('open'))) return
  await box.click()
}

/**
 * goTo navigates by visible label exactly as an operator does. Deck
 * scenes go via the roll rail's name buttons; everything still living
 * in the account chip's menu ("Run setup…", ...) via its menu row of
 * the same text. SCENES above is the list of the former, and is the
 * only thing that decides which route a label takes.
 *
 * After #647 that split moved: Settings, Entities and Audit log are
 * deck destinations now, and the menu keeps only theme, Run setup…,
 * change password, SSO linking, sign out and About.
 *
 * For a scene, waits until the card has actually rolled to centre --
 * appState.view flips on click, but the smooth scroll runs ~700ms and a
 * scenario reading geometry mid-roll would see a card in flight.
 */
export async function goTo(page, label, { unfold = true } = {}) {
  const scene = SCENES[label]
  if (scene) {
    await page.click(`.roll-rail button.rail-name:text-is("${scene.rail}")`)
    await page.waitForFunction(
      (c) => {
        const deck = document.querySelector('.deck')
        const el = deck?.querySelector(`.card[data-card="${c}"]`)
        if (!el) return false
        // Bounding rects, not offsetTop vs scrollTop: offsetTop is
        // measured from the offset parent, so anything above the deck
        // (the connection banner, say) shifts it and the two never agree.
        return Math.abs(el.getBoundingClientRect().top - deck.getBoundingClientRect().top) < 2
      },
      scene.card,
      { timeout: 10000 },
    )
    if (scene.tab) {
      // Round 30 (#697/#700) moved the docket's tabs into SceneBar's own
      // switcher (.switch[role="tablist"] .sw). They are still
      // `role="tab"` buttons (SceneBar.svelte:77-99), but the label now
      // sits straight in the button: the inner `.tlabel` span this used
      // to match through is gone. Ten scenarios died here, every one of
      // them before its own first assertion (#692, #667).
      await page.click(`.card[data-card="${scene.card}"] [role="tab"]:text-is("${scene.tab}")`)
    }
    // Every arrival at the stream, not just the first. FilterBar's
    // `expanded` is component-local $state(false), so the card comes back
    // folded each time the deck rolls away and back -- it does not
    // remember how the last visit left it. #662 unfolded once in
    // session(), which left live-connection-states and live-waterfall
    // timing out on `input.rule` after navigating away and returning
    // (#667).
    if (scene.card === 'live' && unfold) await unfoldStreamFilter(page)
  } else {
    await openAccountMenu(page)
    // Say which label is missing, and what the menu does hold, rather
    // than letting page.click wait its full 30s and throw a bare
    // TimeoutError. That timeout kills the scenario before it prints a
    // RESULT line, so the run records a silent death and the log never
    // says why -- four scenarios were lost that way when #647 moved
    // Settings and Entities out of this menu and into the deck (#667).
    const row = page.locator(`.account .menu button.row:text-is("${label}")`)
    if ((await row.count()) === 0) {
      const rows = await page.locator('.account .menu button.row').allTextContents()
      throw new Error(
        `goTo(${JSON.stringify(label)}): no such account-menu row, and it is not a deck scene either. ` +
          `The menu holds: ${rows.map((r) => JSON.stringify(r.trim())).join(', ') || '(none)'}. ` +
          `If this destination moved into the deck, add it to SCENES in live-browser.mjs.`,
      )
    }
    await row.click()
    await page.waitForSelector('.account .menu', { state: 'detached', timeout: 5000 })
  }
}

export async function session({ waitForEvents = 0, dismissSetup = true, landing = 'stream', unfoldFilter = true } = {}) {
  browser = await launchBrowser()
  // ignoreHTTPSErrors, because the certificate under test is one
  // mikroview generated for itself seconds ago -- self-signed, with no
  // chain to verify against.
  //
  // Needed only since the container environment arrived: live-env.sh
  // serves plain HTTP on loopback, so this never came up, and all
  // fifteen scenarios failed at page.goto with ERR_CERT_AUTHORITY_INVALID
  // the first time they were pointed at the image, which serves HTTPS as
  // it ships. Accepting the certificate is right here and is not a hole:
  // whether a *router* should trust it is a different question, and one
  // live-routeros.sh's `trust` step covers properly against real
  // RouterOS rather than by waving it through.
  const page = await browser.newPage({ ignoreHTTPSErrors: true })
  const consoleErrors = []
  const record = (text) => {
    if (isUntrustedCertServiceWorkerError(text)) return
    consoleErrors.push(text)
  }
  page.on('pageerror', (e) => record(String(e)))
  page.on('console', (m) => {
    if (m.type() === 'error') record(m.text())
  })

  await page.goto(URL_BASE, { waitUntil: 'networkidle' })
  await page.fill('input[autocomplete="username"]', USER)
  await page.fill('input[autocomplete="current-password"]', PASS)
  await page.click('button[type="submit"]')
  // #main-content is the one marker present on every signed-in view
  // (App.svelte wraps all of them in it) -- unlike the old `input.rule`
  // wait, it does not assume which view is the landing page.
  await page.waitForSelector('#main-content', { timeout: 15000 })

  // Before anything else touches the page: a first-run instance layers
  // the setup modal over the shell, and every scenario but the wizard's
  // own wants it out of the way. See dismissSetupWizard.
  if (dismissSetup) await dismissSetupWizard(page)

  if (landing === 'stream') {
    await goTo(page, 'Stream', { unfold: unfoldFilter })
    // Wait for whichever shape was asked for. A scenario that opted out
    // is testing the closed box itself, so waiting for input.rule would
    // both time out and destroy the state under test.
    //
    // The closed shape is the always-present type-ahead input inside the
    // box (#697). It is not `button.fold-trigger`: that control is
    // retired, so waiting for it timed out for every scenario that
    // opted out, exactly as the open shape did for the rest (#667).
    await page.waitForSelector(unfoldFilter ? 'input.rule' : '.filterline input.fbtype', { timeout: 15000 })
  }

  if (waitForEvents > 0) {
    // Scoped to the Stream card: the deck keeps neighbouring cards
    // mounted, and their scenes render .row elements of their own, so a
    // bare .row count can be satisfied before any event has rendered.
    await page.waitForFunction(
      (n) => document.querySelectorAll('.card[data-card="live"] .row').length >= n,
      waitForEvents,
      { timeout: 20000 },
    )
  }
  return { page, consoleErrors }
}

/**
 * waitForFlag waits for a flag against `target` to exist on the server,
 * and returns what it found. Call it after feeding a scan and before
 * asserting on the Flags UI (#354).
 *
 * It exists to split one ambiguous failure into two clear ones. A
 * scenario that feeds a scan and then waits on a card has two ways to
 * fail -- the flag was never raised, or it was raised and the UI did not
 * show it -- and a Playwright locator timeout cannot tell them apart. It
 * reports only that something never became visible, which is the least
 * useful half of the answer.
 *
 * It is also the race itself. The Flags list refreshes on mount and then
 * every STATS_REFRESH_MS (5s, App.svelte), so a 15s wait gets about
 * three attempts; ingest running a few seconds behind on a shared
 * instance that earlier scenarios have pushed hundreds of events through
 * is enough to miss all of them. Waiting for the server first means the
 * UI assertion starts from a state where the flag definitely exists.
 *
 * Polled through the signed-in page rather than a separate HTTP client,
 * so it uses the session cookie already established and the same
 * listener the browser is talking to.
 */
export async function waitForFlag(page, target, { timeoutMs = 20000 } = {}) {
  const deadline = Date.now() + timeoutMs
  let seen = []
  while (Date.now() < deadline) {
    seen = await page.evaluate(async () => {
      const res = await fetch('/api/flags', { cache: 'no-store' })
      if (!res.ok) return []
      const body = await res.json()
      const list = Array.isArray(body) ? body : (body.flags ?? [])
      return list.map((f) => ({ type: f.type, target: f.target, cleared: !!f.cleared }))
    })
    if (seen.some((f) => f.target === target && !f.cleared)) {
      return { ok: true, seen, message: `a flag for ${target} reached the server` }
    }
    await page.waitForTimeout(500)
  }
  // The whole flag list only on failure -- that is when it is evidence.
  // Printing it on the way past would bury every other line in the run.
  const summary = seen.length
    ? seen.map((f) => `${f.type}:${f.target}${f.cleared ? ' (cleared)' : ''}`).join(', ')
    : 'none at all'
  return {
    ok: false,
    seen,
    message: `no flag for ${target} reached the server within ${timeoutMs}ms -- server has: ${summary}`,
  }
}

/**
 * responsive asserts the main thread still answers -- a hung tab cannot
 * run an evaluate at all, so a timeout here is the failure.
 */
export async function responsive(page, forMs = 2000) {
  const deadline = Date.now() + forMs
  while (Date.now() < deadline) {
    try {
      await page.evaluate(() => document.title, { timeout: 1500 })
    } catch {
      return false
    }
    await page.waitForTimeout(200)
  }
  return true
}

export function done() {
  browser?.close()
  console.log(failed ? 'RESULT: FAIL' : 'RESULT: PASS')
  process.exit(failed ? 1 : 0)
}
