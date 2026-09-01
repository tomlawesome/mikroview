// SPDX-License-Identifier: AGPL-3.0-only

import { fetchDefinitions, updateDefinition } from './api'
import type { DetectorScope, DetectorSettings } from './types'

// Live per-detector on/off + scope settings -- admin-only, mirrors
// flags.svelte.ts's shape.
//
// Sourced from GET /api/definitions since issue #407 replaced
// /api/detectors wholesale, and projected down to what this page needs.
// The projection is deliberately in this one place rather than in the
// component: the detector page is being replaced in phase 2, and keeping
// its own shape stable here is what makes that a page rewrite rather
// than a rewiring of everything that touches it.
//
// Every *detection* definition is listed, not a frozen subset. Five of
// them (unexpected_mail_sender, stale_rule, known_bad_ip, netclass,
// reputation) were always-on passes with no toggle at all before the
// engine port gave them an envelope, and they appear here for the first
// time -- a definition that is evaluating but cannot be seen or switched
// off is exactly the coverage question this page exists to answer.
//
// An unavailable definition (one this binary cannot identify -- see
// StoredDefinition.Available) is skipped: it is preserved server-side
// and never evaluated, so offering a toggle for it would suggest an
// effect it cannot have.
class DetectorSettingsState {
  list = $state<DetectorSettings[]>([])

  async refresh() {
    const { definitions } = await fetchDefinitions()
    this.list = definitions
      .filter((d) => d.intent === 'detection' && d.available)
      .map((d) => ({
        name: d.id,
        label: d.name,
        description: d.description,
        enabled: d.enabled,
        scope: d.scope ?? {},
        learning: d.learning,
        params: d.params,
        paramSchema: d.paramSchema,
      }))
  }

  async update(name: string, enabled: boolean, scope: DetectorScope): Promise<string | null> {
    const result = await updateDefinition(name, { enabled, scope })
    if (typeof result === 'string') return result
    await this.refresh()
    return null
  }

  // updateParams writes a definition's numeric tuning (#677's port-scan
  // window row: threshold/window) through the same PUT /api/definitions/
  // {id} the bench's enabled/scope editing above already uses --
  // deliberately not a second store or endpoint, since these are the
  // same underlying definition. The server validates against the
  // definition's own paramSchema (engine.DefinitionsStore.SetParams),
  // so an out-of-range value comes back as the returned error string
  // rather than silently clamping.
  async updateParams(name: string, params: Record<string, unknown>): Promise<string | null> {
    const result = await updateDefinition(name, { params })
    if (typeof result === 'string') return result
    await this.refresh()
    return null
  }
}

export const detectorSettingsState = new DetectorSettingsState()
