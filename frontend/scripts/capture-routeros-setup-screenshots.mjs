// SPDX-License-Identifier: AGPL-3.0-only
//
// One-off dev tool, not a live-check scenario -- deliberately named
// outside the `live-*.mjs` glob so scripts/run-scenarios.sh (and
// `make live-check`) never picks it up: it produces images, not
// pass/fail checks, and doesn't fit the check()/done() contract the
// other scripts share. Same shape as
// capture-engine-room-screenshots.mjs, which it borrows its login and
// framing mechanics from.
//
// Produces four images docs/routeros-setup.md needs:
//
//  - docs/screenshots/setup-wizard-ledger.png: the wizard's seven-step
//    ledger with step 1 open, which is the path that enrols the FIRST
//    router (account menu ▸ Run setup…). #1284's "docs show both paths
//    with screenshots".
//  - docs/screenshots/entities-add-a-router.png: the Entities routers
//    row with its "+ add a router" berth, which is the path that adds
//    every LATER one. Also #1284.
//  - docs/screenshots/setup-wizard-send-logs.png: the Send logs step
//    once a token is minted, carrying the "Token good until ..." line
//    and its Reroll control. #1305.
//  - docs/screenshots/entities-refused-sender.png: a refused-sender
//    card next to the router cards on Entities. Also #1305.
//
// Both wizard shots are clipped to the modal from the step list down
// rather than taken whole. The block above that line is the address
// field, and its "This machine also answers on ..." line prints the
// capture host's own LAN and docker-bridge addresses -- real addresses
// of whoever ran this, published into a public repo, and nothing the
// documentation needs. The clip starts below it; everything each
// marker asked for is inside.
//
// The Send logs shot has a second host-specific thing on screen: a
// live enrolment token, minted for real so the "Token good until ..."
// line is real too. That line can't just be framed out the way the
// address field is -- it sits directly under the one <pre> that also
// carries the token itself, and the marker wants both on screen. So the
// token is blanked in the DOM immediately before the shot, in place
// rather than by cropping around it (see that block below for the
// check that fails the run rather than silently screenshotting a live
// token if the line it expects to blank isn't there).
//
// deviceScaleFactor 2, matching engine-room-people-door.png: all four
// are element-sized crops rather than full 1600x900 views, so they are
// rendered at 2x to stay legible when a browser scales them down to
// the width of a docs column.
//
// Usage:
//   export MV_DEMO_DEVICES=1
//   eval "$(scripts/live-env.sh up)"       # routers row has routers in it
//   scripts/live-env.sh syslog 200   # rule-tagged events, for the
//                                     # ledger's "N of N carry an
//                                     # action" rules-step receipt
//   scripts/seed-demo.py entities    # named routers/hosts/ports
//   scripts/seed-demo.py feed --once # one tick per declared router, so
//                                     # each router card's "events/s
//                                     # now" is > 0 -- fleet.ts reads a
//                                     # 5-minute recent window, so one
//                                     # tick is enough; --once exits
//                                     # rather than running forever
//   cd frontend && node scripts/capture-routeros-setup-screenshots.mjs
//   scripts/live-env.sh down   (from the repo root)

import { chromium } from 'playwright'
import { fileURLToPath } from 'url'
import path from 'path'
// feedRawFrom is the one live-browser.mjs helper this borrows: it opens
// a real TLS connection to the running instance's syslog listener and
// makes it appear to come from a given address, which is the only way
// to put a "refused sender" or a fresh enrolment on screen -- there is
// no plaintext listener left to hand-roll one against (see its own doc
// comment). Everything else here stays independent of live-browser.mjs
// on purpose, matching capture-engine-room-screenshots.mjs: this is a
// one-off tool, not a scenario, and importing session()/check()/done()
// would pull in a contract it doesn't use.
import { feedRawFrom } from './live-browser.mjs'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..')
const URL_BASE = process.env.MV_URL
const USER = process.env.MV_USER
const PASS = process.env.MV_PASS
if (!URL_BASE || !USER || !PASS) {
  console.error('MV_URL/MV_USER/MV_PASS unset -- run: eval "$(scripts/live-env.sh up)"')
  process.exit(2)
}

