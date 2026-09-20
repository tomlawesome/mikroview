// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
  announceStep,
  backupReceipt,
  backupReceiptForDevice,
  backupStep,
  buildLedger,
  caStep,
  certificateCovers,
  finishHeadline,
  firstOpenStep,
  forcedPastRecord,
  hostname,
  nameReceipt,
  nameStep,
  notObserved,
  portOf,
  refusedSince,
  refusedWarning,
  registerReceipt,
  registerStep,
  ROUTER_STEPS,
  pushStep,
  rulesStep,
  silenceExplanation,
  sourceSplitObservation,
  sourceSplitReceipt,
  sourceSplitShortfall,
  sourceSplits,
  srcAddressCommand,
  syslogReceipt,
  syslogStep,
  tokenExpired,
  tokenLine,
} from './setupsteps'
import type { Device, RouterBackupsResponse, SetupMark, SetupStatus } from './types'

function backups(over: Partial<RouterBackupsResponse> = {}): RouterBackupsResponse {
  return {
    enabled: true,
    keyUnreadable: false,
    routers: [],
    totalGenerations: 0,
    totalRouters: 0,
    totalBytes: 0,
    lock: { passphraseSet: false, locked: false, unlockedForYou: false, minPassphraseLength: 12, idleTimeoutSeconds: 900 },
    ...over,
  }
}

function status(over: Partial<SetupStatus> = {}): SetupStatus {
  return {
    instance: {
      tlsEnabled: true,
      hosts: ['192.0.2.10'],
      syslogPort: ':6514',
      syslogEnabled: true,
      address: '',
      addressCandidates: [],
      backupTransport: 'sftp',
    },
    sources: [],
    devices: [],
    pushKinds: ['filter-rule', 'address-list', 'dhcp-lease', 'arp'],
    marks: [],
    witnesses: [],
    ...over,
  }
}

describe('address handling', () => {
  it('strips ports, including from IPv6 literals', () => {
    expect(hostname('192.0.2.10:8080')).toBe('192.0.2.10')
    expect(hostname('192.0.2.10')).toBe('192.0.2.10')
    expect(hostname('[2001:db8::1]:8080')).toBe('2001:db8::1')
  })

  it('takes the port off a listen address', () => {
    expect(portOf(':6514')).toBe('6514')
    expect(portOf('0.0.0.0:6514')).toBe('6514')
  })
})

describe('certificate cover check', () => {
  it('accepts an address the certificate names', () => {
    expect(certificateCovers(status(), '192.0.2.10:8080')).toBe(true)
  })

  // The failure the owner actually hit: reaching MikroView by an address
  // tls.hosts does not list. It surfaces three steps later as
  // "name verification failed", pointing at the router.
  it('rejects an address the certificate does not name', () => {
    expect(certificateCovers(status(), '192.0.2.99:8080')).toBe(false)
  })

  it('falls back to localhost/127.0.0.1 when tls.hosts is unset', () => {
    const s = status({
      instance: { tlsEnabled: true, hosts: [], syslogPort: ':6514', syslogEnabled: true, address: '', addressCandidates: [], backupTransport: 'sftp' },
    })
    expect(certificateCovers(s, '127.0.0.1:8080')).toBe(true)
    expect(certificateCovers(s, '192.168.1.5:8080')).toBe(false)
  })

  it('is not a question when both HTTP TLS and syslog are off', () => {
    const s = status({
      instance: { tlsEnabled: false, hosts: [], syslogPort: ':6514', syslogEnabled: false, address: '', addressCandidates: [], backupTransport: 'sftp' },
    })
    expect(certificateCovers(s, 'anything:8080')).toBe(true)
  })

  // #374: tls.enabled=false only turns off HTTPS on the API port. When
  // syslog TLS is on (main.go loads/generates the certificate whenever
  // cfg.TLS.Enabled || cfg.Listen.SyslogTLS != ""), the router still
  // gets a certificate whose SANs come from tls.hosts, and a mismatch
  // still fails the router's handshake exactly as it would with HTTP
  // TLS on. The short-circuit must key off syslogEnabled too, not just
  // tlsEnabled.
  it('still checks the host when HTTP TLS is off but syslog TLS is on', () => {
    const s = status({
      instance: { tlsEnabled: false, hosts: [], syslogPort: ':6514', syslogEnabled: true, address: '', addressCandidates: [], backupTransport: 'sftp' },
    })
    expect(certificateCovers(s, '192.0.2.30:18084')).toBe(false)
  })

  it('accepts a covered host when HTTP TLS is off but syslog TLS is on', () => {
    const s = status({
      instance: {
        tlsEnabled: false,
        hosts: ['192.0.2.30'],
        syslogPort: ':6514',
        syslogEnabled: true,
        address: '',
        addressCandidates: [],
        backupTransport: 'sftp',
      },
    })
    expect(certificateCovers(s, '192.0.2.30:18084')).toBe(true)
  })
})

