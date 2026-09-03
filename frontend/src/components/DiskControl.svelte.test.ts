// SPDX-License-Identifier: AGPL-3.0-only
//
// Round 42's disk group at the component (#910): dragging only proposes
// and the link names the deletion, turning on is immediate because it
// deletes nothing, a viewer reads the same statements without a lock
// icon, and without a key there is no control at all.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

const setHistorySettings = vi.fn()
vi.mock('../lib/api', () => ({
  setHistorySettings: (body: unknown) => setHistorySettings(body),
}))

import DiskControl from './DiskControl.svelte'
import type { HistorySettings, Stats } from '../lib/types'

const MIB = 1024 * 1024
const GIB = 1024 * MIB

function story(overrides: Partial<HistorySettings> = {}): HistorySettings {
  return {
    keyed: true,
    enabled: true,
    days: 30,
    maxBytes: GIB,
    held: { days: 27, oldest: '2026-08-07', newest: '2026-09-02', bytes: 812 * MIB },
    capped: false,
    bytesPerDay: 30 * MIB,
    ...overrides,
  }
}

const stats = {
  oldestHeld: new Date(Date.now() - 9 * 3600 * 1000).toISOString(),
} as unknown as Stats

beforeEach(() => {
  setHistorySettings.mockReset()
  setHistorySettings.mockImplementation(async (body: { enabled: boolean; days: number; maxBytes: number }) =>
    story({ ...body, held: body.enabled ? story().held : null }),
  )
})

describe('an admin', () => {
  it('is offered the drag, the figure in effect, and the row', () => {
    render(DiskControl, { props: { settings: story(), stats, canEdit: true } })
    const slider = screen.getByRole('slider', { name: 'Days kept on disk' })
    expect(slider.getAttribute('aria-valuemin')).toBe('1')
    expect(slider.getAttribute('aria-valuemax')).toBe('365')
    expect(slider.getAttribute('aria-valuenow')).toBe('30')
    expect(slider.getAttribute('aria-valuetext')).toBe('30 days, under a 1 GiB cap')
    expect(screen.getByText(/^27 days · since .* · 812 MiB — filling$/)).toBeTruthy()
    expect(screen.getByRole('button', { name: '1 GiB' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'turn off' })).toBeTruthy()
    expect(screen.getByText('mounted at start')).toBeTruthy()
  })

  it('proposes fewer days rather than applying them, and the link names the deletion', async () => {
    const onchanged = vi.fn()
    render(DiskControl, { props: { settings: story(), stats, canEdit: true, onchanged } })
    const slider = screen.getByRole('slider', { name: 'Days kept on disk' })

    expect(screen.queryByRole('button', { name: /^delete/ })).toBeNull()

    await fireEvent.keyDown(slider, { key: 'PageDown' }) // 30 -> 15
    expect(setHistorySettings).not.toHaveBeenCalled()
    expect(slider.getAttribute('aria-valuenow')).toBe('15')
    expect(screen.getByRole('button', { name: 'delete 12 days' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'keep all 27' })).toBeTruthy()
    expect(screen.getByText(/15 days holds ~450 MiB at today's rate/)).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'delete 12 days' }))
    expect(setHistorySettings).toHaveBeenCalledTimes(1)
    expect(setHistorySettings.mock.calls[0][0]).toEqual({ enabled: true, days: 15, maxBytes: GIB })
    await vi.waitFor(() => expect(onchanged).toHaveBeenCalledTimes(1))
    expect(onchanged.mock.calls[0][0].days).toBe(15)
    expect(screen.queryByRole('button', { name: /^delete/ })).toBeNull()
  })

  it('puts the handle back on keep, without asking the server', async () => {
    render(DiskControl, { props: { settings: story(), stats, canEdit: true } })
    const slider = screen.getByRole('slider', { name: 'Days kept on disk' })

    await fireEvent.keyDown(slider, { key: 'Home' })
    expect(slider.getAttribute('aria-valuenow')).toBe('1')
    await fireEvent.click(screen.getByRole('button', { name: 'keep all 27' }))
    expect(slider.getAttribute('aria-valuenow')).toBe('30')
    expect(setHistorySettings).not.toHaveBeenCalled()
  })

  it('more days is a plain apply, with the cap sentence', async () => {
    render(DiskControl, { props: { settings: story(), stats, canEdit: true } })
    const slider = screen.getByRole('slider', { name: 'Days kept on disk' })

    await fireEvent.keyDown(slider, { key: 'End' })
    expect(screen.getByText(/365 days would need .* the 1 GiB cap would hold ~34 of them/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'apply' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'keep 30 days' })).toBeTruthy()
  })

  it('opens the cap in place and names what a smaller one would let go', async () => {
    render(DiskControl, { props: { settings: story(), stats, canEdit: true } })
    await fireEvent.click(screen.getByRole('button', { name: '1 GiB' }))
    const field = screen.getByRole('textbox', { name: 'Byte cap, MiB' }) as HTMLInputElement
    expect(field.value).toBe('1024')

    await fireEvent.input(field, { target: { value: '512' } })
    expect(screen.getByText(/512 MiB holds ~17 days at today's rate/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'delete 10 days' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'keep 1 GiB' })).toBeTruthy()

    await fireEvent.keyDown(field, { key: 'Enter' })
    expect(setHistorySettings).toHaveBeenCalledTimes(1)
    expect(setHistorySettings.mock.calls[0][0]).toEqual({ enabled: true, days: 30, maxBytes: 512 * MIB })
  })

  it('turning off is a proposal that names every day on disk, and shows off in the row', async () => {
    render(DiskControl, { props: { settings: story(), stats, canEdit: true } })
    await fireEvent.click(screen.getByRole('button', { name: 'turn off' }))
    expect(setHistorySettings).not.toHaveBeenCalled()
    expect(screen.getByText(/off deletes all 27 days on disk/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'delete 27 days' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'keep them' })).toBeTruthy()
    expect(screen.getByText('off')).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'delete 27 days' }))
    expect(setHistorySettings.mock.calls[0][0]).toEqual({ enabled: false, days: 30, maxBytes: GIB })
  })

  it('turning on is immediate: nothing to delete, so no proposal', async () => {
    render(DiskControl, { props: { settings: story({ enabled: false, held: null }), stats, canEdit: true } })
    expect(screen.getByText('nothing')).toBeTruthy()
    expect(screen.getByText(/nothing on disk — events live in memory only, ~9 h of them/)).toBeTruthy()
    expect(screen.queryByRole('img', { name: /days held on disk/ })).toBeNull()
    expect(screen.getByRole('slider', { name: 'Days kept on disk' })).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'turn on' }))
    expect(setHistorySettings).toHaveBeenCalledTimes(1)
    expect(setHistorySettings.mock.calls[0][0]).toEqual({ enabled: true, days: 30, maxBytes: GIB })
  })

  it('is shown the server’s refusal in its own words', async () => {
    setHistorySettings.mockResolvedValue('days must be between 1 and 365')
    render(DiskControl, { props: { settings: story(), stats, canEdit: true } })
    const slider = screen.getByRole('slider', { name: 'Days kept on disk' })
    await fireEvent.keyDown(slider, { key: 'ArrowRight' })
    await fireEvent.click(screen.getByRole('button', { name: 'apply' }))
    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'days must be between 1 and 365')
  })
})

