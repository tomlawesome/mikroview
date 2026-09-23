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
import http from 'node:http'
import https from 'node:https'
import { setGlobalDispatcher, Agent } from 'undici'
import fs from 'node:fs'
import { createHmac } from 'node:crypto'
import { fileURLToPath } from 'url'
import path from 'path'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..')

const URL_BASE = process.env.MV_URL
const USER = process.env.MV_USER
const PASS = process.env.MV_PASS

// Exported for #1291: minting an enrolment token asks the admin to
// re-enter their password at that moment, so a scenario driving the
// real ledger has to type it into the wizard as an operator would.
export const adminPassword = PASS
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
    // Chromium only, and not optional there: session()'s newPage sets
    // the per-context ignoreHTTPSErrors, which covers navigation and
    // page.request against the self-signed certificate the container
    // image serves HTTPS with -- but a service worker's own script
    // fetch is checked earlier, at the network stack Chromium launches
    // with, which that per-context override never reaches (the same
    // "Chromium refuses to register one over a certificate outside its
    // trust store regardless" isUntrustedCertServiceWorkerError already
    // documents). Without this flag, registration fails with a
    // SecurityError before it ever reaches 'activated', which is why
    // live-sw-navigation's `navigator.serviceWorker.ready` hung until
    // its own timeout in the container gate: not a slow activation to
    // wait longer for, but one that Chromium had already refused.
    // Verified this is Chromium-specific, not a gap in every engine:
    // Firefox activates the same worker over the same certificate with
    // no equivalent flag at all.
    const args = BROWSER_NAME === 'chromium' ? ['--ignore-certificate-errors'] : []
    return await ENGINES[BROWSER_NAME].launch({ args })
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

let sessionOpened = false

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
 *
 * feedRawFrom is the same with a source address: which address the
 * lines appear to arrive from (127.0.0.0/8), and so which device they
 * land under. The harness declares one router on 127.0.0.1, so anything
 * else auto-discovers as a router config.yaml has not declared. #600
 * needs one of those -- a device whose name nothing but the app decides
 * -- and it is the only way to get one without declaring a second
 * device for every scenario.
 */
export function feedRaw(...lines) {
  // Any number of lines go over one connection: live-env.sh's `raw`
  // opens a TLS session per call, so a scenario feeding two hundred
  // lines one call at a time paid two hundred handshakes and process
  // starts for them (#1061).
  execFileSync(ENV_SCRIPT, ['raw', ...lines], {
    stdio: 'ignore',
    cwd: REPO,
  })
}

/** eventsTotal reads the instance's lifetime event count (/api/stats). */
export async function eventsTotal(page) {
  const stats = await page.request.get(`${URL_BASE}/api/stats`).then((r) => r.json())
  return Number(stats.total ?? 0)
}

/**
 * waitForEventsTotal polls until the instance has counted at least `n`
 * events in its lifetime -- the wait a fixed sleep after a feed was
 * standing in for (#1061). Throws naming the count reached, so a line
 * the parser refused shows up as a short count rather than as whatever
 * the next check happened to say.
 */
export async function waitForEventsTotal(page, n, { timeoutMs = 15000 } = {}) {
  const deadline = Date.now() + timeoutMs
  let seen = -1
  while (Date.now() < deadline) {
    seen = await eventsTotal(page)
    if (seen >= n) return seen
    await new Promise((r) => setTimeout(r, 100))
  }
  throw new Error(`waited ${timeoutMs}ms for ${n} events; the instance has counted ${seen}`)
}

/**
 * feedAndSettle feeds the lines and returns once the instance has counted
 * every one of them. Use it where a scenario fed and then slept.
 */
export async function feedAndSettle(page, ...lines) {
  const before = await eventsTotal(page)
  feedRaw(...lines)
  return waitForEventsTotal(page, before + lines.length)
}

