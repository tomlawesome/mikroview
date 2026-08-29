// SPDX-License-Identifier: AGPL-3.0-only
//
// #502: an operator authors a detector, and it fires.
//
// Why a live scenario and not only Go tests. The unit tests prove each
// joint on its own -- the envelope validates, the builder builds, the
// dispatch index places it -- and none of them prove the one thing the
// issue actually asks for: that a detector described entirely by stored
// data, created through the real API on the real binary, evaluates real
// events arriving over the real syslog listener and raises a real flag.
// Every previous defect in this area was of that shape: a definition
// that stored fine and was never registered, or was registered and never
// reached by the ingest path. Neither end can see that alone.
//
// Shares one instance with every other scenario here, so it uses its own
// source address and destination port, and cleans up after itself.

import { session, check, done, feedRaw, waitForFlag } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

// Unique to this scenario, so nothing another scenario feeds can be
// mistaken for what this one is watching for.
const SOURCE = '198.51.100.222'
const PORT = 8443
const RULE = 'live-custom-detection'

const { page } = await session()

async function api(method, path_, body) {
  const res = await page.request.fetch(`${URL_BASE}${path_}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  const text = await res.text()
  let parsed = null
  try {
    parsed = text ? JSON.parse(text) : null
  } catch {
    parsed = null
  }
  return { status: res.status(), body: parsed, text }
}

const created = []

// --- authoring a detector ----------------------------------------------

const detector = await api('POST', '/api/definitions', {
  name: 'live-custom-detection watch',
  intent: 'detection',
  detection: {
    conditions: [{ field: 'destinationPort', operator: 'equals', values: [String(PORT)] }],
    key: 'perSource',
    counting: 'total',
    detailTemplate: `{Count} attempts against port ${PORT} from {SourceAddress}`,
    threshold: 4,
    window: '60s',
  },
})
check(detector.status === 201, `a custom detection is created (${detector.status} ${detector.text.slice(0, 200)})`)
const detectorID = detector.body?.id
check(typeof detectorID === 'string' && detectorID.length > 0, 'it is given a server-generated id')
if (detectorID) created.push(detectorID)

check(
  detector.body?.provenance?.origin === 'custom' && detector.body?.kind === 'declarative',
  `an operator-authored detector is custom and declarative (${detector.body?.provenance?.origin}/${detector.body?.kind})`,
)
check(
  detector.body?.detection?.conditions?.[0]?.field === 'destinationPort',
  `its conditions come back with it (${JSON.stringify(detector.body?.detection?.conditions)})`,
)
// Structure in the block, tunables in Params -- the split #502 ratified.
// Threshold and window must be reachable by the same params editor that
// tunes every shipped detector, not by an editing path of their own.
check(
  detector.body?.params?.threshold === 4 && detector.body?.params?.window === '60s',
  `its threshold and window are ordinary params (${JSON.stringify(detector.body?.params)})`,
)
check(
  Array.isArray(detector.body?.paramSchema) && detector.body.paramSchema.some((p) => p.name === 'threshold'),
  'it declares its own param schema, so the existing params editor can render it',
)
check(
  detector.body?.available === true,
  'the created detector is available -- an unbuildable one would be shelved and never evaluated',
)
check(
  detector.body?.dispatch?.alwaysConsulted === false,
  `narrowed by its destination-port condition, so it is not consulted on every event (${JSON.stringify(detector.body?.dispatch)})`,
)

// --- and it fires ------------------------------------------------------

// Four matching events, the threshold this detector was created with.
// Sent through the real listener, not injected into the store.
for (let i = 0; i < 4; i++) {
  feedRaw(
    `firewall,info D|${RULE}| forward: in:ether1 out:bridge1, connection-state:new, ` +
      `proto TCP (SYN), ${SOURCE}:${41000 + i}->192.168.1.10:${PORT}, len 60`,
  )
}

const raised = await waitForFlag(page, SOURCE)
check(raised.ok, `the operator's own detector raised a flag: ${raised.message}`)
const flag = raised.seen.find((f) => f.target === SOURCE)
check(
  flag?.type === detectorID,
  `the flag is this detector's, not a shipped one's (type=${flag?.type}, want ${detectorID})`,
)

// The detail sentence is the operator's template, rendered -- the
// placeholder set is closed and resolved at create time, so a template
// that reached this point must render.
const flagged = await api('GET', '/api/flags')
const detail = (flagged.body?.flags ?? []).find((f) => f.target === SOURCE && f.type === detectorID)?.detail
check(
  detail === `4 attempts against port ${PORT} from ${SOURCE}`,
  `the operator's detail template rendered (${JSON.stringify(detail)})`,
)

// Replay answers over the stored corpus for a custom detection the same
// way it does for anything else declarative -- the claim that
// replayability comes free is only true if the inspection path can build
// one.
check(
  detector.body?.replay?.known === true && detector.body?.replay?.capable === true,
  `the detector declares itself replayable (${JSON.stringify(detector.body?.replay)})`,
)
const replay = await api('POST', `/api/definitions/${encodeURIComponent(detectorID)}/replay`, {})
check(replay.status === 200, `replay answers over the stored corpus (${replay.status})`)
check(
  Boolean(replay.body?.receipt) !== Boolean(replay.body?.decline),
  `a replay answers with exactly one of a receipt or a decline (${JSON.stringify(replay.body)})`,
)

// --- the refusals and the disclosure ------------------------------------

// A template naming a placeholder this detector could never resolve is
// refused before anything is stored, rather than failing at the moment it
// should have fired.
const badTemplate = await api('POST', '/api/definitions', {
  name: 'live-custom-detection bad template',
  intent: 'detection',
  detection: {
    conditions: [{ field: 'destinationPort', operator: 'equals', values: ['9999'] }],
    key: 'perSource',
    counting: 'total',
    detailTemplate: '{Count} across {Ports}',
    threshold: 2,
    window: '60s',
  },
})
check(
  badTemplate.status === 400,
  `a detail template naming an unresolvable placeholder is refused (${badTemplate.status})`,
)

// A detector that cannot be narrowed is accepted -- watching one source
// is a legitimate question -- but says plainly what it costs.
const broad = await api('POST', '/api/definitions', {
  name: 'live-custom-detection broad',
  intent: 'detection',
  detection: {
    conditions: [{ field: 'sourceAddress', operator: 'equals', values: ['203.0.113.201'] }],
    key: 'perSource',
    counting: 'total',
    detailTemplate: '{Count} events from {SourceAddress}',
    threshold: 3,
    window: '60s',
  },
})
check(broad.status === 201, `an un-narrowable detector is accepted, not refused (${broad.status})`)
if (broad.body?.id) created.push(broad.body.id)
check(
  broad.body?.dispatch?.alwaysConsulted === true &&
    (broad.body?.dispatch?.reason ?? '').length > 0,
  `it discloses that it is evaluated against every event (${JSON.stringify(broad.body?.dispatch)})`,
)

// --- cleanup ------------------------------------------------------------

for (const id of created) {
  const removed = await api('DELETE', `/api/definitions/${encodeURIComponent(id)}`)
  check(removed.status === 200, `the custom detection ${id} is deleted (${removed.status})`)
}

const finalList = await api('GET', '/api/definitions')
const leftovers = (finalList.body?.definitions ?? []).filter((d) =>
  (d.name ?? '').startsWith('live-custom-detection'),
)
check(leftovers.length === 0, `this scenario left nothing behind (${leftovers.length} leftover(s))`)

done()
