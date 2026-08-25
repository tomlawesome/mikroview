// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { matchesAddressQuery, parseAddress, parseCidr } from './addressMatch'

// Table tests for the precedence rules #438 pins down: a full IP is an
// exact match, a CIDR is containment, anything else is a substring
// against the label and/or the raw address -- in that order, with no
// mode switch.
describe('matchesAddressQuery precedence', () => {
  const cases: {
    name: string
    query: string
    candidates: { ip?: string; hostName?: string }[]
    want: boolean
  }[] = [
    {
      name: 'exact IPv4 match',
      query: '203.0.113.5',
      candidates: [{ ip: '203.0.113.5' }],
      want: true,
    },
    {
      name: 'exact IPv4 non-match against a different host',
      query: '203.0.113.5',
      candidates: [{ ip: '203.0.113.50' }],
      want: false,
    },
    {
      name: 'a full IP does not fall back to substring against a similar-looking address',
      query: '203.0.113.5',
      // Would match as a substring, but the exact-IP rule takes over
      // entirely once the query itself parses as a whole address.
      candidates: [{ ip: '203.0.113.50' }, { hostName: '203.0.113.5-ish' }],
      want: false,
    },
    {
      name: 'IPv4 CIDR containment',
      query: '203.0.113.0/24',
      candidates: [{ ip: '203.0.113.200' }],
      want: true,
    },
    {
      name: 'IPv4 CIDR exclusion',
      query: '203.0.113.0/25',
      candidates: [{ ip: '203.0.113.200' }],
      want: false,
    },
    {
      name: '/0 matches every address of that family',
      query: '0.0.0.0/0',
      candidates: [{ ip: '8.8.8.8' }],
      want: true,
    },
    {
      name: 'exact IPv6 match',
      query: '2001:db8::1',
      candidates: [{ ip: '2001:0db8:0000:0000:0000:0000:0000:0001' }],
      want: true,
    },
    {
      name: 'IPv6 CIDR containment',
      query: '2001:db8::/32',
      candidates: [{ ip: '2001:db8:abcd::1' }],
      want: true,
    },
    {
      name: 'IPv6 CIDR exclusion',
      query: '2001:db8::/32',
      candidates: [{ ip: '2001:db9::1' }],
      want: false,
    },
    {
      name: 'an IPv4 query never matches an IPv6 candidate',
      query: '10.0.0.1',
      candidates: [{ ip: '::ffff:a00:1' }],
      want: false,
    },
    {
      name: 'substring match against the displayed label',
      query: 'nas',
      candidates: [{ ip: '203.0.113.5', hostName: 'nas-basement' }],
      want: true,
    },
    {
      name: 'substring match against the raw address (partial IP)',
      query: '203.0',
      candidates: [{ ip: '203.0.113.5', hostName: 'nas-basement' }],
      want: true,
    },
    {
      name: 'substring match is case-insensitive',
      query: 'NAS',
      candidates: [{ ip: '203.0.113.5', hostName: 'nas-basement' }],
      want: true,
    },
    {
      name: 'substring match fails when neither label nor address contains it',
      query: 'printer',
      candidates: [{ ip: '203.0.113.5', hostName: 'nas-basement' }],
      want: false,
    },
    {
      name: 'a malformed CIDR (prefix out of range) falls back to substring',
      query: '203.0.113.5/99',
      candidates: [{ hostName: '203.0.113.5/99 literally' }],
      want: true,
    },
    {
      name: 'a malformed CIDR still fails the substring bucket if it does not appear anywhere',
      query: '203.0.113.5/99',
      candidates: [{ ip: '203.0.113.5' }],
      want: false,
    },
    {
      name: 'an empty query matches everything',
      query: '',
      candidates: [],
      want: true,
    },
    {
      name: 'a whitespace-only query matches everything',
      query: '   ',
      candidates: [{ ip: '203.0.113.5' }],
      want: true,
    },
    {
      name: 'any candidate matching is enough (NAT counterpart alongside the real address)',
      query: '198.51.100.9',
      candidates: [{ ip: '10.0.0.5', hostName: 'workstation' }, { ip: '198.51.100.9' }],
      want: true,
    },
    {
      name: 'the NAT counterpart participates in substring matching too, with no label',
      query: '51.100',
      candidates: [{ ip: '10.0.0.5', hostName: 'workstation' }, { ip: '198.51.100.9' }],
      want: true,
    },
  ]

  for (const c of cases) {
    it(c.name, () => {
      expect(matchesAddressQuery(c.query, c.candidates)).toBe(c.want)
    })
  }
})

describe('parseAddress', () => {
  it('rejects an octet over 255', () => {
    expect(parseAddress('203.0.113.256')).toBeNull()
  })

  it('rejects a bare hostname', () => {
    expect(parseAddress('nas-basement')).toBeNull()
  })

  it('parses an IPv6 address with embedded IPv4', () => {
    const parsed = parseAddress('::ffff:192.0.2.1')
    expect(parsed).not.toBeNull()
    expect(parsed?.family).toBe(6)
  })
})

describe('parseCidr', () => {
  it('rejects a prefix longer than the family allows', () => {
    expect(parseCidr('203.0.113.0/33')).toBeNull()
    expect(parseCidr('2001:db8::/129')).toBeNull()
  })

  it('rejects an address with no slash', () => {
    expect(parseCidr('203.0.113.0')).toBeNull()
  })
})