export function feedRawFrom(sourceIp, ...lines) {
  execFileSync(ENV_SCRIPT, ['rawfrom', sourceIp, ...lines], {
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

/**
 * isNavigationCancelledFetch filters the one message an engine other
 * than Chromium prints when a navigation cuts off a fetch still in
 * flight -- the app's own mount-time requests, cancelled by the reload
 * session() does after resetting the instance, or by a scenario's own
 * reload.
 *
 * Chromium drops a cancelled fetch silently. Firefox rejects it with a
 * bare `AbortError: The operation was aborted. ` that reaches
 * `pageerror` as an unhandled rejection, and WebKit logs `Fetch API
 * cannot load <url> due to access control checks.` to the console. The
 * harness reload confirmed both: the messages land within 200ms of
 * page.reload() and name the requests App.svelte starts on mount.
 * Nothing the app does can avoid them and no user sees a consequence,
 * so they are filtered here, narrowly: the Firefox text exactly, and
 * the WebKit one only for the app's own host, since a genuine
 * cross-origin refusal is still worth reporting.
 */
function isNavigationCancelledFetch(text) {
  if (text === 'AbortError: The operation was aborted. ') return true
  // WebKit prints the URL with a space after the scheme ("http: /127...")
  // so the host is matched rather than the whole address.
  return (
    text.startsWith('Fetch API cannot load ') &&
    text.endsWith(' due to access control checks.') &&
    text.includes(new URL(URL_BASE).host)
  )
}

/**
 * isScreenshotStyleRefusal filters the one message WebKit prints when
 * Playwright takes a screenshot: to hide the text caret it appends a
 * <style> element to the page, and mikroview's `default-src 'self'`
 * policy refuses it with "Refused to apply a stylesheet because its
 * hash, its nonce, or 'unsafe-inline' appears in neither the style-src
 * directive nor the default-src directive of the Content Security
 * Policy." The message arrived on exactly the page.screenshot() calls
 * and on no other step. Chromium and Firefox hide the caret another
 * way. Filtered on WebKit only, so an inline style the app itself
 * injected would still be reported by the other two engines.
 */
function isScreenshotStyleRefusal(text) {
  return BROWSER_NAME === 'webkit' && text.startsWith('Refused to apply a stylesheet because its hash, its nonce, or ')
}

/**
 * isResizeObserverLoopNotice filters "ResizeObserver loop completed with
 * undelivered notifications." The browser prints it when a resize
 * callback itself changes a size it is observing, so the remaining
 * notifications are held to the next frame -- the spec says to report
 * it, not to throw, and nothing is lost. WebKit surfaces it as a page
 * error where the other two do not; it arrived in the first WebKit run
 * on two layout-heavy scenarios (live-fall-composition,
 * live-topography-layout) and on no functional step.
 */
function isResizeObserverLoopNotice(text) {
  return text.includes('ResizeObserver loop completed with undelivered notifications')
}

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
  // The answer is `{ devices: [...] }`, not a bare array: reading it as
  // one made the guard never fire, so every scenario paid the full
  // ten-second wait for a modal that could not open (#1060).
  const body = await page.request.get(`${URL_BASE}/api/devices`).then((r) => r.json())
  const devices = Array.isArray(body) ? body : body?.devices
  if (Array.isArray(devices) && devices.length > 0) return
  const modal = page.locator('.setup-wizard')
  await modal.waitFor({ state: 'visible', timeout: 10000 }).catch(() => {})
  if (await modal.count()) {
    await page.keyboard.press('Escape')
    await modal.waitFor({ state: 'detached', timeout: 5000 })
  }
}

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
  // #1134: Log every rule joined the deck for the edit tier, so it is
  // reached the same way every other page is.
  'Log every rule': { rail: 'Log every rule', card: 'log-every-rule' },
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
  // Click the always-present hint input, not the box's own bounding-box
  // centre. `.fbox` is a flex-wrap row (FilterBar.svelte) whose content
  // shifts with the active filter chips, and FilterPresetsMenu's `.saved`
  // root -- pinned to the box's right end -- calls stopPropagation() on
  // every click inside it, so reaching for a saved filter doesn't also
  // unfold the strip (same reasoning as each chip's own `.chip-x`). Once
  // enough chips are active the row can wrap or shift far enough that a
  // plain box.click() lands in that dead zone: the click is swallowed,
  // the box never opens, and whatever comes next times out waiting for
  // input.rule -- live-waterfall's third boundary/carrier handoff (three
  // chips: interface, chain, port) is exactly the shape that moves the
  // centre point onto `.saved`. `.fbtype` (flex:1, min-width 60px) is the
  // one part of the box that never stops that propagation -- the
  // "genuine control" FilterBar.svelte's own comment names it as.
  await box.locator('input.fbtype').click()
  // #1246: that click also opens the token bar's field menu, which hangs
  // over the strip and the first rows of the table until focus leaves
  // the box or something outside it is clicked -- exactly as it does for
  // an operator. A scenario that unfolds the strip wants the strip, not
  // the menu, so hand focus on to the strip's own rule field: a click
  // inside the bar keeps the strip open (FilterBar's onWindowClick) and
  // closes the menu (its onBoxFocusOut). Without this, the next click on
  // a row hits a menu item instead (live-stream-table, 2026-09-16).
  const rule = page.locator('input.rule')
  await rule.waitFor({ state: 'visible', timeout: 15000 })
  await rule.click()
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
    ).catch(async (err) => {
      // #1011: seven scenarios died here in one run and every one of
      // them reported only "Timeout 10000ms exceeded", which cannot tell
      // the two possible causes apart -- the rail click never changed
      // the view, or the view changed and the roll never settled. Both
      // look identical in the log, and the scenario dies before printing
      // anything of its own, so the failing set names scenarios rather
      // than the one helper they share. Report the page's actual state
      // and then rethrow: behaviour on success is unchanged.
      // page.evaluate has no timeout of its own, so on a page that has
      // stopped answering it would hang and turn this 10s failure into a
      // stuck run. Race it.
      const seen = await Promise.race([
        // unref so a resolved evaluate does not leave a live timer
        // holding the process open for another five seconds.
        new Promise((r) => setTimeout(() => r({ evaluateTimedOut: true }), 5000).unref()),
        page
        .evaluate((c) => {
          const deck = document.querySelector('.deck')
          const el = deck?.querySelector(`.card[data-card="${c}"]`)
          return {
            deckPresent: !!deck,
            cardPresent: !!el,
            cards: [...(deck?.querySelectorAll('.card') ?? [])].map((n) => n.dataset.card),
            offsetFromDeckTop:
              el && deck ? el.getBoundingClientRect().top - deck.getBoundingClientRect().top : null,
          }
        }, scene.card)
        .catch((e) => ({ evaluateFailed: String(e) })),
      ])
      console.log(`  goTo("${label}") timed out waiting for card "${scene.card}": ${JSON.stringify(seen)}`)
      throw err
    })
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

/**
 * resetInstance puts the shared instance back to having seen nothing:
 * events, flags, matches, pushed router tables, definitions,
 * suggestions and every account's preferences record (#1283 -- the
 * fresh browser context used to give each scenario fresh preferences
 * for free) all go; accounts, sessions, devices, ingest tokens,
 * settings and the setup ledger stay (#1064, POST /api/test/reset).
 *
 * Scenarios in a shard share one instance and run in filename order, so
 * without this each one inherits whatever its siblings left -- and pays
 * for it in neutraliser rules, scenario-private interface names and
 * enough traffic to out-rank somebody else's leftovers. Pipelines 819 and
 * 820 were both that failure.
 *
 * The route exists only where the process was started with
 * MV_TEST_HOOKS=1 -- live-env.sh and live-container.sh both do. A 404 is
 * not a weaker guarantee to carry on under: it means the target is not a
 * test instance, and these scenarios sign in with fixed harness
 * credentials and some of them create accounts, mint tokens and push
 * data, so this refuses rather than risk doing that to a real mikroview.
 *
 * Returns whether a reset happened, so the caller knows to reload.
 */
async function resetInstance(page) {
  const res = await page.request.fetch(`${URL_BASE}/api/test/reset`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  })
  if (res.status() === 404) {
    throw new Error(
      'POST /api/test/reset answered 404 -- this is not a test instance (it was not started with MV_TEST_HOOKS=1). ' +
        'The live scenarios create accounts and tokens and push data; refusing to run them against it.',
    )
  }
  if (res.status() !== 200) {
    throw new Error(`POST /api/test/reset answered ${res.status()} -- the instance was not reset, so this run would be judging residue`)
  }
  return true
}