describe('step status', () => {
  it('blocks the CA step when the certificate cannot cover the address', () => {
    const s = caStep(status(), '192.0.2.99:8080')
    expect(s.state).toBe('blocked')
    expect(s.detail).toContain('tls.hosts')
  })

  // #1213: an empty address is "not answered yet", not a wrong answer --
  // reporting it as a certificate mismatch would send the operator to
  // edit tls.hosts for a problem that is really the header field above
  // the steps sitting unanswered.
  it('does not read an unanswered address as a certificate mismatch', () => {
    const s = caStep(status(), '')
    expect(s.state).not.toBe('blocked')
  })

  it('reports the CA step done once something has fetched it', () => {
    const s = caStep(status({ sources: [{ source: '192.0.2.1', caFetchedAt: '2026-08-13T00:00:00Z' }] }), '192.0.2.10')
    expect(s.state).toBe('done')
  })

  it('blocks the syslog step when the listener is switched off', () => {
    const s = syslogStep(
      status({
        instance: { tlsEnabled: true, hosts: [], syslogPort: '', syslogEnabled: false, address: '', addressCandidates: [], backupTransport: 'sftp' },
      }),
    )
    expect(s.state).toBe('blocked')
    expect(s.detail).toContain('listen.syslogTls')
  })

  // Connected but silent is a real, common state -- and it is not the
  // same as "cannot reach me", which is why the connection is observed
  // separately from events.
  it('separates connected-but-no-events from no connection', () => {
    const connected = status({ sources: [{ source: '1.2.3.4', syslogFirstSeenAt: '2026-08-13T00:00:00Z' }] })
    expect(syslogStep(connected).state).toBe('done')
    expect(rulesStep(connected).state).toBe('waiting')
    expect(rulesStep(connected).detail).toContain('log=yes')
  })

  // Events with no decoded action look completely healthy on every
  // other measure. This is the state the wizard exists to name.
  it('flags events arriving with no decoded action', () => {
    const s = rulesStep(
      status({ devices: [{ device: 'r', configured: true, sourceIp: '1.2.3.4', events: 40, decodedActions: 0 }] }),
    )
    expect(s.state).toBe('partial')
    expect(s.detail).toBe('Events are arriving.')
    expect(s.shortfall).toContain('log-prefix')
  })

  it('reports partial tagging honestly', () => {
    const s = rulesStep(
      status({ devices: [{ device: 'r', configured: true, sourceIp: '1.2.3.4', events: 10, decodedActions: 4 }] }),
    )
    expect(s.state).toBe('partial')
    expect(s.detail).toBe('4 of 10 events carry an action.')
    expect(s.shortfall).toBe('The other 6 do not — some rules are still untagged.')
  })

  it('names which push blocks are missing rather than just "incomplete"', () => {
    const s = pushStep(
      status({
        devices: [
          {
            device: 'r',
            configured: true,
            sourceIp: '1.2.3.4',
            events: 1,
            decodedActions: 1,
            pushedKinds: { 'filter-rule': '2026-08-13T00:00:00Z' },
          },
        ],
      }),
    )
    expect(s.state).toBe('partial')
    expect(s.shortfall).toContain('address-list')
    expect(s.shortfall).toContain('dhcp-lease')
  })
})

// --- A partial step's shortfall (#1132) --------------------------------
//
// Owner ruling: what arrived and what is still missing are two
// statements, not one sentence -- the shortfall gets its own warning
// box, so the green arrived line never carries the bad news. The rules
// module's part of that is producing the two strings separately; the
// wizard's is rendering them as two boxes.

describe('a partial step states its shortfall apart from its arrival', () => {
  it('splits the push step into what arrived and what is still missing', () => {
    const s = pushStep(
      status({
        devices: [
          {
            device: 'r',
            configured: true,
            sourceIp: '1.2.3.4',
            events: 1,
            decodedActions: 1,
            pushedKinds: { 'filter-rule': '2026-08-13T00:00:00Z', arp: '2026-08-13T00:00:00Z' },
          },
        ],
      }),
    )
    expect(s.state).toBe('partial')
    expect(s.detail).toBe('Arrived: arp, filter-rule.')
    expect(s.shortfall).toBe('Still missing: address-list, dhcp-lease.')
    // The arrived line never carries the shortfall, in any wording: it
    // is rendered green, and a green box saying what is missing is the
    // fault this split exists to fix.
    expect(s.detail).not.toContain('missing')
  })

  it('leaves the shortfall unset on every state that is not partial', () => {
    const everything = status({
      devices: [
        {
          device: 'r',
          configured: true,
          sourceIp: '1.2.3.4',
          events: 4,
          decodedActions: 4,
          pushedKinds: {
            'filter-rule': '2026-08-13T00:00:00Z',
            'address-list': '2026-08-13T00:00:00Z',
            'dhcp-lease': '2026-08-13T00:00:00Z',
            arp: '2026-08-13T00:00:00Z',
          },
        },
      ],
    })
    expect(pushStep(everything).shortfall).toBeUndefined()
    expect(pushStep(status()).shortfall).toBeUndefined()
    expect(rulesStep(everything).shortfall).toBeUndefined()
    expect(caStep(status(), '192.0.2.99:8080').shortfall).toBeUndefined()
  })

  // The announcement is the same two statements in the same order, so a
  // screen reader is not told a step arrived and left to assume the
  // rest.
  it('speaks the shortfall with the arrival', () => {
    const partial = status({
      devices: [
        {
          device: 'r',
          configured: true,
          sourceIp: '1.2.3.4',
          events: 1,
          decodedActions: 1,
          pushedKinds: { 'filter-rule': '2026-08-13T00:00:00Z' },
        },
      ],
    })
    const ledger = buildLedger(partial, [], 'h')
    expect(announceStep(ledger[4])).toBe(
      'Step 5 of 7 — Push router state — Arrived: filter-rule. Still missing: address-list, dhcp-lease, arp.',
    )
  })
})

