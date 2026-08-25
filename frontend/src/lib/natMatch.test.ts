// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import type { RouterNatRule } from './api'
import {
  addressSpecMatches,
  nameMatches,
  partitionNatTable,
  portSpecMatches,
  type NatEventFacts,
} from './natMatch'

function rule(over: Partial<RouterNatRule> = {}): RouterNatRule {
  return { ordinal: 0, comment: '', chain: 'srcnat', action: 'masquerade', ...over }
}

// A srcnat translation, of the shape internal/routeros/parser.go
// produces: pre-translation addresses in src/dst, the translated port
// out of the NAT annotation, RouterOS's upper-case protocol.
const event: NatEventFacts = {
  chain: 'srcnat',
  protocol: 'TCP',
  srcIp: '192.168.88.20',
  dstIp: '198.51.100.53',
  dstPort: 443,
  natPort: 51258,
  inInterface: 'bridge1',
  outInterface: 'ether1',
}

describe('portSpecMatches', () => {
  it('reads single ports, lists and ranges', () => {
    expect(portSpecMatches('443', 443)).toBe(true)
    expect(portSpecMatches('443', 80)).toBe(false)
    expect(portSpecMatches('80,443', 443)).toBe(true)
    expect(portSpecMatches('1000-2000', 1500)).toBe(true)
    expect(portSpecMatches('1000-2000', 2001)).toBe(false)
  })

  it('inverts a negated spec rather than giving up on it', () => {
    expect(portSpecMatches('!443', 443)).toBe(false)
    expect(portSpecMatches('!443', 80)).toBe(true)
  })

  it('answers "cannot tell" rather than "no match" for anything unrecognised', () => {
    expect(portSpecMatches('http', 80)).toBeNull()
    expect(portSpecMatches('-90', 80)).toBeNull()
    expect(portSpecMatches('443', undefined)).toBeNull()
    expect(portSpecMatches('', 443)).toBeNull()
  })
})

describe('addressSpecMatches', () => {
  it('reads literals, CIDRs and from-to ranges, v4 and v6', () => {
    expect(addressSpecMatches('192.168.88.20', '192.168.88.20')).toBe(true)
    expect(addressSpecMatches('192.168.88.0/24', '192.168.88.20')).toBe(true)
    expect(addressSpecMatches('192.168.88.0/24', '10.0.0.1')).toBe(false)
    expect(addressSpecMatches('192.168.88.10-192.168.88.30', '192.168.88.20')).toBe(true)
    expect(addressSpecMatches('192.168.88.10-192.168.88.30', '192.168.88.40')).toBe(false)
    expect(addressSpecMatches('2001:db8::/32', '2001:db8::5')).toBe(true)
  })

  it('cannot decide an address-list name, and says so instead of excluding', () => {
    expect(addressSpecMatches('wan-hosts', '192.168.88.20')).toBeNull()
    expect(addressSpecMatches('!wan-hosts', '192.168.88.20')).toBeNull()
    expect(addressSpecMatches('192.168.88.0/24', undefined)).toBeNull()
  })
})

describe('nameMatches', () => {
  it('compares case-insensitively, since RouterOS logs protocols upper-case', () => {
    expect(nameMatches('tcp', 'TCP')).toBe(true)
    expect(nameMatches('udp', 'TCP')).toBe(false)
  })

  it('has no answer when the event carries no value at all', () => {
    expect(nameMatches('tcp', '')).toBeNull()
  })
})

describe('partitionNatTable', () => {
  it('rules a rule out only on a positive contradiction, and names it', () => {
    const table = [
      rule({ ordinal: 0, protocol: 'udp' }),
      rule({ ordinal: 1, chain: 'dstnat' }),
      rule({ ordinal: 2, disabled: true, protocol: 'tcp' }),
      rule({ ordinal: 3, protocol: 'tcp', outInterface: 'ether1' }),
    ]
    const p = partitionNatTable(table, event)

    expect(p.total).toBe(4)
    expect(p.couldHave.map((v) => v.rule.ordinal)).toEqual([3])
    expect(p.ruledOut.map((v) => v.ruledOut)).toEqual([
      'protocol udp ≠ tcp',
      'chain dstnat, event is srcnat',
      'disabled',
    ])
  })

  it('keeps a rule it cannot evaluate, and reports the condition it could not decide', () => {
    const p = partitionNatTable([rule({ srcAddress: 'wan-hosts' })], event)
    expect(p.couldHave).toHaveLength(1)
    expect(p.couldHave[0].notEvaluable).toEqual(['src-address=wan-hosts'])
    expect(p.ruledOut).toHaveLength(0)
  })

  it('does not read RouterOS’s "(unknown 0)" interface placeholder as an interface name', () => {
    const noIn: NatEventFacts = { ...event, inInterface: '(unknown 0)' }
    const p = partitionNatTable([rule({ inInterface: 'ether1' })], noIn)
    expect(p.ruledOut).toHaveLength(0)
    expect(p.couldHave[0].notEvaluable).toEqual(['in-interface=ether1'])
  })

  it('will not exclude on chain when the event is not itself on a NAT chain', () => {
    // A forward-chain line carrying a NAT annotation reports a
    // translation some earlier NAT rule performed; its chain says
    // nothing about which NAT chain that rule sat in.
    const forwarded: NatEventFacts = { ...event, chain: 'forward' }
    const p = partitionNatTable([rule({ chain: 'dstnat', protocol: 'tcp' })], forwarded)
    expect(p.ruledOut).toHaveLength(0)
    expect(p.couldHave[0].notEvaluable).toContain('chain=dstnat')
  })

  it('preserves RouterOS order within each half', () => {
    const table = [
      rule({ ordinal: 0, protocol: 'tcp' }),
      rule({ ordinal: 1, protocol: 'udp' }),
      rule({ ordinal: 2, protocol: 'tcp' }),
      rule({ ordinal: 3, protocol: 'icmp' }),
    ]
    const p = partitionNatTable(table, event)
    expect(p.couldHave.map((v) => v.rule.ordinal)).toEqual([0, 2])
    expect(p.ruledOut.map((v) => v.rule.ordinal)).toEqual([1, 3])
  })

  it('rules out on the translated port a to-ports rule would have produced', () => {
    const p = partitionNatTable([rule({ chain: 'dstnat', toPorts: '8080' })], {
      chain: 'dstnat',
      natPort: 9090,
    })
    expect(p.ruledOut[0].ruledOut).toBe('to-ports 8080, translated port 9090')
  })

  it('reports a pre-schema push as undiscriminable and rules nothing out', () => {
    const table = [
      rule({ ordinal: 0, comment: 'masquerade out', chain: 'srcnat', action: 'masquerade' }),
      rule({ ordinal: 1, comment: 'port forward', chain: 'dstnat', action: 'dst-nat' }),
    ]
    const p = partitionNatTable(table, event)
    expect(p.discriminable).toBe(false)
    // Rule 1's chain contradicts the event, but the floor shows the
    // whole table rather than subtracting on the one field an old push
    // script still sent.
    expect(p.ruledOut).toHaveLength(0)
    expect(p.couldHave).toHaveLength(2)
  })

  it('is discriminable as soon as one rule carries one usable field', () => {
    const table = [rule({ ordinal: 0 }), rule({ ordinal: 1, protocol: 'udp' })]
    expect(partitionNatTable(table, event).discriminable).toBe(true)
  })
})
