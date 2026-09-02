// SPDX-License-Identifier: AGPL-3.0-only
//
// The pure half of the watchers station's in-place editing panel (#787):
// turning a definition's server-declared param schema into typed form
// fields, and turning a scope's four axes into removable chips and back.
//
// Kept out of EngineRoomWatchers.svelte for the same reason
// detectorCopy.ts is: this is data shaping with no rendering in it, and
// the round-tripping (a value read out of a param, edited, and written
// back in the exact representation the server validates) is the part
// worth testing directly rather than through a component.
//
// Nothing here re-lists what any definition's knobs are. The schema
// arrives from GET /api/definitions/schema and this module only decides
// how to *render* what it says -- re-stating the knobs in TypeScript is
// the duplication docs/decisions/evaluation-engine.md section 4 exists to
// remove.

import { parseGoDurationSeconds } from './format'
import type { DefinitionParamSchema, DetectorScope, ListMode } from './types'

// How a param's control is drawn. Deliberately a smaller set than
// ParamType: several declared types render the same control, and the
// panel should not grow a branch per type name.
//
//  - 'number'  -- int and float, a spinner bounded by the schema.
//  - 'seconds' -- duration, edited as a plain second count and written
//                 back as the Go duration string the server normalizes to.
//  - 'bool'    -- a checkbox.
//  - 'enum'    -- a select over the schema's own closed value set.
//  - 'list'    -- portList/hostList/stringList, edited as chips.
export type ParamControl = 'number' | 'seconds' | 'bool' | 'enum' | 'list'

export function controlFor(type: DefinitionParamSchema['type']): ParamControl {
  switch (type) {
    case 'int':
    case 'float':
      return 'number'
    case 'duration':
      return 'seconds'
    case 'bool':
      return 'bool'
    case 'enum':
      return 'enum'
    default:
      return 'list'
  }
}

// One rendered tuning field: the schema entry, the control to draw, the
// bounds already converted into the unit the control edits in, and the
// current value in that same unit.
export interface ParamField {
  schema: DefinitionParamSchema
  control: ParamControl
  label: string
  // Bounds in the control's own unit -- for a duration that is seconds,
  // not the nanoseconds ParamSchema.Min/Max carry (see engine.ParamSchema's
  // doc comment on how bounds are interpreted per type). undefined means
  // the schema declares no bound, which is not the same as a bound of 0.
  min?: number
  max?: number
  value: number | boolean | string | string[]
}

const NANOS_PER_SECOND = 1e9

// paramLabel words a schema name for an operator: the schema's names are
// Go-ish camelCase identifiers ("baselineFloorDuration"), and the panel
// shows sentence-cased words. The description underneath is the server's
// own, never rewritten here.
export function paramLabel(name: string): string {
  const spaced = name
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replace(/[_-]+/g, ' ')
    .trim()
  if (spaced.length === 0) return name
  return spaced[0].toUpperCase() + spaced.slice(1).toLowerCase()
}

// paramFields builds the panel's field list from the schema and the
// definition's current param values. Schema order is preserved: it is the
// order the definition declares its own knobs in, which is a more useful
// order than anything this layer could invent.
export function paramFields(
  schema: DefinitionParamSchema[] | undefined,
  params: Record<string, unknown> | undefined,
): ParamField[] {
  return (schema ?? []).map((s) => {
    const control = controlFor(s.type)
    const raw = params?.[s.name]
    return {
      schema: s,
      control,
      label: paramLabel(s.name),
      min: boundIn(control, s.min),
      max: boundIn(control, s.max),
      value: valueIn(control, raw, s),
    }
  })
}

function boundIn(control: ParamControl, bound: number | undefined): number | undefined {
  if (bound === undefined || bound === null) return undefined
  if (control === 'seconds') return bound / NANOS_PER_SECOND
  return bound
}

function valueIn(
  control: ParamControl,
  raw: unknown,
  s: DefinitionParamSchema,
): number | boolean | string | string[] {
  switch (control) {
    case 'number':
      return typeof raw === 'number' ? raw : (s.min ?? 0)
    case 'seconds':
      return typeof raw === 'string' ? Math.round(parseGoDurationSeconds(raw)) : 0
    case 'bool':
      return raw === true
    case 'enum':
      return typeof raw === 'string' ? raw : (s.enumValues?.[0] ?? '')
    case 'list':
      return Array.isArray(raw) ? raw.map((v) => String(v)) : []
  }
}