// --- The claim ledger (#487) -------------------------------------------

function mark(step: number, outcome: 'skipped' | 'forced', over: Partial<SetupMark> = {}): SetupMark {
  return { step, outcome, actor: 'tom', at: '2026-08-23T09:00:00Z', ...over }
}

function device(over: Partial<Device> = {}): Device {
  return {
    id: 'r1',
    name: 'r1',
    sourceIp: '192.0.2.1',
    configured: false,
    firstSeen: '2026-08-23T09:00:00Z',
    lastSeen: '2026-08-23T09:00:00Z',
    eventCount: 1,
    status: 'live',
    ...over,
  }
}

// --- The source-address split (#442) -----------------------------------
//
// A router declared under one address whose logs arrive from another.
// The server pairs the silent declared device with every undeclared
// address that is streaming (Registry.MultihomedCandidates); what is
// tested here is the wording -- the ratified copy on #442, verbatim.

describe('the source-address split', () => {
  const declared = device({
    id: 'office',
    name: 'office',
    sourceIp: '192.168.88.1',
    configured: true,
    eventCount: 0,
    status: 'never_seen',
    multihomedCandidates: ['10.0.20.1'],
  })
  const arriving = device({ id: '10.0.20.1', name: '10.0.20.1', sourceIp: '10.0.20.1' })
  const connected = status({ sources: [{ source: '10.0.20.1', syslogFirstSeenAt: '2026-08-13T00:00:00Z' }] })

  // Two statements, two boxes (#1132): what arrived reads in the
  // arrived voice, and the declared address that is silent is the
  // shortfall beside it -- still both facts, still no diagnosis.
  it('reads as partial, in the voice of evidence composed wrongly, never blocked', () => {
    const s = syslogStep(connected, [declared, arriving])
    expect(s.state).toBe('partial')
    expect(s.detail).toBe("Connected — but from 10.0.20.1, an address you haven't declared.")
    expect(s.shortfall).toBe('192.168.88.1, which you declared in config.yaml, has sent nothing.')
  })

  it('carries the split into the step list receipt', () => {
    expect(syslogReceipt(connected, [declared, arriving])).toBe('syslog from 10.0.20.1 · declared 192.168.88.1 silent')
    const ledger = buildLedger(connected, [declared, arriving], 'h')
    expect(ledger[2].key).toBe('syslog')
    expect(ledger[2].status.state).toBe('partial')
    expect(ledger[2].outcome).toBe('done')
    expect(ledger[2].receipt).toBe('syslog from 10.0.20.1 · declared 192.168.88.1 silent')
  })

  // The existing receipt already states a match; no new words.
  it('says nothing new when the declared device is the one sending', () => {
    const speaking = device({ ...declared, eventCount: 5, status: 'live', multihomedCandidates: undefined })
    const s = syslogStep(connected, [speaking])
    expect(s.state).toBe('done')
    expect(sourceSplits([speaking])).toEqual([])
    expect(syslogReceipt(connected, [speaking])).toContain('syslog connected from 10.0.20.1')
  })

  // The server returns candidates, not a diagnosis, so every arriving
  // address is listed and none is picked.
  it('lists every arriving address rather than picking one', () => {
    const two = device({ ...declared, multihomedCandidates: ['10.0.20.1', '10.0.30.1'] })
    const splits = sourceSplits([two])
    expect(sourceSplitObservation(splits)).toBe(
      "Connected — but from 10.0.20.1 and 10.0.30.1, addresses you haven't declared.",
    )
    expect(sourceSplitShortfall(splits)).toBe(
      '192.168.88.1, which you declared in config.yaml, has sent nothing.',
    )
    expect(sourceSplitReceipt(splits)).toBe('syslog from 10.0.20.1, 10.0.30.1 · declared 192.168.88.1 silent')
  })

  // The remedy keeps the declared address: the command needs only that
  // one value, and it is printed, never run.
  it('prints the src-address command with the declared address filled in', () => {
    expect(srcAddressCommand('192.168.88.1')).toBe('/system logging action set mikroview src-address=192.168.88.1')
  })

  // Only a declared device with a pairing is a split. An undeclared
  // device never is, whatever the server sent alongside it.
  it('ignores undeclared devices and declared ones with no pairing', () => {
    expect(sourceSplits([arriving, device({ ...declared, multihomedCandidates: [] })])).toEqual([])
  })
})

