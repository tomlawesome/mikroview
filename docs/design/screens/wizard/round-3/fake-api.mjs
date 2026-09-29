// SPDX-License-Identifier: AGPL-3.0-only
//
// A fake MikroView backend for round 3's screenshots (#1374): serves the
// real built frontend (frontend/dist) and answers just enough of
// /api/... to drive the real SetupWizard.svelte component into every
// state its status functions can produce, without a router, without a
// database, and without internal/api at all.
//
// Not a mock of the whole app -- a mock of exactly what the app shell and
// the wizard call. Every route the wizard or App.svelte's boot sequence
// touches is answered with a realistic shape (see frontend/src/lib/types.ts,
// which mirrors internal/api's JSON 1:1); anything else gets a safe empty
// body rather than a 404 or 500, because the deck's other scenes may render
// underneath the wizard modal and must never crash it.
//
// Scenario selection: GET /scenario/<name> sets a cookie and resets that
// scenario's mutable state fresh, then redirects to /. Everything after
// that reads and writes one scenario's state, keyed off the cookie --
// this is what lets capture.mjs script a few real interactions (naming a
// router, minting a token, registering it) inside one continuous story
// per scenario, the same way an operator would.
//
// One fictional data story throughout: instance 192.168.13.15:8080,
// router "rb5009" with declared sourceIp 192.168.13.1, and an
// undeclared address 192.168.254.1 arriving instead (the source-address
// split, #442).

import http from 'node:http'
import fs from 'node:fs'
import path from 'node:path'
import crypto from 'node:crypto'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const DIST = path.join(here, '..', '..', '..', '..', '..', 'frontend', 'dist')

const PORT = Number(process.env.FAKE_API_PORT || 8791)
const HOST = '127.0.0.1'

// --- the story's fixed facts --------------------------------------------

const STORY = {
  address: '192.168.13.15:8080',
  routerId: 'rb5009',
  routerName: 'rb5009',
  declaredIp: '192.168.13.1',
  arrivingIp: '192.168.254.1',
}

function iso(offsetMs = 0) {
  return new Date(Date.now() + offsetMs).toISOString()
}

const HOUR = 3600_000

// --- scenario seeds -------------------------------------------------------
//
// Each seed is a function returning a fresh, deep, mutable state object.
// `flags` carries small behaviour switches capture.mjs's scripted
// interactions rely on (an always-in-the-past mint, an always-fresh
// refused sender) -- these are honest about being fixture-only knobs, not
// something the real server does; see the README's coverage notes.

function freshSeed() {
  return {
    instance: {
      tlsEnabled: true,
      hosts: [],
      syslogPort: ':6514',
      syslogEnabled: true,
      address: '',
      addressCandidates: [],
      backupTransport: 'sftp',
    },
    sources: [],
    statusDevices: [],
    pushKinds: ['filter-rule', 'arp', 'ip-address'],
    marks: [],
    witnesses: [],
    devices: [],
    unattributed: [],
    refused: [],
    backups: { enabled: true, keyUnreadable: false, routers: [], totalGenerations: 0, totalRouters: 0, totalBytes: 0, port: '10.0.40.5:47022', lock: LOCK_OPEN, lowSpace: false },
    tokens: [],
    enrolments: {}, // device id -> { token, expiresAt, mintedAt }
  }
}

const LOCK_OPEN = { passphraseSet: true, locked: false, unlockedForYou: true, minPassphraseLength: 12, idleTimeoutSeconds: 900 }

function caBlockedSeed() {
  const s = freshSeed()
  s.instance.address = STORY.address
  s.instance.hosts = ['localhost', '127.0.0.1'] // missing 192.168.13.15
  return s
}

function sourceSplitSeed() {
  const s = freshSeed()
  s.instance.address = STORY.address
  s.instance.hosts = ['localhost', '127.0.0.1', '192.168.13.15']
  s.devices = [
    device({
      id: STORY.routerId,
      name: STORY.routerName,
      sourceIp: STORY.declaredIp,
      configured: true,
      status: 'never_seen',
      multihomedCandidates: [STORY.arrivingIp],
    }),
  ]
  s.unattributed = [{ address: STORY.arrivingIp, lines: 812, firstSeen: iso(-2 * HOUR), lastSeen: iso(-60_000) }]
  return s
}

