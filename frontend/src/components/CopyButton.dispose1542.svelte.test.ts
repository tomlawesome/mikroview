// SPDX-License-Identifier: AGPL-3.0-only
//
// #1542: unmounting a copy control mid-flash leaves no timer pending, and a
// copy that settles after unmount writes no state.

import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'

vi.mock('../lib/clipboard', () => ({ copyToClipboard: vi.fn() }))

import { copyToClipboard } from '../lib/clipboard'
import CopyButton from './CopyButton.svelte'

afterEach(() => {
  vi.useRealTimers()
})

describe('CopyFlash teardown (#1542)', () => {
  it('leaves no timer pending when unmounted mid-flash', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(true)
    vi.useFakeTimers()
    const { unmount } = render(CopyButton, { value: 'x', label: 'thing' })
    await fireEvent.click(screen.getByRole('button'))
    await vi.advanceTimersByTimeAsync(10)
    expect(vi.getTimerCount()).toBe(1)
    unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('starts no timer for a copy that settles after unmount', async () => {
    let settle!: (ok: boolean) => void
    vi.mocked(copyToClipboard).mockReturnValue(new Promise<boolean>((r) => (settle = r)))
    vi.useFakeTimers()
    const { unmount } = render(CopyButton, { value: 'x', label: 'thing' })
    await fireEvent.click(screen.getByRole('button'))
    unmount()
    settle(true)
    await vi.advanceTimersByTimeAsync(10)
    expect(vi.getTimerCount()).toBe(0)
  })
})