describe('the claim ledger', () => {
  // The count is stable whatever the state: a ledger that grew and
  // shrank would be a different promise every time it was opened. What
  // does change it is which ledger was opened (#1284) -- first-run
  // setup is the certificate step plus the five router ones, and the
  // router ledger is those five on their own.
  it('always has exactly seven steps on first-run setup', () => {
    expect(buildLedger(status(), [], 'h').length).toBe(7)
    expect(buildLedger(status({ sources: [{ source: '1.2.3.4', caFetchedAt: '2026-08-23T09:00:00Z' }] }), [device()], 'h').length).toBe(7)
  })

  // The router ledger is embedded, not copied: the same five steps in
  // the same order, minus the certificate step in front of them.
  it('is the same six router steps, without the certificate step, on the router ledger', () => {
    const setup = buildLedger(status(), [], 'h')
    const router = buildLedger(status(), [], 'h', null, 'sftp', { steps: ROUTER_STEPS })
    expect(router.map((s) => s.key)).toEqual(['name', 'syslog', 'rules', 'push', 'backup', 'register'])
    expect(router.map((s) => s.key)).toEqual(setup.slice(1).map((s) => s.key))
    // Numbered from one in the list being walked, but recorded under
    // the number the server stores, which is the v0.5 order frozen:
    // "Name your router" moved forward for #1284, its marks did not.
    // Register is 7 in both: it is new with #1291 and has no earlier
    // number to preserve.
    expect(router.map((s) => s.n)).toEqual([1, 2, 3, 4, 5, 6])
    expect(router.map((s) => s.canonical)).toEqual([5, 2, 3, 4, 6, 7])
  })

  // A mark is persisted under the server's own fixed number, so it
  // means a step and not a row: step 2 is Send logs in every ledger,
  // whichever position Send logs is walked in.
  it('reads a mark back on the same step whichever ledger is open', () => {
    const marked = status({ marks: [mark(2, 'forced')] })
    const setup = buildLedger(marked, [], 'h')
    const router = buildLedger(marked, [], 'h', null, 'sftp', { steps: ROUTER_STEPS })
    expect(setup[2].key).toBe('syslog')
    expect(setup[2].outcome).toBe('forced')
    expect(router[1].key).toBe('syslog')
    expect(router[1].outcome).toBe('forced')
  })

  it('reads a step with no evidence and no decision as open, with an honest gap', () => {
    const ledger = buildLedger(status(), [], '192.0.2.10')
    expect(ledger[0].outcome).toBe('open')
    expect(ledger[0].flavour).toBe('waiting')
    expect(ledger[0].receipt).toBe('')
  })

  // "arrived (green, dated, sourced)" -- a receipt says what arrived,
  // when, and from where. A green tick with no receipt is the wizard
  // asking to be believed rather than showing its evidence.
  it('carries a dated, sourced receipt once evidence arrives', () => {
    const ledger = buildLedger(
      status({ sources: [{ source: '192.0.2.1', caFetchedAt: '2026-08-23T09:00:00Z' }] }),
      [],
      '192.0.2.10',
    )
    expect(ledger[0].outcome).toBe('done')
    expect(ledger[0].flavour).toBe('arrived')
    expect(ledger[0].receipt).toContain('192.0.2.1')
    expect(ledger[0].receipt).toContain('ca.crt')
  })

  it('records a skip quietly, naming who and when', () => {
    const ledger = buildLedger(status({ marks: [mark(1, 'skipped')] }), [], '192.0.2.10')
    expect(ledger[0].outcome).toBe('skipped')
    expect(ledger[0].receipt).toContain('skipped by tom')
  })

  // Forced is not failed. The record is explicit: if evidence later
  // arrives the step flips to done and stops explaining anybody's
  // silence -- the line stays in the audit log as history, not as a scar
  // the interface keeps pointing at.
  it('lets evidence outrank a forced-past mark', () => {
    const forcedOnly = buildLedger(status({ marks: [mark(2, 'forced')] }), [], '192.0.2.10')
    expect(forcedOnly[2].outcome).toBe('forced')

    const thenArrived = buildLedger(
      status({
        marks: [mark(2, 'forced')],
        sources: [{ source: '192.0.2.1', syslogFirstSeenAt: '2026-08-23T09:05:00Z' }],
      }),
      [],
      '192.0.2.10',
    )
    expect(thenArrived[2].outcome).toBe('done')
    expect(thenArrived[2].receipt).toContain('syslog connected')
  })

  // Tagging rules counts and can only count upward, and naming has
  // nothing to wait for -- Next is always free on both, so neither can
  // raise the heavy warning. The rule follows the step, not its row:
  // the same two are free on the router ledger, one number lower.
  it('marks only the steps with a waiting check as checkable', () => {
    const ledger = buildLedger(status(), [], '192.0.2.10')
    expect(ledger.map((s) => `${s.key}:${s.hasCheck}`)).toEqual([
      'ca:true',
      'name:false',
      'syslog:true',
      'rules:false',
      'push:true',
      'backup:true',
      'register:false',
    ])
    const router = buildLedger(status(), [], '192.0.2.10', null, 'sftp', { steps: ROUTER_STEPS })
    expect(router.map((s) => s.hasCheck)).toEqual([false, true, false, true, true, false])
  })

  it('reads a partially tagged rule set as counting, not as half-failed', () => {
    const ledger = buildLedger(
      status({ devices: [{ device: 'r', configured: true, sourceIp: '1.2.3.4', events: 10, decodedActions: 4 }] }),
      [],
      '192.0.2.10',
    )
    expect(ledger[3].key).toBe('rules')
    expect(ledger[3].flavour).toBe('counting')
    expect(ledger[3].outcome).toBe('done')
    expect(ledger[3].receipt).toContain('4 of 10')
  })

  // The mikroview-side check logic this design inherits (#371/#374) is
  // not one of the four observation flavours: nothing is being waited
  // for, because nothing router-side can work yet.
  it('never dresses a mikroview-side problem up as patient waiting', () => {
    const ledger = buildLedger(status(), [], '192.0.2.99:8080')
    expect(ledger[0].status.state).toBe('blocked')
    expect(ledger[0].flavour).toBe('attention')
  })

  // Naming moved from last to first and now creates the router (#1284),
  // so the old "nothing to name" row is retired: with no router in hand
  // the step is quiet and has nothing to wait for, and once this walk
  // has named one the row is its own evidence.
  it('is quiet until this walk has named a router, then reads done', () => {
    const quiet = nameStep([])
    expect(quiet.state).toBe('quiet')
    expect(quiet.detail).toContain('Nothing to wait for')
    // Another router already existing is not this walk's router.
    expect(nameStep([device({ id: 'other' })]).state).toBe('quiet')

    const named = nameStep([device({ id: 'edge-1', name: 'edge-1' })], 'edge-1')
    expect(named.state).toBe('done')
    expect(named.detail).toContain('edge-1')
    expect(nameReceipt([device({ id: 'edge-1', name: 'edge-1' })], 'edge-1')).toBe('named edge-1')
  })
})

