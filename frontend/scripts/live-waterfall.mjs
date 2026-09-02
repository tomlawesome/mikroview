// SPDX-License-Identifier: AGPL-3.0-only
//
// #616: the fall as the landing page. live-roll-rail.mjs already proves
// "The fall" is what session() lands on; this scenario is the fall's own
// behaviour, and needs a real pushed filter-rule table (not just synthetic
// events) to exercise the one thing worth a browser for: telling a dark
// (unlogged) boundary from an observed one with real cadence, that a
// carrier renders per port (Fable's 2026-08-29 review on #616 rejected
// the first cut's aggregate-lane simplification and asked for this back),
// and that clicking a boundary or a carrier hands off to Stream with its
// filters filled.
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

import { session, feedSyslog, feedRaw, check, responsive, done, goTo } from './live-browser.mjs'

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
      {
        // The cap test's own boundary. ether1/bridge1 is feedSyslog's
        // fixed template pair, shared by most of the suite, so a port
        // count asserted there is whatever the rest of the run left
        // behind (209 on one full run, not this scenario's 12). This
        // pair is fed by nothing but the cap loop below.
        ordinal: 2,
        comment: 'cap lane',
        chain: 'forward',
        action: 'drop',
        srcAddressList: '',
        logPrefix: 'D|cap-test|',
        log: true,
        inInterface: 'ether6',
        outInterface: 'bridge6',
      },
      {
        // #801's quiet band: a boundary that really does log and that
        // nothing in this suite ever feeds, so it is empty for an
        // honest reason. That is the whole distinction round 36 asks
        // the fall to draw -- this band and the silent ether9/bridge9
        // one above are both blank, and only this one is blank because
        // nothing came.
        ordinal: 3,
        comment: 'quiet by choice',
        chain: 'forward',
        action: 'drop',
        srcAddressList: '',
        logPrefix: 'D|quiet-lane|',
        log: true,
        inInterface: 'ether7',
        outInterface: 'bridge7',
      },
    ],
  })) === 200,
  'the filter-rule table is accepted through the real ingest endpoint',
)

// Real, visible cadence on the observed boundary -- feedSyslog's own
// fixed shape always targets tcp/443, so this is a steady talker on one
// carrier (port 443).
feedSyslog(30, 'fall-cadence')

// Twelve distinct ports on the cap boundary (ether6/bridge6, fed by
// nothing else in the suite), one event each -- enough to push its port
// count past MAX_CARRIERS (8) and exercise the cap + "+n quieter"
// affordance the review asked for, without the shared-instance port
// pollution that made this count unassertable on ether1/bridge1.
for (let p = 6000; p < 6012; p++) {
  feedRaw(
    `firewall,info D|cap-test| forward: in:ether6 out:bridge6, connection-state:new, proto TCP (SYN), 203.0.113.50:5000->192.168.1.10:${p}, len 60`,
  )
}

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

// Locators scoped to this scenario's own two boundaries, by exact label
// -- other scenarios' own bands can coexist on this shared instance.
const observedBand = page.locator('.fall .band').filter({ has: page.locator('.band-label:text-is("ether1 → bridge1")') })
const darkBand = page.locator('.fall .band').filter({ has: page.locator('.band-label:text-is("ether9 → bridge9")') })

// --- Honesty: dark != quiet, and it is never colour alone -----------------
check(await darkBand.evaluate((el) => el.classList.contains('dark')), 'the unlogged boundary is marked dark')
check(
  (await darkBand.locator('.band-caption.bad').count()) > 0,
  'a dark band carries its own text caption, not colour alone',
)
check(
  (await darkBand.textContent()).includes('blank because nothing is logged'),
  'the dark band states the honesty distinction in words',
)

// --- Visible cadence on the observed boundary ------------------------------
// WATCHED *or* a named flag, not WATCHED alone: on the shared suite
// instance, another scenario's port-scan flag can sit inside the fall's
// window and flip this band's caption to "✱ TYPE". Both are the
// observed side of the honesty distinction -- what this scenario must
// rule out is the band reading quiet, dark or unknown.
//
// "WATCHED", not round 30's "WATCH HOLDING ✓": the owner retired that
// wording and its tick in round 36 (#790, #801) -- "watched in green
// says everything we need".
const observedCaption = (await observedBand.locator('.band-caption').first().textContent())?.trim() ?? ''
check(
  /^(WATCHED|✱ )/.test(observedCaption),
  `the observed boundary with real traffic reads as observed -- got "${observedCaption}"`,
)
check(
  !observedCaption.includes('✓'),
  `the band caption carries no tick -- the ink is the verdict (got "${observedCaption}")`,
)

