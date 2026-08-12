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

  async create(name: string, kind: 'api' | 'ingest', device?: string): Promise<string | null> {
    const result = await createToken(name, kind, device)
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
}

export const tokensState = new TokensState()
