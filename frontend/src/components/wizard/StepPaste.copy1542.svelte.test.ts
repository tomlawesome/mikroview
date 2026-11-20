// SPDX-License-Identifier: AGPL-3.0-only
//
// #1542 (C12): the wizard's paste step "Copy" flips its own label to
// "copied" / "copy failed" and keeps its ledger line (wizardRun.copied /
// copiedAt, which the ledger reads). No toast.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync, tick } from 'svelte'

vi.hoisted(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia
})

vi.mock('../../lib/clipboard', () => ({ copyToClipboard: vi.fn() }))

import { copyToClipboard } from '../../lib/clipboard'
import { toastState } from '../../lib/toast.svelte'
import { wizardState } from '../../lib/wizard.svelte'
import { wizardRun } from '../../lib/wizardRun.svelte'
import StepPaste from './StepPaste.svelte'

async function clickCopy(): Promise<HTMLElement> {
  wizardRun.reset()
  wizardRun.name = 'rb5009'
  wizardRun.addr = '192.0.2.1'
  wizardRun.stage = 'paste'
  wizardState.commands = {
    steps: {
      caTrust: { commands: '/certificate import', note: '' },
      syslog: { commands: '/log info "mikroview-enrol demo0000demo0000demo"', note: '' },
      schedule: { commands: '', note: '' },
      backup: { commands: '', note: '' },
      backupSchedule: { commands: '', note: '' },
    },
  } as never
  wizardState.enrolment = { token: 'demo0000demo0000demo', expiresAt: '2026-09-27T14:17:00Z' }
  render(StepPaste)
  await tick()
  const btn = screen.getByRole('button', { name: 'Copy' })
  await fireEvent.click(btn)
  await tick()
  flushSync()
  return btn
}

describe('StepPaste copy label flip (#1542 C12)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    toastState.message = null
  })

  it('flips its own label to "copied" and still records the ledger line', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(true)
    const btn = await clickCopy()
    expect(btn.textContent?.trim()).toBe('copied')
    expect(wizardRun.copiedAt).not.toBe('')
  })

  it('flips its own label to "copy failed" when the copy fails', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(false)
    const btn = await clickCopy()
    expect(btn.textContent?.trim()).toBe('copy failed')
  })

  it('raises no toast', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(true)
    await clickCopy()
    expect(toastState.message).toBeNull()
  })
})
