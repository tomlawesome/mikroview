// SPDX-License-Identifier: AGPL-3.0-only
//
// #1363: the case #1362 was actually about -- an already-open tab whose
// server was upgraded out from under it. Nothing else in the gate proves
// this: the vitest suite (lib/freshness.svelte.test.ts) pins the
// reload/banner decision against a fake registration and a stubbed
// fetch, which is right for that file's own logic but cannot show that
// a *real* service worker actually hands over control before the page
// reloads, or that a real browser reload really lands on the new
// bundle. live-sw-navigation.mjs is the sibling this was modelled on --
// same "needs a real, non-self-destroying worker" reasoning.
//
// live-env.sh's `upgrade` rebuilds and restarts the instance in place
// (same data, same config) with a new VERSION baked into both the
// binary and the frontend bundle it stamps -- the one thing `up` cannot
// give a scenario, since nothing has a tab open on a fresh instance yet.
//
// Run by scripts/live-freshness-reload.sh, standalone, on its own
// instance -- not by scripts/run-scenarios.sh's shared-instance phase.
// Restarting the server mid-run would silently wipe whatever earlier
// scenarios fed into that shared instance's in-memory state for every
// scenario that runs after this one in filename order (see that .sh
// file's own comment). run-scenarios.sh excludes this file by name for
// the same reason live-routeros-real.mjs is excluded.
//
// A restart -- this scenario's `upgrade`, and a real deploy alike --
// does not survive with the session: internal/api/auth.go's own comment
// on SessionStore is explicit that it is "generated once, held only in
// memory, lost on restart". So the reload this scenario proves is about
// the *tab*, not the *session* -- each pass below signs back in after
// its reload lands, the same way a real operator would after any
// restart, upgrade or not. Console noise from the moment the instance
// is actually down (a WebSocket refused, a fetch racing the restart) is
// expected here, since causing exactly that is what `upgradeServer()`
// does -- unlike live-sw-navigation.mjs, this does not assert a clean
// console.
//
// Two passes, quiet then busy, on the same tab and the same upgrade
// mechanics -- 7a's split is the point of this scenario, not just the
// reload path on its own:
//   - quiet: nothing open, nothing typed -- the page must reload itself,
//     landing on the new version with the new worker in control.
//   - busy: a focused field holds text -- the page must show the
//     banner instead and wait for a click, never reloading on its own
//     (7a), and the click must be the thing that finally moves it on.

import { session, check, done, upgradeServer, completeSecondFactor, goTo } from './live-browser.mjs'

const { page } = await session()

const USER = process.env.MV_USER
const PASS = process.env.MV_PASS

// A plain Node-side fetch, deliberately never page.evaluate: the page
// can reload itself at any moment once an upgrade lands -- that is the
// entire point of this scenario -- and a version check run *in* the
// page races its own execution context being torn down under it. This
// asks the server directly, off to the side of whatever the tab is
// doing.
async function currentVersion() {
  const res = await fetch(`${process.env.MV_URL}/api/healthz`, { cache: 'no-store' })
  return (await res.json()).version
}

/** Waits for the freshness reload -- automatic, or the banner's own click --
 * to have actually happened: the marker set before it is gone once a real
 * navigation has taken place, unlike a Svelte re-render. */
async function waitForReload() {
  return page
    .waitForFunction(() => window.__mvBeforeUpgrade === undefined, { timeout: 20000 })
    .then(() => true)
    .catch(() => false)
}

function markBeforeReload() {
  return page.evaluate(() => {
    window.__mvBeforeUpgrade = true
  })
}

/** Signs back in after a reload landed on the login screen -- the
 * SessionStore did not survive the restart underneath it (see the file
 * header) -- and returns to the Stream, unfolded, the same shape
 * session() leaves a fresh sign-in in. */
async function signInAgain() {
  await page.waitForSelector('input[autocomplete="username"]', { timeout: 15000 })

  const codeBox = 'input[autocomplete="one-time-code"]'
  // Retried whole, not just the fill: a login form freshly re-mounted
  // after a real navigation (unlike session()'s very first, untouched
  // paint) has been seen accepting the fill -- page.inputValue agrees
  // right afterwards -- and still reaching handleSubmit with an empty
  // username, on a host busy enough to be slow settling the reactive
  // effects a fresh mount installs (three full builds run back to back
  // for this scenario alone). Never touches completeSecondFactor until
  // the code box actually shows, so a slow-settling password step never
  // burns one of the account's five-per-five-minutes login attempts nor
  // a TOTP window for nothing.
  let ready = false
  for (let attempt = 0; attempt < 5 && !ready; attempt++) {
    await page.fill('input[autocomplete="username"]', USER)
    await page.fill('input[autocomplete="current-password"]', PASS)
    await page.click('button[type="submit"]')
    ready = await Promise.race([
      page.waitForSelector(codeBox, { timeout: 4000 }).then(
        () => true,
        () => false,
      ),
      page.waitForSelector('#main-content', { timeout: 4000 }).then(
        () => true,
        () => false,
      ),
    ])
  }
  if (!ready) {
    console.error('signInAgain: stuck at', page.url())
    console.error('signInAgain: page text:', (await page.evaluate(() => document.body.innerText)).slice(0, 800))
    throw new Error('signInAgain: the password step never produced a code box or a signed-in page')
  }
  await completeSecondFactor(page)
  await page
    .waitForSelector('#main-content', { timeout: 15000 })
    .catch(async (e) => {
      console.error('signInAgain: stuck at', page.url())
      console.error('signInAgain: page text:', (await page.evaluate(() => document.body.innerText)).slice(0, 800))
      throw e
    })
  await goTo(page, 'Stream', { unfold: true })
  await page.waitForSelector('input.rule', { timeout: 15000 })
}

