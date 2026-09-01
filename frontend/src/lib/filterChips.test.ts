// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { buildFilterChips } from './filterChips'
import { emptyFilters, type Device } from './types'

const devices: Device[] = [
  {
    id: 'd1',
    name: 'border',
    sourceIp: '10.0.0.1',
    configured: true,
    firstSeen: '',
    lastSeen: '',
    eventCount: 0,
    status: 'live',
  } as Device,
]

describe('buildFilterChips', () => {
  it('is empty when no filter is active', () => {
    expect(buildFilterChips(emptyFilters(), [])).toEqual([])
  })

  it('renders one chip per active scalar field, label and value apart', () => {
    expect(buildFilterChips({ ...emptyFilters(), action: 'drop' }, [])).toEqual([
      { key: 'action', label: 'action', value: 'drop' },
    ])
  })

  it('resolves a device id to its name', () => {
    const chips = buildFilterChips({ ...emptyFilters(), device: 'd1' }, devices)
    expect(chips).toEqual([{ key: 'device', label: 'device', value: 'border' }])
  })

  it('falls back to the raw id if the device is not (yet) known', () => {
    const chips = buildFilterChips({ ...emptyFilters(), device: 'ghost' }, [])
    expect(chips).toEqual([{ key: 'device', label: 'device', value: 'ghost' }])
  })

  it('combines source query, scope and country into one chip', () => {
    const chips = buildFilterChips(
      { ...emptyFilters(), srcQuery: 'cam-porch', srcScope: 'internal', srcCountry: 'DE' },
      [],
    )
    expect(chips).toEqual([{ key: 'source', label: 'source', value: 'cam-porch · internal · DE' }])
  })

  it('renders source and destination as independent chips', () => {
    const chips = buildFilterChips({ ...emptyFilters(), srcScope: 'internal', dstScope: 'external' }, [])
    expect(chips.map((c) => c.key)).toEqual(['source', 'destination'])
  })

  it('ignores ruleRegex on its own -- it modifies rule, it is not a filter', () => {
    expect(buildFilterChips({ ...emptyFilters(), ruleRegex: true }, [])).toEqual([])
  })

  it('orders chips device, action, chain, proto, source, destination, port, interface, rule', () => {
    const chips = buildFilterChips(
      {
        ...emptyFilters(),
        rule: 'wan-in-drop',
        port: '445',
        protocol: 'tcp',
        action: 'drop',
        interface: 'ether1',
        chain: 'forward',
        device: 'd1',
        dstScope: 'internal',
      },
      devices,
    )
    expect(chips.map((c) => c.key)).toEqual([
      'device',
      'action',
      'chain',
      'proto',
      'destination',
      'port',
      'interface',
      'rule',
    ])
  })
})