// --- Restored per-port carriers (Fable's 2026-08-29 review, twice: the
// deviation review asked for carriers back at all; the rendering-spec
// follow-up asked for thin dash columns positioned by port, not full-
// band-width fills) -----------------------------------------------------
// A carrier renders for the steady talker (port 443): a label, a live
// spectrum peak above the NOW line, and bucketed dash marks below it --
// the whole point the aggregate-lane first cut lost.
// The port labels render at rig level, not nested in each band's <g> --
// they are collision-culled across the whole rig (Fall.svelte's
// portLabels) -- so membership in the observed band is a geometry fact:
// the label's x must land inside that band's own horizontal extent.
await page.locator('.fall .carrier-label[data-port="443"]').first().waitFor({ timeout: 30000 })
const labelInObservedBand = await page.evaluate(() => {
  const band = [...document.querySelectorAll('.fall .band')].find((b) =>
    b.querySelector('.band-label')?.textContent?.includes('ether1 → bridge1'),
  )
  if (!band) return 'no observed band'
  const r = band.getBoundingClientRect()
  return [...document.querySelectorAll('.fall .carrier-label[data-port="443"]')].some((l) => {
    const lr = l.getBoundingClientRect()
    const mid = lr.left + lr.width / 2
    return l.textContent.trim() === ':443 HTTPS' && mid >= r.left && mid <= r.right
  })
})
check(
  labelInObservedBand === true,
  `a carrier renders for the steady talker on port 443, labelled ":443 HTTPS" at the observed band's foot (got ${JSON.stringify(labelInObservedBand)})`,
)
check(
  (await observedBand.locator('.spectrum .spec[data-port="443"]').count()) > 0,
  'the live spectrum strip renders a wave for the port-443 carrier -- "what is arriving this instant"',
)
check(
  ((await page.locator('.fall .now-caption').textContent()) ?? '').includes('NOW ·'),
  'the NOW line carries its labelled moment -- "the spectrum above is this instant"',
)
check(
  (await observedBand.locator('.band-epithet').textContent())?.trim() === 'log the household',
  "the band's epithet is the pushed rule's own comment, not an invented name",
)
const dashCount = await observedBand.locator('.waterfall .mark[data-port="443"]').count()
check(dashCount > 0, 'the carrier draws bucketed dash marks below the NOW line, not an aggregate lane bar')
check(
  dashCount < 60,
  `the dashes are gapped, not a solid fill across every bucket -- got ${dashCount} of 60 buckets marked`,
)
// Thin, not a fill: each dash is DASH_WIDTH (3) wide in the waterfall's
// 0-100 coordinate space, a fraction of the ~100-wide band -- nowhere
// near "spans the column".
const dashWidth = await observedBand.locator('.waterfall .mark[data-port="443"]').first().getAttribute('width')
check(Number(dashWidth) <= 4, `a carrier's dash is a thin strip, not a fill -- got width="${dashWidth}"`)

// --- Cap + "+n quieter" affordance -----------------------------------------
// 12 distinct ports landed on the dedicated cap boundary (ether6/
// bridge6, fed by nothing else in the suite); the cap keeps only the 8
// most recently active as individual carriers.
const capBand = page.locator('.fall .band').filter({ has: page.locator('.band-label:text-is("ether6 → bridge6")') })
await capBand.waitFor({ timeout: 10000 })
const carrierCount = await capBand.locator('.carrier-hit').count()
check(carrierCount === 8, `at most 8 carriers render per band -- got ${carrierCount}`)
const quieterText = await capBand.locator('.quieter').textContent()
check(
  quieterText?.trim() === '+4 quieter ▸',
  `the remaining 4 ports fold into a "+n quieter ▸" affordance -- got "${quieterText}"`,
)
// #801: it sits beneath that band's port labels now, not inside the
// fall -- the drawing puts it below the foot, where it reads as one
// more of the band's names rather than a mark in its traffic.
const quieterY = Number(await capBand.locator('.quieter').getAttribute('y'))
const portLabelY = Number(await page.locator('.fall .carrier-label').first().getAttribute('y'))
check(
  quieterY > portLabelY,
  `"+n quieter ▸" sits below the port labels -- got y=${quieterY} against labels at y=${portLabelY}`,
)

