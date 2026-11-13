// SPDX-License-Identifier: AGPL-3.0-only

import { createToken, fetchTokens, revokeToken } from './api'
import type { ApiToken } from './types'

// Admin-only management of API bearer tokens -- read-only (issue #101)
// and RouterOS ingest (#186/#326) --
// its own small state module, matching flags.svelte.ts's pattern rather
// than folding this into authState.
class TokensState {
  list = $state<ApiToken[]>([])
  // Set to the freshly created token immediately after create() -- the
  // one and only moment its raw `value` is ever available client-side;
  // TokensOverlay shows a copy-once banner for it, then it's discarded
  // (see clearJustCreated). A refresh() afterward would never bring it
  // back, since the server itself never retains the raw value either.
  justCreated = $state<ApiToken | null>(null)

  async refresh() {
    this.list = await fetchTokens()
  }

  // password is the calling admin's own (#1202): a token outlives the
  // session that minted it, so every mint re-asks for it.
  async create(
    name: string,
    kind: 'api' | 'ingest',
    device: string | undefined,
    password: string,
  ): Promise<string | null> {
    const result = await createToken(name, kind, device, password)
    if (typeof result === 'string') return result
    this.justCreated = result
    await this.refresh()
    return null
  }

  clearJustCreated() {
    this.justCreated = null
  }

  async revoke(id: string): Promise<string | null> {
    const err = await revokeToken(id)
    if (err) return err
    this.list = this.list.filter((t) => t.id !== id)
    if (this.justCreated?.id === id) this.justCreated = null
    return null
  }

  // #1083, v0.6.0 pre-release audit Security stage: tokensState was
  // missed from the original batch. `justCreated` is a raw, live bearer
  // token -- TokensOverlay's copy-once banner keeps rendering it after
  // logout, so it must be gone before the next admin signs in on this
  // tab, not just cleared the one way clearJustCreated() already
  // covered.
  reset() {
    this.list = []
    this.justCreated = null
  }
}

export const tokensState = new TokensState()
