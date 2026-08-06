import { describe, expect, it } from 'vitest'
import { discoverHosts, discoverPorts, discoverRules } from './discoveredEntities'
import type { ClientEvent, Entity, RuleUsage } from './types'

function ev(overrides: Partial<ClientEvent>): ClientEvent {
  return {
    id: 1,
    time: '2026-01-01T00:00:00.000Z',
    deviceId: 'core',
    sourceIp: '192.168.1.1',
    action: 'accept',
    ruleLabel: 'r13',
    chain: 'forward',
    raw: '',
    receivedAt: 0,
    ...overrides,
  }
}

describe('discoverHosts', () => {
  it('collects distinct srcIp/dstIp not already named', () => {
    const events = [
      ev({ srcIp: '10.0.0.1', dstIp: '10.0.0.2', time: '2026-01-01T00:00:00.000Z' }),
      ev({ srcIp: '10.0.0.1', dstIp: '10.0.0.3', time: '2026-01-01T00:00:01.000Z' }),
    ]
    const got = discoverHosts(events, [])
    expect(got.map((g) => g.key).sort()).toEqual(['10.0.0.1', '10.0.0.2', '10.0.0.3'])
  })

  it('excludes IPs that already have a host entity', () => {
    const events = [ev({ srcIp: '10.0.0.1', dstIp: '10.0.0.2' })]
    const entities: Entity[] = [{ type: 'host', key: '10.0.0.1', label: 'NAS' }]
    const got = discoverHosts(events, entities)
    expect(got.map((g) => g.key)).toEqual(['10.0.0.2'])
  })

  it('is not confused by an entity of a different type with the same key', () => {
    const events = [ev({ srcIp: '10.0.0.1' })]
    const entities: Entity[] = [{ type: 'rule', key: '10.0.0.1', label: 'coincidence' }]
    const got = discoverHosts(events, entities)
    expect(got.map((g) => g.key)).toEqual(['10.0.0.1'])
  })

  it('sorts newest-seen first', () => {
    const events = [
      ev({ srcIp: '10.0.0.1', time: '2026-01-01T00:00:00.000Z' }),
      ev({ srcIp: '10.0.0.2', time: '2026-01-01T00:05:00.000Z' }),
    ]
    const got = discoverHosts(events, [])
    expect(got.map((g) => g.key)).toEqual(['10.0.0.2', '10.0.0.1'])
  })

  it('ignores events with no srcIp/dstIp', () => {
    const got = discoverHosts([ev({ srcIp: undefined, dstIp: undefined })], [])
    expect(got).toEqual([])
  })
})

describe('discoverPorts', () => {
  it('collects distinct srcPort/dstPort not already named', () => {
    const events = [ev({ srcPort: 443, dstPort: 8291 })]
    const got = discoverPorts(events, [])
    expect(got.map((g) => g.key).sort()).toEqual(['443', '8291'])
  })

  it('excludes port 0 (absent) as a candidate', () => {
    const got = discoverPorts([ev({ srcPort: 0, dstPort: 0 })], [])
    expect(got).toEqual([])
  })

  it('excludes ports that already have a port entity', () => {
    const events = [ev({ srcPort: 8291 })]
    const entities: Entity[] = [{ type: 'port', key: '8291', label: 'Winbox' }]
    expect(discoverPorts(events, entities)).toEqual([])
  })
})

describe('discoverRules', () => {
  it('filters out rules that already have a rule entity', () => {
    const usage: RuleUsage[] = [
      { rule: 'r13', firstSeen: '', lastSeen: '2026-01-01T00:00:00.000Z', count: 5 },
      { rule: 'r99', firstSeen: '', lastSeen: '2026-01-01T00:01:00.000Z', count: 1 },
    ]
    const entities: Entity[] = [{ type: 'rule', key: 'r13', label: 'Block scanners' }]
    const got = discoverRules(usage, entities)
    expect(got).toEqual([{ key: 'r99', lastSeen: '2026-01-01T00:01:00.000Z' }])
  })

  it('sorts newest-fired first', () => {
    const usage: RuleUsage[] = [
      { rule: 'old', firstSeen: '', lastSeen: '2026-01-01T00:00:00.000Z', count: 1 },
      { rule: 'new', firstSeen: '', lastSeen: '2026-01-02T00:00:00.000Z', count: 1 },
    ]
    const got = discoverRules(usage, [])
    expect(got.map((g) => g.key)).toEqual(['new', 'old'])
  })

  it('skips a blank rule label defensively', () => {
    const usage: RuleUsage[] = [{ rule: '', firstSeen: '', lastSeen: '2026-01-01T00:00:00.000Z', count: 1 }]
    expect(discoverRules(usage, [])).toEqual([])
  })
})
