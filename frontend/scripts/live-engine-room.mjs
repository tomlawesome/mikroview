// SPDX-License-Identifier: AGPL-3.0-only
//
// Settings as the shelf (#633, rounds 23-25), driven in a real browser.
// The five-station signal path (#490) is replaced wholesale: one page,
// groups reporting live truth. Round 32 (#767) mounted keys (under
// ingest) and people (under account) directly in the card, in the
// card's own row grammar, retiring the two side doors that used to
// carry them below the shelf.
//
// The room's claims survive the restyle, and none of them is visible
// from the code or from a unit test with a mocked store:
//
//  1. "Every number on the page is arrived traffic" -- a component test
//     renders whatever number it was handed, so it cannot tell a live
//     figure from a placeholder. Feeding real syslog and watching the
//     memory group's buffer count climb can.
//  2. "Tuning unfolds, it does not navigate" -- the detector bench opens
//     from detection's tune row and the page must still be Settings
//     afterwards, the bench folded in place rather than routed to.
//  3. The viewer grammar: affordances absent rather than disabled, the
//     people group absent entirely -- and, the part no DOM assertion
//     covers, a viewer's session never even *asks* for the account
//     list. GET /api/auth/users is admin-only (#657), so a viewer
//     issuing it would be a page that loads and immediately 403s.

import { session, feedSyslog, check, done, goTo } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session({ waitForEvents: 40 })

const PEOPLE = '#people'
const MACHINES = '#keys'

// goTo's own wait (SCENES in live-browser.mjs, waiting for the engineroom card to centre) is what proves arrival --
// this used to also wait for `.page-header h2`, but #700 unmounted PageHeader from EngineRoom.svelte entirely, so
// that selector no longer exists anywhere on the page (#667 group E).
await goTo(page, 'Settings')

// --- The page is the groups, with keys and people mounted in place ------

const groupNames = await page.$$eval('.stsection h3', (els) => els.map((e) => e.textContent.trim()))
check(
  JSON.stringify(groupNames) ===
    JSON.stringify(['ingest', 'keys', 'detection', 'memory', 'disk', 'account', 'people']),
  `the groups render in order -- ingest, keys, detection, memory, disk, account, people -- got ${JSON.stringify(groupNames)}`,
)
check(
  await page.locator(`${MACHINES} h3`).isVisible(),
  'the keys group renders for an admin',
)
check(
  await page.locator(`${PEOPLE} h3`).isVisible(),
  'the people group renders for an admin',
)

// The shelf holds the whole deck, whatever order an earlier scenario
// left it in, and exactly one card wears the sign-in mark.
const shelfNames = await page.$$eval('.stshelf .stcard .nm', (els) => els.map((e) => e.textContent.trim()))
// Seven, not five: #647 (#634 round 23) put Entities and Settings on the
// deck as its last two cards, and #653 widened both to the user tier
// (deckCards.ts:34-52 -- `canEdit` carries them, so an admin sees seven).
// This scenario drives the shelf as an admin, so seven is the whole deck.
const DECK_CARDS = ['The fall', 'Topography', 'Metrics', 'Stream', 'The docket', 'Entities', 'Settings']
check(
  shelfNames.length === DECK_CARDS.length && DECK_CARDS.every((n) => shelfNames.includes(n)),
  `the shelf holds all ${DECK_CARDS.length} deck cards -- got ${JSON.stringify(shelfNames)}`,
)
check(
  (await page.$$('.stshelf .stcard.first .lands')).length === 1,
  'exactly one shelf card says sign-in lands on it, and it is the first',
)

// --- Claim 1: every number is arrived traffic ---------------------------
// The memory group's buffer count is the honest one to pin: it is a
// whole number the server publishes, so a placeholder or a stale render
// is visible as a number that does not move when more events land.
//
// The row's first figure is the MiB ceiling (#796, since #823) -- a
// configured budget, not traffic, so it never moves on its own. The held
// count (#842) is the live one, and it sits right before " of " ("8 412
// of ~201 000 events"), so that is what this scenario reads and waits on.

const BUFFER_ROW = '.stsection:has(h3:text-is("memory")) .orow:has-text("event buffer") .ov'

const bufferCount = () =>
  page.$eval(BUFFER_ROW, (el) => {
    const m = el.textContent.match(/([\d,\s ]+)\s+of\s/)
    return m ? Number(m[1].replace(/\D/g, '')) : null
  })

const before = await bufferCount()
check(before !== null && before > 0, `memory says how many events the buffer holds (got ${before})`)

feedSyslog(60, 'live-engine-room')
const climbed = await page
  .waitForFunction(
    (was) => {
      // Plain DOM traversal, not BUFFER_ROW: this runs inside the page,
      // where Playwright's :has-text/:text-is pseudo-selectors do not
      // exist. And deliberately no fallback to "the first number on the
      // page": the ingest group's events/s moves on its own, so a
      // fallback would let this check pass without ever reading the
      // buffer. Reads the held count before " of ", not the row's first
      // figure, which has been the MiB ceiling -- a static budget -- since
      // #823 (#842).
      const og = [...document.querySelectorAll('.stsection')].find(
        (g) => g.querySelector('h3')?.textContent.trim() === 'memory',
      )
      const row = og && [...og.querySelectorAll('.orow')].find((r) => r.textContent.includes('event buffer'))
      const el = row?.querySelector('.ov')
      if (!el) return false
      const m = el.textContent.match(/([\d,\s ]+)\s+of\s/)
      return m ? Number(m[1].replace(/\D/g, '')) > was : false
    },
    before,
    { timeout: 20000 },
  )
  .then(() => true, () => false)
