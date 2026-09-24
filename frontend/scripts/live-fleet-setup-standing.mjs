// SPDX-License-Identifier: AGPL-3.0-only
//
// #1241: the router-side leg of the upgrade contract. A router's pasted
// push script sends a page saying what it left on the router -- the
// mikroview logging action, the rule feeding it, and which wizard
// version wrote the script -- and GET /api/devices answers, per device,
// whether that still matches what the current wizard would leave:
// `setup: {standing, scriptVersion, currentVersion, reportedAt}`.
// routersetup_test.go pins the comparison itself (internal/setup);
// this drives the real ingest endpoint with a real bearer token and
// reads the real answer back, both from the API and from the card a
// real admin sees in Entities (Fleet.svelte's own comment: the two
// surfaces read the same lib/fleet.ts helpers and cannot drift).
//
// Three standings, #1241's own "done when": never reported (a router
// that has pushed other router state but not this page), current (the
// page matches what today's wizard would leave), and behind (an older
// wizard version). All three need a device that actually exists.
//
// #1281 changed what a push may do once that device exists: since it
// added handleIngestRouterOS's own IsEnrolledAt(tok.Device,
// s.ClientIP(r)) check, a push is 403 unless it arrives from the
// device's own declared or enrolled address -- and a device an ingest
// token merely names, with no address of any kind yet, is enrolled
// nowhere, so its very first push (the one #1170 relied on to Ensure it
// into existence) refused outright. Each device below is now declared
// by name first (POST /api/devices, admin path), enrolled at a loopback
// address of its own via the syslog gate exactly as an operator would,
// and only then pushed to -- bound, via Node's own http/https client,
// to the address it was just enrolled at, since a push from anywhere
// else is exactly what #1281 now refuses.

import { session, check, done, enrolDevice, goTo, launchBrowser, pushFrom, enrolFactorAndSignIn } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
// PortOf(cfg.Listen.SyslogTLS) is what the instance's own wizard would
// write into the action's remote-port -- see internal/routeros/commands.go
// WizardLogging and main.go's SetupInstance.SyslogPort. live-env.sh
// exports the same port under this name (docs/../SKILL.md's "Driving a
// real router" section), so the push below can match it without the
// instance's own address ever entering the comparison (RouterSetup's
// own doc comment: an unset address is not compared at all).
const SYSLOG_TLS_PORT = process.env.MV_SYSLOG_TLS_PORT

// The two devices this scenario needs, and the loopback address each is
// enrolled at -- nothing else in this scenario feeds from either.
const NEVER_ID = 'mv1241-never-reported'
const NEVER_ADDR = '127.0.0.30'
const CURRENT_ID = 'mv1241-current'
const CURRENT_ADDR = '127.0.0.31'

const { page, consoleErrors } = await session()