/**
 * A desktop window, wider than every width-dependent rule the app has:
 * the docket's 1300px narrow breakpoint (lib/viewport.svelte.ts) and the
 * stream table's starting-columns one (lib/columns.svelte.ts), which
 * #1117 moved from 1500 to 1600 -- and it is a max-width query, so 1600
 * itself is the narrow side now. 1920x1080 is the common desktop the
 * fifteen columns were re-measured against, and sits clear of both.
 *
 * Playwright's own default is 1280x720, which is below both, so a
 * scenario about a *desktop* surface has to say so rather than inherit a
 * width that now means "narrow". Pass it to session() as `viewport`; a
 * scenario testing the narrow side sets its own instead.
 */
export const DESKTOP_VIEWPORT = { width: 1920, height: 1080 }

/**
 * session launches a browser and signs in, returning a live page.
 *
 * Its own landing default is 'stream' (#616 retired #544's interim --
 * the fall is the real landing page now, not Stream) so that every
 * scenario written against the old landing keeps working unmodified:
 * session() signs in, then navigates to Stream itself before returning,
 * exactly where those scenarios already assume they start. Pass
 * `landing: 'fall'` (live-fall.mjs's own case) to stay on the fall
 * instead of being moved off it.
 */
// ---- The second step at sign-in (#1253) ----------------------------------
//
// Every local account must now hold a second factor, so the right
// password on its own no longer signs anybody in: the door asks for a
// code instead. scripts/live-env.sh enrols an authenticator-app factor
// for the admin when it stands the instance up, and exports the secret
// as MV_TOTP_SECRET precisely so this can finish the job.
//
// Kept here, in session()'s own sign-in, rather than pushed into every
// scenario: none of them are about the login door, and the two that are
// (live-passkeys.mjs and the authenticator scenarios) drive it
// themselves from a signed-out page.

