// SPDX-License-Identifier: AGPL-3.0-only
//
// What the watchers station's scope add boxes suggest (#787): hosts from
// Entities, rule labels from the router-pushed filter tables. Both are
// things the app already knows, which is the point -- an operator
// restricting a detector to a host should not have to go and read the IP
// off another page and retype it.
//
// Suggestions only. Every add box still accepts a value that is not in
// the list: a CIDR nobody has named, a rule label on a router that has
// not pushed yet. A datalist that refused unknown values would make the
// editor narrower than the API it drives.
//
// Nothing here contacts a router. The rule labels come from tables the
// router's own scheduled script pushed to mikroview (#186), which is the
// only place they can come from -- see AGENTS.md's "mikroview observes;
// it never scans or connects".

import { fetchEntities, fetchRouterRules } from './api'
import type { Device } from './types'

// The fixed set the classification axis accepts (internal/store.Scope).
// Not fetched: it is a closed set in the engine, and asking the server
// for three constants would be a request that can only ever have one
// answer.
export const CLASSIFICATIONS = ['internal', 'external'] as const

class ScopeSuggestionsState {
  // Host keys an operator has named in Entities, with their labels for
  // display. Sorted by key so the list reads the same on every open.
  hosts = $state<Array<{ key: string; label: string }>>([])
  // Distinct non-empty log prefixes across every pushed filter table --
  // the rule labels a scope's `rules` axis matches on.
  rules = $state<string[]>([])
  loaded = $state(false)

  // refresh is best-effort on purpose. A viewer cannot read Entities
  // (user-tier, see internal/api's handleEntitiesList) and a deployment
  // whose routers have never pushed has no rule tables at all -- neither
  // is an error worth putting in front of an operator who is editing a
  // threshold, so a failed half simply suggests nothing and the add box
  // still accepts anything typed into it.
  async refresh(devices: Device[]): Promise<void> {
    const [hosts, rules] = await Promise.all([this.loadHosts(), this.loadRules(devices)])
    this.hosts = hosts
    this.rules = rules
    this.loaded = true
  }

  private async loadHosts(): Promise<Array<{ key: string; label: string }>> {
    try {
      const entities = await fetchEntities()
      return entities
        .filter((e) => e.type === 'host' && e.key.length > 0)
        .map((e) => ({ key: e.key, label: e.label ?? '' }))
        .sort((a, b) => a.key.localeCompare(b.key))
    } catch {
      return []
    }
  }

  private async loadRules(devices: Device[]): Promise<string[]> {
    const seen = new Set<string>()
    const tables = await Promise.all(
      devices.map(async (d) => {
        try {
          return await fetchRouterRules(d.id)
        } catch {
          return null
        }
      }),
    )
    for (const table of tables) {
      if (!table?.available) continue
      for (const rule of table.rules ?? []) {
        const prefix = (rule.logPrefix ?? '').trim()
        if (prefix.length > 0) seen.add(prefix)
      }
    }
    return [...seen].sort((a, b) => a.localeCompare(b))
  }
}

export const scopeSuggestionsState = new ScopeSuggestionsState()
