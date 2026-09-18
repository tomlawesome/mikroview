// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { UserSummary } from './types'

vi.mock('./api', () => ({
  createUser: vi.fn(),
  deleteUser: vi.fn(),
  fetchUsers: vi.fn(),
  resetUserPassword: vi.fn(),
}))

import { createUser, deleteUser, fetchUsers, resetUserPassword } from './api'
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
    expect(createUser).toHaveBeenCalledWith('carol', 'password456', 'user')
    expect(usersState.list).toHaveLength(2)
  })

  it('passes the viewer tier through when asked for one (#653)', async () => {
    vi.mocked(createUser).mockResolvedValue(null)
    vi.mocked(fetchUsers).mockResolvedValue([user()])

    const result = await usersState.create('dana', 'password456', 'viewer')

    expect(result).toBeNull()
    expect(createUser).toHaveBeenCalledWith('dana', 'password456', 'viewer')
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


// #1251. The code comes back once and is handed straight to the caller:
// usersState is a module-level singleton, and a credential parked on it
// would outlive the dialog that showed it for the rest of the session.
describe('UsersState.resetPassword', () => {
  const issued = {
    username: 'bob',
    code: 'ABCD-EFGH-JKLM-NPQR',
    expiresAt: '2026-09-19T00:00:00Z',
  }

  it('returns the issued code and refreshes the list', async () => {
    vi.mocked(resetUserPassword).mockResolvedValue(issued)
    vi.mocked(fetchUsers).mockResolvedValue([user()])

    const result = await usersState.resetPassword('id-1')

    expect(result).toEqual(issued)
    expect(resetUserPassword).toHaveBeenCalledWith('id-1')
    expect(fetchUsers).toHaveBeenCalled()
  })

  it('keeps no copy of the code on the store itself', async () => {
    vi.mocked(resetUserPassword).mockResolvedValue(issued)
    vi.mocked(fetchUsers).mockResolvedValue([user()])

    await usersState.resetPassword('id-1')

    expect(JSON.stringify(usersState)).not.toContain(issued.code)
  })

  it('surfaces a refusal and does not refresh', async () => {
    vi.mocked(resetUserPassword).mockResolvedValue(
      'this account signs in through your identity provider',
    )

    const result = await usersState.resetPassword('id-1')

    expect(result).toBe('this account signs in through your identity provider')
    expect(fetchUsers).not.toHaveBeenCalled()
  })
})
