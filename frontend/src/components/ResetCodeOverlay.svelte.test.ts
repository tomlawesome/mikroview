// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

vi.mock('../lib/clipboard', () => ({ copyToClipboard: vi.fn() }))

import { copyToClipboard } from '../lib/clipboard'
import ResetCodeOverlay from './ResetCodeOverlay.svelte'

const reset = {
  username: 'bilbo',
  code: 'ABCD-EFGH-JKLM-NPQR',
  expiresAt: '2026-09-19T00:00:00Z',
}

beforeEach(() => {
  vi.resetAllMocks()
})

describe('ResetCodeOverlay (#1251)', () => {
  it('shows the code grouped, for whoever is about to read it aloud', () => {
    render(ResetCodeOverlay, { props: { reset, onclose: () => {} } })

    expect(screen.getByTestId('reset-code').textContent).toBe('ABCD-EFGH-JKLM-NPQR')
  })

  // The whole delivery mechanism is a person telling another person, so
  // the dialog has to say how, and say that waiting is not an option.
  it('says how to hand it over and when it stops working', () => {
    render(ResetCodeOverlay, { props: { reset, onclose: () => {} } })

    // Collapsed: the sentences wrap in the markup, so the line breaks
    // are formatting rather than anything a reader sees.
    const words = (document.body.textContent ?? '').replace(/\s+/g, ' ')
    expect(words).toContain('in person or over a call you trust')
    expect(words).toContain('24 hours or on first use')
    expect(words).toContain('only time it is shown')
  })

  it('copies the code on request', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(true)

    render(ResetCodeOverlay, { props: { reset, onclose: () => {} } })
    await fireEvent.click(screen.getByRole('button', { name: /copy code/i }))

    expect(copyToClipboard).toHaveBeenCalledWith('ABCD-EFGH-JKLM-NPQR')
    expect(await screen.findByRole('button', { name: /copied/i })).toBeTruthy()
  })

  // A clipboard that refused must not claim otherwise -- the admin would
  // paste nothing into the call they are already on.
  it('does not claim to have copied when the clipboard refused', async () => {
    vi.mocked(copyToClipboard).mockResolvedValue(false)

    render(ResetCodeOverlay, { props: { reset, onclose: () => {} } })
    await fireEvent.click(screen.getByRole('button', { name: /copy code/i }))

    expect(screen.queryByRole('button', { name: /^copied$/i })).toBeNull()
  })

  it('closes on Done and on Escape', async () => {
    const onclose = vi.fn()

    render(ResetCodeOverlay, { props: { reset, onclose } })
    await fireEvent.click(screen.getByRole('button', { name: /done/i }))
    expect(onclose).toHaveBeenCalledTimes(1)

    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onclose).toHaveBeenCalledTimes(2)
  })
})
