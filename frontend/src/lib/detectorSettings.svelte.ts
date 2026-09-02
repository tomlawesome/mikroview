// SPDX-License-Identifier: AGPL-3.0-only

import {
  cloneDefinition,
  fetchDefinitionSchema,
  fetchDefinitions,
  resetDefinition,
  updateDefinition,
  type DefinitionUpdate,
} from './api'
import type { DefinitionParamSchema, DetectorScope, DetectorSettings } from './types'

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

  // Every param schema this deployment declares, keyed by definition id
  // (#787). The editor renders its typed fields from *this*, not from the
  // paramSchema copy that also rides on each row above: one source, so a
  // control and the validation behind it cannot come to disagree. The row
  // copy stays because #677's port-scan window row already reads it.
  schema = $state<Record<string, DefinitionParamSchema[]>>({})

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
        origin: d.provenance?.origin ?? 'shipped',
        overridden: Object.keys(d.distance ?? {}).length > 0,
      }))
  }

  // refreshSchema is separate from refresh, and failure is survivable: the
  // schema endpoint is user-tier while the list is open to a viewer too
  // (internal/api's handleDefinitionsSchema and handleDefinitionsList), so
  // a viewer's fetch answers 403. A viewer has no editing panel to render
  // fields into, so the honest result is an empty schema map rather than
  // an error thrown across a page that was only ever going to show facts.
  async refreshSchema() {
    try {
      this.schema = await fetchDefinitionSchema()
    } catch {
      this.schema = {}
    }
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

  // edit is the editing panel's one write (#787): a definition's name,
  // tuning params and scope go up in a single PUT, because they are one
  // Save press and one definition. Sending them as three requests would
  // let a rejected threshold land after an accepted scope, leaving the
  // panel showing an edit that half-happened.
  //
  // Absent fields mean "leave this alone" server-side (see
  // DefinitionUpdate), so a panel with no name field never clears a name.
  async edit(name: string, update: DefinitionUpdate): Promise<string | null> {
    const result = await updateDefinition(name, update)
    if (typeof result === 'string') return result
    await this.refresh()
    return null
  }

  // reset discards every param override in one call, putting a shipped
  // definition back to exactly what it shipped with. Scope is untouched:
  // the server resets params only (handleDefinitionsReset), and a button
  // labelled "reset" that silently also cleared an operator's host
  // exclusions would be doing something nobody asked it to.
  async reset(name: string): Promise<string | null> {
    const result = await resetDefinition(name)
    if (typeof result === 'string') return result
    await this.refresh()
    return null
  }

  // clone asks the server for a copy under a new name and returns the new
  // definition's id, so the bench can open the copy's panel on it.
  //
  // The server refuses this for a definition whose logic is compiled in
  // rather than stored as data, and says why (handleDefinitionsClone). The
  // refusal is returned verbatim rather than reworded: it names the
  // operation that does exist for such a definition, which a generic
  // "clone failed" would throw away.
  async clone(name: string, cloneAs: string): Promise<{ id: string } | string> {
    const result = await cloneDefinition(name, cloneAs)
    if (typeof result === 'string') return result
    await this.refresh()
    // The schema map is keyed by definition id, and the copy has an id
    // nothing has ever asked about (#810). Without this, the panel that
    // opens on the copy a moment later would render no tuning fields for
    // a detector that does declare threshold and window.
    await this.refreshSchema()
    return { id: result.id }
  }
}

export const detectorSettingsState = new DetectorSettingsState()
