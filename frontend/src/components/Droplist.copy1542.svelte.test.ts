// SPDX-License-Identifier: AGPL-3.0-only
//
// #1542 (C12): the drop list's copy links flip to "copied" / "copy failed".
// (The success label is already pinned elsewhere; a failed copy used to
// say nothing at all.)

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', () => ({
  createDroplistEntry: vi.fn(),
  deleteDroplistEntry: vi.fn(),
  mintDroplistKey: vi.fn(),
  revokeDroplistKey: vi.fn(),
  fetchSetupCommands: vi.fn(),
  fetchBlocklistBuilder: vi.fn(() => new Promise(() => {})),
  fetchBlocklistCommands: vi.fn(),
  createToken: vi.fn(),
}))
vi.mock('../lib/clipboard', () => ({ copyToClipboard: vi.fn() }))

import { fetchSetupCommands } from '../lib/api'
import { copyToClipboard } from '../lib/clipboard'
import { toastState } from '../lib/toast.svelte'
import Droplist from './Droplist.svelte'
import type { DroplistResponse } from '../lib/types'

const resp: DroplistResponse = {
  listName: 'mikroview-drops',
  entries: [],
  key: { present: false },
  ownRangesKnown: true,
  setup: {
    scheduler: '/system scheduler add name=mikroview-drop-pull',
    rule: '/ip firewall filter add chain=forward action=drop',
    disableRule: '/ip firewall filter disable [find]',
    emptyList: '/ip firewall address-list remove [find]',
  },
}

async function openSetupAndCopyRule(): Promise<HTMLElement> {
  render(Droplist, { props: { resp, onrefresh: vi.fn() } })
  await fireEvent.click(screen.getByRole('button', { name: 'setup ▸' }))
  const btns = screen.getAllByRole('button', { name: 'copy' })
  const btn = btns[1] // drop rule
  await fireEvent.click(btn)
  await waitFor(() => expect(copyToClipboard).toHaveBeenCalled())
  await Promise.resolve()
  flushSync()
  return btn
}

describe('Droplist copy label flip (#1542 C12)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    toastState.message = null
    vi.mocked(fetchSetupCommands).mockResolvedValue({
      routeros: { minimum: '7.18', newest: '7.24.4', rows: [], upgrades: [] },
      picked: null,
      routers: [],
      steps: {},
    } as never)
  })

  it('shows "copy failed" on the clicked link after a failed copy', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(false)
    const btn = await openSetupAndCopyRule()
    expect(btn.textContent?.trim()).toBe('copy failed')
  })

  it('shows "copied" on the clicked link after a successful copy, with no toast', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(true)
    const btn = await openSetupAndCopyRule()
    expect(btn.textContent?.trim()).toBe('copied')
    expect(toastState.message).toBeNull()
  })
})
