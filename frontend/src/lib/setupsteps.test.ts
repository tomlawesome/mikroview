// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
  buildLedger,
  caStep,
  caTrustCommands,
  certificateCovers,
  finishHeadline,
  firstOpenStep,
  forcedPastRecord,
  hostname,
  nameStep,
  notObserved,
  portOf,
  pushBlock,
  pushScript,
  pushStep,
  rulesStep,
  silenceExplanation,
  syslogCommands,
  syslogStep,
} from './setupsteps'
import type { Device, SetupMark, SetupStatus } from './types'

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

describe('generated commands', () => {
  // The wizard must never emit a placeholder: a saved script still
  // containing <mikroview-host> fails much later, somewhere else.
  const placeholders = /<[a-z-]+>/

  it('fills in the address for the CA fetch', () => {
    const cmd = caTrustCommands('192.0.2.10:8080')
    expect(cmd).toContain('https://192.0.2.10:8080/ca.crt')
    expect(cmd).not.toMatch(placeholders)
  })

  it('uses the configured syslog port, not an assumed one', () => {
    expect(syslogCommands('192.0.2.10:8080', ':16514')).toContain('remote-port=16514')
  })

  it('sends syslog to the host without the web port', () => {
    const cmd = syslogCommands('192.0.2.10:8080', ':6514')
    expect(cmd).toContain('remote=192.0.2.10')
    expect(cmd).not.toContain('remote=192.0.2.10:8080')
  })

  it('embeds the token in every push block', () => {
    const script = pushScript('192.0.2.10:8080', 'tok-123', ['filter-rule', 'arp'])
    expect(script.match(/Bearer tok-123/g)).toHaveLength(2)
    expect(script).not.toMatch(placeholders)
  })

  it('renames RouterOS fields to mikroview names', () => {
    const block = pushBlock('h', 't', 'filter-rule')
    expect(block).toContain('"logPrefix"=($v->"log-prefix")')
    expect(block).toContain('"srcAddressList"=($v->"src-address-list")')
    // #408's fields. connection-state is a set, passed through as the
    // array RouterOS sends rather than joined by the script.
    expect(block).toContain('"connectionState"=($v->"connection-state")')
    expect(block).toContain('"inInterface"=($v->"in-interface")')
    expect(block).toContain('"outInterface"=($v->"out-interface")')
    // The wrapping that makes it a list of records rather than one
    // merged map -- silently wrong without it.
    expect(block).toContain('{$rec}')
  })

  it('reports the router version on every block, on the payload not a record', () => {
    const script = pushScript('h', 't', ['filter-rule', 'arp'])
    expect(script.match(/"routerosVersion"=\[\/system\/resource get version\]/g)).toHaveLength(2)
    // On the envelope beside kind/page/pages -- never inside the
    // per-record map, which describes a rule and not the router.
    expect(script).not.toContain('"routerosVersion"=[/system/resource get version]; "comment"')
  })

  it('gives each block its own variables, since they share one script', () => {
    const script = pushScript('h', 't', ['filter-rule', 'arp'])
    expect(script).toContain('ruleRecs')
    expect(script).toContain('arpRecs')
  })

  it('emits nothing for a kind it does not know', () => {
    expect(pushBlock('h', 't', 'not-a-kind')).toBe('')
  })

  // #627: the pushed /ip/address table, same renaming contract as the
  // filter-rule case above.
  it('renames /ip/address fields to mikroview names', () => {
    const block = pushBlock('h', 't', 'ip-address')
    expect(block).toContain('/ip/address print as-value')
    expect(block).toContain('"address"=($v->"address")')
    expect(block).toContain('"network"=($v->"network")')
    expect(block).toContain('"interface"=($v->"interface")')
    expect(block).toContain('"comment"=($v->"comment")')
    expect(block).toContain('{$rec}')
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

describe('the claim ledger', () => {
  // The count of five is stable whatever the state: the record is
  // explicit that step 5's row always exists, marked "nothing to name"
  // until a push surfaces an unnamed device. A ledger that grew and
  // shrank would be a different promise every time it was opened.
  it('always has exactly five steps', () => {
    expect(buildLedger(status(), [], 'h').length).toBe(5)
    expect(buildLedger(status({ sources: [{ source: '1.2.3.4', caFetchedAt: '2026-08-23T09:00:00Z' }] }), [device()], 'h').length).toBe(5)
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
  // heavy warning.
  it('marks only the steps with a waiting check as checkable', () => {
    const ledger = buildLedger(status(), [], '192.0.2.10')
    expect(ledger.map((s) => s.hasCheck)).toEqual([true, true, false, true, false])
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
      status({ marks: [1, 2, 3, 4, 5].map((n) => mark(n, 'skipped')) }),
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