function rulesSeed(kind) {
  const s = freshSeed()
  s.instance.address = STORY.address
  s.instance.hosts = ['localhost', '127.0.0.1', '192.168.13.15']
  s.sources = [{ source: STORY.declaredIp, caFetchedAt: iso(-3 * HOUR), syslogFirstSeenAt: iso(-3 * HOUR), syslogLastSeenAt: iso(-60_000) }]
  s.devices = [device({ id: STORY.routerId, name: STORY.routerName, sourceIp: STORY.declaredIp, configured: true, status: 'live' })]
  const total = 15234
  const decoded = kind === 'done' ? total : kind === 'some' ? 6031 : 0
  s.statusDevices = [{ device: STORY.declaredIp, configured: true, sourceIp: STORY.declaredIp, events: total, decodedActions: decoded }]
  return s
}

function backupBlockedSeed() {
  const s = mostlyDoneSeed()
  s.backups = { enabled: false, keyUnreadable: false, routers: [], totalGenerations: 0, totalRouters: 0, totalBytes: 0, lock: LOCK_OPEN }
  return s
}

function lostRouterSeed() {
  const s = mostlyDoneSeed()
  s.backups.routers[0].missed = 6
  s.backups.routers[0].lastArrival = iso(-30 * HOUR)
  return s
}

function mostlyDoneSeed() {
  const s = freshSeed()
  s.instance.address = STORY.address
  s.instance.hosts = ['localhost', '127.0.0.1', '192.168.13.15']
  s.sources = [{ source: STORY.declaredIp, caFetchedAt: iso(-9 * HOUR), syslogFirstSeenAt: iso(-9 * HOUR), syslogLastSeenAt: iso(-60_000) }]
  const pushedKinds = { 'filter-rule': iso(-10 * 60_000), arp: iso(-10 * 60_000), 'ip-address': iso(-10 * 60_000) }
  s.statusDevices = [{ device: STORY.declaredIp, configured: true, sourceIp: STORY.declaredIp, events: 15234, decodedActions: 15234, pushedKinds }]
  s.devices = [
    device({
      id: STORY.routerId,
      name: STORY.routerName,
      sourceIp: STORY.declaredIp,
      configured: true,
      status: 'live',
      routerosVersion: '7.16.2',
      routerosStanding: 'reviewed',
      acceptedIp: STORY.declaredIp,
      enrolledAt: iso(-9 * HOUR),
      registeredAt: iso(-9 * HOUR),
    }),
  ]
  s.backups = {
    enabled: true,
    keyUnreadable: false,
    routers: [
      {
        device: STORY.routerId,
        generations: [
          { id: 'g1', backupArrivedAt: iso(-33 * HOUR), rscArrivedAt: iso(-33 * HOUR), backupBytes: 401_452, rscBytes: 37_112, header: 'encrypted' },
          { id: 'g2', backupArrivedAt: iso(-9 * HOUR), rscArrivedAt: iso(-9 * HOUR), backupBytes: 412_004, rscBytes: 38_009, header: 'encrypted' },
        ],
        intervalKnown: true,
        intervalSeconds: 86400,
        lastArrival: iso(-9 * HOUR),
        missed: 0,
      },
    ],
    totalGenerations: 2,
    totalRouters: 1,
    totalBytes: 401_452 + 37_112 + 412_004 + 38_009,
    port: '10.0.40.5:47022',
    lock: LOCK_OPEN,
  }
  return s
}

function device(fields) {
  return {
    firstSeen: iso(-9 * HOUR),
    lastSeen: iso(-60_000),
    eventCount: 15234,
    ...fields,
  }
}

// walkthrough* seeds start empty -- everything else is created live by
// capture.mjs actually driving the UI (name the router, mint the token,
// register it), the same way an operator would.
function walkthroughSeed() {
  const s = freshSeed()
  s.instance.address = STORY.address
  s.instance.hosts = ['localhost', '127.0.0.1', '192.168.13.15']
  return s
}

const SCENARIOS = {
  'fresh-install': { seed: freshSeed },
  'ca-blocked': { seed: caBlockedSeed },
  'source-split': { seed: sourceSplitSeed },
  'rules-waiting': { seed: () => rulesSeed('waiting') },
  'rules-partial-undecoded': { seed: () => rulesSeed('none') },
  'rules-partial-some': { seed: () => rulesSeed('some') },
  'backup-blocked': { seed: backupBlockedSeed },
  'lost-router': { seed: lostRouterSeed },
  'mostly-done': { seed: mostlyDoneSeed },
  walkthrough: { seed: walkthroughSeed },
  'walkthrough-expired-token': { seed: walkthroughSeed, mintExpiresPast: true },
  'walkthrough-refused': { seed: walkthroughSeed, alwaysRefused: true },
}