const TOTP_SECRET = process.env.MV_TOTP_SECRET

// base32Decode is RFC 4648 without padding -- node has no built-in, and
// the alternative is a dependency for fifteen lines.
function base32Decode(s) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = 0
  let value = 0
  const out = []
  for (const ch of s.replace(/=+$/, '').toUpperCase()) {
    const idx = alphabet.indexOf(ch)
    if (idx === -1) throw new Error(`MV_TOTP_SECRET is not base32: unexpected ${JSON.stringify(ch)}`)
    value = (value << 5) | idx
    bits += 5
    if (bits >= 8) {
      bits -= 8
      out.push((value >> bits) & 0xff)
    }
  }
  return Buffer.from(out)
}

// The same RFC 6238 computation internal/auth/totp.go does: HMAC-SHA1
// of the big-endian 30-second counter, dynamic truncation, six digits.
function totpCode(secret, counter) {
  const buf = Buffer.alloc(8)
  buf.writeBigUInt64BE(BigInt(counter))
  const mac = createHmac('sha1', base32Decode(secret)).update(buf).digest()
  const offset = mac[mac.length - 1] & 0x0f
  const truncated = mac.readUInt32BE(offset) & 0x7fffffff
  return String(truncated % 1000000).padStart(6, '0')
}

// freshTotpCode hands back a code for a time step this instance has not
// already spent, waiting for the next one if it has to.
//
// Submitting a code that cannot work is not free, which is what makes
// this necessary rather than tidy. VerifyTOTP's replay guard refuses any
// counter already accepted, and a refused code is a *failed* login:
// handleAuthLoginFactor reserves the same LoginLimiter buckets
// handleAuthLogin does, five attempts per five minutes per account and
// per source address, released only on success. So retrying a doomed
// code three times, twice, locks the admin out of its own harness -- and
// every scenario after it fails on a 429 that has nothing to do with
// what it was testing. That is exactly how the first full-suite run
// went: 22 scenarios failed, cascading from the first few collisions.
//
// The spent counter lives in $MV_DIR, not in this process, because each
// scenario is its own node process and the guard is server-side and
// shared. scripts/live-env.sh seeds it with the counter it spent
// confirming enrolment.
const COUNTER_FILE = process.env.MV_DIR ? path.join(process.env.MV_DIR, 'totp-last-counter') : null