check(climbed, `the buffer count rises as events arrive -- it is live traffic, not a placeholder (was ${before})`)

const ingestText = (await page.textContent('.stsection:has(h3:text-is("ingest"))')) ?? ''
check(
  /listening/.test(ingestText),
  'ingest names the listening port -- the pathway in is a stated fact',
)
check(
  /[\d.]+\s*events\/s arriving now/.test(ingestText),
  `ingest states a real events/s rate rather than a placeholder`,
)

// The detection group's "N of M on" has to agree with the server's own
// definitions list, whatever an earlier scenario left toggled.
const detectorsRow = (await page.textContent('.stsection:has(h3:text-is("detection")) .orow:has-text("detectors")'))?.trim() ?? ''
const defs = await page.request
  .get(`${URL_BASE}/api/definitions`)
  .then(async (r) => (await r.json()).definitions ?? [])
const running = defs.filter((d) => d.enabled).length
check(
  detectorsRow.includes(`${running} of ${defs.length} on`),
  `detection counts what the server actually runs (ui "${detectorsRow}", api ${running} of ${defs.length})`,
)

// --- Claim 2: tuning unfolds in place, it does not navigate -------------

await page.click('.olink:has-text("tune")')
await page.waitForSelector('.bench .row')

// .page-header h2 is gone with #700 (see above); the roll rail's own current-scene marker is what proves the page
// underneath the unfolded bench is still Settings.
check(
  (await page
    .$eval('.roll-rail button.rail-name[aria-current="page"]', (e) => e.textContent.trim())
    .catch(() => null)) === 'Settings',
  'the page is still Settings -- the bench unfolded in place, it did not navigate away',
)
check(
  (await page.$$('.bench .row')).length > 0,
  'the open bench shows the detectors',
)

await page.click('.olink:has-text("close the bench")')
await page.waitForSelector('.bench', { state: 'detached' })
check(true, 'closing the bench folds it away and the page is whole again')

// --- Claim 3: the viewer grammar ----------------------------------------
// #657 (predating round 32) narrowed GET /api/tokens to admin-only, the
// same footing GET /api/auth/users was already on -- issuing keys is a
// setup task, not using the product. So keys and people are both absent
// for a viewer, not a read-only rendering of either: this used to assert
// the machines door stayed viewer-readable with its verbs gone, which
// stopped being true the moment #657 landed. Round 32/#767 keeps both
// groups on that same admin-only footing (see EngineRoom.svelte's own
// doc comment on the point).

const VIEWER_USER = 'live-viewer-490'
const VIEWER_PASS = 'live-viewer-490-password'

await page.click(`${PEOPLE} .ogfoot .olink`)
await page.waitForSelector(`${PEOPLE} .pform`)
await page.fill(`${PEOPLE} .pform input[aria-label="username"]`, VIEWER_USER)
await page.fill(`${PEOPLE} .pform input[aria-label="password"]`, VIEWER_PASS)
// #653: the form defaults to a "can change things" account, so the
// read-only tier this claim is about has to be chosen explicitly --
// which also drives the selector itself, since without it the viewer
// tier has no route in from the UI at all.
await page.click(`${PEOPLE} .pform button:has-text("can only look")`)
await page.click(`${PEOPLE} .pform button:has-text("let them in")`)
await page.waitForSelector(`${PEOPLE} .prow:has-text("${VIEWER_USER}")`)
check(
  await page.isVisible(`${PEOPLE} .prow:has-text("${VIEWER_USER}") .pr:has-text("can only look")`),
  'the people group marks the new account as read-only',
)

// --- The viewer half of this scenario moved to live-viewer-surfaces.mjs --
//
// It used to sign a viewer in here and walk Settings, proving #490's
// grammar -- rows absent for a tier that cannot use them, never disabled.
// #657 then ruled Settings out of a viewer's navigation entirely, and a
// viewer has no other route in: navigation is `appState.view` mutation
// from the UI only (BottomBar.svelte:174, the roll rail), there are no URL
// routes, and a viewer's deck carries no `engineroom` card
// (deckCards.ts:45-50). Asking for it leaves Deck.svelte:48's activeIndex
// at -1 and nothing mounts, so `goTo(viewerPage, 'Settings')` waits 30s for
// a rail button that cannot exist and throws, taking the rest of this
// scenario with it.
//
// The coverage was not lost: live-viewer-surfaces.mjs already carries it,
// against the *user* tier -- who can still open the page, so who the
// grammar now has to hold for. Its own comment records the move
// ("live-engine-room.mjs used to assert this against a viewer, whose route
// into the room #657 removed"), claims 1-3 there. This block was the
// leftover, live-checking an unreachable state; #706 migrated the
// assertions and left it standing.
//
// What is deliberately *not* migrated: "a viewer never even asks for
// /api/auth/users or /api/tokens". On a page a viewer cannot open, that is
// true for free and proves nothing. The server-side gate it was standing in
// for is pinned directly -- live-viewer-surfaces.mjs claim 4 and
// internal/api/tokens_test.go.

// --- Clean up: this account should not outlive the scenario -------------
// Arm-then-confirm (round 28's gesture, retained rather than a confirm()
// dialog): a click arms remove, a second click on the same button
// confirms it.

const remove = page.locator(`${PEOPLE} .prow:has-text("${VIEWER_USER}") .remove`)
await remove.click()
await remove.click()
await page.waitForSelector(`${PEOPLE} .prow:has-text("${VIEWER_USER}")`, { state: 'detached' })
check(true, `the viewer account "${VIEWER_USER}" is removed again`)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
