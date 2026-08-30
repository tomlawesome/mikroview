// SPDX-License-Identifier: AGPL-3.0-only

import { createUser, deleteUser, fetchUsers } from './api'
import type { UserSummary } from './types'

// Admin-only account management (issue #133) -- its own small state
// module, matching tokens.svelte.ts rather than growing authState, which
// is about the *current* session and is read on every view.
class UsersState {
  list = $state<UserSummary[]>([])

  async refresh() {
    this.list = await fetchUsers()
  }

  // role defaults to 'user' so an existing caller creates exactly the
  // account it always did -- see api.ts's createUser for why admin is
  // not on offer.
  async create(
    username: string,
    password: string,
    role: 'user' | 'viewer' = 'user',
  ): Promise<string | null> {
    const err = await createUser(username, password, role)
    if (err) return err
    await this.refresh()
    return null
  }

  async remove(id: string): Promise<string | null> {
    const err = await deleteUser(id)
    if (err) return err
    // Dropped locally rather than re-fetching: the server has already
    // confirmed, and nothing else in this list can have changed as a
    // result. (The token list can -- deletion revokes that account's
    // tokens -- but TokensOverlay refreshes on open, so it never shows
    // a stale row.)
    this.list = this.list.filter((u) => u.id !== id)
    return null
  }
}

export const usersState = new UsersState()