const SHOTS = path.join(REPO, 'docs', 'screenshots')

const browser = await chromium.launch()

async function signedInPage() {
  const context = await browser.newContext({
    viewport: { width: 1600, height: 900 },
    deviceScaleFactor: 2,
    colorScheme: 'dark',
    ignoreHTTPSErrors: true,
  })
  const page = await context.newPage()
  await page.goto(URL_BASE, { waitUntil: 'networkidle' })
  await page.fill('input[autocomplete="username"]', USER)
  await page.fill('input[autocomplete="current-password"]', PASS)
  await page.click('button[type="submit"]')
  await page.waitForSelector('#main-content', { timeout: 15000 })
  await page.waitForTimeout(2000)
  return { context, page }
}

// --- The ledger: the path that enrols the first router ------------------
{
  const { context, page } = await signedInPage()
  await page.locator('.deck .card .account button').first().click()
  await page.waitForSelector('.account .menu')
  await page.click('.account .menu button.row:text-is("Run setup…")')
  await page.waitForSelector('.setup-wizard', { state: 'visible', timeout: 10000 })
  // The step list's receipts ("466 of 466 events carry an action") come
  // from /api/setup/status, which resolves after the modal paints.
  // Wait for the "rules" step's own receipt to actually say so, rather
  // than a fixed sleep and hoping the API won by then -- capturing too
  // early leaves the ledger reading "nothing has arrived yet" against an
  // instance that has heard plenty.
  await page.waitForFunction(
    () => {
      const receipt = document.querySelector('.setup-wizard .steps li:nth-child(4) .step-receipt')
      return !!receipt && /events carry an action/.test(receipt.textContent ?? '')
    },
    null,
    { timeout: 15000 },
  )
  const title = (await page.locator('#setup-wizard-title').textContent())?.trim()
  if (title !== 'Trust the certificate') {
    throw new Error(`wizard opened at ${JSON.stringify(title)}, not step 1 -- the shot is meant to show the ledger at its first step`)
  }
  const modal = await page.locator('.setup-wizard').boundingBox()
  const middle = await page.locator('.setup-wizard .middle').boundingBox()
  await page.screenshot({
    path: path.join(SHOTS, 'setup-wizard-ledger.png'),
    clip: {
      x: modal.x,
      y: middle.y,
      width: modal.width,
      height: modal.y + modal.height - middle.y,
    },
  })
  console.log('captured setup-wizard-ledger.png')
  await context.close()
}

// --- The berth: the path that adds every later router -------------------
{
  const { context, page } = await signedInPage()
  await page.click('.roll-rail button.rail-name:text-is("Entities")')
  await page.waitForFunction(
    () => {
      const deck = document.querySelector('.deck')
      const el = deck?.querySelector('.card[data-card="entities"]')
      if (!el) return false
      return Math.abs(el.getBoundingClientRect().top - deck.getBoundingClientRect().top) < 2
    },
    null,
    { timeout: 15000 },
  )
  // Each router card's live rate arrives with the first stats poll after
  // the card mounts; wait for an actual positive rate on at least one
  // live card rather than a fixed sleep, or the row reads as a fleet
  // that has never said anything.
  await page.waitForFunction(
    () => {
      const rows = Array.from(document.querySelectorAll('.card[data-card="entities"] .fcard.live .frow'))
      return rows.some((el) => {
        const m = /([\d.]+) events\/s now/.exec(el.textContent ?? '')
        return m !== null && parseFloat(m[1]) > 0
      })
    },
    null,
    { timeout: 15000 },
  )
  const row = page.locator('.card[data-card="entities"] .og').first()
  const berth = await row.locator('.fcard.berth').count()
  if (berth !== 1) {
    throw new Error(`found ${berth} "+ add a router" berths, want exactly 1 -- sign in as an admin`)
  }
  const box = await row.boundingBox()
  const pad = 12
  await page.screenshot({
    path: path.join(SHOTS, 'entities-add-a-router.png'),
    clip: { x: box.x - pad, y: box.y - pad, width: box.width + pad * 2, height: box.height + pad * 2 },
  })
  console.log('captured entities-add-a-router.png')
  await context.close()
}

