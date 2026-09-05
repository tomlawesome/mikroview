// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
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
  nameStep,
  notObserved,
  portOf,
  pushStep,
  rulesStep,
  silenceExplanation,
  sourceSplitObservation,
  sourceSplitReceipt,
  sourceSplits,
  srcAddressCommand,
  syslogReceipt,
  syslogStep,
} from './setupsteps'
import type { Device, RouterBackupsResponse, SetupMark, SetupStatus } from './types'

function backups(over: Partial<RouterBackupsResponse> = {}): RouterBackupsResponse {
  return { enabled: true, routers: [], totalGenerations: 0, totalRouters: 0, totalBytes: 0, ...over }
}

function status(over: Partial<SetupStatus> = {}): SetupStatus {
  return {
    instance: { tlsEnabled: true, hosts: ['192.0.2.10'], syslogPort: ':6514', syslogEnabled: true },
    sources: [],
    devices: [],
    pushKinds: ['filter-rule', 'address-list', 'dhcp-lease', 'arp'],
    marks: [],
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
    const s = status({ instance: { tlsEnabled: true, hosts: [], syslogPort: ':6514', syslogEnabled: true } })
    expect(certificateCovers(s, '127.0.0.1:8080')).toBe(true)
    expect(certificateCovers(s, '192.168.1.5:8080')).toBe(false)
  })

  it('is not a question when both HTTP TLS and syslog are off', () => {
    const s = status({ instance: { tlsEnabled: false, hosts: [], syslogPort: ':6514', syslogEnabled: false } })
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
    const s = status({ instance: { tlsEnabled: false, hosts: [], syslogPort: ':6514', syslogEnabled: true } })
    expect(certificateCovers(s, '192.168.11.30:18084')).toBe(false)
  })

  it('accepts a covered host when HTTP TLS is off but syslog TLS is on', () => {
    const s = status({
      instance: { tlsEnabled: false, hosts: ['192.168.11.30'], syslogPort: ':6514', syslogEnabled: true },
    })
    expect(certificateCovers(s, '192.168.11.30:18084')).toBe(true)
  })
})