async function freshTotpCode(secret) {
  for (;;) {
    const counter = Math.floor(Date.now() / 1000 / 30)
    let spent = -1
    try {
      spent = Number.parseInt(fs.readFileSync(COUNTER_FILE, 'utf8').trim(), 10)
    } catch {
      spent = -1
    }
    if (!Number.isFinite(spent) || counter > spent) {
      if (COUNTER_FILE) fs.writeFileSync(COUNTER_FILE, String(counter))
      return totpCode(secret, counter)
    }
    // +1s so the server's clock has certainly crossed the boundary too.
    await new Promise((r) => setTimeout(r, 30000 - (Date.now() % 30000) + 1000))
  }
}

// completeFactorOverApi finishes a sign-in made with fetch rather than
// through the screen -- POST /api/auth/login answers 200 with a
// pending-factor body, not a session, for any account holding a factor,
// which since #1253 is every local account. Exported because one
// scenario (live-change-password.mjs) signs a second, genuinely
// separate client in to watch it be signed out again.
//
// Retries across time steps for the same reason completeSecondFactor
// does: one code, one sign-in, and a second attempt inside the same
// 30-second window is refused as a replay.
export async function completeFactorOverApi(request, urlBase = URL_BASE) {
  if (!TOTP_SECRET) {
    throw new Error('completeFactorOverApi needs MV_TOTP_SECRET -- run `eval "$(scripts/live-env.sh up)"`')
  }
  const res = await request.fetch(`${urlBase}/api/auth/login/factor`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: { code: await freshTotpCode(TOTP_SECRET) },
  })
  // Not retried, for the reason completeSecondFactor gives: the code was
  // already known-unspent, so a refusal is a fault worth surfacing rather
  // than an attempt worth spending.
  return res.status() === 200
}

