// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { UserSummary } from './types'

vi.mock('./api', () => ({
  createUser: vi.fn(),
  deleteUser: vi.fn(),
  fetchUsers: vi.fn(),
}))

import { createUser, deleteUser, fetchUsers } from './api'
import { usersState } from './users.svelte'

function user(overrides: Partial<UserSummary> = {}): UserSummary {
  return {
    id: 'id-1',
    username: 'bob',
    role: 'user',
    createdAt: '2026-08-01T00:00:00Z',
    hasLocalPassword: true,
    sso: false,
    ...overrides,
  }
}

beforeEach(() => {
  vi.resetAllMocks()
  usersState.list = []
})

describe('UsersState.create', () => {
  // The list has to reflect the new account without the operator
  // reopening the panel -- otherwise "Add" looks like it did nothing.
  it('refreshes the list after a successful create', async () => {
    vi.mocked(createUser).mockResolvedValue(null)
    vi.mocked(fetchUsers).mockResolvedValue([user(), user({ id: 'id-2', username: 'carol' })])

    const result = await usersState.create('carol', 'password456')

    expect(result).toBeNull()
    expect(createUser).toHaveBeenCalledWith('carol', 'password456')
    expect(usersState.list).toHaveLength(2)
  })

  it('surfaces the error and does not refresh when the create is refused', async () => {
    vi.mocked(createUser).mockResolvedValue('username already exists')

    const result = await usersState.create('bob', 'password456')

    expect(result).toBe('username already exists')
    expect(fetchUsers).not.toHaveBeenCalled()
  })
})

describe('UsersState.remove', () => {
  it('drops the row once the server confirms', async () => {
    usersState.list = [user(), user({ id: 'id-2', username: 'carol' })]
    vi.mocked(deleteUser).mockResolvedValue(null)

    const result = await usersState.remove('id-1')

    expect(result).toBeNull()
    expect(usersState.list.map((u) => u.id)).toEqual(['id-2'])
  })

  // A refused delete -- the admin account, most likely -- must leave the
  // row exactly where it was. Removing it optimistically would show the
  // account as gone when it is still very much there.
  it('keeps the row when the delete is refused', async () => {
    usersState.list = [user({ id: 'id-1', username: 'alice', role: 'admin' })]
    vi.mocked(deleteUser).mockResolvedValue('the admin account cannot be deleted')

    const result = await usersState.remove('id-1')

    expect(result).toBe('the admin account cannot be deleted')
    expect(usersState.list).toHaveLength(1)
  })
})