// --- #801: the empty band's quiet statement, and the window-cap chip ------
// The distinction the drawing is after: two blank columns side by side,
// one blank because nothing is logged there and one blank because
// nothing came. Only the second says "quiet, not dark", and it says it
// in the quiet ink -- never the dark band's red.
const quietBand = page.locator('.fall .band').filter({ has: page.locator('.band-label:text-is("ether7 → bridge7")') })
await quietBand.waitFor({ timeout: 10000 })
const quietText = (await quietBand.textContent()) ?? ''
check(
  quietText.includes('logged — quiet, not dark'),
  'a logged boundary that caught nothing states it, rather than drawing a blank column',
)
check(
  quietText.includes('nothing in these 15 m'),
  'the quiet statement counts the window actually drawn, not a fixed span',
)
check(
  (await quietBand.locator('.quiet-anno').count()) === 2 &&
    (await quietBand.locator('.quiet-anno.bad-anno').count()) === 0,
  'the quiet statement is in the quiet ink, never the dark red -- quiet is a fact, not a fault',
)
check(
  (await quietBand.locator('.band-caption').textContent())?.trim() === 'WATCHED',
  'the quiet-but-logged band still reads WATCHED',
)
// ...and the dark band next to it still says the opposite thing, so the
// two states have not collapsed into one.
check(
  !((await darkBand.textContent()) ?? '').includes('quiet, not dark'),
  'a dark boundary never claims to be merely quiet',
)

// The window-cap chip states a truncated window. This instance's own
// window is not truncated -- the fall asks for 5 000 events and the
// suite never puts that many inside one 15-minute span -- so what is
// assertable here is that the chip stays away when it would be untrue.
// Deliberately not forced: feeding 5 000 events to make it appear would
// pollute the shared instance every later scenario reads. The present
// branch is pinned in Fall.svelte.test.ts instead.
check(
  !((await page.locator('.fall .attention').textContent()) ?? '').includes('this window holds more'),
  'the window-cap chip stays absent while the window really does hold everything',
)

// --- Honest captioning on the synthetic "other traffic" band --------------
const otherBand = page.locator('.fall .band').filter({ has: page.locator('.band-label:text-is("other traffic")') })
if (await otherBand.count()) {
  check(
    (await otherBand.locator('.band-caption').textContent())?.trim().length > 0,
    'the "other traffic" band carries its own honest caption, not a bare label',
  )
}

// --- Click-through: a boundary or a carrier hands off to Stream, filtered -
await observedBand.locator('.band-head').click()
await page.waitForSelector('input.rule', { timeout: 5000 })
const ifaceFilter = await page.inputValue('input[aria-label="Interface"]')
check(
  ifaceFilter === 'ether1' || ifaceFilter === 'bridge1',
  `clicking the band fills Stream's interface filter -- got "${ifaceFilter}"`,
)
const chainFilter = await page.inputValue('select[aria-label="Chain"]')
check(chainFilter === 'forward', `clicking the band fills Stream's chain filter -- got "${chainFilter}"`)

// Back to the fall, and this time click the carrier itself -- the port
// should join the filter set too.
await goTo(page, 'The fall')
await page.waitForSelector('.fall .band', { timeout: 10000 })
await page
  .locator('.fall .band')
  .filter({ has: page.locator('.band-label:text-is("ether1 → bridge1")') })
  .locator('.carrier-hit[data-port="443"]')
  .click()
await page.waitForSelector('input.rule', { timeout: 5000 })
const portFilter = await page.inputValue('input[placeholder="Port — number or service"]')
check(portFilter === '443', `clicking a carrier also fills Stream's port filter -- got "${portFilter}"`)

// --- ...and so does the quieter count, from the band's foot ----------------
// #801's "Done when": clicking "+n quieter ▸" opens the stream filtered
// to that boundary -- the way in for the ports too quiet to draw.
//
// Deliberately last of the three handoffs, and not where the "+n quieter"
// assertions above sit. Placed there it was this scenario's *first*
// navigation to the stream, and the live view's first mount ran past a
// 5s wait on a loaded host while the two handoffs below passed on the
// same timeout once warm. It sits at the very bottom of an 800-unit rig
// inside a scrolling card, so it is also scrolled to rather than assumed
// on screen.
await goTo(page, 'The fall')
await page.waitForSelector('.fall .band', { timeout: 10000 })
const quieterLink = page
  .locator('.fall .band')
  .filter({ has: page.locator('.band-label:text-is("ether6 → bridge6")') })
  .locator('.quieter')
await quieterLink.scrollIntoViewIfNeeded()
await quieterLink.click()
await page.waitForSelector('input.rule', { timeout: 15000 })
const quieterIface = await page.inputValue('input[aria-label="Interface"]')
check(
  quieterIface === 'ether6' || quieterIface === 'bridge6',
  `clicking "+n quieter ▸" fills Stream's interface filter -- got "${quieterIface}"`,
)

// --- prefers-reduced-motion disables the now-line pulse --------------------
await page.emulateMedia({ reducedMotion: 'reduce' })
await goTo(page, 'The fall')
await page.waitForSelector('.fall .band', { timeout: 10000 })
const animName = await page.$eval('.now-dot', (el) => getComputedStyle(el).animationName)
check(animName === 'none', `reduced motion disables the now-line pulse -- got animation-name "${animName}"`)

check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