// Waits for whichever of the two outcomes the password produced, so a
// server without the door costs nothing here rather than a fixed
// timeout: either the app is already up, or the code box is.
async function completeSecondFactor(page) {
  const codeBox = 'input[autocomplete="one-time-code"]'
  // Which of the two outcomes the password produced is decided by which
  // wait wins, not by asking afterwards. page.isVisible() answers from
  // the DOM as it stands at that instant, and the auth screen re-renders
  // as it swaps the password step for the code step -- so a check made
  // between the wait and the render said "no code box" for a code box
  // that was about to appear, and sign-in was skipped. Intermittent, and
  // the same trap live-passkeys.mjs hit.
  const outcome = await Promise.race([
    page.waitForSelector(codeBox, { timeout: 20000 }).then(
      () => 'factor',
      () => 'gave-up',
    ),
    page.waitForSelector('#main-content', { timeout: 20000 }).then(
      () => 'signed-in',
      () => 'gave-up',
    ),
  ])
  if (outcome === 'signed-in') return
  if (outcome === 'gave-up') {
    // No code box, but the step may still be here leading with a
    // passkey: an account holding both factors offers whichever suits
    // the origin first, and passkeys win wherever they are usable (a
    // scenario driving MV_PUBLIC_URL, say). The switch back to the
    // authenticator app is a plain button on that screen, and this
    // harness always has the secret, never a virtual authenticator.
    const toApp = page.locator('button:has-text("Use your authenticator app instead")')
    if (!(await toApp.count())) return
    await toApp.first().click()
    if (
      !(await page
        .waitForSelector(codeBox, { timeout: 10000 })
        .then(() => true)
        .catch(() => false))
    ) {
      return
    }
  }
  if (!TOTP_SECRET) {
    throw new Error(
      'sign-in stopped at the second-factor step but MV_TOTP_SECRET is unset -- ' +
        're-run `eval "$(scripts/live-env.sh up)"`, which enrols the factor and exports it',
    )
  }

  // Retried across time steps, because one authenticator code is good
  // for exactly one sign-in. VerifyTOTP's replay guard
  // (TOTPLastCounter, internal/auth/store.go) refuses any counter it
  // has already accepted, and it advances on every success -- so a
  // second sign-in inside the same 30-second window is refused however
  // correct the code is. Two scenarios in a row do exactly that, and so
  // does the very first one after live-env.sh enrols the factor, since
  // confirming enrolment burns that window's code too.
  //
  // Nothing here can shorten the wait: the server accepts a code only
  // within one step of its own clock, so there is no "next" code to
  // reach for -- the only thing that helps is the window turning over.
  // Costs nothing when the window has already moved on, which is the
  // common case for anything but a fast scenario following another.
  await page.fill(codeBox, await freshTotpCode(TOTP_SECRET))
  await page.click('button[type="submit"]')
  const signedIn = await page
    .waitForSelector('#main-content', { timeout: 15000 })
    .then(() => true)
    .catch(() => false)
  // Deliberately not retried. freshTotpCode already guarantees an unspent
  // counter, so a refusal here is a real fault -- a drifted secret, a
  // cleared factor -- and trying again would only spend the account's
  // five-per-five-minutes allowance on it and turn one broken scenario
  // into every later one failing on a 429.
  if (!signedIn) {
    throw new Error(
      'the second-factor step refused a fresh code from MV_TOTP_SECRET -- the exported secret and ' +
        'the enrolled factor have drifted apart; re-run `eval "$(scripts/live-env.sh up)"`',
    )
  }
}

