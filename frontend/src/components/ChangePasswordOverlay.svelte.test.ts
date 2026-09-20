// SPDX-License-Identifier: AGPL-3.0-only
//
// #1182: neither the Change password overlay nor About took keyboard
// focus when it opened, and neither trapped Tab -- focus stayed on the
// page body and Tab walked the page behind the dialog, which left the
// password fields unreachable by keyboard. Both now use the same
// trapFocus action the setup wizard's modal already uses. Covers that
// and nothing else about either overlay; the wider focus-trap behaviour
// is lib/focusTrap.test.ts's.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  changePassword: vi.fn(async () => null),
  fetchVersion: vi.fn(async () => ({ version: 'v0.0.0-test', commit: 'abc', builtAt: '' })),
}))

import { authState } from '../lib/auth.svelte'
import ChangePasswordOverlay from './ChangePasswordOverlay.svelte'
import AboutOverlay from './AboutOverlay.svelte'

beforeEach(() => {
  cleanup()
  authState.showChangePassword = false
})

describe('the Change password overlay takes and keeps keyboard focus (#1182)', () => {
  it('moves focus into the dialog when it opens', async () => {
    authState.showChangePassword = true
    const { container } = render(ChangePasswordOverlay)

    const dialog = container.querySelector<HTMLElement>('[role="dialog"]')!
    expect(dialog.contains(document.activeElement)).toBe(true)
    expect(document.activeElement).not.toBe(document.body)
  })

  it('wraps Tab from the last control back into the dialog', async () => {
    authState.showChangePassword = true
    const { container } = render(ChangePasswordOverlay)

    const dialog = container.querySelector<HTMLElement>('[role="dialog"]')!
    const focusable = dialog.querySelectorAll<HTMLElement>('button, input')
    const last = focusable[focusable.length - 1]
    last.focus()
    await fireEvent.keyDown(last, { key: 'Tab' })

    expect(dialog.contains(document.activeElement)).toBe(true)
    expect(document.activeElement).toBe(focusable[0])
  })
})

describe('the About overlay takes and keeps keyboard focus (#1182)', () => {
  it('moves focus into the dialog when it opens', async () => {
    const { container } = render(AboutOverlay, { open: true })

    const dialog = container.querySelector<HTMLElement>('[role="dialog"]')!
    expect(dialog.contains(document.activeElement)).toBe(true)
  })

  it('wraps Shift+Tab from the first control back to the last', async () => {
    const { container } = render(AboutOverlay, { open: true })

    const dialog = container.querySelector<HTMLElement>('[role="dialog"]')!
    const focusable = dialog.querySelectorAll<HTMLElement>('a[href], button')
    const first = focusable[0]
    first.focus()
    await fireEvent.keyDown(first, { key: 'Tab', shiftKey: true })

    expect(document.activeElement).toBe(focusable[focusable.length - 1])
  })
})
