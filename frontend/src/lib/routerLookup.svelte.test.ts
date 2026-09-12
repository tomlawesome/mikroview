// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { natChip, natTitle } from './routerLookup.svelte'

// #1167: the lookup header read "NAT rule — logged" with a chip beside
// it already reading "logged". One of the two had to go, and the chip is
// the one both surfaces (the sheet and the desktop popover) draw from.
describe('the NAT lookup header (#1167)', () => {
  it('names the router, in either mode, and never the mode', () => {
    expect(natTitle('rb5009', 'logged')).toBe('NAT rule — rb5009')
    expect(natTitle('rb5009', 'not-logged')).toBe('NAT table — rb5009')
  })

  it('leaves the mode to the chip, which is the only place it is said', () => {
    expect(natChip('logged')).toBe('logged')
    expect(natChip('not-logged')).toBe('not logged')
    expect(natTitle('rb5009', 'logged')).not.toContain(natChip('logged'))
  })
})
