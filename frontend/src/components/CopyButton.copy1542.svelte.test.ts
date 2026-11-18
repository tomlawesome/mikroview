// SPDX-License-Identifier: AGPL-3.0-only
//
// #1542 (C12): the copy control flips its own label to "copied" /
// "copy failed"; no toast for copy.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/clipboard', () => ({ copyToClipboard: vi.fn() }))

import { copyToClipboard } from '../lib/clipboard'
import { toastState } from '../lib/toast.svelte'
import CopyButton from './CopyButton.svelte'

async function clickCopy(): Promise<HTMLElement> {
  render(CopyButton, { value: '192.0.2.7', label: 'source IP' })
  const btn = screen.getByRole('button', { name: /^Copy source IP/ })
  await fireEvent.click(btn)
  await Promise.resolve()
  await Promise.resolve()
  flushSync()
  return btn
}

describe('CopyButton label flip (#1542 C12)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    toastState.message = null
  })

  it('shows "copied" on its own label after a successful copy', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(true)
    const btn = await clickCopy()
    expect(copyToClipboard).toHaveBeenCalledWith('192.0.2.7')
    expect(btn.textContent?.trim()).toBe('copied')
  })

  it('shows "copy failed" on its own label after a failed copy', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(false)
    const btn = await clickCopy()
    expect(btn.textContent?.trim()).toBe('copy failed')
  })

  it('raises no toast for copy, success or failure', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(true)
    await clickCopy()
    expect(toastState.message).toBeNull()
  })

  it('raises no toast when the copy fails', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(false)
    await clickCopy()
    expect(toastState.message).toBeNull()
  })
})
