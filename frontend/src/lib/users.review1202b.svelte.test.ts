// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 second review U12 (note_34147), the store half. deleteUser answers
// USER_DELETED_TOKENS_REMAIN when gauntlet says 500 #partially-completed (the
// account is deleted, its tokens were not revoked). usersState.remove then
// refreshes the list from the server, so the row goes, and hands the message
// back for the screen. Written before the fix.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { UserSummary } from './types'

vi.mock('./api', () => ({
  INCORRECT_PASSWORD: 'incorrect password',
  USER_DELETED_TOKENS_REMAIN: 'kai was deleted, but their tokens were not revoked: revoke that account\'s tokens by hand',
  clearUserPasskeys: vi.fn(),
  clearUserTOTP: vi.fn(),
  createUser: vi.fn(),
  deleteUser: vi.fn(),
  fetchUsers: vi.fn(),
  resetUserPassword: vi.fn(),
}))

import { deleteUser, fetchUsers, USER_DELETED_TOKENS_REMAIN } from './api'
import { usersState } from './users.svelte'

function user(id: string, username: string): UserSummary {
  return { id, username, role: 'user', createdAt: '2026-08-01T00:00:00Z', hasLocalPassword: true, sso: false }
}

beforeEach(() => {
  vi.resetAllMocks()
  usersState.list = [user('u1', 'alex'), user('u2', 'kai')]
})

describe('removing an account whose tokens could not be revoked (#1202 review U12)', () => {
  // #1202 review U12
  it('refreshes the list so the row goes, and hands the message back', async () => {
    vi.mocked(deleteUser).mockResolvedValue(USER_DELETED_TOKENS_REMAIN)
    vi.mocked(fetchUsers).mockResolvedValue([user('u1', 'alex')])

    const result = await usersState.remove('u2', 'hunter2')

    expect(deleteUser).toHaveBeenCalledWith('u2', 'hunter2')
    expect(fetchUsers).toHaveBeenCalled()
    expect(usersState.list.map((u) => u.username)).toEqual(['alex'])
    expect(result).toBe(USER_DELETED_TOKENS_REMAIN)
  })

  // #1202 review U12: pair -- an ordinary refusal keeps the row and does not refresh.
  it('a plain refusal keeps the row, does not refresh, and is not the tokens message', async () => {
    vi.mocked(deleteUser).mockResolvedValue('the account could not be deleted')

    const result = await usersState.remove('u2', 'hunter2')

    expect(result).toBe('the account could not be deleted')
    expect(result).not.toBe(USER_DELETED_TOKENS_REMAIN)
    expect(fetchUsers).not.toHaveBeenCalled()
    expect(usersState.list.map((u) => u.username)).toEqual(['alex', 'kai'])
  })

  // #1202 review U12: pair -- plain success still answers null.
  it('plain success answers null and drops the row', async () => {
    vi.mocked(deleteUser).mockResolvedValue(null)

    expect(await usersState.remove('u2', 'hunter2')).toBeNull()
    expect(usersState.list.map((u) => u.username)).toEqual(['alex'])
  })
})
