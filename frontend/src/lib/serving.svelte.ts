// SPDX-License-Identifier: AGPL-3.0-only
//
// The "seen serving" lens's own state (#1320): whether it is on, and
// what the server answered. Same split as portFilter.svelte.ts next
// door: serving.ts holds the arithmetic and the wording, this holds the
// fetched answer.
//
// Unlike the port filter there is no picker: the lens has nothing to
// narrow, only to turn on, so `open` here means the whole thing --
// there is no separate "active" once settled.
import { fetchServing, type ServingHost, type ServingResponse } from './api'

const EMPTY: ServingResponse = { generatedAt: 0, windowSeconds: 0, hosts: [] }

class ServingState {
  /** Whether the lens is on. */
  open = $state(false)

  /**
   * The server's answer. Empty until a fetch lands, and empty again if
   * one fails -- the same choice portFilterState.refresh makes, for the
   * same reason: a transient error dims nothing rather than filtering to
   * a stale answer nobody asked for.
   */
  answer = $state<ServingResponse>(EMPTY)

  /** True once the answer in hand belongs to this bout of being on. */
  settled = $state(false)

  private fetchId = 0

  /** The addresses the lens lights, for the dots and the lane tallies. */
  hosts = $derived(new Set(this.answer.hosts.map((h) => h.ip)))

  /** One host's own served-port list, for its dot's title and the
   * reach's chip row. */
  byIp = $derived(new Map(this.answer.hosts.map((h) => [h.ip, h] as [string, ServingHost])))

  /** True once the lens is on, settled, and nothing answered. */
  nothingSeen = $derived(this.open && this.settled && this.answer.hosts.length === 0)

  /** Turns the lens on and reads the window; a no-op while already on. */
  async turnOn() {
    if (this.open) return
    this.open = true
    this.settled = false
    await this.refresh()
  }

  /** Turns the lens off entirely -- the pill's own click and the mutual
   * clear with the port filter both land here. */
  clear() {
    this.open = false
    this.settled = false
    this.answer = EMPTY
  }

  async refresh() {
    const id = ++this.fetchId
    try {
      const res = await fetchServing()
      if (id !== this.fetchId) return
      this.answer = res
      this.settled = true
    } catch {
      if (id !== this.fetchId) return
      // Deliberately not settled: an unread answer is not an answer of
      // zero hosts. Marking it settled would put "nothing answered in
      // the window" on the map when all that happened was a dropped
      // request.
      this.answer = EMPTY
      this.settled = false
    }
  }
}

export const servingState = new ServingState()