// paramsFromFields writes the edited fields back into the wire shape PUT
// /api/definitions/{id} takes. A duration goes back as a Go duration
// string and a portList as numbers, because that is what the server's own
// ValidateParams accepts -- sending the control's editing unit instead
// would be rejected, and coercing it here rather than in the component is
// what keeps that single conversion in one place.
export function paramsFromFields(fields: ParamField[]): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const f of fields) {
    switch (f.control) {
      case 'number':
        out[f.schema.name] = Number(f.value)
        break
      case 'seconds':
        out[f.schema.name] = `${Math.round(Number(f.value))}s`
        break
      case 'bool':
        out[f.schema.name] = f.value === true
        break
      case 'enum':
        out[f.schema.name] = String(f.value)
        break
      case 'list': {
        const items = (f.value as string[]) ?? []
        out[f.schema.name] = f.schema.type === 'portList' ? items.map((v) => Number(v)) : items
        break
      }
    }
  }
  return out
}

// --- scope chips -----------------------------------------------------------

// Which of the four scope axes a chip row edits. classification is not
// among them: it holds one value, not a list (see types.Scope), so it
// stays the select it already was.
export type ChipAxis = 'hosts' | 'ports' | 'rules'

// ScopeDraft is the panel's working copy of a definition's scope: each
// list axis as an array of chip strings (ports included -- they are
// numbers on the wire but strings while being edited), plus the three
// modes and the classification value.
export interface ScopeDraft {
  hosts: string[]
  ports: string[]
  rules: string[]
  hostsMode: ListMode
  portsMode: ListMode
  rulesMode: ListMode
  classification: DetectorScope['classification']
}

export function scopeDraftFrom(scope: DetectorScope | undefined): ScopeDraft {
  const sc = scope ?? {}
  return {
    hosts: [...(sc.hosts ?? [])],
    ports: (sc.ports ?? []).map((p) => String(p)),
    rules: [...(sc.rules ?? [])],
    hostsMode: sc.hostsMode ?? '',
    portsMode: sc.portsMode ?? '',
    rulesMode: sc.rulesMode ?? '',
    classification: sc.classification ?? '',
  }
}

export function scopeFromDraft(d: ScopeDraft): DetectorScope {
  return {
    hosts: [...d.hosts],
    hostsMode: d.hostsMode,
    ports: d.ports.map((p) => Number(p)).filter((n) => Number.isInteger(n) && n > 0),
    portsMode: d.portsMode,
    rules: [...d.rules],
    rulesMode: d.rulesMode,
    classification: d.classification,
  }
}

// A range wide enough to be a typo rather than an intention. RouterOS
// happily accepts "1-65535" in a rule, but expanding one into 65535
// individual scope chips is not editing, it is a wall -- so the add box
// refuses it and says so instead of quietly producing something nobody
// can undo by hand.
const MAX_RANGE_PORTS = 256

// parsePortEntry reads one thing typed into the ports add box: a single
// port ("22"), or an inclusive range ("8000-8010"). Returns the ports it
// expands to, or an error sentence for the panel to show.
//
// Ranges expand rather than being stored as a range, because
// DetectorScope.ports is a number list on the wire (internal/detect.Scope)
// -- there is nowhere to put "8000-8010" that the engine would read.
export function parsePortEntry(text: string): { ports: number[]; error?: string } {
  const raw = text.trim()
  if (raw.length === 0) return { ports: [] }

  const range = raw.match(/^(\d+)\s*-\s*(\d+)$/)
  if (range) {
    const lo = Number(range[1])
    const hi = Number(range[2])
    if (!validPort(lo) || !validPort(hi)) {
      return { ports: [], error: `${raw} is not a port range: ports run from 1 to 65535` }
    }
    if (hi < lo) return { ports: [], error: `${raw} runs backwards: the low port comes first` }
    if (hi - lo + 1 > MAX_RANGE_PORTS) {
      return {
        ports: [],
        error: `${raw} is ${hi - lo + 1} ports; add at most ${MAX_RANGE_PORTS} at a time`,
      }
    }
    const ports: number[] = []
    for (let p = lo; p <= hi; p++) ports.push(p)
    return { ports }
  }

  const one = Number(raw)
  if (!validPort(one)) return { ports: [], error: `${raw} is not a port number` }
  return { ports: [one] }
}

function validPort(n: number): boolean {
  return Number.isInteger(n) && n >= 1 && n <= 65535
}

// addChip appends value to list unless it is already there -- a scope
// axis is a set, and a duplicated chip would render twice and mean once.
// Comparison is on the trimmed string, so "22 " never becomes a second
// port 22.
export function addChip(list: string[], value: string): string[] {
  const v = value.trim()
  if (v.length === 0 || list.includes(v)) return list
  return [...list, v]
}

export function removeChip(list: string[], value: string): string[] {
  return list.filter((v) => v !== value)
}

// cloneName is the name a copy is offered under, matching what the server
// itself appends when a clone request carries no name (see internal/api's
// handleDefinitionsClone) -- so the row the operator sees the instant they
// press clone says the same thing the stored definition will.
export function cloneName(name: string): string {
  return `${name} (copy)`
}
