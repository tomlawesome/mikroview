// SPDX-License-Identifier: AGPL-3.0-only
//
// #1542 (C12): "Copy the hour" flips its own label to "copied" /
// "copy failed"; no toast for copy.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { buildHour } from '../lib/metricsSeries'
import type { TimeBucket } from '../lib/types'

vi.mock('../lib/clipboard', () => ({ copyToClipboard: vi.fn() }))

import { copyToClipboard } from '../lib/clipboard'
import { toastState } from '../lib/toast.svelte'
import MetricsTable from './MetricsTable.svelte'

const traffic: TimeBucket[] = [
  { time: new Date(Date.UTC(2026, 7, 24, 13, 0, 0)).toISOString(), byAction: { accept: 400, drop: 9 } },
]

async function clickCopy(): Promise<HTMLElement> {
  render(MetricsTable, { hour: buildHour(traffic, [], []), cursor: -1, onselect: () => {} })
  const btn = screen.getByRole('button', { name: /^Copy the hour/ })
  await fireEvent.click(btn)
  await Promise.resolve()
  await Promise.resolve()
  flushSync()
  return btn
}

describe('MetricsTable copy label flip (#1542 C12)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    toastState.message = null
  })

  it('shows "copied" on its own label after a successful copy', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(true)
    const btn = await clickCopy()
    expect(btn.textContent?.trim()).toBe('copied')
  })

  it('shows "copy failed" on its own label after a failed copy', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(false)
    const btn = await clickCopy()
    expect(btn.textContent?.trim()).toBe('copy failed')
  })

  it('raises no toast for copy', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(true)
    await clickCopy()
    expect(toastState.message).toBeNull()
  })
})