// Register (#1291, Ruling 24): the ledger's last step. Owner's ruling
// was that Next does not act here -- only the "Register this router"
// button does -- so the quiet sentence must say that, not claim Next
// records anything.
describe('registerStep', () => {
  it('is quiet until registered, and its sentence does not claim Next records the confirmation', () => {
    const quiet = registerStep([device({ id: 'edge-1' })], 'edge-1')
    expect(quiet.state).toBe('quiet')
    // Next has no acting branch for this step (CHECKED.register stays
    // false); only the Register button does. The sentence must not
    // claim otherwise, and must point at the button that does act.
    expect(quiet.detail).not.toContain('Next records')
    expect(quiet.detail).toContain('Register')
  })

  it('reads done once the server holds a registeredAt, and says so', () => {
    const registered = registerStep(
      [device({ id: 'edge-1', name: 'edge-1', registeredAt: '2026-09-19T09:00:00Z' })],
      'edge-1',
    )
    expect(registered.state).toBe('done')
    expect(registered.detail).toContain('edge-1')
    expect(
      registerReceipt([device({ id: 'edge-1', name: 'edge-1', registeredAt: '2026-09-19T09:00:00Z' })], 'edge-1'),
    ).toBe('registered edge-1')
  })
})

// --- Step 6: back up the router (#394, round 45) ------------------------

describe('backupStep', () => {
  it('reads null (never asked, or a non-admin session) the same as nothing arrived yet -- never as "no key"', () => {
    const s = backupStep(null)
    expect(s.state).toBe('waiting')
    expect(s.detail).not.toContain('key')
  })

  it('is blocked, in the disabled-step voice, once the server actually says no key is mounted', () => {
    const s = backupStep(backups({ enabled: false }))
    expect(s.state).toBe('blocked')
    expect(s.detail).toContain('No key file is mounted')
    expect(s.detail).toContain('a push would be refused')
  })

  // #1133: the observation line says what was observed and what to do
  // about it; the model -- that mikroview holds this key and seals the
  // history and the state store with it too -- is the lead's job, said
  // once. The old detail was the lead's sentence repeated, and wrong
  // about the model ("a key it does not hold" is the vault passphrase).
  it('leaves the explanation to the lead rather than repeating it, and never says mikroview does not hold the key', () => {
    const s = backupStep(backups({ enabled: false }))
    expect(s.detail).not.toContain('does not hold')
    // "above": the observation line sits under the key field it points at.
    expect(s.detail).toMatch(/Put the key above in place/)
  })

  it('waits once a key is mounted but nothing has pushed yet', () => {
    const s = backupStep(backups({ routers: [] }))
    expect(s.state).toBe('waiting')
  })

  // #1220: an unreachable drop-box port times out partway through the
  // upload, which reads as a stalled push rather than a network
  // problem. Naming the port here is the cheap hint available without
  // a live connection-attempt signal.
  it('hints at the drop box port while waiting, when the server reports one', () => {
    const s = backupStep(backups({ routers: [], port: ':47022' }))
    expect(s.state).toBe('waiting')
    expect(s.detail).toContain('port 47022')
  })

  it('waits with the plain wording when no port is reported', () => {
    const s = backupStep(backups({ routers: [] }))
    expect(s.detail).not.toContain('port')
  })

  it('reads done, with the newest pair in the detail, once something has arrived', () => {
    const b = backups({
      routers: [
        {
          device: 'rb5009',
          generations: [
            { id: 'g0', backupArrivedAt: '2026-09-02T03:00:00Z', rscArrivedAt: '2026-09-02T03:00:05Z', backupBytes: 412000, rscBytes: 38000 },
          ],
          intervalKnown: false,
          missed: 0,
        },
      ],
    })
    const s = backupStep(b)
    expect(s.state).toBe('done')
    expect(s.detail).toContain('rb5009.backup')
    expect(s.detail).toContain('rb5009.rsc')
  })
})

