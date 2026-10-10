// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 review U8: the store half. usersState.remove / resetPassword /
// clearFactor / clearPasskeys take the calling admin's own password as a
// second argument and hand it straight to api.ts. usersState is a
// module-level singleton, so it must keep no copy of the password.
// Written before the fix.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { UserSummary } from './types'

vi.mock('./api', () => ({
  USER_DELETED_TOKENS_REMAIN: 'deleted, tokens remain',
  INCORRECT_PASSWORD: 'incorrect password',
  clearUserPasskeys: vi.fn(),
  clearUserTOTP: vi.fn(),
  createUser: vi.fn(),
  deleteUser: vi.fn(),
  fetchUsers: vi.fn(),
  resetUserPassword: vi.fn(),
}))

import { clearUserPasskeys, clearUserTOTP, deleteUser, fetchUsers, resetUserPassword } from './api'
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

const issued = { username: 'bob', code: 'ABCD-EFGH-JKLM-NPQR', expiresAt: '2026-10-11T00:00:00Z' }

beforeEach(() => {
  vi.resetAllMocks()
  usersState.list = [user()]
})

describe('the store passes the admin password through (#1202 review U8)', () => {
  it('remove(id, password) calls deleteUser(id, password) and drops the row', async () => {
    vi.mocked(deleteUser).mockResolvedValue(null)
    expect(await usersState.remove('id-1', 'hunter2')).toBeNull()
    expect(deleteUser).toHaveBeenCalledWith('id-1', 'hunter2')
    expect(usersState.list).toHaveLength(0)
  })

  it('resetPassword(id, password) calls resetUserPassword(id, password) and returns the code', async () => {
    vi.mocked(resetUserPassword).mockResolvedValue(issued)
    vi.mocked(fetchUsers).mockResolvedValue([user()])
    expect(await usersState.resetPassword('id-1', 'hunter2')).toEqual(issued)
    expect(resetUserPassword).toHaveBeenCalledWith('id-1', 'hunter2')
  })

  it('clearFactor(id, password) calls clearUserTOTP(id, password)', async () => {
    vi.mocked(clearUserTOTP).mockResolvedValue(null)
    vi.mocked(fetchUsers).mockResolvedValue([user()])
    expect(await usersState.clearFactor('id-1', 'hunter2')).toBeNull()
    expect(clearUserTOTP).toHaveBeenCalledWith('id-1', 'hunter2')
  })

  it('clearPasskeys(id, password) calls clearUserPasskeys(id, password)', async () => {
    vi.mocked(clearUserPasskeys).mockResolvedValue(null)
    vi.mocked(fetchUsers).mockResolvedValue([user()])
    expect(await usersState.clearPasskeys('id-1', 'hunter2')).toBeNull()
    expect(clearUserPasskeys).toHaveBeenCalledWith('id-1', 'hunter2')
  })

  it('keeps no copy of the password on the store after any of the four calls', async () => {
    vi.mocked(deleteUser).mockResolvedValue(null)
    vi.mocked(resetUserPassword).mockResolvedValue(issued)
    vi.mocked(clearUserTOTP).mockResolvedValue(null)
    vi.mocked(clearUserPasskeys).mockResolvedValue(null)
    vi.mocked(fetchUsers).mockResolvedValue([user()])

    await usersState.resetPassword('id-1', 'sw0rdfish-secret')
    await usersState.clearFactor('id-1', 'sw0rdfish-secret')
    await usersState.clearPasskeys('id-1', 'sw0rdfish-secret')
    await usersState.remove('id-1', 'sw0rdfish-secret')

    expect(JSON.stringify(usersState)).not.toContain('sw0rdfish-secret')
  })
})

// Pairs: a refusal (wrong password, rate limit, conflict) is handed back as
// text, the row stays, and the list is not refreshed.
describe('the store hands a refusal back and changes nothing (#1202 review U8)', () => {
  it.each(['incorrect password', 'too many attempts, try again later', 'the last admin cannot be changed'])(
    'remove keeps the row when refused with "%s"',
    async (reason) => {
      vi.mocked(deleteUser).mockResolvedValue(reason)
      expect(await usersState.remove('id-1', 'wrong')).toBe(reason)
      expect(usersState.list).toHaveLength(1)
    },
  )

  it('resetPassword returns a wrong-password refusal as text and does not refresh', async () => {
    vi.mocked(resetUserPassword).mockResolvedValue('incorrect password')
    expect(await usersState.resetPassword('id-1', 'wrong')).toBe('incorrect password')
    expect(fetchUsers).not.toHaveBeenCalled()
  })

  it('clearFactor returns a wrong-password refusal as text and does not refresh', async () => {
    vi.mocked(clearUserTOTP).mockResolvedValue('incorrect password')
    expect(await usersState.clearFactor('id-1', 'wrong')).toBe('incorrect password')
    expect(fetchUsers).not.toHaveBeenCalled()
  })

  it('clearPasskeys returns a wrong-password refusal as text and does not refresh', async () => {
    vi.mocked(clearUserPasskeys).mockResolvedValue('incorrect password')
    expect(await usersState.clearPasskeys('id-1', 'wrong')).toBe('incorrect password')
    expect(fetchUsers).not.toHaveBeenCalled()
  })
})