describe('a viewer', () => {
  it('reads the same statements without a drag, a lock, or a link', () => {
    const { container } = render(DiskControl, { props: { settings: story(), stats, canEdit: false } })
    expect(screen.queryByRole('slider')).toBeNull()
    expect(screen.getByRole('img', { name: '30 days are kept on disk, under a 1 GiB cap' })).toBeTruthy()
    expect(screen.getByText(/^27 days · since .* · 812 MiB — filling$/)).toBeTruthy()
    expect(screen.queryByRole('button')).toBeNull()
    expect(container.querySelector('[disabled]')).toBeNull()
    expect(container.textContent).toContain('30 days · at most 1 GiB')
  })

  it('sees full when the cap decides', () => {
    render(DiskControl, {
      props: {
        settings: story({
          maxBytes: 768 * MIB,
          capped: true,
          held: { days: 25, oldest: '2026-08-09', newest: '2026-09-02', bytes: 768 * MIB },
        }),
        stats,
        canEdit: false,
      },
    })
    expect(screen.getByText(/^25 days · since .* · 768 MiB — full$/)).toBeTruthy()
  })
})

describe('without a key', () => {
  it('is two statements and a link to the guide, no control', () => {
    render(DiskControl, { props: { settings: story({ keyed: false, enabled: false, held: null }), stats, canEdit: true } })
    expect(screen.queryByRole('slider')).toBeNull()
    expect(screen.queryByRole('button')).toBeNull()
    expect(screen.getByText('nothing')).toBeTruthy()
    expect(screen.getByText(/none mounted — nothing is kept on disk without one/)).toBeTruthy()
    const link = screen.getByRole('link', { name: 'how to mount one' })
    expect(link.getAttribute('href')).toContain('docs/configuration.md#')
    expect(link.getAttribute('rel')).toBe('noopener noreferrer')
  })
})
