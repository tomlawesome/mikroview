// SPDX-License-Identifier: AGPL-3.0-only
//
// The coverage declarations the map's Coverage lens paints from
// (#630/#392's second source): an admin's on-record statement that a
// quiet boundary-direction is quiet on purpose. Derivation cannot know
// intent -- a rule chosen deliberately and one left over from 2019 look
// identical in the table -- so this is the one acknowledgement, stored
// with its reason, never nagged about again.
import {
  deleteCoverageDeclaration,
  fetchCoverageDeclarations,
  putCoverageDeclaration,
  type CoverageDeclaration,
} from './api'

class CoverageState {
  declarations = $state<CoverageDeclaration[]>([])
  /** Last write's failure, shown inline in the declare panel. */
  error = $state<string | null>(null)

  async refresh() {
    try {
      this.declarations = await fetchCoverageDeclarations()
    } catch {
      // Absence reads as "nothing declared", which is also the honest
      // state while the store cannot be read -- dark stays dark.
    }
  }

  byKey = $derived.by(() => new Map(this.declarations.map((d) => [d.key, d])))

  async declare(key: string, reason: string): Promise<boolean> {
    this.error = null
    const res = await putCoverageDeclaration(key, reason)
    if (typeof res === 'string') {
      this.error = res
      return false
    }
    await this.refresh()
    return true
  }

  async undeclare(key: string): Promise<boolean> {
    this.error = null
    const err = await deleteCoverageDeclaration(key)
    if (err) {
      this.error = err
      return false
    }
    await this.refresh()
    return true
  }
}

export const coverageState = new CoverageState()
