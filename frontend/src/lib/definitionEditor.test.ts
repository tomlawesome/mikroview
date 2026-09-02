// SPDX-License-Identifier: AGPL-3.0-only
//
// #787: the round trip the watchers editor depends on. A value read out
// of a definition's params, shown in a control, edited and written back
// has to land in the exact representation internal/engine's
// ValidateParams accepts -- a duration as a Go duration string, a port
// list as numbers -- or the server refuses an edit the operator has no
// way to see was malformed.

import { describe, expect, it } from 'vitest'
import {
  addChip,
  cloneName,
  controlFor,
  paramFields,
  paramLabel,
  parsePortEntry,
  paramsFromFields,
  removeChip,
  scopeDraftFrom,
  scopeFromDraft,
} from './definitionEditor'
import { parseGoDurationSeconds } from './format'
import type { DefinitionParamSchema } from './types'

// The shape GET /api/definitions/schema really returns for port_scan --
// the two params #677's window row already edits, with the nanosecond
// bounds engine.ParamSchema stores a duration bound in.
const PORT_SCAN: DefinitionParamSchema[] = [
  {
    name: 'threshold',
    type: 'int',
    description: 'Distinct destination ports before a source is flagged.',
    min: 2,
    max: 1000,
  },
  {
    name: 'window',
    type: 'duration',
    description: 'How long the distinct-port count is accumulated over.',
    min: 1e9,
    max: 3600e9,
  },
]

describe('controlFor', () => {
  it('draws int and float with the same numeric control', () => {
    expect(controlFor('int')).toBe('number')
    expect(controlFor('float')).toBe('number')
  })

  it('gives every list type a chip/list control rather than a branch each', () => {
    expect(controlFor('portList')).toBe('list')
    expect(controlFor('hostList')).toBe('list')
    expect(controlFor('stringList')).toBe('list')
  })

  it('edits a duration as seconds', () => {
    expect(controlFor('duration')).toBe('seconds')
  })
})

describe('paramLabel', () => {
  it('words a camelCase schema name', () => {
    expect(paramLabel('baselineFloorDuration')).toBe('Baseline floor duration')
    expect(paramLabel('threshold')).toBe('Threshold')
  })
})

describe('paramFields', () => {
  it('builds one field per schema entry, in the schema’s own order', () => {
    const fields = paramFields(PORT_SCAN, { threshold: 15, window: '1m0s' })
    expect(fields.map((f) => f.schema.name)).toEqual(['threshold', 'window'])
  })

  it('reads a duration param into whole seconds', () => {
    const [, window] = paramFields(PORT_SCAN, { threshold: 15, window: '1m0s' })
    expect(window.control).toBe('seconds')
    expect(window.value).toBe(60)
  })

  it('converts a duration’s nanosecond bounds into the seconds the control edits in', () => {
    const [, window] = paramFields(PORT_SCAN, { threshold: 15, window: '1m0s' })
    expect(window.min).toBe(1)
    expect(window.max).toBe(3600)
  })

  it('leaves a numeric bound alone', () => {
    const [threshold] = paramFields(PORT_SCAN, { threshold: 15, window: '1m0s' })
    expect(threshold.min).toBe(2)
    expect(threshold.max).toBe(1000)
  })

  it('keeps "no bound" distinct from "a bound of zero"', () => {
    const schema: DefinitionParamSchema[] = [
      { name: 'ratio', type: 'float', description: '', min: 0 },
    ]
    const [ratio] = paramFields(schema, { ratio: 0.8 })
    expect(ratio.min).toBe(0)
    expect(ratio.max).toBeUndefined()
  })

  it('renders a field for a param the definition has no value for', () => {
    const [threshold] = paramFields(PORT_SCAN, {})
    expect(threshold.value).toBe(2)
  })

  it('has no fields at all when the server declares no schema for the definition', () => {
    expect(paramFields(undefined, { threshold: 15 })).toEqual([])
  })
})

