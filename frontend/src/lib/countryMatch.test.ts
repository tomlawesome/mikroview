// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { matchesCountry, UNKNOWN_COUNTRY } from './countryMatch'

describe('matchesCountry', () => {
  it('an empty filter matches everything', () => {
    expect(matchesCountry(true, 'US', '')).toBe(true)
    expect(matchesCountry(false, undefined, '')).toBe(true)
  })

  it('matches a resolved country case-insensitively', () => {
    expect(matchesCountry(true, 'US', 'US')).toBe(true)
    expect(matchesCountry(true, 'us', 'US')).toBe(true)
    expect(matchesCountry(true, 'US', 'us')).toBe(true)
    expect(matchesCountry(true, 'US', 'GB')).toBe(false)
  })

  it('the Unknown sentinel matches an address whose country was not determined', () => {
    expect(matchesCountry(true, undefined, UNKNOWN_COUNTRY)).toBe(true)
    expect(matchesCountry(true, '', UNKNOWN_COUNTRY)).toBe(true)
  })

  it('the Unknown sentinel does not match a row with no address on this side at all', () => {
    // Nothing to call "undetermined" -- there's no address here for a
    // country lookup to have applied to in the first place.
    expect(matchesCountry(false, undefined, UNKNOWN_COUNTRY)).toBe(false)
  })

  it('the Unknown sentinel does not match a resolved country', () => {
    expect(matchesCountry(true, 'US', UNKNOWN_COUNTRY)).toBe(false)
  })
})
