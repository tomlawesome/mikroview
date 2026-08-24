// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { spokenLabel } from './rail.svelte'

// The rail's count badge and #546's broken ring are independent per the
// design record -- a row could in principle carry both -- so this is the
// one place their composition is decided, rather than each marker
// deciding it separately and one silently winning.
describe('spokenLabel', () => {
  it('returns the bare label when nothing applies', () => {
    expect(spokenLabel('Flags', [])).toBe('Flags')
  })

  it('appends a single reason with an em dash', () => {
    expect(spokenLabel('Flags', ['6 open'])).toBe('Flags — 6 open')
  })

  it("matches the ratified ring wording for Watchlist's own reason", () => {
    expect(
      spokenLabel('Watchlist', ["3 watches can't be checked: the firewall rules they need aren't being logged"]),
    ).toBe("Watchlist — 3 watches can't be checked: the firewall rules they need aren't being logged")
  })

  it('joins two reasons with a semicolon if a row ever carries both a count and a ring', () => {
    expect(spokenLabel('Watchlist', ['6 open', "1 watch can't be checked: it needs logging"])).toBe(
      "Watchlist — 6 open; 1 watch can't be checked: it needs logging",
    )
  })
})