// --- The mint: Send logs once a token is on the page --------------------
{
  const { context, page } = await signedInPage()
  await page.click('.roll-rail button.rail-name:text-is("Entities")')
  await page.waitForFunction(
    () => {
      const deck = document.querySelector('.deck')
      const el = deck?.querySelector('.card[data-card="entities"]')
      if (!el) return false
      return Math.abs(el.getBoundingClientRect().top - deck.getBoundingClientRect().top) < 2
    },
    null,
    { timeout: 15000 },
  )
  await page.click('button.berth-trigger[aria-label="Add a router"]')
  const wizard = page.locator('.setup-wizard')
  await wizard.waitFor({ timeout: 10000 })
  // Named like a real fleet member, not like capture scaffolding: this
  // name is in frame at step 1 ("named branch-hex"), and seed-demo.py's
  // own devices (border-rb5009, office-hex, ...) and the refused-sender
  // shot's rb5009 set the voice a shipped doc image should match.
  await page.fill('#router-name', 'branch-hex')
  // Next is what creates the router on the Name step, and it advances
  // the ledger to Send logs itself (SetupWizard.svelte's onNext) -- there
  // is no second Next to click for that move.
  await page.click('.setup-wizard footer button.primary:text-is("Next")')
  await wizard.locator('.mint-ask').waitFor({ timeout: 10000 })
  const title = (await page.locator('#setup-wizard-title').textContent())?.trim()
  if (title !== 'Send logs') {
    throw new Error(`wizard opened at ${JSON.stringify(title)}, not Send logs -- the shot is meant to show that step`)
  }

  // The mint form asks for the router's own address (whatever the
  // enrolment window binds to) and the admin's password (minting is
  // what opens the port, so re-typing it is the point) -- same two
  // fields live-enrolment.mjs's mintFor drives. 203.0.113.0/24 is
  // RFC 5737's documentation range, already this suite's convention for
  // an address that means nothing beyond the shot (live-token-copy.mjs).
  await page.fill('#setup-wizard-enrol-address', '203.0.113.50')
  await page.fill('#setup-wizard-enrol-password', PASS)
  await page.click('.setup-wizard .mint-ask button:text-is("Mint the token")')

  // Minting closes the mint-ask form and prints the enrol block with a
  // real token as its last line, plus the "Token good until ..." line
  // this shot exists to show. Wait for that exact countdown text rather
  // than a fixed sleep or the form closing -- either can be true a beat
  // before the token is actually the thing on screen.
  await page.waitForFunction(
    () => {
      const el = document.querySelector('.setup-wizard .token-life')
      return !!el && /^Token good until \d\d:\d\d \(\d{1,2} minutes?\) · Reroll$/.test((el.textContent ?? '').trim())
    },
    null,
    { timeout: 15000 },
  )

  // The live token itself, not just the countdown line, is what #1305
  // exists to keep off this page. The block that carries it is one
  // <pre> with no per-line markup, so there is nothing to clip around --
  // the token is blanked in the DOM instead, in place, right before the
  // shot. Matched by the same 20-char [a-z0-9] shape
  // internal/device/enrolment.go's enrolLineRE accepts, so this can't
  // match a different line by accident. If the line isn't there, this
  // throws rather than screenshotting whatever the block actually says
  // -- a silent miss here is exactly how a real token would reach git.
  const pre = wizard.locator('.body pre').first()
  const redacted = await pre.evaluate((el) => {
    const text = el.textContent ?? ''
    const m = text.match(/mikroview-enrol [a-z0-9]{20}/)
    if (!m) return false
    el.textContent = text.replace(m[0], `mikroview-enrol ${'\u2588'.repeat(20)}`)
    return true
  })
  if (!redacted) {
    throw new Error('the Send logs block has no "mikroview-enrol <token>" line to redact -- refusing to screenshot it')
  }

  // Same clip shape as the ledger shot above: from the step list down,
  // which is what keeps the address field's host-specific LAN/bridge
  // addresses out of frame here too.
  const modal = await page.locator('.setup-wizard').boundingBox()
  const middle = await page.locator('.setup-wizard .middle').boundingBox()
  await page.screenshot({
    path: path.join(SHOTS, 'setup-wizard-send-logs.png'),
    clip: {
      x: modal.x,
      y: middle.y,
      width: modal.width,
      height: modal.y + modal.height - middle.y,
    },
  })
  console.log('captured setup-wizard-send-logs.png')

  // The capture's own router doesn't belong in whatever instance this
  // ran against once the shot is taken -- same reasoning as
  // capture-engine-room-screenshots.mjs's extra account. The header is
  // load-bearing: every mutating request needs auth.go's CSRF header or
  // the server 403s it, and a DELETE whose response is never checked
  // "succeeds" either way -- caught only by the ghost router it leaves
  // behind turning up, unlabelled, in a later shot of this same run.
  const deviceId = await page.evaluate(async () => {
    const res = await fetch('/api/devices')
    const body = await res.json()
    return body.devices?.find((d) => d.name === 'branch-hex')?.id ?? null
  })
  if (deviceId) {
    const deleteStatus = await page.evaluate(async (id) => {
      const res = await fetch(`/api/devices/${encodeURIComponent(id)}`, {
        method: 'DELETE',
        headers: { 'X-Requested-With': 'mikroview' },
      })
      return res.status
    }, deviceId)
    if (deleteStatus !== 204) {
      throw new Error(`deleting the capture's own router failed (DELETE /api/devices/${deviceId} -> ${deleteStatus})`)
    }
  }
  await context.close()
}