describe('backupReceipt', () => {
  it('is empty with nothing arrived', () => {
    expect(backupReceipt(null)).toBe('')
    expect(backupReceipt(backups())).toBe('')
  })

  it('names the newest pair across every router, not the first', () => {
    const b = backups({
      routers: [
        {
          device: 'rb5009',
          generations: [{ id: 'g0', backupArrivedAt: '2026-08-24T03:00:00Z', rscArrivedAt: '2026-08-24T03:00:05Z', backupBytes: 1000, rscBytes: 100 }],
          intervalKnown: false,
          missed: 0,
        },
        {
          device: 'hap-ax2',
          generations: [{ id: 'g1', backupArrivedAt: '2026-09-02T03:00:00Z', rscArrivedAt: '2026-09-02T03:00:05Z', backupBytes: 2000, rscBytes: 200 }],
          intervalKnown: false,
          missed: 0,
        },
      ],
    })
    const receipt = backupReceipt(b)
    expect(receipt).toContain('hap-ax2.backup')
    expect(receipt).toContain('kept under the key')
    expect(receipt).not.toContain('rb5009')
  })
})

describe('backupReceiptForDevice', () => {
  it('is empty for a router the vault has never heard of', () => {
    expect(backupReceiptForDevice(backups(), 'rb5009')).toBe('')
  })

  it('states this one router\'s own kept count, not the fleet total', () => {
    const b = backups({
      routers: [
        {
          device: 'rb5009',
          generations: Array.from({ length: 10 }, (_, i) => ({ id: `g${i}`, backupArrivedAt: '2026-09-02T03:00:00Z', rscArrivedAt: '2026-09-02T03:00:05Z' })),
          intervalKnown: true,
          missed: 0,
        },
      ],
    })
    expect(backupReceiptForDevice(b, 'rb5009')).toContain('10 pairs kept')
  })
})

describe('reopening the ledger', () => {
  it('lands on the first step still waiting', () => {
    const ledger = buildLedger(
      status({
        sources: [{ source: '1.2.3.4', caFetchedAt: '2026-08-23T09:00:00Z' }],
        // The server's own numbers: 5 is Name your router and 2 is Send
        // logs, whichever position each is walked in.
        marks: [mark(5, 'skipped'), mark(2, 'skipped')],
      }),
      [],
      '192.0.2.10',
    )
    // Walked in order: 1 has evidence, 2 and 3 were decided -- 4, Tag
    // firewall rules, is the first still waiting.
    expect(firstOpenStep(ledger)).toBe(4)
  })

  it('falls back to the first step when nothing is left open', () => {
    const ledger = buildLedger(
      // All seven, including Register (7) -- leaving it out left this
      // test exercising the very bug below by accident rather than on
      // purpose.
      status({ marks: [1, 2, 3, 4, 5, 6, 7].map((n) => mark(n, 'skipped')) }),
      [],
      '192.0.2.10',
    )
    expect(firstOpenStep(ledger)).toBe(1)
  })

  // Register is quiet -- there is nothing to wait for -- but unlike
  // every other quiet step, quiet does not mean answered: nobody has
  // pressed the Register button. Treating it as not-open (the #1291
  // bug) meant reopening a finished walk skipped straight past a
  // router nobody had confirmed.
  it('lands on Register when every other step is decided but it has not been pressed', () => {
    const ledger = buildLedger(
      status({ marks: [1, 2, 3, 4, 5, 6].map((n) => mark(n, 'skipped')) }),
      [],
      '192.0.2.10',
    )
    expect(ledger[6].key).toBe('register')
    expect(firstOpenStep(ledger)).toBe(7)
  })
})

describe('the forced-past record', () => {
  // The amber button quotes the exact record it will write, before it is
  // pressed. The record is the feature, so it is never a surprise
  // produced after the fact.
  it('quotes step, what was not observed, who and when', () => {
    const ledger = buildLedger(status(), [], '192.0.2.10')
    const line = forcedPastRecord(ledger[2], 'tom', new Date('2026-08-23T09:00:00Z'))
    expect(line).toContain('setup · step 2 forced past')
    expect(line).toContain('no router has opened a syslog connection')
    expect(line).toContain('tom')
  })

  // The number quoted is the one the server stores, not the row the
  // operator is looking at: on the router ledger Name your router is
  // step 1 of 5 and is recorded as step 5, because that is where it
  // sits in the ledger the marks are kept in.
  it('quotes the canonical step number, not the row, on the router ledger', () => {
    const router = buildLedger(status(), [], '192.0.2.10', null, 'sftp', { steps: ROUTER_STEPS })
    expect(router[0].n).toBe(1)
    expect(forcedPastRecord(router[0], 'tom', new Date('2026-08-23T09:00:00Z'))).toContain(
      'setup · step 5 forced past',
    )
  })

  // With a token minted and unspent, what has not happened is the
  // enrolment, and its consequence is what the record is worth writing
  // down: an un-enrolled router's logs are not merely absent, they
  // arrive and are dropped.
  it('words an un-enrolled router’s forced-past line as refusal, not silence', () => {
    const enrolling = buildLedger(status(), [], '192.0.2.10', null, 'sftp', {
      steps: ROUTER_STEPS,
      device: 'edge-1',
      enrolling: true,
    })
    expect(enrolling[1].key).toBe('syslog')
    expect(notObserved(enrolling[1])).toBe('router not enrolled; its logs are refused until it is')
  })

  // A witness is a fleet-wide memory: some router once opened a syslog
  // connection. On a router ledger the Send logs step is about this
  // router's enrolment, which the first router's connection last month
  // says nothing about -- read from the witness, a second router would
  // show Send logs done before it had enrolled, and the wrong-sender
  // box (rendered only while the step waits) could never appear for it.
  it('does not let a fleet-wide syslog witness stand in for this router’s enrolment', () => {
    const witnessed = status({
      witnesses: [{ step: 2, receipt: 'syslog connected from 192.0.2.1', at: '2026-08-23T09:00:00Z' }],
    })
    const fleet = buildLedger(witnessed, [], 'h')
    expect(fleet[2].key).toBe('syslog')
    expect(fleet[2].outcome).toBe('done')
    expect(fleet[2].witnessed).toBe(true)

    const bare = device({ id: 'edge-2', name: 'edge-2', sourceIp: '', acceptedIp: '', eventCount: 0 })
    const router = buildLedger(witnessed, [bare], 'h', null, 'sftp', {
      steps: ROUTER_STEPS,
      device: 'edge-2',
      enrolling: true,
    })
    expect(router[1].key).toBe('syslog')
    expect(router[1].outcome).toBe('open')
    expect(router[1].flavour).toBe('waiting')
    expect(router[1].witnessed).toBe(false)
  })

  it('says the check could not run when the problem is on mikroview’s side', () => {
    const ledger = buildLedger(status(), [], '192.0.2.99:8080')
    expect(notObserved(ledger[0])).toContain('could not run')
  })
})

