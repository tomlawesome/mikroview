// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { headlineFor, storyFor } from './flagNarrative'
import type { Flag } from './types'

function baseFlag(overrides: Partial<Flag> = {}): Flag {
  return {
    id: 'f1',
    type: 'port_scan',
    target: '198.51.100.77',
    detail: 'd',
    count: 1,
    firstSeen: '2026-01-01T13:41:02Z',
    lastSeen: '2026-01-01T13:41:42Z',
    cleared: false,
    ...overrides,
  }
}

// Every headline/story must be non-empty, one-or-more sentences, and
// must never repeat the raw target string verbatim -- the where column
// already shows it, and the ratified voice ("One source, twenty doors.")
// never does either.
function expectWellFormedHeadline(headline: string, target: string) {
  expect(headline.length).toBeGreaterThan(0)
  expect(headline).not.toContain(target)
}

describe('headlineFor / storyFor per flag type (#678)', () => {
  it('port_scan: names the door count, not the raw port list', () => {
    const f = baseFlag({
      type: 'port_scan',
      target: '198.51.100.77',
      evidence: { ports: Array.from({ length: 20 }, (_, i) => 1000 + i) },
    })
    expect(headlineFor(f)).toBe('One source, twenty doors.')
    expectWellFormedHeadline(headlineFor(f), f.target)
    expect(storyFor(f)).toContain('198.51.100.77')
    expect(storyFor(f)).toContain('20 ports')
    expect(storyFor(f)).toContain('(1000–1019)')
  })

  it('port_scan falls back to count when no evidence.ports is carried', () => {
    const f = baseFlag({ type: 'port_scan', count: 7, evidence: undefined })
    expect(storyFor(f)).toContain('7 ports')
  })

  it('critical_port: names the specific ports touched', () => {
    const f = baseFlag({
      type: 'critical_port',
      target: '203.0.113.9',
      count: 3,
      evidence: { ports: [22, 3389] },
    })
    expect(headlineFor(f)).toBe('Knocking on doors that matter.')
    expect(storyFor(f)).toContain('3 attempts')
    expect(storyFor(f)).toContain('ports 22 and 3389')
  })

  it('activity_spike: cites the confidence score only when the flag carries one', () => {
    const withConfidence = baseFlag({ type: 'activity_spike', count: 500, confidence: 82 })
    expect(storyFor(withConfidence)).toContain('82%')

    const without = baseFlag({ type: 'activity_spike', count: 500, confidence: undefined })
    expect(storyFor(without)).not.toContain('%')
  })

  it('global_spike: network-wide, no single source named', () => {
    const f = baseFlag({ type: 'global_spike', target: 'global', count: 40 })
    expect(headlineFor(f)).toBe('Busier than usual, across the board.')
    expect(storyFor(f)).toMatch(/whole network/)
    expect(storyFor(f)).toContain('40 times')
  })

  it('distributed_brute_force: names the source count and the shared target', () => {
    const f = baseFlag({
      type: 'distributed_brute_force',
      target: 'port 22',
      count: 15,
      evidence: { hosts: ['203.0.113.1', '203.0.113.2', '203.0.113.3'] },
    })
    expect(headlineFor(f)).toBe('Many sources, the same lock.')
    expect(storyFor(f)).toContain('Three distinct sources')
    expect(storyFor(f)).toContain('port 22')
    expect(storyFor(f)).toContain('15 attempts')
  })

  it('outbound_anomaly: names the destination breadth', () => {
    const f = baseFlag({
      type: 'outbound_anomaly',
      target: '10.0.20.14',
      evidence: { hosts: ['198.51.100.1', '198.51.100.2'] },
    })
    expect(headlineFor(f)).toBe("Reaching a lot of places it hasn't before.")
    expect(storyFor(f)).toContain('two distinct destinations')
  })

  it('internal_recon: names the internal host breadth', () => {
    const f = baseFlag({
      type: 'internal_recon',
      target: '10.0.10.5',
      evidence: { hosts: ['10.0.10.1', '10.0.10.2', '10.0.10.3'] },
    })
    expect(headlineFor(f)).toBe('Checking what else is on the network.')
    expect(storyFor(f)).toContain('three other hosts')
  })

  it('rule_spike: names the rule and its fire count', () => {
    const f = baseFlag({ type: 'rule_spike', target: 'wan-in-drop', count: 200 })
    expect(headlineFor(f)).toBe('One rule doing a lot more work than usual.')
    expect(storyFor(f)).toContain('Rule wan-in-drop')
    expect(storyFor(f)).toContain('200 times')
  })

  it('repeated_drops: the machine-regular cadence, refused every time', () => {
    const f = baseFlag({
      type: 'repeated_drops',
      target: '10.0.20.14 -> port 445',
      count: 9,
      firstSeen: '2026-01-01T13:28:00Z',
      lastSeen: '2026-01-01T13:44:00Z', // 8 gaps of 2 minutes across 9 attempts
    })
    expect(headlineFor(f)).toBe('Same ask, same refusal, still asking.')
    expect(storyFor(f)).toContain('Nine identical attempts')
    expect(storyFor(f)).toContain('machine-regular, not a person retrying')
    expect(storyFor(f)).toContain('refused')
  })

  it('low_slow_scan: the pacing, not a burst', () => {
    const f = baseFlag({ type: 'low_slow_scan', target: '203.0.113.50', count: 40 })
    expect(headlineFor(f)).toBe('A scan paced to stay under the radar.')
    expect(storyFor(f)).toContain('40 attempts')
  })

  it('off_hours_activity: a clock window with no history', () => {
    const f = baseFlag({ type: 'off_hours_activity', target: '10.0.10.31', count: 12 })
    expect(headlineFor(f)).toBe('Awake at a time it has no history of.')
    expect(storyFor(f)).toContain('12 events')
    expect(storyFor(f)).toContain('no established history')
  })

  it('device_silence: names when it last checked in', () => {
    const f = baseFlag({ type: 'device_silence', target: 'cam-porch', lastSeen: '2026-01-01T02:12:44Z' })
    expect(headlineFor(f)).toBe('Gone quiet.')
    expect(storyFor(f)).toContain('cam-porch')
    expect(storyFor(f)).toMatch(/since \d{2}:\d{2}/)
  })

  it('new_device: never seen before', () => {
    const f = baseFlag({ type: 'new_device', target: 'AA:BB:CC:DD:EE:FF' })
    expect(headlineFor(f)).toBe('A device mikroview has never seen before.')
    expect(storyFor(f)).toContain('AA:BB:CC:DD:EE:FF')
    expect(storyFor(f)).toContain('never been seen')
  })

  it('stale_rule: the rule stopped mattering', () => {
    const f = baseFlag({ type: 'stale_rule', target: 'guest-egress' })
    expect(headlineFor(f)).toBe('A rule that stopped mattering.')
    expect(storyFor(f)).toContain('Rule guest-egress')
  })

  it('unexpected_mail_sender: a device sending mail with no history of it', () => {
    const f = baseFlag({ type: 'unexpected_mail_sender', target: '10.0.20.14', count: 3 })
    expect(headlineFor(f)).toBe("Something that shouldn't send mail, sending mail.")
    expect(storyFor(f)).toContain('3 outbound connections')
    expect(storyFor(f)).toContain('SMTP')
  })

  it('known_bad_ip: leans on the flag\'s own detail for which list matched', () => {
    const f = baseFlag({
      type: 'known_bad_ip',
      target: '203.0.113.66',
      detail: 'Matches Spamhaus DROP (203.0.113.0/24)',
    })
    expect(headlineFor(f)).toBe('A source already known to be bad.')
    expect(storyFor(f)).toContain('matches Spamhaus DROP')
    expect(storyFor(f)).toContain('not a judgment based on anything seen here')
  })

  it('an operator-authored custom detection type falls back to its own detail rather than crashing', () => {
    const f = baseFlag({ type: 'live-custom-detection watch' as Flag['type'], detail: 'a custom pattern fired' })
    expect(headlineFor(f)).toBe('a custom pattern fired')
    expect(() => storyFor(f)).not.toThrow()
    expect(storyFor(f)).toContain(f.target)
  })
})

// Every one of the sixteen shipped types must produce non-empty prose --
// a silent gap here would show as a blank drawer, not a crash, so this
// exists to catch that directly rather than relying on it turning up in
// a component test.
describe('every shipped flag type produces a headline and a story', () => {
  const types: Flag['type'][] = [
    'port_scan',
    'activity_spike',
    'critical_port',
    'global_spike',
    'distributed_brute_force',
    'outbound_anomaly',
    'internal_recon',
    'rule_spike',
    'repeated_drops',
    'low_slow_scan',
    'off_hours_activity',
    'device_silence',
    'new_device',
    'stale_rule',
    'unexpected_mail_sender',
    'known_bad_ip',
  ]

  it.each(types)('%s', (type) => {
    const f = baseFlag({ type })
    expect(headlineFor(f).length).toBeGreaterThan(0)
    expect(storyFor(f).length).toBeGreaterThan(0)
  })
})
