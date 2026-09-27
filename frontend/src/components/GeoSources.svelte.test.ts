// SPDX-License-Identifier: AGPL-3.0-only
//
// Settings' "country and network owner" group at the component (#1352):
// the top line naming the source in use, the three rows and the "in use"
// mark, each keyed source's write-once field (set posts the key and
// clears the field; the key is never drawn again), the two-click remove,
// the server's own words on a refusal and on a failed download, and the
// precedence sentence under the card. The admin-only gate is EngineRoom's
// and is tested there.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  setGeoIpinfoToken: vi.fn(),
  removeGeoIpinfoToken: vi.fn(),
  setGeoMaxmindKey: vi.fn(),
  removeGeoMaxmindKey: vi.fn(),
}))

import { removeGeoIpinfoToken, setGeoIpinfoToken, setGeoMaxmindKey } from '../lib/api'
import { appState } from '../lib/state.svelte'
import GeoSources from './GeoSources.svelte'
import type { GeoKeyedSourceStatus, GeoSettings } from '../lib/types'

const noKey: GeoKeyedSourceStatus = {
  keySet: false,
  setAt: null,
  setBy: null,
  loaded: false,
  fetchedAt: null,
  nextRefresh: null,
  lastError: null,
}

function settings(over: Partial<GeoSettings> = {}, sources: Partial<GeoSettings['sources']> = {}): GeoSettings {
  return {
    source: 'dbip',
    sources: {
      dbip: { loaded: true, fetchedAt: new Date(Date.now() - 3.5 * 3600_000).toISOString(), nextRefresh: null, lastError: null },
      ipinfo: noKey,
      maxmind: noKey,
      ...sources,
    },
    ...over,
  }
}

const withIpinfo = (): GeoSettings =>
  settings(
    { source: 'ipinfo' },
    { ipinfo: { ...noKey, keySet: true, setBy: 'tom', setAt: new Date().toISOString(), loaded: true } },
  )

function row(source: string): HTMLElement {
  const el = document.querySelector(`[data-source="${source}"]`)
  if (!el) throw new Error(`no row for ${source}`)
  return el as HTMLElement
}

beforeEach(() => {
  vi.resetAllMocks()
  appState.now = Date.now()
})

describe('the source in use', () => {
  it('names the live source on the top line and marks its row, and only its row', () => {
    render(GeoSources, { props: { settings: settings(), onchanged: vi.fn() } })
    expect(screen.getByTestId('geo-in-use').textContent).toBe('Flags from DB-IP Lite')
    expect(screen.getAllByText('in use')).toHaveLength(1)
    expect(row('dbip').textContent).toContain('in use')
    expect(row('dbip').textContent).toContain('no key needed')
    expect(row('dbip').textContent).toMatch(/last fetched 3h ago/)
  })

  it('says no source is loaded yet when there is none, with no row marked', () => {
    render(GeoSources, { props: { settings: settings({ source: null }), onchanged: vi.fn() } })
    expect(screen.getByTestId('geo-in-use').textContent).toBe('No flag source loaded yet')
    expect(screen.queryByText('in use')).toBeNull()
  })

  it('shows a failed download in the server\'s own words', () => {
    render(GeoSources, {
      props: {
        settings: settings({}, { dbip: { loaded: false, fetchedAt: null, nextRefresh: null, lastError: 'download failed: 503 Service Unavailable' } }),
        onchanged: vi.fn(),
      },
    })
    expect(row('dbip').textContent).toContain('not fetched yet')
    expect(screen.getByText('download failed: 503 Service Unavailable')).toBeTruthy()
  })

  it('states the fixed precedence under the card', () => {
    render(GeoSources, { props: { settings: settings(), onchanged: vi.fn() } })
    expect(screen.getByText('Setting a key switches the source: IPinfo, then MaxMind, then DB-IP.')).toBeTruthy()
  })
})