describe('the finish', () => {
  it('reads the ledger back, counting evidence separately from decisions', () => {
    const ledger = buildLedger(
      status({
        sources: [
          { source: '1.2.3.4', caFetchedAt: '2026-08-23T09:00:00Z', syslogFirstSeenAt: '2026-08-23T09:00:00Z' },
        ],
        devices: [{ device: 'r', configured: true, sourceIp: '1.2.3.4', events: 10, decodedActions: 10 }],
        marks: [mark(5, 'skipped')],
      }),
      [],
      '192.0.2.10',
    )
    const headline = finishHeadline(ledger)
    expect(headline).toContain('Logs are flowing.')
    expect(headline).toContain('Three steps stand on evidence')
    expect(headline).toContain('one was skipped')
    // #1166: the tally is a sentence of its own, so it starts like one
    // -- "Logs are flowing. three steps stand on evidence." did not.
    expect(headline).toBe('Logs are flowing. Three steps stand on evidence; one was skipped.')
  })

  it('does not claim anything is flowing when nothing has arrived', () => {
    expect(finishHeadline(buildLedger(status(), [], '192.0.2.10'))).toContain('Nothing has arrived')
  })
})

describe('explaining a silence elsewhere', () => {
  // "The record is the feature": a forced-past line surfaces wherever a
  // silence needs explaining. An empty surface with no decision behind
  // it is simply empty -- inventing a cause for it would be the
  // opposite of the point.
  it('says nothing when the ledger explains nothing', () => {
    expect(silenceExplanation([])).toBeNull()
  })

  it('names the step, the decision, who made it and what was not observed', () => {
    // Step 2 is Send logs in the server's own ledger, whichever
    // position Send logs is walked in.
    const line = silenceExplanation([mark(2, 'forced', { note: 'no router has opened a syslog connection' })])
    expect(line).toContain('step 2')
    expect(line).toContain('Send logs')
    expect(line).toContain('forced past')
    expect(line).toContain('tom')
    expect(line).toContain('no router has opened a syslog connection')
  })

  // Amber is loud and dashes are quiet -- when both exist, the loud one
  // is the one a silence is explained by.
  it('prefers a forced-past line over a skip', () => {
    const line = silenceExplanation([mark(1, 'skipped'), mark(4, 'forced')])
    expect(line).toContain('step 4')
  })
})

// --- Enrolment (#1281, with #1284) --------------------------------------

