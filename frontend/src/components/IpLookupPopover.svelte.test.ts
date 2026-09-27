// SPDX-License-Identifier: AGPL-3.0-only
//
// #1352: the IP popover's network owner line -- "Network · AS13335
// Cloudflare, Inc." under the country when the local source knows it,
// nothing at all when it does not.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', () => ({
  lookupIp: vi.fn(async () => ({ ip: '1.1.1.1', countryCode: 'US', isp: 'Cloudflare' })),
  geoLookup: vi.fn(async () => ({ country: 'US', asn: 13335, asName: 'Cloudflare, Inc.' })),
}))

import { geoLookup, lookupIp } from '../lib/api'
import { ipLookupState } from '../lib/ipLookup.svelte'
import IpLookupPopover from './IpLookupPopover.svelte'

function openFor(ip: string) {
  render(IpLookupPopover)
  ipLookupState.open(ip, new DOMRect(10, 10, 20, 20))
  flushSync()
}

beforeEach(() => {
  vi.mocked(geoLookup).mockClear()
})

afterEach(() => {
  ipLookupState.close()
  cleanup()
})

describe('the network owner line (#1352)', () => {
  it('draws "Network · AS… owner" under the country when it is known', async () => {
    openFor('1.1.1.1')
    await vi.waitFor(() =>
      expect(screen.getByTestId('ip-lookup-owner').textContent?.trim()).toBe('Network · AS13335 Cloudflare, Inc.'),
    )
    expect(geoLookup).toHaveBeenCalledWith('1.1.1.1')
    // Directly under the country row, not somewhere further down.
    const owner = screen.getByTestId('ip-lookup-owner')
    expect(owner.previousElementSibling?.textContent).toContain('Country')
  })

  it('draws nothing when the source does not know the owner', async () => {
    vi.mocked(geoLookup).mockResolvedValueOnce({ country: 'US', asn: null, asName: null })
    openFor('1.1.1.1')
    await vi.waitFor(() => expect(screen.getByText('Country')).toBeTruthy())
    flushSync()
    expect(screen.queryByTestId('ip-lookup-owner')).toBeNull()
  })

  it('still names the owner when the reputation lookup found nothing', async () => {
    vi.mocked(lookupIp).mockResolvedValueOnce({ ip: '1.1.1.1' } as never)
    openFor('1.1.1.1')
    await vi.waitFor(() => expect(screen.getByTestId('ip-lookup-owner')).toBeTruthy())
    expect(screen.queryByText('No intel found for this IP')).toBeNull()
  })
})
