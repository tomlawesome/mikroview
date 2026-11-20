// SPDX-License-Identifier: AGPL-3.0-only
//
// #1542 (C12): the config-upgrade block's copy button flips to "copied" /
// "copy failed"; no toast for copy. (Success label is already pinned in
// ConfigUpgrade.svelte.test.ts; the failure label and the toast are new.)

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', () => ({ fetchConfigUpgrade: vi.fn() }))
vi.mock('../lib/clipboard', () => ({ copyToClipboard: vi.fn() }))

import { fetchConfigUpgrade } from '../lib/api'
import { copyToClipboard } from '../lib/clipboard'
import { toastState } from '../lib/toast.svelte'
import ConfigUpgrade from './ConfigUpgrade.svelte'

async function clickCopy(): Promise<HTMLElement> {
  vi.mocked(fetchConfigUpgrade).mockResolvedValue({
    version: 'v1.2.3',
    settings: [{ key: 'geoip', block: '# geoip:' }],
  })
  render(ConfigUpgrade)
  await Promise.resolve()
  await Promise.resolve()
  flushSync()
  const btn = screen.getByRole('button', { name: 'copy' })
  await fireEvent.click(btn)
  await Promise.resolve()
  await Promise.resolve()
  flushSync()
  return btn
}

describe('ConfigUpgrade copy label flip (#1542 C12)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    toastState.message = null
  })

  it('shows "copy failed" on the clicked button after a failed copy', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(false)
    const btn = await clickCopy()
    expect(btn.textContent?.trim()).toBe('copy failed')
  })

  it('raises no toast after a successful copy', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(true)
    await clickCopy()
    expect(toastState.message).toBeNull()
  })

  it('raises no toast after a failed copy', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(false)
    await clickCopy()
    expect(toastState.message).toBeNull()
  })
})
