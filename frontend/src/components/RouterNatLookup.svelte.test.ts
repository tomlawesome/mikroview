// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import RouterNatLookup from './RouterNatLookup.svelte'
import { routerLookupState as st } from '../lib/routerLookup.svelte'
import { appState } from '../lib/state.svelte'
import type { Device } from '../lib/types'

// #1195: the same router read one way in the desktop popover's header
// and another in the phone sheet's, because each surface resolved the
// name for itself. This body is the half both surfaces share -- it was
// printing the raw id on each of them -- so these pin the friendly name
// in the two states that name the router at all. The id is what the
// lookup fetches by and stays on st.device; it is not what is shown.
function device(overrides: Partial<Device> = {}): Device {
  return {
    id: 'live-router',
    name: 'Live Router',
    sourceIp: '10.0.0.1',
    configured: true,
    firstSeen: '2026-01-01T00:00:00Z',
    lastSeen: '2026-01-01T00:00:00Z',
    eventCount: 1,
    status: 'live',
    ...overrides,
  }
}

describe('the NAT lookup body names the router the operator knows (#1195)', () => {
  beforeEach(() => {
    appState.devices = [device()]
    st.device = 'live-router'
    st.loading = false
    st.error = null
    st.natRules = []
    st.natMatches = []
    st.natPartition = null
  })

  afterEach(() => {
    appState.devices = []
    st.device = ''
    st.available = false
  })

  it('says which router pushed nothing, by its configured name', () => {
    st.available = false
    render(RouterNatLookup)
    flushSync()

    expect(screen.getByText(/No NAT table pushed by “Live Router”/)).toBeTruthy()
    expect(document.body.textContent).not.toContain('live-router')
  })

  it('says which router pushed an empty table, by its configured name', () => {
    st.available = true
    st.natMode = 'not-logged'
    render(RouterNatLookup)
    flushSync()

    expect(screen.getByText(/“Live Router” has pushed its NAT table and it is empty/)).toBeTruthy()
    expect(document.body.textContent).not.toContain('live-router')
  })

  it('falls back to the id for a device that has declared no name', () => {
    appState.devices = []
    st.available = false
    render(RouterNatLookup)
    flushSync()

    expect(screen.getByText(/No NAT table pushed by “live-router”/)).toBeTruthy()
  })
})