const WORLD = new Map()

function resetScenario(name) {
  const def = SCENARIOS[name] ?? SCENARIOS['fresh-install']
  WORLD.set(name, def.seed())
}

function stateFor(name) {
  if (!WORLD.has(name)) resetScenario(name)
  return WORLD.get(name)
}

function scenarioDef(name) {
  return SCENARIOS[name] ?? SCENARIOS['fresh-install']
}

// --- RouterOS command fixtures -------------------------------------------
//
// Realistic shapes, copied from internal/routeros/commands_test.go's own
// expectations (TestCaTrustCommands, TestSyslogCommands*, TestScheduleCommands,
// TestBackupScriptMatchesRound45) rather than invented -- see AGENTS.md's
// "Building a ratified design": drawn, not impressioned.

function caTrustCommands(address) {
  if (!address) return ''
  return (
    `/tool fetch url="https://${address}/ca.crt" check-certificate=no dst-path=mikroview-ca.crt\n` +
    `/certificate import file-name=mikroview-ca.crt passphrase=""`
  )
}

function syslogCommands(address, syslogPort, enrolToken) {
  if (!address) return ''
  const host = address.split(':')[0]
  const port = (syslogPort || ':6514').replace(/^:/, '')
  const enrol = enrolToken ? `\n/log info "mikroview-enrol ${enrolToken}"` : ''
  return (
    `:if ([:len [/system logging action find name=mikroview]] = 0) do={ /system logging action add name=mikroview target=remote remote=${host} remote-port=${port} remote-protocol=syslog remote-log-format=syslog src-address=0.0.0.0 } else={ /system logging action set [find name=mikroview] target=remote remote=${host} remote-port=${port} remote-protocol=syslog remote-log-format=syslog src-address=0.0.0.0 }\n` +
    `:if ([:len [/system logging find action=mikroview]] = 0) do={ /system logging add topics=firewall,info action=mikroview }` +
    enrol
  )
}

const RULE_TAGGING_COMMANDS =
  '/ip firewall filter set [find where !dynamic action=drop] log=yes log-prefix="D|drop|"\n' +
  '/ip firewall filter set [find where !dynamic action=reject] log=yes log-prefix="R|reject|"\n' +
  '\n' +
  '# An accept rule matching established or related traffic logs every\n' +
  '# packet, not every connection -- that is your whole traffic volume.\n' +
  '/ip firewall filter set [find where !dynamic and action=accept and !(connection-state~"established") and !(connection-state~"related")] log=yes log-prefix="A|accept|"'

function pushScript(address, token) {
  if (!address || !token) return ''
  return (
    `# fetches /ip firewall filter, /ip arp and /ip address, POSTs each as JSON\n` +
    `# to https://${address}/api/router-state with "Authorization: Bearer ${token}"\n` +
    `:local recs [:toarray ""]\n:set recs ($recs, 1)`
  )
}

function scheduleCommands(source) {
  if (!source) return ''
  return (
    `:if ([:len [/system script find name=mv-push]] = 0) do={ /system script add name=mv-push policy=read,test source="${source.replace(/"/g, '\\"')}" } else={ /system script set [find name=mv-push] policy=read,test source="${source.replace(/"/g, '\\"')}" }\n` +
    `:if ([:len [/system scheduler find name=mv-push]] = 0) do={ /system scheduler add name=mv-push interval=20m policy=read,test on-event="/system script run mv-push" } else={ /system scheduler set [find name=mv-push] interval=20m policy=read,test on-event="/system script run mv-push" disabled=no }\n` +
    `/system script run mv-push`
  )
}

function backupScript(port, device, token) {
  if (!port || !device || !token) return ''
  const source =
    `\n` +
    `  /system backup save name=mv-backup dont-encrypt=yes\n` +
    `  /export hide-sensitive file=mv-export\n` +
    `  /tool fetch mode=sftp upload=yes address=10.0.40.5 port=${port} user=${device} password="${token}" src-path=mv-backup.backup dst-path=${device}.backup\n` +
    `  /tool fetch mode=sftp upload=yes address=10.0.40.5 port=${port} user=${device} password="${token}" src-path=mv-export.rsc dst-path=${device}.rsc\n` +
    `  /file remove mv-backup.backup\n` +
    `  /file remove mv-export.rsc\n`
  return `:if ([:len [/system script find name=mv-backup]] = 0) do={ /system script add name=mv-backup policy=read,write,test,sensitive source="${source.replace(/"/g, '\\"')}" } else={ /system script set [find name=mv-backup] policy=read,write,test,sensitive source="${source.replace(/"/g, '\\"')}" }`
}

