// SPDX-License-Identifier: AGPL-3.0-only
//
// Builds the scene bar's active-filter summary (#683, round 29
// ratified: `.scenebar .controls .search` -- "action:<em>drop</em>
// boundary:<em>iot→lan</em> ⌫") from the live filter state, so an
// active filter is visible on the bar rather than only inside the
// (folded, by default) filter row. `label` and `value` stay separate
// so the component can render the value the way the mockup's own
// `.search em` does -- accented, the label plain.

import type { Device, Filters } from './types'

export interface FilterChip {
  key: string
  label: string
  value: string
}

// Joins whichever of a side's three fields (query, scope, country) are
// actually set -- the ratified chip ("boundary: iot→lan") shows only
// what was picked, not every field's empty placeholder.
function sideValue(query: string, scope: string, country: string): string {
  return [query, scope, country].filter(Boolean).join(' · ')
}

export function buildFilterChips(filters: Filters, devices: Device[]): FilterChip[] {
  const chips: FilterChip[] = []

  if (filters.device) {
    const d = devices.find((dv) => dv.id === filters.device)
    chips.push({ key: 'device', label: 'device', value: d ? d.name : filters.device })
  }
  if (filters.action) chips.push({ key: 'action', label: 'action', value: filters.action })
  if (filters.chain) chips.push({ key: 'chain', label: 'chain', value: filters.chain })
  if (filters.protocol) chips.push({ key: 'proto', label: 'proto', value: filters.protocol })
  if (filters.srcScope || filters.srcQuery || filters.srcCountry) {
    chips.push({ key: 'source', label: 'source', value: sideValue(filters.srcQuery, filters.srcScope, filters.srcCountry) })
  }
  if (filters.dstScope || filters.dstQuery || filters.dstCountry) {
    chips.push({
      key: 'destination',
      label: 'destination',
      value: sideValue(filters.dstQuery, filters.dstScope, filters.dstCountry),
    })
  }
  if (filters.port) chips.push({ key: 'port', label: 'port', value: filters.port })
  if (filters.interface) chips.push({ key: 'interface', label: 'interface', value: filters.interface })
  if (filters.rule) chips.push({ key: 'rule', label: 'rule', value: filters.rule })

  return chips
}
