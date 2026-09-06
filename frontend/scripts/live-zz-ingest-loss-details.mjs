// SPDX-License-Identifier: AGPL-3.0-only
//
// #1001: the ingest-loss banners' `details` link, end to end in a real
// browser -- the ratified drawing (docs/design/concepts/ingest-loss-995,
// scenes 4 and 5) gives every real-loss banner a small `details` control
// on its right edge, and clicking it puts the operator in front of the
// counters the banner is summarising.
//
// What only a real browser can show: that a loss counter moving on the
// server actually produces the control; that clicking it lands on the
// Settings view with the ingest section on screen, from wherever the
// operator happened to be; and that a viewer -- who has no Settings card
// at all (deckCards.ts, #785) -- is not offered a link into a view that
// would be empty for them. ingestLossBanners.test.ts covers which
// banners carry the field; sectionLink.ts's registry is what makes the
// destination a compile-time name rather than a string. Neither can see
// any of the above.
//
// **Named to sort last on purpose.** This scenario trips a real ingest
// loss counter, and those never reset for the life of the instance, so
// the banner it provokes would sit over the top of every scenario that
// ran after it. `zz` keeps it behind live-ws-revocation.mjs, the last
// scenario alphabetically before it.
//
// Not covered here, deliberately: the expanded stack (scene 5), where
// the lead row carries the link and the rows under it do not. That needs
// two loss counters above zero at once, and the harness can raise only
// one of them on demand -- `dropped` needs the ingest queue genuinely
// overrun, `rejectedConfigured` needs the per-source connection limit
// hit, and `wsDropped` is a browser-side counter for a tab falling
// behind. ingestLossBanners.test.ts asserts which banners carry the
// field, and ConnectionBanner.svelte renders it on `i === 0` only.

import { session, feedRaw, check, done, goTo, launchBrowser } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

const DETAIL = '.banner .detail'

// --- 1: a healthy instance offers nothing ---------------------------------
// The link belongs to a loss, not to the banner strip: with every
// counter at zero there is no banner at all, so there is nothing to
// click. This is also the guard on the check below -- if a `details`
// control were already on screen here, the click test after it would
// prove nothing about the counter that follows.

check(
  (await page.locator(DETAIL).count()) === 0,
  'no `details` control is on screen while every ingest counter is at zero',
)

// --- 2: a real loss raises a banner that carries the link -----------------
// One message past maxTCPMessageBytes (64KB, tcp_listener.go): the
// listener truncates it, counts it oversized and remembers the host it
// came from. That is the `oversized` banner's whole input, and it is the
// one loss a test can provoke without pushing the instance into a state
// later scenarios would inherit.

feedRaw(`firewall,info live-1001-oversized: ${'x'.repeat(70000)}`)

// App.svelte polls stats every STATS_REFRESH_MS (5s), so the banner
// arrives on the next poll rather than immediately.
await page.waitForSelector(DETAIL, { timeout: 25000 })

const leadText = await page.locator('.banner').first().innerText()
check(
  leadText.includes('Non-RouterOS sender'),
  `the oversized message raises its own banner -- got ${JSON.stringify(leadText)}`,
)
check(
  (await page.locator(DETAIL).count()) === 1,
  `exactly one \`details\` control is offered -- got ${await page.locator(DETAIL).count()}`,
)

// Right-hand edge of the banner, per the drawing's `.detail` rule -- the
// affordance sits opposite the text it belongs to, not inline with it.
const placement = await page.evaluate(() => {
  const el = document.querySelector('.banner .detail')
  const banner = el?.closest('.banner')
  if (!el || !banner) return null
  const a = el.getBoundingClientRect()
  const b = banner.getBoundingClientRect()
  return { fromRight: Math.round(b.right - a.right), inside: a.top >= b.top && a.bottom <= b.bottom }
})
check(
  placement !== null && placement.fromRight < 40 && placement.inside,
  `the control sits on the banner's right edge -- got ${JSON.stringify(placement)}`,
)

// --- 3: clicking it lands on the ingest counters --------------------------