function backupScheduleCommands() {
  return (
    `:if ([:len [/system scheduler find name=mv-backup]] = 0) do={ /system scheduler add name=mv-backup interval=1d start-time=03:00:00 policy=read,write,test,sensitive on-event="/system script run mv-backup" } else={ /system scheduler set [find name=mv-backup] interval=1d start-time=03:00:00 policy=read,write,test,sensitive on-event="/system script run mv-backup" disabled=no }`
  )
}

const ROUTEROS_TABLE = {
  minimum: '6.49',
  newest: '7.16.2',
  rows: [
    { from: '6.49', to: '7.15.3', dialect: 'legacy', verifiedBy: 'lab rb941', note: '' },
    { from: '7.16', to: '7.16.2', dialect: 'current', verifiedBy: 'lab rb5009', note: '' },
  ],
}

// --- HTTP plumbing ---------------------------------------------------------

function readCookie(req, name) {
  const raw = req.headers.cookie || ''
  for (const part of raw.split(';')) {
    const [k, ...rest] = part.trim().split('=')
    if (k === name) return decodeURIComponent(rest.join('='))
  }
  return ''
}

function sendJSON(res, status, body) {
  const text = JSON.stringify(body ?? {})
  res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'Content-Length': Buffer.byteLength(text) })
  res.end(text)
}

function sendText(res, status, text) {
  res.writeHead(status, { 'Content-Type': 'text/plain; charset=utf-8' })
  res.end(text)
}

async function readBody(req) {
  const chunks = []
  for await (const chunk of req) chunks.push(chunk)
  if (chunks.length === 0) return {}
  try {
    return JSON.parse(Buffer.concat(chunks).toString('utf8'))
  } catch {
    return {}
  }
}

