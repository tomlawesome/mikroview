// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { natChip, natTitle, routerLookupState } from './routerLookup.svelte'
import { appState } from './state.svelte'

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

// #1195: the same router read "NAT rule — live-router" in the desktop
// popover and "NAT rule — Live Router" in the phone sheet, because the
// popover was handed the raw id it was opened with and the sheet the
// configured display name. One name, resolved in the store, so neither
// surface can resolve it differently again.
describe('the router the lookup names (#1195)', () => {
  it('resolves the configured display name for the device it was opened with', () => {
    const devices = appState.devices
    try {
      appState.devices = [
        { id: 'live-router', name: 'Live Router' },
        { id: 'rb5009', name: 'Edge' },
      ] as typeof appState.devices
      routerLookupState.device = 'live-router'
      expect(routerLookupState.deviceName).toBe('Live Router')
      expect(natTitle(routerLookupState.deviceName, 'logged')).toBe('NAT rule — Live Router')

      routerLookupState.device = 'rb5009'
      expect(routerLookupState.deviceName).toBe('Edge')
    } finally {
      appState.devices = devices
      routerLookupState.device = ''
    }
  })

  it('falls back to the id for a device that has declared no name', () => {
    const devices = appState.devices
    try {
      appState.devices = [] as typeof appState.devices
      routerLookupState.device = '10.0.0.1'
      expect(routerLookupState.deviceName).toBe('10.0.0.1')
    } finally {
      appState.devices = devices
      routerLookupState.device = ''
    }
  })
})