export async function session({
  dismissSetup = true,
  landing = 'stream',
  unfoldFilter = true,
  keep = false,
  viewport = undefined,
  mocksApi = false,
} = {}) {
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
  // The viewport is set on the page rather than after it, because two of
  // the app's width rules are read once at module load (the stream's
  // starting column set is the worked example) -- a resize afterwards
  // would arrive too late to decide them.
  // mocksApi, for a scenario that answers an /api route itself with
  // page.route: once the app's service worker controls the page, an
  // /api request is fetched by the worker, and only Chromium lets
  // Playwright's routes see a worker's fetches -- under WebKit the mock
  // never fires and the real server answers (live-setup-wizard-source-
  // split saw the real router where it had mocked a split one). Keeping
  // the worker out of that scenario's context is what makes the mock
  // hold on every engine; the app runs the same without one.
  const page = await browser.newPage({
    ignoreHTTPSErrors: true,
    ...(viewport ? { viewport } : {}),
    ...(mocksApi ? { serviceWorkers: 'block' } : {}),
  })
  const consoleErrors = []
  // Set only while completeSecondFactor is retrying a refused code (see
  // its own comment): a refused authenticator code is a 401 the browser
  // logs as a console error, and it is this harness's own sign-in
  // making it, not the app misbehaving. Without this, every scenario
  // that follows another inside one 30-second window fails "no console
  // errors" for a login that then succeeded.
  let signingIn = false
  const record = (text) => {
    if (signingIn) return
    if (isUntrustedCertServiceWorkerError(text)) return
    if (isNavigationCancelledFetch(text)) return
    if (isScreenshotStyleRefusal(text)) return
    if (isResizeObserverLoopNotice(text)) return
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
  signingIn = true
  try {
    await completeSecondFactor(page)
  } finally {
    signingIn = false
  }
  // #main-content is the one marker present on every signed-in view
  // (App.svelte wraps all of them in it) -- unlike the old `input.rule`
  // wait, it does not assume which view is the landing page.
  await page.waitForSelector('#main-content', { timeout: 15000 })

  // Signed in: the first moment an admin request can reset. Once per
  // process -- a scenario that opens a second session is opening a
  // second tab on its own instance, not starting again. keep: true opts
  // out for a scenario that deliberately wants what a sibling left
  // behind.
  //
  // The shell has already fetched the stream by now, so the page is
  // reloaded after a reset or it goes on showing what was cleared.
  if (!keep && !sessionOpened && (await resetInstance(page))) {
    await page.reload({ waitUntil: 'networkidle' })
    await page.waitForSelector('#main-content', { timeout: 15000 })
  }
  sessionOpened = true

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

  return { page, consoleErrors }
}

/**
 * waitForStreamRows waits for the Stream card to have rendered at least
 * `n` event rows. Feed first, then call this: session() used to do it
 * from a `waitForEvents` option, back when scenarios fed before signing
 * in (#1065).
 *
 * Scoped to the Stream card: the deck (#616) keeps neighbouring cards
 * mounted, and their scenes render .row elements of their own, so a bare
 * .row count can be satisfied before any event has rendered.
 */
export async function waitForStreamRows(page, n, { timeoutMs = 20000 } = {}) {
  await page.waitForFunction(
    (want) => document.querySelectorAll('.card[data-card="live"] .row').length >= want,
    n,
    { timeout: timeoutMs },
  )
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

/**
 * enrolDevice declares a router by name, mints its one-time token and
 * redeems it from `addr` over syslog -- the same sequence the ledger
 * walks an operator through (#1281), driven directly because most
 * scenarios need a router in place rather than a wizard to drive.
 *
 * Shared because since #1281 every scenario that pushes needs it: a
 * push is refused unless it arrives from the device's own declared or
 * enrolled address (internal/api/ingest.go, IsEnrolledAt), and only the
 * harness's own `live-router` is declared at 127.0.0.1.
 *
 * `request` is a Playwright APIRequestContext (page.request) or anything
 * with the same fetch(url, {method, headers, data}) shape: the caller
 * already has one carrying the admin session.
 */
export async function enrolDevice(request, base, id, addr, { timeoutMs = 15000 } = {}) {
  const call = async (method, path, data) => {
    const res = await request.fetch(`${base}${path}`, {
      method,
      headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
      data,
    })
    return { status: res.status(), body: res.status() < 400 ? await res.json().catch(() => null) : null }
  }

  const created = await call('POST', '/api/devices', { name: id })
  if (created.status !== 201) {
    check(false, `${id} is declared by name (got ${created.status})`)
    return false
  }
  // #1291: minting re-proves the admin's identity at that moment, and
  // binds the enrolment window to the one address the token may be
  // redeemed from -- which here is the address this helper is about to
  // feed the enrol line from.
  const mint = await call('POST', `/api/devices/${encodeURIComponent(id)}/enrolment`, {
    password: PASS,
    expectedAddress: addr,
  })
  if (!mint.body?.token) {
    check(false, `an enrolment token is minted for ${id} (got ${mint.status})`)
    return false
  }

  feedRawFrom(addr, `<14>Jan  1 00:00:00 ${id} mikroview-enrol ${mint.body.token}`)

  const deadline = Date.now() + timeoutMs
  let enrolled = false
  while (Date.now() < deadline && !enrolled) {
    const { body } = await call('GET', '/api/devices')
    enrolled = (body?.devices ?? []).some((d) => d.id === id && d.acceptedIp === addr)
    if (!enrolled) await new Promise((r) => setTimeout(r, 500))
  }
  check(enrolled, `${id} enrols at ${addr} over syslog before anything is pushed to it`)
  return enrolled
}

/**
 * pushFrom sends an ingest push with the request's own local address
 * bound to `localAddress`. Since #1281 the ingest handler answers 403
 * for a push from anywhere but the device's enrolled address, and
 * fetch() cannot choose one -- it leaves the address to the kernel's
 * routing. Node's own http/https client is what exposes localAddress.
 *
 * Resolves to the status code.
 */
export function pushFrom(base, localAddress, token, payload) {
  const url = new URL(`${base}/api/ingest/routeros`)
  const mod = url.protocol === 'https:' ? https : http
  const body = JSON.stringify(payload)
  return new Promise((resolve, reject) => {
    const req = mod.request(
      {
        hostname: url.hostname,
        port: url.port,
        path: url.pathname,
        method: 'POST',
        localAddress,
        rejectUnauthorized: false,
        headers: {
          Authorization: `Bearer ${token}`,
          'Content-Type': 'application/json',
          'Content-Length': Buffer.byteLength(body),
        },
      },
      (res) => {
        res.resume()
        resolve(res.statusCode)
      },
    )
    req.on('error', reject)
    req.end(body)
  })
}

/**
 * grantClipboard makes `navigator.clipboard.readText()` answer in a
 * scenario, so a copy control is checked by what it put on the clipboard
 * and not only by its toast.
 *
 * Chromium grants the permission for real, so the read is the browser's
 * own clipboard. Firefox refuses `clipboard-read` as a permission name
 * outright, and WebKit gates readText on a user gesture the harness
 * cannot supply, so on those two the page's writeText is wrapped to keep
 * the last text handed to it and readText answers from that. That still
 * proves what the app handed to the clipboard API -- the thing every
 * caller is checking. The real write still runs, and a rejection still
 * reaches the app, so an engine refusing the write is not hidden.
 */
export async function grantClipboard(page) {
  if (BROWSER_NAME === 'chromium') {
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: URL_BASE })
    return
  }
  await page.evaluate(() => {
    const real = navigator.clipboard.writeText.bind(navigator.clipboard)
    let last = ''
    Object.defineProperty(navigator.clipboard, 'writeText', {
      configurable: true,
      value: (text) => {
        last = String(text)
        return real(text)
      },
    })
    Object.defineProperty(navigator.clipboard, 'readText', {
      configurable: true,
      value: async () => last,
    })
  })
}