// --- The refused card: a stranger beside the routers ---------------------
{
  const { context, page } = await signedInPage()

  // A source nothing has enrolled and no token is pending for -- the
  // "closed port" case live-enrolment.mjs's step f proves -- refused at
  // TCP accept, before TLS, before a byte of the line below is read.
  // feedRawFrom throws on exactly that refusal (the connection itself is
  // reset), so the try/catch is the assertion: if this address were
  // somehow accepted, there would be nothing refused to screenshot, and
  // that is worth failing loudly over rather than quietly capturing a
  // card that doesn't mean what the docs say it means.
  // feedRawFrom binds this process's own outgoing socket to the address
  // given -- see live-browser.mjs's own comment on feedRawFrom -- so it
  // has to be a real address this host can actually send from. Only
  // 127.0.0.0/8 is: the whole /8 routes to loopback, so any address in
  // it binds, where an RFC 5737 documentation address (used elsewhere
  // in this suite for content that only has to look real) does not,
  // and fails at socket.bind rather than at the server's own refusal --
  // confirmed live, the wrong way, before this address was picked.
  // 127.0.0.99 is outside both MV_DEMO_DEVICES' 127.0.0.1-6 and
  // live-enrolment.mjs's 127.0.0.21-26, so it can't collide with either.
  const REFUSED_IP = '127.0.0.99'
  const refusedLine =
    'firewall,info D|capture-refused-sender| forward: in:ether1 out:bridge1, connection-state:new, ' +
    'proto TCP (SYN), 203.0.113.60:51500->192.168.1.10:8291, len 60'
  let refusedAtConnect = false
  try {
    feedRawFrom(REFUSED_IP, refusedLine)
  } catch {
    refusedAtConnect = true
  }
  if (!refusedAtConnect) {
    throw new Error(`a connection from ${REFUSED_IP} was accepted -- nothing was refused, so there is no card to capture`)
  }

  // Confirm the refusal landed before navigating anywhere: Entities'
  // own refused-card effect only re-fetches when a *device's* identity
  // changes (Entities.svelte's deviceSignature), not on every poll tick,
  // so a card fed after the page has already mounted could sit unread
  // until something else happens to change that signature. Fed and
  // confirmed first, then mounted, the page's own first fetch picks it
  // up -- no reload, no guessing at the poll cadence.
  const deadline = Date.now() + 20000
  let onList = false
  while (Date.now() < deadline && !onList) {
    const list = await page.request.get(`${URL_BASE}/api/devices/refused`).then((r) => r.json())
    onList = Array.isArray(list) && list.some((r) => r.ip === REFUSED_IP)
    if (!onList) await new Promise((r) => setTimeout(r, 500))
  }
  if (!onList) {
    throw new Error(`GET /api/devices/refused never listed ${REFUSED_IP} -- nothing to capture`)
  }

  await page.click('.roll-rail button.rail-name:text-is("Entities")')
  await page.waitForFunction(
    () => {
      const deck = document.querySelector('.deck')
      const el = deck?.querySelector('.card[data-card="entities"]')
      if (!el) return false
      return Math.abs(el.getBoundingClientRect().top - deck.getBoundingClientRect().top) < 2
    },
    null,
    { timeout: 15000 },
  )
  const row = page.locator('.card[data-card="entities"] .og').first()
  const refusedCard = row.locator('.fcard.refused', { hasText: REFUSED_IP })
  await refusedCard.waitFor({ state: 'visible', timeout: 20000 })
  const cardCount = await refusedCard.count()
  if (cardCount !== 1) {
    throw new Error(`found ${cardCount} refused cards for ${REFUSED_IP}, want exactly 1`)
  }

  // "beside the routers" is the marker's own framing, so the crop is the
  // union of the refused card and whichever real router card sits next
  // to it in the row -- not the whole fcards strip (that's already
  // entities-add-a-router.png's shot) and not the refused card alone,
  // which on its own would read as any old unattributed source rather
  // than one drawn next to a fleet. Read from the DOM rather than a
  // fixed offset because the row is flex-wrapped: which card lands next
  // to the refused one depends on how many routers the instance has.
  const box = await page.evaluate(() => {
    const cards = Array.from(document.querySelectorAll('.card[data-card="entities"] .og .fcards > .fcard'))
    const refusedIndex = cards.findIndex((el) => el.classList.contains('refused'))
    if (refusedIndex < 0) return null
    let neighbor = null
    for (let i = refusedIndex - 1; i >= 0; i--) {
      if (!cards[i].classList.contains('refused') && !cards[i].classList.contains('berth')) {
        neighbor = cards[i]
        break
      }
    }
    if (!neighbor) return null
    const a = cards[refusedIndex].getBoundingClientRect()
    const b = neighbor.getBoundingClientRect()
    const left = Math.min(a.left, b.left)
    const top = Math.min(a.top, b.top)
    const right = Math.max(a.right, b.right)
    const bottom = Math.max(a.bottom, b.bottom)
    return { x: left, y: top, width: right - left, height: bottom - top }
  })
  if (!box) {
    throw new Error('no router card sits beside the refused card to crop around -- seed the instance with routers first')
  }
  const pad = 12
  await page.screenshot({
    path: path.join(SHOTS, 'entities-refused-sender.png'),
    clip: { x: box.x - pad, y: box.y - pad, width: box.width + pad * 2, height: box.height + pad * 2 },
  })
  console.log('captured entities-refused-sender.png')
  await context.close()
}

await browser.close()