describe('step status', () => {
  it('blocks the CA step when the certificate cannot cover the address', () => {
    const s = caStep(status(), '192.0.2.99:8080')
    expect(s.state).toBe('blocked')
    expect(s.detail).toContain('tls.hosts')
  })

  it('reports the CA step done once something has fetched it', () => {
    const s = caStep(status({ sources: [{ source: '192.0.2.1', caFetchedAt: '2026-08-13T00:00:00Z' }] }), '192.0.2.10')
    expect(s.state).toBe('done')
  })

  it('blocks the syslog step when the listener is switched off', () => {
    const s = syslogStep(status({ instance: { tlsEnabled: true, hosts: [], syslogPort: '', syslogEnabled: false } }))
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
    expect(s.detail).toContain('log-prefix')
  })

  it('reports partial tagging honestly', () => {
    const s = rulesStep(
      status({ devices: [{ device: 'r', configured: true, sourceIp: '1.2.3.4', events: 10, decodedActions: 4 }] }),
    )
    expect(s.state).toBe('partial')
    expect(s.detail).toContain('4 of 10')
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
    expect(s.detail).toContain('address-list')
    expect(s.detail).toContain('dhcp-lease')
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

  it('reads as partial, in the voice of evidence composed wrongly, never blocked', () => {
    const s = syslogStep(connected, [declared, arriving])
    expect(s.state).toBe('partial')
    expect(s.detail).toBe(
      "Connected — but from 10.0.20.1, an address you haven't declared, while 192.168.88.1, " +
        'which you declared in config.yaml, has sent nothing.',
    )
  })

  it('carries the split into the step list receipt', () => {
    expect(syslogReceipt(connected, [declared, arriving])).toBe('syslog from 10.0.20.1 · declared 192.168.88.1 silent')
    const ledger = buildLedger(connected, [declared, arriving], 'h')
    expect(ledger[1].status.state).toBe('partial')
    expect(ledger[1].outcome).toBe('done')
    expect(ledger[1].receipt).toBe('syslog from 10.0.20.1 · declared 192.168.88.1 silent')
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
      "Connected — but from 10.0.20.1 and 10.0.30.1, addresses you haven't declared, while " +
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
  // The count of six is stable whatever the state: the record is
  // explicit that step 5's row always exists, marked "nothing to name"
  // until a push surfaces an unnamed device, and step 6 (#394) is the
  // same kind of always-there row. A ledger that grew and shrank would
  // be a different promise every time it was opened.
  it('always has exactly six steps', () => {
    expect(buildLedger(status(), [], 'h').length).toBe(6)
    expect(buildLedger(status({ sources: [{ source: '1.2.3.4', caFetchedAt: '2026-08-23T09:00:00Z' }] }), [device()], 'h').length).toBe(6)
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
    expect(forcedOnly[1].outcome).toBe('forced')

    const thenArrived = buildLedger(
      status({
        marks: [mark(2, 'forced')],
        sources: [{ source: '192.0.2.1', syslogFirstSeenAt: '2026-08-23T09:05:00Z' }],
      }),
      [],
      '192.0.2.10',
    )
    expect(thenArrived[1].outcome).toBe('done')
    expect(thenArrived[1].receipt).toContain('syslog connected')
  })

  // Step 3 counts and can only count upward, and step 5 has nothing to
  // wait for -- Next is always free on both, so neither can raise the
  // heavy warning. Step 6 does have a waiting check, the same shape as
  // step 4's.
  it('marks only the steps with a waiting check as checkable', () => {
    const ledger = buildLedger(status(), [], '192.0.2.10')
    expect(ledger.map((s) => s.hasCheck)).toEqual([true, true, false, true, false, true])
  })

  it('reads a partially tagged rule set as counting, not as half-failed', () => {
    const ledger = buildLedger(
      status({ devices: [{ device: 'r', configured: true, sourceIp: '1.2.3.4', events: 10, decodedActions: 4 }] }),
      [],
      '192.0.2.10',
    )
    expect(ledger[2].flavour).toBe('counting')
    expect(ledger[2].outcome).toBe('done')
    expect(ledger[2].receipt).toContain('4 of 10')
  })

  // The mikroview-side check logic this design inherits (#371/#374) is
  // not one of the four observation flavours: nothing is being waited
  // for, because nothing router-side can work yet.
  it('never dresses a mikroview-side problem up as patient waiting', () => {
    const ledger = buildLedger(status(), [], '192.0.2.99:8080')
    expect(ledger[0].status.state).toBe('blocked')
    expect(ledger[0].flavour).toBe('attention')
  })

  it('says there is nothing to name until a push surfaces an unnamed device', () => {
    expect(nameStep([]).detail).toContain('Nothing to name')
    expect(nameStep([device({ configured: true })]).detail).toContain('Nothing to name')
    const undeclared = nameStep([device({ configured: false, sourceIp: '192.0.2.44' })])
    expect(undeclared.state).toBe('quiet')
    expect(undeclared.detail).toContain('192.0.2.44')
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
    expect(s.detail).toContain('none is mounted')
  })

  it('waits once a key is mounted but nothing has pushed yet', () => {
    const s = backupStep(backups({ routers: [] }))
    expect(s.state).toBe('waiting')
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
        marks: [mark(2, 'skipped')],
      }),
      [],
      '192.0.2.10',
    )
    // 1 has evidence, 2 was decided -- 3 is the first still waiting.
    expect(firstOpenStep(ledger)).toBe(3)
  })

  it('falls back to the first step when nothing is left open', () => {
    const ledger = buildLedger(
      status({ marks: [1, 2, 3, 4, 5, 6].map((n) => mark(n, 'skipped')) }),
      [],
      '192.0.2.10',
    )
    expect(firstOpenStep(ledger)).toBe(1)
  })
})

describe('the forced-past record', () => {
  // The amber button quotes the exact record it will write, before it is
  // pressed. The record is the feature, so it is never a surprise
  // produced after the fact.
  it('quotes step, what was not observed, who and when', () => {
    const ledger = buildLedger(status(), [], '192.0.2.10')
    const line = forcedPastRecord(ledger[1], 'tom', new Date('2026-08-23T09:00:00Z'))
    expect(line).toContain('setup · step 2 forced past')
    expect(line).toContain('no router has opened a syslog connection')
    expect(line).toContain('tom')
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
        marks: [mark(4, 'skipped')],
      }),
      [],
      '192.0.2.10',
    )
    const headline = finishHeadline(ledger)
    expect(headline).toContain('Logs are flowing.')
    expect(headline).toContain('three steps stand on evidence')
    expect(headline).toContain('one was skipped')
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
