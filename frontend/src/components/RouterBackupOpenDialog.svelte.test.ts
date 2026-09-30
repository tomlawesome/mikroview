// SPDX-License-Identifier: AGPL-3.0-only
//
// The one dialog that opens the router-backup drop box (#1361), shared
// by Settings → router backups and the wizard's Back up nightly choice:
// the password (owner's ruling 3a), the trust caveat, the HTTPS
// alternative, the refusal that keeps it open, and the state handed up
// on success -- read from the server's answer, not from what was asked.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  setRouterBackupSwitch: vi.fn(),
}))

import { setRouterBackupSwitch } from '../lib/api'
import RouterBackupOpenDialog from './RouterBackupOpenDialog.svelte'

describe('RouterBackupOpenDialog (#1361)', () => {
  beforeEach(() => {
    vi.mocked(setRouterBackupSwitch).mockReset()
  })

  it('asks for the password and carries the trust caveat and the HTTPS alternative, calling nothing yet', () => {
    render(RouterBackupOpenDialog, { props: { onopened: vi.fn(), oncancel: vi.fn() } })
    expect(screen.getByLabelText('your password')).toBeTruthy()
    expect(screen.getByText(/RouterOS never checks who it is sending to/)).toBeTruthy()
    expect(screen.getByText(/HTTPS-only alternative/)).toBeTruthy()
    // Nothing to open with until a password is typed.
    expect((screen.getByRole('button', { name: 'open' }) as HTMLButtonElement).disabled).toBe(true)
    expect(vi.mocked(setRouterBackupSwitch)).not.toHaveBeenCalled()
  })

  it('wears the host it is drawn in', () => {
    const { unmount } = render(RouterBackupOpenDialog, { props: { onopened: vi.fn(), oncancel: vi.fn() } })
    expect(document.querySelector('.dbox.settings')).toBeTruthy()
    unmount()
    render(RouterBackupOpenDialog, { props: { look: 'wizard', onopened: vi.fn(), oncancel: vi.fn() } })
    expect(document.querySelector('.dbox.wizard')).toBeTruthy()
  })

  it('cancel tells the host and calls nothing', async () => {
    const oncancel = vi.fn()
    render(RouterBackupOpenDialog, { props: { onopened: vi.fn(), oncancel } })
    await fireEvent.click(screen.getByRole('button', { name: 'cancel' }))
    expect(oncancel).toHaveBeenCalledTimes(1)
    expect(vi.mocked(setRouterBackupSwitch)).not.toHaveBeenCalled()
  })

  it('opens with the password and hands the server-stated state up', async () => {
    vi.mocked(setRouterBackupSwitch).mockResolvedValue({ open: true, port: '47022' })
    const onopened = vi.fn()
    render(RouterBackupOpenDialog, { props: { onopened, oncancel: vi.fn() } })
    await fireEvent.input(screen.getByLabelText('your password'), { target: { value: 'hunter2' } })
    await fireEvent.click(screen.getByRole('button', { name: 'open' }))
    await waitFor(() => expect(onopened).toHaveBeenCalledWith({ open: true, port: '47022' }))
    expect(vi.mocked(setRouterBackupSwitch)).toHaveBeenCalledWith(true, 'hunter2')
  })

  it('Enter in the field opens too', async () => {
    vi.mocked(setRouterBackupSwitch).mockResolvedValue({ open: true, port: '47022' })
    const onopened = vi.fn()
    render(RouterBackupOpenDialog, { props: { onopened, oncancel: vi.fn() } })
    const field = screen.getByLabelText('your password')
    await fireEvent.input(field, { target: { value: 'hunter2' } })
    await fireEvent.keyDown(field, { key: 'Enter' })
    await waitFor(() => expect(onopened).toHaveBeenCalledTimes(1))
  })

  it("a refusal shows the server's words and stays open, handing nothing up", async () => {
    vi.mocked(setRouterBackupSwitch).mockResolvedValue('incorrect password')
    const onopened = vi.fn()
    render(RouterBackupOpenDialog, { props: { onopened, oncancel: vi.fn() } })
    await fireEvent.input(screen.getByLabelText('your password'), { target: { value: 'wrong' } })
    await fireEvent.click(screen.getByRole('button', { name: 'open' }))
    await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('incorrect password'))
    expect(screen.getByLabelText('your password')).toBeTruthy()
    expect(onopened).not.toHaveBeenCalled()
  })
})