async function api(method, path_, body) {
  const res = await page.request.fetch(`${URL_BASE}${path_}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  // 204 (DELETE) carries no body, so parsing unconditionally threw.
  return { status: res.status(), body: res.status() < 400 ? await res.json().catch(() => null) : null }
}

async function issueIngestToken(device, name) {
  const res = await api('POST', '/api/tokens', { name, kind: 'ingest', device })
  check(res.status === 201, `an ingest token is issued for ${device} (${res.status})`)
  return res.body?.value
}

// enrolDevice and pushFrom are the harness's own (live-browser.mjs):
// since #1281 a device an ingest token merely names is enrolled
// nowhere, so its very first push -- the one #1170 relied on to
// Ensure it into existence -- is refused outright. Each device below
// is declared by name, enrolled at a loopback address of its own over
// syslog exactly as an operator would, and only then pushed to, bound
// to that address.
const push = (localAddress, token, payload) => pushFrom(URL_BASE, localAddress, token, payload)
const enrol = (id, addr) => enrolDevice(page.request, URL_BASE, id, addr)

function loggingPayload(wizardVersion) {
  return {
    kind: 'logging',
    page: 1,
    pages: 1,
    routerosVersion: '7.16.1',
    wizardVersion,
    records: [
      {
        type: 'action',
        name: 'mikroview',
        target: 'remote',
        remote: '10.0.0.5',
        remotePort: String(SYSLOG_TLS_PORT),
        remoteProtocol: 'tls',
        remoteLogFormat: 'syslog',
        checkCertificate: 'yes',
      },
      { type: 'rule', topics: 'firewall,info', action: 'mikroview', disabled: 'no' },
    ],
  }
}

async function setupOf(deviceId) {
  const { body } = await api('GET', '/api/devices')
  return body?.devices?.find((d) => d.id === deviceId)?.setup ?? null
}

async function waitForStanding(deviceId, standing, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs
  let last = null
  while (Date.now() < deadline) {
    last = await setupOf(deviceId)
    if (last?.standing === standing) return last
    await new Promise((r) => setTimeout(r, 500))
  }
  return last
}

check(!!SYSLOG_TLS_PORT, `MV_SYSLOG_TLS_PORT is set (${SYSLOG_TLS_PORT}) -- the logging push below is built around it`)

// The shipped wizard version, read back rather than hard-coded. The
// backend fills currentVersion even for a router that has never
// reported, and hard-coding it here meant a version bump failed this
// scenario instead of being carried by it.
let currentVersion = null

// --- never reported: router state pushed, the logging page never sent ---

const neverEnrolled = await enrol(NEVER_ID, NEVER_ADDR)
const neverToken = neverEnrolled ? await issueIngestToken(NEVER_ID, 'mv1241-never-reported') : null

if (neverToken) {
  const arpStatus = await push(NEVER_ADDR, neverToken, {
    kind: 'arp',
    page: 1,
    pages: 1,
    records: [{ address: '192.168.1.50', mac: 'aa:bb:cc:dd:ee:ff' }],
  })
  check(
    arpStatus === 200,
    `an ARP table push (router state, not the logging page), bound to its enrolled address, is accepted (${arpStatus})`,
  )

  const { body: afterArp } = await api('GET', '/api/devices')
  check(
    afterArp?.devices?.some((d) => d.id === NEVER_ID),
    `${NEVER_ID} is still in the device registry after the push`,
  )

  const neverSetup = await setupOf(NEVER_ID)
  currentVersion = neverSetup?.currentVersion ?? null
  check(
    Number.isInteger(currentVersion) && currentVersion > 0,
    `the shipped wizard version reads back as a whole number (got ${JSON.stringify(currentVersion)})`,
  )
  check(
    neverSetup?.standing === 'never reported',
    `a device that has pushed router state but never the logging page reports "never reported" (got ${JSON.stringify(neverSetup)})`,
  )
} else {
  check(false, 'skipped the "never reported" push -- no ingest token was issued')
}

// --- current, then behind: the same device's standing after each push ---

const currentEnrolled = await enrol(CURRENT_ID, CURRENT_ADDR)
const token = currentEnrolled ? await issueIngestToken(CURRENT_ID, 'mv1241-setup-standing') : null

if (token) {
  const currentStatus = await push(CURRENT_ADDR, token, loggingPayload(currentVersion))
  check(currentStatus === 200, `a logging page at the current wizard version, bound to its enrolled address, is accepted (${currentStatus})`)

  const { body: afterPush } = await api('GET', '/api/devices')
  check(
    afterPush?.devices?.some((d) => d.id === CURRENT_ID),
    `${CURRENT_ID} is still in the device registry after the push`,
  )

  const current = await waitForStanding(CURRENT_ID, 'current')
  check(
    current?.standing === 'current',
    `a router reporting the current wizard version and no drift stands "current" (got ${JSON.stringify(current)})`,
  )
  check(
    current?.scriptVersion === currentVersion && current?.currentVersion === currentVersion,
    `scriptVersion and currentVersion both read ${currentVersion} while current (got ${JSON.stringify(current)})`,
  )

  const behindStatus = await push(CURRENT_ADDR, token, loggingPayload(currentVersion - 1))
  check(behindStatus === 200, `a second push at an older wizard version, bound to the same enrolled address, is accepted (${behindStatus})`)

  const behind = await waitForStanding(CURRENT_ID, 'behind')
  check(
    behind?.standing === 'behind',
    `the same router reporting an older wizard version now stands "behind" (got ${JSON.stringify(behind)})`,
  )
  check(
    behind?.scriptVersion === currentVersion - 1 && behind?.currentVersion === currentVersion,
    `scriptVersion drops to ${currentVersion - 1} against currentVersion ${currentVersion} while behind (got ${JSON.stringify(behind)})`,
  )

  // --- the same fact, read off a real card ------------------------------
  //
  // Entities' router row is split into registeredRouters/
  // unregisteredRouters (Entities.svelte, gated on device.Info.Configured,
  // which config.yaml sets once at startup and nothing live can flip) --
  // a push-created device, like this one, always lands in the
  // "unregistered" half, whose card is a deliberately different,
  // narrower shape (`its lines are kept; it has no name and no zones
  // until it is registered`) that does not print setupEcho() at all.
  // Fleet.svelte carries the same line unconditionally, for every
  // device regardless of registration -- "a stale router is *why* the
  // log looks wrong" applies whether or not it's been named -- so a real
  // viewer account, which lands on Fleet rather than Entities, is the
  // surface that actually answers #1241's own done-when for a device
  // like this one.
  const VIEWER_USER = 'live-viewer-1241'
  const VIEWER_PASS = 'live-viewer-1241-password'

  await goTo(page, 'Settings')
  await page.click('#people .ogfoot .olink')
  await page.waitForSelector('#people .pform')
  await page.fill('#people .pform input[aria-label="username"]', VIEWER_USER)
  await page.fill('#people .pform input[aria-label="password"]', VIEWER_PASS)
  await page.click('#people .pform button:has-text("can only look")')
  await page.click('#people .pform button:has-text("let them in")')
  await page.waitForSelector(`#people .prow:has-text("${VIEWER_USER}")`)
  check(true, `the viewer account "${VIEWER_USER}" is created from the people door`)

  let cardChecked = false
  let viewerBrowser
  try {
    viewerBrowser = await launchBrowser()
    const ctx = await viewerBrowser.newContext({ ignoreHTTPSErrors: true })
    const vp = await ctx.newPage()
    await vp.goto(URL_BASE, { waitUntil: 'networkidle' })
    await vp.fill('input[autocomplete="username"]', VIEWER_USER)
    await vp.fill('input[autocomplete="current-password"]', VIEWER_PASS)
    await vp.click('button[type="submit"]')
    await enrolFactorAndSignIn(vp)
    await goTo(vp, 'Fleet')

    const card = vp.locator('.fcard', { hasText: CURRENT_ID })
    await card.waitFor({ timeout: 15000 })
    const cardText = (await card.textContent()) ?? ''
    check(
      cardText.includes('setup behind · paste Trust the certificate again'),
      `a viewer's Fleet card for ${CURRENT_ID} names the step to paste again (got: ${cardText.replace(/\s+/g, ' ').trim()})`,
    )
    cardChecked = true
  } catch (e) {
    check(false, `could not read the viewer's Fleet card for ${CURRENT_ID}: ${e}`)
  } finally {
    await viewerBrowser?.close().catch(() => {})
  }
  check(cardChecked, 'the Fleet card check ran to completion')
} else {
  check(false, 'skipped the current/behind pushes -- no ingest token was issued')
}

// Leave the fleet as this scenario found it. Both devices above push
// logging tables, and coverage is read across the whole fleet -- left
// behind, they make "no rule anywhere logs" false for every scenario
// that runs after this one (live-watchlist-coverage and
// live-watchlist-broken-ring both failed on exactly that).
for (const id of [NEVER_ID, CURRENT_ID]) {
  const gone = await api('DELETE', `/api/devices/${encodeURIComponent(id)}`)
  check(gone.status === 204, `${id} is deleted so later scenarios see the fleet as it was (${gone.status})`)
}

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
