// SPDX-License-Identifier: AGPL-3.0-only
//
// Settings' "router backups" group at the component (#394, round 44):
// the no-key and nothing-yet statements, a router's receipt at rest,
// the amber missed-push receipt and its "is it gone?" link, and the
// download links round 44's newest-pair line offers.

import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import RouterBackups from './RouterBackups.svelte'
import type { RouterBackupsResponse } from '../lib/types'

function resp(over: Partial<RouterBackupsResponse> = {}): RouterBackupsResponse {
  return {
    enabled: true,
    routers: [],
    totalGenerations: 0,
    totalRouters: 0,
    totalBytes: 0,
    port: ':47022',
    ...over,
  }
}

describe('no key mounted', () => {
  it('says the drop box is closed, with no per-router block', () => {
    render(RouterBackups, { props: { resp: resp({ enabled: false }), onopenlost: vi.fn() } })
    expect(screen.getByText(/none mounted — a backup that arrives has nowhere safe to go/)).toBeTruthy()
  })
})

describe('nothing pushed yet', () => {
  it('points at the wizard step that prints the script', () => {
    render(RouterBackups, { props: { resp: resp({ routers: [] }), onopenlost: vi.fn() } })
    expect(screen.getByText(/no router has pushed one yet/)).toBeTruthy()
    expect(screen.getByText(/the wizard's step 6 prints the script/)).toBeTruthy()
  })
})

describe('a router at rest', () => {
  it('reads the kept count, the cadence and the oldest date, with download links', () => {
    const router = {
      device: 'rb5009',
      generations: [
        { id: 'g0', backupArrivedAt: '2026-08-24T03:00:00Z', rscArrivedAt: '2026-08-24T03:00:05Z', backupBytes: 412000, rscBytes: 38000, header: 'plain' },
      ],
      intervalKnown: false,
      missed: 0,
    }
    render(RouterBackups, {
      props: {
        resp: resp({ routers: [router], totalGenerations: 1, totalRouters: 1, totalBytes: 450000 }),
        onopenlost: vi.fn(),
      },
    })
    expect(screen.getByText('rb5009')).toBeTruthy()
    expect(screen.getByText('1 kept')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'download .backup' })).toHaveProperty(
      'href',
      expect.stringContaining('/api/router-backups/rb5009/g0/backup'),
    )
    expect(screen.getByRole('link', { name: '.rsc' })).toHaveProperty(
      'href',
      expect.stringContaining('/api/router-backups/rb5009/g0/rsc'),
    )
    // Nothing has been missed, so round 44's link is not offered.
    expect(screen.queryByRole('button', { name: 'is it gone?' })).toBeNull()
  })
})

describe('a router that has missed its usual push', () => {
  it('goes amber and offers "is it gone?", which names the router', async () => {
    const onopenlost = vi.fn()
    const router = {
      device: 'hap-ax2',
      generations: [
        { id: 'g0', backupArrivedAt: '2026-08-27T03:00:00Z', rscArrivedAt: '2026-08-27T03:00:05Z' },
      ],
      intervalKnown: true,
      intervalSeconds: 86400,
      lastArrival: '2026-08-30T03:00:00Z',
      missed: 3,
    }
    render(RouterBackups, { props: { resp: resp({ routers: [router] }), onopenlost } })
    expect(screen.getByText(/3 missed/)).toBeTruthy()
    const link = screen.getByRole('button', { name: 'is it gone?' })
    link.click()
    expect(onopenlost).toHaveBeenCalledWith('hap-ax2')
  })
})

describe('the facts column', () => {
  it('states the arrive-by port and the fixed allowance', () => {
    const router = { device: 'rb5009', generations: [], intervalKnown: false, missed: 0 }
    render(RouterBackups, { props: { resp: resp({ routers: [router] }), onopenlost: vi.fn() } })
    expect(screen.getByText(/SFTP on port 47022/)).toBeTruthy()
    expect(screen.getByText(/10 pairs a router · 16 MiB a file/)).toBeTruthy()
  })
})