/**
 * clickSvgText clicks a control drawn as SVG text -- the fall's quieter
 * link, the topography's `trace ▸` token and `Run setup ▸`. Playwright's
 * own click cannot land on one under WebKit: the box it computes for a
 * <text> is not where the glyphs are, so every retry ends "outside of
 * the viewport" and the scenario dies at the click. The page's own
 * geometry is right, so the element is scrolled into view and hit at
 * the centre of its getBoundingClientRect with a real mouse click.
 * Still a hit-tested click: a control the engine itself cannot reach
 * is still reported as one.
 *
 * The same click serves a host dot. Its throbbing ring (`.h-halo`,
 * `.h-nb`) animates stroke-width inside the clickable <g>, and Firefox
 * alone counts stroke in an SVG element's box, so while the ring throbs
 * Playwright never sees the dot hold still and its own click times out
 * ("element is not stable", 30 s). Whether the ring is drawn by click
 * time depends on the feed, which is why it failed one run in three.
 */
export async function clickSvgText(page, locator) {
  await locator.waitFor({ state: 'visible' })
  const [x, y] = await locator.evaluate((el) => {
    el.scrollIntoView({ block: 'center', inline: 'center' })
    const r = el.getBoundingClientRect()
    return [r.x + r.width / 2, r.y + r.height / 2]
  })
  await page.mouse.click(x, y)
}

export function done() {
  browser?.close()
  console.log(failed ? 'RESULT: FAIL' : 'RESULT: PASS')
  process.exit(failed ? 1 : 0)
}
