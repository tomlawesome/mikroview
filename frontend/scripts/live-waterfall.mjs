// SPDX-License-Identifier: AGPL-3.0-only
//
// #616: the fall as the landing page. live-nav-rail.mjs already proves
// "The fall" is what session() lands on; this scenario is the fall's own
// behaviour, and needs a real pushed filter-rule table (not just synthetic
// events) to exercise the one thing worth a browser for: telling a dark
// (unlogged) boundary from an observed one with real cadence, and that
// clicking either hands off to Stream with its filters filled.
//
// Named live-waterfall.mjs rather than #616's own literal live-fall.mjs
// (a deliberate deviation from the issue text, recorded on the PR) --
// live-router-lookup.mjs asserts "no filter-rule table has been pushed
// yet" against the one shared device every scenario here pushes through,
// and live-watchlist-coverage.mjs starts by assuming the table
// live-watchlist-broken-ring.mjs leaves behind (non-logging). "live-fall"
// sorts before both and would break the first outright and race the
// second; "live-waterfall" sorts after all three watchlist-*.mjs
// scenarios and before live-ws-revocation.mjs (which touches neither),
// so this scenario's own push -- covering both spots since #445 -- lands
// clear of every existing ordering assumption instead of adding a new one
// nothing else knows to avoid.

import { session, feedSyslog, check, responsive, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

feedSyslog(3, 'fall-probe')
const { page, consoleErrors } = await session({ landing: 'fall' })

// The ingest token must be scoped to exactly the device the probe events
// carry (see live-router-lookup.mjs's own comment on this same lookup),
// or the pushed table attaches to a different device and every boundary
// below reports 'unknown' instead of 'dark'/'observed'.
let DEVICE
for (let i = 0; i < 40 && !DEVICE; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const res = await page.request.get(`${URL_BASE}/api/devices`)
  if (res.ok()) DEVICE = (await res.json()).devices?.[0]?.id
}
check(!!DEVICE, `the instance reports the device events arrive from (${DEVICE})`)

const tokenRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-fall', kind: 'ingest', device: DEVICE },
})
check(tokenRes.status() === 201, `an ingest token is issued (${tokenRes.status()})`)
const token = (await tokenRes.json()).value

async function push(payload) {
  const res = await fetch(`${URL_BASE}/api/ingest/routeros`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  return res.status
}

// Two boundaries: one that logs (matches feedSyslog's own fixed
// in:ether1/out:bridge1, forward shape, so real cadence lands on it),
// and one that never logs -- the honesty distinction #616 asks the fall
// to draw, not the taxonomy of what those interfaces "are".
check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      {
        ordinal: 0,
        comment: 'log the household',
        chain: 'forward',
        action: 'drop',
        srcAddressList: '',
        logPrefix: 'D|fall-probe|',
        log: true,
        inInterface: 'ether1',
        outInterface: 'bridge1',
      },
      {
        ordinal: 1,
        comment: 'silent guest egress',
        chain: 'forward',
        action: 'drop',
        srcAddressList: '',
        logPrefix: '',
        inInterface: 'ether9',
        outInterface: 'bridge9',
      },
    ],
  })) === 200,
  'the filter-rule table is accepted through the real ingest endpoint',
)

// Real, visible cadence on the observed boundary.
feedSyslog(30, 'fall-cadence')

// fallState polls (Fall.svelte), so the freshly-pushed table takes up to
// one poll interval to reach the page -- the generous timeout here is
// that poll, not a flaky wait. Waiting on the *specific* label rather
// than a band count: this shared instance can already carry bands from
// earlier scenarios' own pushes (a stale "forward" band plus the fall's
// own unmatched-events bucket already satisfy a bare count of 2), which
// raced this exact check to a false-positive pass before the real push
// had landed.
await page.waitForFunction(
  () => [...document.querySelectorAll('.fall .band .band-label')].some((e) => e.textContent.includes('ether1')),
  null,
  { timeout: 25000 },
)

const bandLabels = await page.$$eval('.fall .band .band-label', (els) => els.map((e) => e.textContent.trim()))
check(
  bandLabels.some((l) => l.includes('ether1') && l.includes('bridge1')),
  `the observed boundary renders as its own band -- got ${JSON.stringify(bandLabels)}`,
)
check(
  bandLabels.some((l) => l.includes('ether9') && l.includes('bridge9')),
  `the dark boundary renders as its own band -- got ${JSON.stringify(bandLabels)}`,
)

// --- Honesty: dark != quiet, and it is never colour alone -----------------
check((await page.$('.fall .band.dark')) !== null, 'the unlogged boundary is marked dark')
check(
  (await page.$$eval('.fall .band.dark .band-caption.bad', (els) => els.length)) > 0,
  'a dark band carries its own text caption, not colour alone',
)
check(
  (await page.textContent('.fall .band.dark')).includes('blank because nothing is logged'),
  'the dark band states the honesty distinction in words',
)

// --- Visible cadence on the observed boundary ------------------------------
const observedCaptions = await page.$$eval('.fall .band:not(.dark) .band-caption', (els) =>
  els.map((e) => e.textContent.trim()),
)
check(
  observedCaptions.some((t) => t.includes('watch holding')),
  `the observed boundary with real traffic reads as holding -- got ${JSON.stringify(observedCaptions)}`,
)

// --- Click-through: a band hands off to Stream, filtered -------------------
await page.click('.fall .band:not(.dark) >> nth=0')
await page.waitForSelector('input.rule', { timeout: 5000 })
const ifaceFilter = await page.inputValue('input[aria-label="Interface"]')
check(
  ifaceFilter === 'ether1' || ifaceFilter === 'bridge1',
  `clicking the band fills Stream's interface filter -- got "${ifaceFilter}"`,
)
const chainFilter = await page.inputValue('select[aria-label="Chain"]')
check(chainFilter === 'forward', `clicking the band fills Stream's chain filter -- got "${chainFilter}"`)

// --- prefers-reduced-motion disables the now-line pulse --------------------
await page.emulateMedia({ reducedMotion: 'reduce' })
await page.click('.rail .item .label:text-is("The fall")')
await page.waitForSelector('.fall .band', { timeout: 10000 })
const animName = await page.$eval('.now-dot', (el) => getComputedStyle(el).animationName)
check(animName === 'none', `reduced motion disables the now-line pulse -- got animation-name "${animName}"`)

check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
