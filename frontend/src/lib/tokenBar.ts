// SPDX-License-Identifier: AGPL-3.0-only
//
// The stream filter box's third face (#1246, round 57): a GitLab-style
// field menu, then a value menu, both writing the same appState.filters
// the named-field strip and the chip summary already read. "The token
// bar is a third face of one state, not a second filter store" -- so
// nothing here holds a filter of its own. Every pick turns into a patch
// the caller hands to appState.setFilter, and what the box then draws is
// buildFilterChips' own chip, unchanged.
//
// The lists come from wherever the app already keeps them (devices,
// chainOptions, ACTION_FILTER_OPTIONS, the persisted seen values of
// #1226); this file only decides which list belongs to which field and
// what a chosen value means. Free text in the same box stays the rule
// search and is never a token, so `rule` is deliberately not a field
// here.

import { ACTION_FILTER_OPTIONS } from './actions'
import type { FilterChip } from './filterChips'
import type { Action, Device, Filters, Scope } from './types'

// The eight bounded filters, in the order the field menu draws them
// (round 57's scene 02). `rule` is not one of them -- see the file
// comment.
export type TokenField =
  | 'device'
  | 'action'
  | 'chain'
  | 'proto'
  | 'interface'
  | 'port'
  | 'source'
  | 'destination'

export const TOKEN_FIELDS: readonly TokenField[] = [
  'device',
  'action',
  'chain',
  'proto',
  'interface',
  'port',
  'source',
  'destination',
] as const

// What the menus are built from, passed in rather than imported, so this
// stays a pure function of the app's own state and can be tested without
// a component.
export interface TokenSources {
  devices: readonly Device[]
  chains: readonly string[]
  // #1226's persisted register, not a scrape of what is on screen: the
  // owner's verdict 1 on round 57 ("it should grow a real list of what
  // it has seen over time, and be persisted") is what these two lists
  // read from.
  protos: readonly string[]
  interfaces: readonly string[]
  srcCountries: readonly { value: string; label: string }[]
  dstCountries: readonly { value: string; label: string }[]
}

export interface TokenMenuItem {
  // For a field item, the field name; for a value item, what
  // tokenPatch() turns into a filter -- a device id, an action, or a
  // `scope:`/`country:`/`query:` sub-field for a side.
  value: string
  label: string
  hint?: string
}

// The seven real actions: ACTION_FILTER_OPTIONS leads with "Any action",
// which is how a <select> says "no filter" and is not a token anyone can
// commit.
const ACTION_VALUES = ACTION_FILTER_OPTIONS.filter((o) => o.value !== '')

function seenHint(values: readonly string[]): string {
  return values.length === 0 ? 'nothing seen yet' : `seen so far: ${values.join(' · ')}`
}

// One line per field, saying what it takes. `action`'s is a count, not
// the list: owner verdict 2 on round 57 was "shorten", and the value
// menu carries the full list anyway.
export function fieldItems(src: TokenSources): TokenMenuItem[] {
  return TOKEN_FIELDS.map((field) => ({ value: field, label: field, hint: fieldHint(field, src) }))
}

function fieldHint(field: TokenField, src: TokenSources): string {
  switch (field) {
    case 'device':
      return 'pick from known devices'
    case 'action':
      return `${ACTION_VALUES.length} values`
    case 'chain':
      return src.chains.join(' · ')
    case 'proto':
      return seenHint(src.protos)
    case 'interface':
      return seenHint(src.interfaces)
    case 'port':
      return typedValueHint('port')
    default:
      return 'scope, name/IP or CIDR, or country'
  }
}

// What a field's value menu offers to click. Empty for `port`, which is
// typed; a side's list is its scope pair plus whatever countries have
// actually been observed on that side, with typing still open beside it
// (typedValueHint below).
export function valueItems(field: TokenField, src: TokenSources): TokenMenuItem[] {
  switch (field) {
    case 'device':
      // The value is the id, because that is what filters.device holds
      // and what buildFilterChips resolves back to a name.
      return src.devices.map((d) => ({ value: d.id, label: d.name }))
    case 'action':
      return ACTION_VALUES.map((o) => ({ value: o.value, label: o.label }))
    case 'chain':
      return src.chains.map((c) => ({ value: c, label: c }))
    case 'proto':
      return src.protos.map((p) => ({ value: p, label: p }))
    case 'interface':
      return src.interfaces.map((i) => ({ value: i, label: i }))
    case 'port':
      return []
    default: {
      const countries = field === 'source' ? src.srcCountries : src.dstCountries
      return [
        { value: 'scope:internal', label: 'internal', hint: 'scope' },
        { value: 'scope:external', label: 'external', hint: 'scope' },
        ...countries.map((c) => ({ value: `country:${c.value}`, label: c.label, hint: 'country' })),
      ]
    }
  }
}

// The line under a value menu that also takes typing, and '' for the
// fields that only take a pick.
export function typedValueHint(field: TokenField): string {
  if (field === 'port') return 'type a number, ↵ to commit'
  if (field === 'source' || field === 'destination') return 'or type a name, IP or CIDR, ↵ to commit'
  return ''
}

export function acceptsTypedValue(field: TokenField): boolean {
  return typedValueHint(field) !== ''
}

// What committing a value writes. A side commits one sub-field at a
// time into the same three fields the strip's own Source/Destination
// group writes, so the box goes on drawing ONE composite chip
// (`source:185.220.101.34 · external · DE`) however many parts have been
// picked -- sideValue() in filterChips.ts does the joining.
export function tokenPatch(field: TokenField, value: string): Partial<Filters> {
  switch (field) {
    case 'device':
      return { device: value }
    case 'action':
      return { action: value as Action }
    case 'chain':
      return { chain: value }
    case 'proto':
      return { protocol: value }
    case 'interface':
      return { interface: value }
    case 'port':
      return { port: value }
    default: {
      const side = field === 'source' ? 'src' : 'dst'
      const [part, ...rest] = value.split(':')
      const raw = rest.join(':')
      if (part === 'scope') return side === 'src' ? { srcScope: raw as Scope } : { dstScope: raw as Scope }
      if (part === 'country') return side === 'src' ? { srcCountry: raw } : { dstCountry: raw }
      return side === 'src' ? { srcQuery: raw } : { dstQuery: raw }
    }
  }
}

export type BackspaceAction = 'character' | 'cancel-pending' | 'remove-last-token'

// One key, one meaning, in whichever box holds the caret (owner verdict
// 3: "character you just typed"). A box with characters in it always
// deletes a character; only an EMPTY free-text box reaches back to the
// last token. An empty value box is not the free-text box, so it drops
// the pending field it is standing in rather than a committed token.
export function backspaceAction(pending: TokenField | null, boxValue: string): BackspaceAction {
  if (boxValue !== '') return 'character'
  return pending === null ? 'remove-last-token' : 'cancel-pending'
}

// The chip Backspace removes: the last committed token. The `rule` chip
// is free text, not a token, so it is skipped -- Backspace in the
// free-text box is already editing that.
export function lastTokenKey(chips: readonly FilterChip[]): string | null {
  for (let i = chips.length - 1; i >= 0; i--) {
    if (chips[i].key !== 'rule') return chips[i].key
  }
  return null
}