describe('the enrolment step', () => {
  const enrolled = (over: Partial<Device> = {}) =>
    device({ id: 'edge-1', name: 'edge-1', configured: true, ...over })

  it('waits for the enrol line at the end of the block', () => {
    const s = syslogStep(status(), [enrolled()], 'edge-1')
    expect(s.state).toBe('waiting')
    expect(s.detail).toContain('enrol line')
  })

  it('arrives when the line has been accepted, saying where from and when', () => {
    const s = syslogStep(
      status(),
      [enrolled({ acceptedIp: '192.168.88.1', enrolledAt: '2026-09-19T14:02:00Z' })],
      'edge-1',
    )
    expect(s.state).toBe('done')
    expect(s.detail).toContain('Enrolled at 192.168.88.1')
    expect(syslogReceipt(status(), [enrolled({ acceptedIp: '192.168.88.1', enrolledAt: '2026-09-19T14:02:00Z' })], 'edge-1'))
      .toContain('enrolled at 192.168.88.1')
  })

  // Re-enrol names the address it already has and says what is still
  // outstanding; the new line replaces it when it arrives.
  it('names the address it already has while a re-enrol waits for the new line', () => {
    const before = syslogStep(
      status(),
      [enrolled({ acceptedIp: '192.168.88.1', enrolledAt: '2026-09-19T14:02:00Z' })],
      'edge-1',
      '2026-09-19T15:00:00Z',
    )
    expect(before.state).toBe('waiting')
    expect(before.detail).toBe('Enrolled at 192.168.88.1 · waiting for the new line')

    const after = syslogStep(
      status(),
      [enrolled({ acceptedIp: '10.0.0.9', enrolledAt: '2026-09-19T15:01:00Z' })],
      'edge-1',
      '2026-09-19T15:00:00Z',
    )
    expect(after.state).toBe('done')
    expect(after.detail).toContain('Enrolled at 10.0.0.9')
  })

  // #1291: enrolledAt is the server's own time.Time, stamped in its
  // local zone; reEnrolSince is minted in the browser as UTC. A string
  // compare sorts "T17:31...+01:00" above "T16:45...Z" even though the
  // first is the earlier instant (16:31Z), so an old enrolment must not
  // read as the new one just because its offset digits are bigger.
  it('reads an old enrolment as still waiting even when its offset digits sort higher', () => {
    const s = syslogStep(
      status(),
      [enrolled({ acceptedIp: '192.168.88.1', enrolledAt: '2026-09-19T17:31:00+01:00' })],
      'edge-1',
      '2026-09-19T16:45:00Z',
    )
    expect(s.state).toBe('waiting')
    expect(s.detail).toBe('Enrolled at 192.168.88.1 · waiting for the new line')
  })

  // The lead says the model once, and only where an enrol line is
  // actually in the block below it.
  it('says what the enrol line does only when there is one', () => {
    const plain = buildLedger(status(), [], 'h')
    const enrolling = buildLedger(status(), [], 'h', null, 'sftp', { steps: ROUTER_STEPS, enrolling: true })
    expect(plain[2].lead).not.toContain('enrolment token')
    expect(enrolling[1].lead).toContain('the only address MikroView accepts this router’s logs from')
  })

  it('states how long the token is good for, and says plainly when it has lapsed', () => {
    const now = new Date('2026-09-19T14:02:00Z')
    expect(tokenLine('2026-09-19T14:17:00Z', now)).toContain('(15 minutes)')
    expect(tokenExpired('2026-09-19T14:17:00Z', now)).toBe(false)
    expect(tokenExpired('2026-09-19T14:01:00Z', now)).toBe(true)
  })

  // The warning box never claims the address is this router -- only the
  // operator knows that.
  it('words refused lines without diagnosing whose they are', () => {
    const line = refusedWarning([
      { ip: '192.168.88.1', firstSeen: '2026-09-19T14:03:00Z', lastSeen: '2026-09-19T14:04:00Z', lines: 12 },
    ])
    expect(line).toBe(
      'Lines from 192.168.88.1 arrived without the enrol line and were refused — if that is this ' +
        'router, paste the whole block, last line included.',
    )
    expect(refusedWarning([])).toBe('')
  })

  // An address refused last week is the fleet strip's business, not
  // evidence about the block just pasted.
  it('keeps only what was first seen after this walk minted its token', () => {
    const old = { ip: '10.0.0.1', firstSeen: '2026-09-18T09:00:00Z', lastSeen: '2026-09-18T09:00:00Z', lines: 3 }
    const fresh = { ip: '10.0.0.2', firstSeen: '2026-09-19T14:03:00Z', lastSeen: '2026-09-19T14:04:00Z', lines: 4 }
    expect(refusedSince([old, fresh], '2026-09-19T14:02:00Z')).toEqual([fresh])
    expect(refusedSince([old, fresh], '')).toEqual([])
  })

  // The server stamps firstSeen in its own zone (an offset, not Z) and
  // the browser mints in UTC; compared as strings, a New York server's
  // "13:03-04:00" reads as before a "14:02Z" mint it actually followed.
  it('compares instants, not strings, when the server writes an offset', () => {
    const offset = { ip: '10.0.0.3', firstSeen: '2026-09-19T13:03:00-04:00', lastSeen: '2026-09-19T13:04:00-04:00', lines: 1 }
    expect(refusedSince([offset], '2026-09-19T14:02:00Z')).toEqual([offset])
    expect(refusedSince([offset], '2026-09-19T17:04:00Z')).toEqual([])
  })
})

// The stored numbers are the server's, and STEP_TITLES is indexed by
// them -- a walking order that moves must not retitle a mark that was
// written before it moved.
describe('the recorded step numbers', () => {
  it('names each stored step number with the step the server meant', () => {
    const named = (n: number) => silenceExplanation([mark(n, 'forced')]) ?? ''
    expect(named(1)).toContain('Trust the certificate')
    expect(named(2)).toContain('Send logs')
    expect(named(3)).toContain('Tag firewall rules')
    expect(named(4)).toContain('Push router state')
    expect(named(5)).toContain('Name your router')
    expect(named(6)).toContain('Back up the router')
  })

  it('records every step under the number the server keeps, not its row', () => {
    const setup = buildLedger(status(), [], '192.0.2.10')
    expect(setup.map((s) => `${s.key}:${s.n}:${s.canonical}`)).toEqual([
      'ca:1:1',
      'name:2:5',
      'syslog:3:2',
      'rules:4:3',
      'push:5:4',
      'backup:6:6',
      'register:7:7',
    ])
  })
})