describe('paramsFromFields', () => {
  it('writes a duration back as the Go duration string the server validates', () => {
    const fields = paramFields(PORT_SCAN, { threshold: 15, window: '1m0s' })
    fields[1].value = 90
    expect(paramsFromFields(fields)).toEqual({ threshold: 15, window: '90s' })
  })

  it('writes an edited threshold back as a number, not the input’s string', () => {
    const fields = paramFields(PORT_SCAN, { threshold: 15, window: '1m0s' })
    fields[0].value = '9' as unknown as number
    expect(paramsFromFields(fields).threshold).toBe(9)
  })

  it('writes a port list back as numbers and a string list as strings', () => {
    const schema: DefinitionParamSchema[] = [
      { name: 'criticalPorts', type: 'portList', description: '' },
      { name: 'vpnInterfaces', type: 'stringList', description: '' },
    ]
    const fields = paramFields(schema, { criticalPorts: [22, 3389], vpnInterfaces: ['wireguard1'] })
    expect(paramsFromFields(fields)).toEqual({
      criticalPorts: [22, 3389],
      vpnInterfaces: ['wireguard1'],
    })
  })

  // Not a byte-identical round trip, and deliberately not asserted as
  // one: the server normalizes a duration through Go's own
  // Duration.String(), so "60s" comes back as "1m0s". What has to hold is
  // that an untouched field writes back the *same duration*, not the same
  // spelling -- an assertion on the spelling would fail on a value the
  // server itself produced.
  it('writes an untouched definition back as the same values, duration included', () => {
    const written = paramsFromFields(paramFields(PORT_SCAN, { threshold: 15, window: '1m0s' }))
    expect(written.threshold).toBe(15)
    expect(parseGoDurationSeconds(String(written.window))).toBe(60)
  })
})

describe('parsePortEntry', () => {
  it('takes a single port', () => {
    expect(parsePortEntry('22')).toEqual({ ports: [22] })
  })

  it('expands an inclusive range, because the wire shape is a number list', () => {
    expect(parsePortEntry('8000-8003')).toEqual({ ports: [8000, 8001, 8002, 8003] })
  })

  it('refuses a range that runs backwards, saying so', () => {
    const { ports, error } = parsePortEntry('90-80')
    expect(ports).toEqual([])
    expect(error).toContain('backwards')
  })

  it('refuses a range too wide to edit as chips, naming the limit', () => {
    const { ports, error } = parsePortEntry('1-65535')
    expect(ports).toEqual([])
    expect(error).toContain('256')
  })

  it('refuses a port outside 1-65535', () => {
    expect(parsePortEntry('70000').error).toBeTruthy()
    expect(parsePortEntry('0').error).toBeTruthy()
  })

  it('refuses a value that is not a number at all', () => {
    expect(parsePortEntry('ssh').error).toContain('not a port number')
  })

  it('treats an empty box as nothing to add, not as an error', () => {
    expect(parsePortEntry('  ')).toEqual({ ports: [] })
  })
})

describe('chips', () => {
  it('never adds the same value twice -- an axis is a set', () => {
    expect(addChip(['22'], '22')).toEqual(['22'])
    expect(addChip(['22'], ' 22 ')).toEqual(['22'])
  })

  it('adds a new value at the end', () => {
    expect(addChip(['22'], '3389')).toEqual(['22', '3389'])
  })

  it('ignores an empty add', () => {
    expect(addChip(['22'], '   ')).toEqual(['22'])
  })

  it('removes exactly the chip named', () => {
    expect(removeChip(['22', '3389'], '22')).toEqual(['3389'])
  })
})

describe('scope draft', () => {
  it('reads every axis into chips, ports as strings while being edited', () => {
    const draft = scopeDraftFrom({
      hosts: ['192.168.1.50'],
      hostsMode: 'deny',
      ports: [22, 3389],
      portsMode: 'allow',
      rules: ['r13'],
      rulesMode: '',
      classification: 'external',
    })
    expect(draft.ports).toEqual(['22', '3389'])
    expect(draft.hosts).toEqual(['192.168.1.50'])
    expect(draft.hostsMode).toBe('deny')
    expect(draft.classification).toBe('external')
  })

  it('writes ports back as numbers', () => {
    const draft = scopeDraftFrom({ ports: [22] })
    draft.ports = ['22', '3389']
    expect(scopeFromDraft(draft).ports).toEqual([22, 3389])
  })

  it('round-trips a scope unchanged', () => {
    const scope = {
      hosts: ['203.0.113.0/24'],
      hostsMode: 'allow' as const,
      ports: [22],
      portsMode: 'deny' as const,
      rules: ['r13'],
      rulesMode: '' as const,
      classification: 'internal' as const,
    }
    expect(scopeFromDraft(scopeDraftFrom(scope))).toEqual(scope)
  })

  it('treats a definition with no scope as every axis unrestricted', () => {
    expect(scopeFromDraft(scopeDraftFrom(undefined))).toEqual({
      hosts: [],
      hostsMode: '',
      ports: [],
      portsMode: '',
      rules: [],
      rulesMode: '',
      classification: '',
    })
  })
})

describe('cloneName', () => {
  it('offers the copy under the same name the server would give it', () => {
    expect(cloneName('Port scan')).toBe('Port scan (copy)')
  })
})
