// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', () => ({
  createUser: vi.fn(),
  deleteUser: vi.fn(),
  fetchUsers: vi.fn(async () => []),
}))

import { createUser, deleteUser, fetchUsers } from '../lib/api'
import { usersState } from '../lib/users.svelte'
import Users from './Users.svelte'

// #548: the page successor to UsersOverlay.svelte -- same create/delete
// behaviour, minus the modal chrome. The overlay itself carried no unit
// test of its own; this is the first one, now that the surface is a
// page reached from the rail rather than a component only ever mounted
// behind a boolean.
describe('Users page (#548)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    usersState.list = []
  })

  it('has a page header titled Users', () => {
    vi.mocked(fetchUsers).mockResolvedValue([])
    render(Users)
    expect(screen.getByRole('heading', { name: 'Users' })).toBeTruthy()
  })

  it('loads the account list on mount', async () => {
    vi.mocked(fetchUsers).mockResolvedValue([
      { id: 'id-1', username: 'carol', role: 'user', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false },
    ])
    render(Users)
    await Promise.resolve()
    await Promise.resolve()
    flushSync()

    expect(screen.getByText('carol')).toBeTruthy()
  })

  it('submits the create form to usersState.create', async () => {
    vi.mocked(fetchUsers).mockResolvedValue([])
    vi.mocked(createUser).mockResolvedValue(null)
    render(Users)
    await Promise.resolve()

    await fireEvent.input(screen.getByPlaceholderText('Username'), { target: { value: 'dave' } })
    await fireEvent.input(screen.getByPlaceholderText('Password'), { target: { value: 'password456' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Add' }))

    expect(createUser).toHaveBeenCalledWith('dave', 'password456')
  })

  it('asks for confirmation before deleting an account, and calls through on confirm', async () => {
    vi.mocked(fetchUsers).mockResolvedValue([
      { id: 'id-2', username: 'erin', role: 'user', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false },
    ])
    vi.mocked(deleteUser).mockResolvedValue(null)
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    render(Users)
    await Promise.resolve()
    await Promise.resolve()
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    expect(confirmSpy).toHaveBeenCalled()
    expect(deleteUser).toHaveBeenCalledWith('id-2')
  })

  it('never offers to delete the admin account', async () => {
    vi.mocked(fetchUsers).mockResolvedValue([
      { id: 'id-admin', username: 'tom', role: 'admin', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false },
    ])
    render(Users)
    await Promise.resolve()
    await Promise.resolve()
    flushSync()

    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(screen.getByText("can't be removed")).toBeTruthy()
  })
})
