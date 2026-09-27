// SPDX-License-Identifier: AGPL-3.0-only
//
// #1352's data credits in About: IPinfo and MaxMind, always present
// whichever source is in use, each with its link. DB-IP is never named
// here -- its credit is the fall's foot, and only while it is live.
import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  fetchHealthz: vi.fn(async () => ({ version: 'test', geoip: true, geoSource: 'dbip' })),
}))

import AboutOverlay from './AboutOverlay.svelte'

describe('About carries the IPinfo and MaxMind credits (#1352)', () => {
  it('credits IPinfo Lite, linked to ipinfo.io', () => {
    render(AboutOverlay, { props: { open: true } })
    const line = screen.getByTestId('about-credit-ipinfo')
    expect(line.textContent?.replace(/\s+/g, ' ').trim()).toBe('Country and network data from IPinfo Lite.')
    const link = line.querySelector('a')
    expect(link?.getAttribute('href')).toBe('https://ipinfo.io')
    expect(link?.getAttribute('rel') ?? '').toContain('noopener')
  })

  it("credits MaxMind in the words its licence asks for, linked to maxmind.com", () => {
    render(AboutOverlay, { props: { open: true } })
    const line = screen.getByTestId('about-credit-maxmind')
    expect(line.textContent?.replace(/\s+/g, ' ').trim()).toBe(
      'This product includes GeoLite data created by MaxMind, available from https://www.maxmind.com.',
    )
    expect(line.querySelector('a')?.getAttribute('href')).toBe('https://www.maxmind.com')
  })

  it('never mentions DB-IP, even while it is the source in use', () => {
    render(AboutOverlay, { props: { open: true } })
    expect(document.body.textContent).not.toMatch(/DB-IP|db-ip/)
  })
})
