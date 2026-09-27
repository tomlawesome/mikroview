// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { countryName, formatUntil, inUseLine, ownerLabel } from './geo'

describe('geo wording (#1352)', () => {
  it('names the source in use, or says there is none yet', () => {
    expect(inUseLine('dbip')).toBe('Flags from DB-IP Lite')
    expect(inUseLine('ipinfo')).toBe('Flags from IPinfo Lite')
    expect(inUseLine('maxmind')).toBe('Flags from MaxMind GeoLite2')
    expect(inUseLine(null)).toBe('No flag source loaded yet')
  })

  it('writes the owner as "AS<n> <name>", with whichever half is known, or null', () => {
    expect(ownerLabel({ country: 'US', asn: 13335, asName: 'Cloudflare, Inc.' })).toBe('AS13335 Cloudflare, Inc.')
    expect(ownerLabel({ country: 'US', asn: 13335, asName: null })).toBe('AS13335')
    expect(ownerLabel({ country: 'US', asn: null, asName: 'Cloudflare, Inc.' })).toBe('Cloudflare, Inc.')
    expect(ownerLabel({ country: 'US', asn: null, asName: null })).toBeNull()
    expect(ownerLabel(null)).toBeNull()
  })

  it('names a country from its code, falling back to the code', () => {
    expect(countryName('GB')).not.toBe('')
    expect(countryName(undefined)).toBe('')
  })

  it('says how long until a time, and "due" once it has passed', () => {
    const now = Date.parse('2026-09-27T12:00:00Z')
    expect(formatUntil('2026-09-27T12:00:30Z', now)).toBe('in 30s')
    expect(formatUntil('2026-09-27T12:10:00Z', now)).toBe('in 10m')
    expect(formatUntil('2026-09-27T15:00:00Z', now)).toBe('in 3h')
    expect(formatUntil('2026-10-03T12:00:00Z', now)).toBe('in 6d')
    expect(formatUntil('2026-09-27T11:00:00Z', now)).toBe('due')
  })
})
