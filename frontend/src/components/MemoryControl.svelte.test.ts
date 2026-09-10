// SPDX-License-Identifier: AGPL-3.0-only
//
// #796's own three lines, at the component: dragging only proposes, a
// viewer cannot move the handle and is not told with a lock icon, and
// the server's refusal is shown in its own words.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

const setStoreMaxMemory = vi.fn()
vi.mock('../lib/api', () => ({
  setStoreMaxMemory: (bytes: number) => setStoreMaxMemory(bytes),
}))

import MemoryControl from './MemoryControl.svelte'
import type { Stats, StoreMemory } from '../lib/types'

const MIB = 1024 * 1024

const mem: StoreMemory = {
  maxMemory: 120 * MIB,
  min: 32 * MIB,
  max: 3584 * MIB,
  hostTotal: 8192 * MIB,
  bytesPerEvent: 624,
  resident: 180 * MIB,
  stored: false,
}

const stats = {
  total: 900_000,
  byAction: {},
  topRules: [],
  timeSeries: [],
  eventsPerSecond: 201649 / (9 * 3600),
  capacity: 201649,
  count: 201649,
  windowSeconds: 86400,
  oldestHeld: new Date(Date.now() - 9 * 3600 * 1000).toISOString(),
  connectedClients: 1,
  memory: mem,
} as unknown as Stats

beforeEach(() => {
  setStoreMaxMemory.mockReset()
  setStoreMaxMemory.mockResolvedValue({ ...mem, maxMemory: 480 * MIB })
})

describe('an admin', () => {
  it('is offered the drag, and the figure in effect', () => {
    render(MemoryControl, { props: { mem, stats, canEdit: true } })
    const slider = screen.getByRole('slider', { name: 'Event buffer size' })
    expect(slider.getAttribute('aria-valuenow')).toBe(String(120 * MIB))
    expect(slider.getAttribute('aria-valuemin')).toBe(String(32 * MIB))
    expect(slider.getAttribute('aria-valuemax')).toBe(String(3584 * MIB))
    expect(screen.getByText('120 MiB')).toBeTruthy()
  })

  it('proposes rather than applies: no apply, no request until the link is clicked', async () => {
    render(MemoryControl, { props: { mem, stats, canEdit: true } })
    const slider = screen.getByRole('slider', { name: 'Event buffer size' })

    // Nothing is offered until something is proposed.
    expect(screen.queryByRole('button', { name: 'apply' })).toBeNull()

    await fireEvent.keyDown(slider, { key: 'ArrowRight' })
    expect(setStoreMaxMemory).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'apply' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'keep 120 MiB' })).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'apply' }))
    expect(setStoreMaxMemory).toHaveBeenCalledTimes(1)
    expect(setStoreMaxMemory.mock.calls[0][0]).toBeGreaterThan(120 * MIB)
  })

  it('puts the handle back where it was on keep, without asking the server', async () => {
    render(MemoryControl, { props: { mem, stats, canEdit: true } })
    const slider = screen.getByRole('slider', { name: 'Event buffer size' })

    await fireEvent.keyDown(slider, { key: 'End' })
    expect(slider.getAttribute('aria-valuenow')).toBe(String(3584 * MIB))

    await fireEvent.click(screen.getByRole('button', { name: 'keep 120 MiB' }))
    expect(setStoreMaxMemory).not.toHaveBeenCalled()
    expect(slider.getAttribute('aria-valuenow')).toBe(String(120 * MIB))
    expect(screen.queryByRole('button', { name: 'apply' })).toBeNull()
  })

  it('says the consequence of a shrink in the drawing’s own words', async () => {
    render(MemoryControl, { props: { mem, stats, canEdit: true } })
    const slider = screen.getByRole('slider', { name: 'Event buffer size' })
    await fireEvent.keyDown(slider, { key: 'Home' })
    // 32 MiB against a full 120 MiB ring: something really does go.
    expect(screen.getByText(/32 MiB holds ~.* at today's rate — everything before \d\d:\d\d lets go/)).toBeTruthy()
  })

  it('shows the server’s own refusal rather than a status code', async () => {
    setStoreMaxMemory.mockResolvedValue('4 GiB is more than this host can spare for the event buffer (3.5 GiB)')
    render(MemoryControl, { props: { mem, stats, canEdit: true } })
    const slider = screen.getByRole('slider', { name: 'Event buffer size' })
    await fireEvent.keyDown(slider, { key: 'End' })
    await fireEvent.click(screen.getByRole('button', { name: 'apply' }))
    expect(
      await screen.findByText('4 GiB is more than this host can spare for the event buffer (3.5 GiB)'),
    ).toBeTruthy()
  })
})

describe('a viewer', () => {
  it('sees the bar and the figure', () => {
    render(MemoryControl, { props: { mem, stats, canEdit: false } })
    expect(screen.getByText('120 MiB')).toBeTruthy()
    expect(
      screen.getByRole('img', {
        name: 'The event buffer is set to 120 MiB, of the 3.5 GiB this host can spare',
      }),
    ).toBeTruthy()
  })

  it('cannot move it -- there is no slider to reach, by pointer or by key', async () => {
    const { container } = render(MemoryControl, { props: { mem, stats, canEdit: false } })
    expect(screen.queryByRole('slider')).toBeNull()

    const svg = container.querySelector('svg.stmemctl') as SVGSVGElement
    expect(svg.getAttribute('tabindex')).toBeNull()
    await fireEvent.keyDown(svg, { key: 'ArrowRight' })
    expect(screen.queryByRole('button', { name: 'apply' })).toBeNull()
    expect(setStoreMaxMemory).not.toHaveBeenCalled()
  })

  // #796, verbatim: "is not told with a lock icon". The whole point is
  // that nothing appears to explain the absence -- no padlock, no
  // "admin only", no greyed-out control.
  it('is not told why with a lock icon or a disabled control', () => {
    const { container } = render(MemoryControl, { props: { mem, stats, canEdit: false } })
    const text = container.textContent ?? ''
    expect(text).not.toMatch(/🔒|lock|admin only|read.only|permission/i)
    expect(container.querySelector('[disabled]')).toBeNull()
    expect(container.querySelector('[aria-disabled]')).toBeNull()
  })
})
