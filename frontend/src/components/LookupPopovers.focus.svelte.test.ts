// SPDX-License-Identifier: AGPL-3.0-only
//
// #1539 C3 (decision 19a): the three row-anchored lookup popovers are
// non-modal dialogs, so closing one returns focus to the control that
// opened it. A keyboard user who tabbed into the popover and pressed
// Escape used to land on <body>.
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', () => ({
  lookupIp: vi.fn(async () => ({ ip: '192.0.2.2', countryCode: 'US', isp: 'Example' })),
  geoLookup: vi.fn(async () => ({ country: 'US', asn: null, asName: null })),
  fetchRouterRules: vi.fn(async () => ({ available: true, rules: [] })),
}))

import { ipLookupState } from '../lib/ipLookup.svelte'
import { portLookupState } from '../lib/portLookup.svelte'
import { routerLookupState } from '../lib/routerLookup.svelte'
import IpLookupPopover from './IpLookupPopover.svelte'
import PortLookupPopover from './PortLookupPopover.svelte'
import RouterLookupPopover from './RouterLookupPopover.svelte'
import { focusedTrigger } from './dialogKit'

const RECT = new DOMRect(10, 10, 20, 20)

afterEach(() => {
  ipLookupState.close()
  portLookupState.close()
  routerLookupState.close()
  cleanup()
})

async function escapeFromInside(trigger: HTMLElement) {
  screen.getByRole('button', { name: 'Close' }).focus()
  expect(document.activeElement).not.toBe(trigger)
  await fireEvent.keyDown(document.body, { key: 'Escape' })
  flushSync()
}

describe('lookup popovers return focus to their trigger on close (#1539 C3)', () => {
  it('the IP popover', async () => {
    const trigger = focusedTrigger()
    render(IpLookupPopover)
    ipLookupState.open('192.0.2.2', RECT, trigger)
    flushSync()
    await escapeFromInside(trigger)
    expect(ipLookupState.anchor).toBeNull()
    expect(document.activeElement).toBe(trigger)
    trigger.remove()
  })

  it('the port popover', async () => {
    const trigger = focusedTrigger()
    render(PortLookupPopover)
    portLookupState.open(443, RECT)
    flushSync()
    await escapeFromInside(trigger)
    expect(portLookupState.anchor).toBeNull()
    expect(document.activeElement).toBe(trigger)
    trigger.remove()
  })

  it('the router popover', async () => {
    const trigger = focusedTrigger()
    render(RouterLookupPopover)
    routerLookupState.openRule('core', 'r1', RECT)
    flushSync()
    await escapeFromInside(trigger)
    expect(routerLookupState.anchor).toBeNull()
    expect(document.activeElement).toBe(trigger)
    trigger.remove()
  })
})