describe('IPinfo', () => {
  it('set posts the token, clears the field and hands the new state up', async () => {
    const next = withIpinfo()
    vi.mocked(setGeoIpinfoToken).mockResolvedValue(next)
    const onchanged = vi.fn()
    render(GeoSources, { props: { settings: settings(), onchanged } })

    const field = screen.getByLabelText('IPinfo token') as HTMLInputElement
    expect(field.type).toBe('password')
    await fireEvent.input(field, { target: { value: 'fake-token-123' } })
    await fireEvent.click(row('ipinfo').querySelector('button') as HTMLButtonElement)

    expect(setGeoIpinfoToken).toHaveBeenCalledWith('fake-token-123')
    expect(onchanged).toHaveBeenCalledWith(next)
    expect(field.value).toBe('')
  })

  it('set is not offered until something is typed', () => {
    render(GeoSources, { props: { settings: settings(), onchanged: vi.fn() } })
    const set = row('ipinfo').querySelector('button') as HTMLButtonElement
    expect(set.textContent?.trim()).toBe('set')
    expect(set.disabled).toBe(true)
  })

  it('a refusal shows the server\'s words and keeps what was typed', async () => {
    vi.mocked(setGeoIpinfoToken).mockResolvedValue('token must not be empty')
    const onchanged = vi.fn()
    render(GeoSources, { props: { settings: settings(), onchanged } })

    const field = screen.getByLabelText('IPinfo token') as HTMLInputElement
    await fireEvent.input(field, { target: { value: 'x' } })
    await fireEvent.click(row('ipinfo').querySelector('button') as HTMLButtonElement)

    expect(screen.getByRole('alert').textContent).toBe('token must not be empty')
    expect(onchanged).not.toHaveBeenCalled()
    expect(field.value).toBe('x')
  })

  it('once set: reads key set, by whom and when, and offers no field -- the key is never shown', () => {
    render(GeoSources, { props: { settings: withIpinfo(), onchanged: vi.fn() } })
    expect(screen.queryByLabelText('IPinfo token')).toBeNull()
    expect(row('ipinfo').textContent).toMatch(/key set · by tom, just now/)
    expect(row('ipinfo').textContent).toContain('in use')
    expect(screen.getByTestId('geo-in-use').textContent).toBe('Flags from IPinfo Lite')
  })

  it('remove needs two clicks', async () => {
    const back = settings()
    vi.mocked(removeGeoIpinfoToken).mockResolvedValue(back)
    const onchanged = vi.fn()
    render(GeoSources, { props: { settings: withIpinfo(), onchanged } })

    const remove = screen.getByRole('button', { name: 'remove' })
    await fireEvent.click(remove)
    expect(removeGeoIpinfoToken).not.toHaveBeenCalled()
    expect(remove.textContent?.trim()).toBe('confirm — delete the key')

    await fireEvent.click(remove)
    expect(removeGeoIpinfoToken).toHaveBeenCalledTimes(1)
    expect(onchanged).toHaveBeenCalledWith(back)
  })

  it('an armed remove disarms on a click anywhere else', async () => {
    render(GeoSources, { props: { settings: withIpinfo(), onchanged: vi.fn() } })
    const remove = screen.getByRole('button', { name: 'remove' })
    await fireEvent.click(remove)
    await fireEvent.click(document.body)
    expect(remove.textContent?.trim()).toBe('remove')
    expect(removeGeoIpinfoToken).not.toHaveBeenCalled()
  })
})

describe('MaxMind', () => {
  it('needs both the account ID and the licence key, posts them, and clears both', async () => {
    const next = settings(
      { source: 'maxmind' },
      { maxmind: { ...noKey, keySet: true, setBy: 'tom', setAt: new Date().toISOString() } },
    )
    vi.mocked(setGeoMaxmindKey).mockResolvedValue(next)
    const onchanged = vi.fn()
    render(GeoSources, { props: { settings: settings(), onchanged } })

    const account = screen.getByLabelText('MaxMind account ID') as HTMLInputElement
    const key = screen.getByLabelText('MaxMind licence key') as HTMLInputElement
    const set = row('maxmind').querySelector('button') as HTMLButtonElement

    await fireEvent.input(account, { target: { value: '123456' } })
    expect(set.disabled).toBe(true)
    await fireEvent.input(key, { target: { value: 'fake-licence' } })
    expect(set.disabled).toBe(false)
    await fireEvent.click(set)

    expect(setGeoMaxmindKey).toHaveBeenCalledWith('123456', 'fake-licence')
    expect(onchanged).toHaveBeenCalledWith(next)
    expect(account.value).toBe('')
    expect(key.value).toBe('')
  })

  it('once set, reads key set with a remove and no fields', () => {
    render(GeoSources, {
      props: {
        settings: settings({}, { maxmind: { ...noKey, keySet: true, setBy: 'kai', setAt: new Date().toISOString() } }),
        onchanged: vi.fn(),
      },
    })
    expect(screen.queryByLabelText('MaxMind licence key')).toBeNull()
    expect(row('maxmind').textContent).toMatch(/key set · by kai/)
    expect(row('maxmind').querySelector('button')?.textContent?.trim()).toBe('remove')
  })
})