const MIME = { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png', '.json': 'application/json; charset=utf-8', '.webmanifest': 'application/manifest+json', '.woff2': 'font/woff2' }

function serveStatic(req, res, pathname) {
  let rel = pathname === '/' ? '/index.html' : pathname
  let file = path.join(DIST, rel)
  if (!file.startsWith(DIST)) {
    sendText(res, 403, 'forbidden')
    return
  }
  if (!fs.existsSync(file) || fs.statSync(file).isDirectory()) file = path.join(DIST, 'index.html')
  const ext = path.extname(file)
  fs.readFile(file, (err, data) => {
    if (err) {
      sendText(res, 404, 'not found')
      return
    }
    res.writeHead(200, { 'Content-Type': MIME[ext] || 'application/octet-stream' })
    res.end(data)
  })
}

// AuthSession: a signed-in admin, TOTP already enrolled, no forced
// password change -- so the app shell opens straight to the deck, no
// login/enrolment door in the way.
function authSession() {
  return {
    setupRequired: false,
    authenticated: true,
    username: 'tom',
    role: 'admin',
    hasLocalPassword: true,
    ssoConnected: false,
    mustChangePassword: false,
    mustEnrolSecondFactor: false,
    ssoAvailable: false,
    signedInSince: iso(-9 * HOUR),
    hasTOTP: true,
    passkeys: { count: 0, status: 'unset' },
  }
}

function devicesEnvelope(s) {
  return { devices: s.devices, unattributed: s.unattributed }
}

function setupStatus(s) {
  return {
    instance: s.instance,
    sources: s.sources,
    devices: s.statusDevices,
    pushKinds: s.pushKinds,
    marks: s.marks,
    witnesses: s.witnesses,
  }
}

function setupCommands(s, req) {
  const address = req.address || ''
  const device = req.device || ''
  const token = req.token || ''
  const enrolToken = req.enrolToken || ''
  const noAddress = !address
  const backupRouter = s.backups.enabled && device && token && s.backups.port
  const pushSrc = pushScript(address, token)
  return {
    routeros: ROUTEROS_TABLE,
    picked: req.version ? { version: req.version, standing: 'reviewed', dialect: 'current' } : null,
    routers: [],
    steps: {
      caTrust: { commands: caTrustCommands(address), note: '', blocked: noAddress ? ['no-address'] : undefined },
      syslog: { commands: syslogCommands(address, req.syslogPort, enrolToken), note: '', blocked: noAddress ? ['no-address'] : undefined },
      ruleTagging: { commands: RULE_TAGGING_COMMANDS, note: '' },
      push: { commands: pushSrc, note: '', blocked: noAddress ? ['no-address'] : undefined },
      schedule: { commands: address && token ? scheduleCommands(pushSrc) : '', note: '', blocked: noAddress ? ['no-address'] : undefined },
      backup: {
        commands: backupRouter ? backupScript(s.backups.port.split(':').pop(), device, token) : '',
        note: '',
        blocked: backupRouter ? undefined : backupBlockedKeys(s, device, token),
      },
      backupSchedule: { commands: backupRouter ? backupScheduleCommands() : '', note: '' },
    },
  }
}

function backupBlockedKeys(s, device, token) {
  const keys = []
  if (!s.backups.enabled) keys.push(s.backups.keyUnreadable ? 'retention-key-unreadable' : 'no-retention-key')
  if (!device) keys.push('no-device')
  if (device && !token) keys.push('no-token')
  return keys.length > 0 ? keys : undefined
}

async function handleApi(req, res, url, scenarioName) {
  const s = stateFor(scenarioName)
  const def = scenarioDef(scenarioName)
  const { pathname } = url
  const method = req.method

  if (method === 'GET' && pathname === '/api/auth/session') return sendJSON(res, 200, authSession())
  if (method === 'GET' && pathname === '/api/me/preferences') return sendJSON(res, 200, { version: 1, prefs: {} })
  if (method === 'GET' && pathname === '/api/healthz') return sendJSON(res, 200, { status: 'ok', time: iso(), uptime: '9h0m0s', uptimeSeconds: 32400, version: 'v0.7.0-round3', geoip: false })

  if (method === 'GET' && pathname === '/api/setup/status') return sendJSON(res, 200, setupStatus(s))
  if (method === 'POST' && pathname === '/api/setup/commands') {
    const body = await readBody(req)
    return sendJSON(res, 200, setupCommands(s, body))
  }
  if (method === 'POST' && pathname === '/api/setup/mark') {
    const body = await readBody(req)
    const mark = { step: body.step, outcome: body.outcome, actor: 'tom', at: iso(), note: body.note || '' }
    s.marks = s.marks.filter((m) => m.step !== mark.step)
    s.marks.push(mark)
    return sendJSON(res, 200, mark)
  }
  if (method === 'POST' && pathname === '/api/setup/address') {
    const body = await readBody(req)
    s.instance.address = body.address || ''
    return sendJSON(res, 200, null)
  }
  if (method === 'PUT' && pathname === '/api/setup/backup-transport') {
    const body = await readBody(req)
    s.instance.backupTransport = body.transport === 'https' ? 'https' : 'sftp'
    return sendJSON(res, 200, null)
  }

  if (method === 'GET' && pathname === '/api/devices') return sendJSON(res, 200, devicesEnvelope(s))
  if (method === 'GET' && pathname === '/api/devices/refused') {
    if (def.alwaysRefused) {
      return sendJSON(res, 200, [{ ip: STORY.arrivingIp, firstSeen: iso(-2000), lastSeen: iso(-1000), lines: 4 }])
    }
    return sendJSON(res, 200, s.refused)
  }
  if (method === 'POST' && pathname === '/api/devices') {
    const body = await readBody(req)
    const name = (body.name || '').trim() || 'my-router'
    const id = name
    const row = device({ id, name, sourceIp: '', configured: true, status: 'never_seen', firstSeen: iso(), lastSeen: iso(), eventCount: 0 })
    s.devices = s.devices.filter((d) => d.id !== id)
    s.devices.push(row)
    return sendJSON(res, 200, row)
  }

  const enrolMatch = pathname.match(/^\/api\/devices\/([^/]+)\/enrolment$/)
  if (method === 'POST' && enrolMatch) {
    const id = decodeURIComponent(enrolMatch[1])
    const body = await readBody(req)
    if (!body.password) return sendText(res, 400, 'a password is required')
    const token = `mvt-${crypto.randomBytes(8).toString('hex')}`
    const mintedAt = Date.now()
    const expiresAt = def.mintExpiresPast ? iso(-60_000) : iso(15 * 60_000)
    s.enrolments[id] = { token, expiresAt, mintedAt }
    const d = s.devices.find((x) => x.id === id)
    if (d) d.enrolment = { pending: true, expiresAt }
    return sendJSON(res, 200, { token, expiresAt })
  }
  const registrationMatch = pathname.match(/^\/api\/devices\/([^/]+)\/registration$/)
  if (method === 'POST' && registrationMatch) {
    const id = decodeURIComponent(registrationMatch[1])
    const d = s.devices.find((x) => x.id === id)
    if (!d) return sendText(res, 404, 'no such router')
    d.registeredAt = iso()
    return sendJSON(res, 200, d)
  }

  if (method === 'POST' && pathname === '/api/tokens') {
    const body = await readBody(req)
    const value = `mvt-${crypto.randomBytes(8).toString('hex')}`
    const row = { id: `tok-${s.tokens.length + 1}`, name: body.name, kind: body.kind, device: body.device, createdAt: iso(), value }
    s.tokens.push(row)
    return sendJSON(res, 200, row)
  }
  if (method === 'GET' && pathname === '/api/tokens') return sendJSON(res, 200, s.tokens.map(({ value, ...rest }) => rest))

  if (method === 'GET' && pathname === '/api/router-backups') return sendJSON(res, 200, s.backups)

  if (method === 'GET' && pathname === '/api/definitions') return sendJSON(res, 200, { definitions: [], coverageEvidence: { complete: true } })
  if (method === 'GET' && pathname === '/api/entities') return sendJSON(res, 200, [])
  if (method === 'GET' && pathname === '/api/events') return sendJSON(res, 200, { events: [], hasMore: false, windowStart: iso(-HOUR), serverTime: iso() })
  if (method === 'GET' && pathname === '/api/stats') {
    return sendJSON(res, 200, {
      total: 0,
      byAction: {},
      topRules: [],
      timeSeries: [],
      eventsPerSecond: 0,
      capacity: 200000,
      count: 0,
      windowSeconds: 3600,
      oldestHeld: null,
      connectedClients: 0,
    })
  }
  if (method === 'GET' && pathname === '/api/flags') return sendJSON(res, 200, { flags: [], timeSeries: [] })
  if (method === 'GET' && pathname === '/api/rules') return sendJSON(res, 200, [])

  // Everything else: a safe, never-500 empty. Array-ish paths (plural,
  // or a known list endpoint) get [], everything else gets {} -- see
  // this file's own header comment for why a generic default is
  // enough here.
  if (method === 'GET') {
    const arrayish = /\/(refused|macs|tops|audit|entities|coverage\/declarations|seen-values|hosts)(\?|$)/.test(pathname)
    return sendJSON(res, 200, arrayish ? [] : {})
  }
  return sendJSON(res, 200, {})
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, `http://${req.headers.host}`)

  const scenarioRouteMatch = url.pathname.match(/^\/scenario\/([^/]+)$/)
  if (scenarioRouteMatch) {
    const name = decodeURIComponent(scenarioRouteMatch[1])
    resetScenario(name)
    res.writeHead(302, {
      Location: '/',
      'Set-Cookie': `mv_scenario=${encodeURIComponent(name)}; Path=/`,
    })
    res.end()
    return
  }

  if (url.pathname.startsWith('/api/')) {
    const scenarioName = readCookie(req, 'mv_scenario') || 'fresh-install'
    try {
      await handleApi(req, res, url, scenarioName)
    } catch (err) {
      // Never 500 -- log and answer the safe empty instead.
      console.error('fake-api error on', url.pathname, err)
      sendJSON(res, 200, {})
    }
    return
  }

  serveStatic(req, res, url.pathname)
})

// Minimal handshake so the live-tail socket (/api/ws) opens and goes
// quiet, rather than looping reconnect attempts in the console for the
// whole capture run. Nothing here ever sends a frame back -- the wizard
// never reads the socket at all (see the sub-agent report this build
// was scoped from); this exists only to stop ws.ts's backoff loop from
// spinning against a closed port.
server.on('upgrade', (req, socket, head) => {
  const url = new URL(req.url, 'http://localhost')
  const key = req.headers['sec-websocket-key']
  if (url.pathname !== '/api/ws' || !key) {
    socket.destroy()
    return
  }
  const accept = crypto
    .createHash('sha1')
    .update(key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11')
    .digest('base64')
  socket.write(
    'HTTP/1.1 101 Switching Protocols\r\n' +
      'Upgrade: websocket\r\n' +
      'Connection: Upgrade\r\n' +
      `Sec-WebSocket-Accept: ${accept}\r\n\r\n`,
  )
  // Leave the socket open and silent; drain and discard whatever the
  // client sends (ping frames, etc.) so it never backs up.
  socket.on('data', () => {})
})

server.listen(PORT, HOST, () => {
  console.log(`fake-api listening on http://${HOST}:${PORT}`)
})

export { PORT, HOST, SCENARIOS }