/** Waits for `upgradeServer()` to actually be answering as a new
 * version before asking the page to notice -- otherwise the freshness
 * check could race the restart, see the old version, and only catch the
 * real mismatch on its own 60s poll, past this scenario's patience. */
async function waitForVersionChange(from) {
  let seen = from
  for (let i = 0; i < 40 && seen === from; i++) {
    await new Promise((r) => setTimeout(r, 250))
    seen = await currentVersion()
  }
  return seen
}

const startVersion = await currentVersion()

// --- quiet: the page has nothing open and reloads itself -----------------

await markBeforeReload()
upgradeServer()

const upgradedVersion = await waitForVersionChange(startVersion)
check(upgradedVersion !== startVersion, `the instance answers a new version after upgrade (was ${startVersion}, now ${upgradedVersion})`)

// No manual nudge: the live socket reconnecting to the just-restarted
// server is by itself one of the ratified triggers (ws.ts), and does
// this for real, the same way a genuine upgrade would. Dispatching a
// synthetic visibilitychange here instead -- an earlier draft of this
// scenario did -- fires App.svelte's *other* visibility subscriber too
// (the #1088 stats-refresh one), repeatedly, on a tab that was never
// actually hidden, which was observed knocking focus off the rule input
// as a side effect and made the busy pass below flicker between passing
// and failing on nothing this feature touches.
check(await waitForReload(), 'a quiet tab reloads itself once it notices the new version')

// The real assertion this scenario exists for: the reloaded document's
// *own* script tag matches what the server serves fresh right now --
// not just that /api/healthz agrees, which says nothing about whether
// the static shell served with it is the old, precached one or the new
// one. And the worker actually controlling the page is the one that
// took over, not still installing.
const shellCheck = await page.evaluate(async () => {
  const scriptSrc = document.querySelector('script[src*="assets/index-"]')?.getAttribute('src')
  const fresh = await fetch('/', { cache: 'no-store' }).then((r) => r.text())
  const freshMatch = fresh.match(/assets\/index-[^."]+\.js/)
  const controller = navigator.serviceWorker.controller
  return { scriptSrc, freshIndexScript: freshMatch?.[0], controllerState: controller?.state ?? null }
})
check(
  !!shellCheck.scriptSrc && shellCheck.scriptSrc.includes(shellCheck.freshIndexScript ?? '\0'),
  `the reloaded shell is the current build, not a precached earlier one (${JSON.stringify(shellCheck)})`,
)
check(shellCheck.controllerState === 'activated', `the new worker is the one in control (${shellCheck.controllerState})`)

await signInAgain()

const bannerAfterQuietReload = await page.getByText('mikroview has been upgraded to a newer version').count()
check(bannerAfterQuietReload === 0, 'no freshness banner once the automatic reload has landed on the new version')

const versionAfterQuietReload = await currentVersion()
check(
  versionAfterQuietReload === upgradedVersion,
  `the reloaded tab is talking to the upgraded instance (${versionAfterQuietReload})`,
)

// --- busy: a focused field holds text, so the tab waits for a click ------

await page.click('input.rule')
await page.type('input.rule', 'freshness-busy')

await markBeforeReload()
upgradeServer()

const secondVersion = await waitForVersionChange(versionAfterQuietReload)
check(
  secondVersion !== versionAfterQuietReload,
  `the instance answers a second new version (was ${versionAfterQuietReload}, now ${secondVersion})`,
)

// Same reasoning as the quiet pass: no synthetic visibilitychange here
// either. The WS reconnect trigger is what notices this mismatch for
// real, on its own timeline, without disturbing the busy field.
const banner = page.getByText('mikroview has been upgraded to a newer version')
await banner.waitFor({ state: 'visible', timeout: 20000 }).catch(() => {})
check(await banner.count(), 'the banner shows once a mismatch is found while the field is busy')

check(!(await page.evaluate(() => window.__mvBeforeUpgrade === undefined)), '7a: a busy tab never reloads on its own')

// Still typed, unsaved, and still there -- the whole reason 7a exists.
check((await page.inputValue('input.rule')) === 'freshness-busy', 'the busy field it would have lost still holds its text')

await page.getByRole('button', { name: 'reload' }).click()

check(await waitForReload(), "the banner's own reload button is what finally moves the tab on")

await signInAgain()

const versionAfterClick = await currentVersion()
check(versionAfterClick === secondVersion, `the tab is now talking to the second upgraded instance (${versionAfterClick})`)

done()
