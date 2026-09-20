// SPDX-License-Identifier: AGPL-3.0-only

import { createUser, deleteUser, fetchUsers, resetUserPassword } from './api'
import type { PasswordResetCode, UserSummary } from './types'

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

  // resetPassword returns the one-time code on success (#1251) and
  // error text otherwise. The code is handed straight back to the caller
  // and never held here: this module is a long-lived singleton, and a
  // credential that outlives the dialog showing it is a credential
  // sitting in memory for the rest of the session with nothing left to
  // do. The list is refreshed because the reset ends that account's
  // sessions, which changes what its row says.
  async resetPassword(id: string): Promise<PasswordResetCode | string> {
    const result = await resetUserPassword(id)
    if (typeof result === 'string') return result
    await this.refresh()
    return result
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

  // #1083, v0.6.0 pre-release audit Security stage: usersState was
  // missed from the original batch. `list` is the admin account list,
  // rendered by EngineRoom with no role guard of its own -- it must not
  // still be sitting there for whoever signs in next on this tab.
  reset() {
    this.list = []
  }
}

export const usersState = new UsersState()