// waitForLanding waits for the Settings card to be the centred one and
// the ingest section to be on screen. Same bounding-rect comparison
// goTo() uses for a card's arrival -- offsetTop is measured from the
// offset parent, which the banner itself shifts.
async function waitForLanding(page) {
  await page.waitForFunction(
    () => {
      const deck = document.querySelector('.deck')
      const card = deck?.querySelector('.card[data-card="engineroom"]')
      if (!deck || !card) return false
      if (Math.abs(card.getBoundingClientRect().top - deck.getBoundingClientRect().top) >= 2) return false
      const anchor = document.getElementById('engineroom-ingest')
      if (!anchor) return false
      const r = anchor.getBoundingClientRect()
      return r.top < window.innerHeight && r.bottom > 0
    },
    undefined,
    { timeout: 10000 },
  )
}

// From the stream, where session() left us.
await page.click(DETAIL)
await waitForLanding(page)
check(true, 'clicking `details` from the stream lands on the ingest section')

// And from somewhere else entirely: the link states a destination, not a
// direction, so where the operator was when the banner caught their eye
// must not matter. The docket is a different card in the deck and
// scrolls its own body, so arriving from it exercises both halves --
// the view change and the scroll.
await goTo(page, 'Flags')
await page.click(DETAIL)
await waitForLanding(page)
check(true, 'clicking `details` from the docket lands on the ingest section too')

// The section is centred, not merely somewhere on the page:
// scrollIntoView({ block: 'center' }) is what keeps a sticky header off
// the thing we just navigated to.
// A generous band, not a pixel: the section can be taller than the space
// left for it once the banner and scene bar have taken their share, and
// a scroll container already at its end cannot centre anything further.
const CENTRE_TOLERANCE_PX = 400

const centring = await page.evaluate(() => {
  const r = document.getElementById('engineroom-ingest').getBoundingClientRect()
  const middle = (r.top + r.bottom) / 2
  return Math.round(Math.abs(middle - window.innerHeight / 2))
})
check(
  centring < CENTRE_TOLERANCE_PX,
  `the ingest section lands near the middle of the viewport -- off by ${centring}px`,
)

// --- 4: a viewer is not offered a door they cannot open -------------------
// A viewer's deck has no Settings card (deckCards.ts, #657/#785), so a
// `details` link would land them on an empty deck. They still see the
// banner: the loss is real and they are entitled to know about it.

const VIEWER_USER = 'live-viewer-1001'
const VIEWER_PASS = 'live-viewer-1001-password'
const PEOPLE = '#people'

await goTo(page, 'Settings')
await page.click(`${PEOPLE} .ogfoot .olink`)
await page.waitForSelector(`${PEOPLE} .pform`)
await page.fill(`${PEOPLE} .pform input[aria-label="username"]`, VIEWER_USER)
await page.fill(`${PEOPLE} .pform input[aria-label="password"]`, VIEWER_PASS)
await page.click(`${PEOPLE} .pform button:has-text("can only look")`)
await page.click(`${PEOPLE} .pform button:has-text("let them in")`)
await page.waitForSelector(`${PEOPLE} .prow:has-text("${VIEWER_USER}")`)

const viewerBrowser = await launchBrowser()
const viewerCtx = await viewerBrowser.newContext({ ignoreHTTPSErrors: true })
const viewerPage = await viewerCtx.newPage()
await viewerPage.goto(URL_BASE, { waitUntil: 'networkidle' })
await viewerPage.fill('input[autocomplete="username"]', VIEWER_USER)
await viewerPage.fill('input[autocomplete="current-password"]', VIEWER_PASS)
await viewerPage.click('button[type="submit"]')
await viewerPage.waitForSelector('#main-content', { timeout: 15000 })

const viewerBanner = viewerPage.locator('.banner').first()
await viewerBanner.waitFor({ timeout: 25000 })
check(
  (await viewerBanner.innerText()).includes('Non-RouterOS sender'),
  'a viewer sees the same ingest-loss banner as an editor',
)
check(
  (await viewerPage.locator(DETAIL).count()) === 0,
  'a viewer is offered no `details` control, because they have no Settings card to land on',
)

await viewerBrowser.close()

// --- clean up the account, so a rerun starts where this one did -----------
// Arm-then-confirm (round 28's gesture): a click arms remove, a second
// click on the same button confirms it. The oversized counter cannot be
// put back -- see the note at the top about why this scenario runs last.

const remove = page.locator(`${PEOPLE} .prow:has-text("${VIEWER_USER}") .remove`)
await remove.click()
await remove.click()
await page.waitForSelector(`${PEOPLE} .prow:has-text("${VIEWER_USER}")`, { state: 'detached' })
check(true, 'the viewer account is removed again')

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
