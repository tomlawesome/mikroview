// SPDX-License-Identifier: AGPL-3.0-only
//
// #1246 (round 57, the token bar): the field list, the value lists, what
// a pick writes into appState.filters, and what Backspace means in each
// of the box's two typing positions. The component test
// (FilterBar.svelte.test.ts) drives the same rules through the real box;
// these pin them where they are decided.

import { describe, expect, it } from 'vitest'
import {
  TOKEN_FIELDS,
  backspaceAction,
  fieldItems,
  lastTokenKey,
  tokenPatch,
  typedValueHint,
  valueItems,
  type TokenSources,
} from './tokenBar'
import type { Device } from './types'

function device(id: string, name: string): Device {
  return { id, name } as Device
}

function sources(over: Partial<TokenSources> = {}): TokenSources {
  return {
    devices: [device('dev-cam', 'cam-porch'), device('dev-nas', 'nas')],
    chains: ['input', 'forward', 'output'],
    protos: ['tcp', 'udp'],
    interfaces: ['ether1', 'bridge-lan'],
    srcCountries: [{ value: 'DE', label: '🇩🇪 DE' }],
    dstCountries: [],
    ...over,
  }
}

describe('the token bar field menu (#1246)', () => {
  it('offers exactly the eight bounded fields, in the ratified order', () => {
    expect(TOKEN_FIELDS).toEqual(['device', 'action', 'chain', 'proto', 'interface', 'port', 'source', 'destination'])
    expect(fieldItems(sources()).map((i) => i.value)).toEqual([...TOKEN_FIELDS])
  })

  it('says what action takes as a count, not as the list (owner verdict 2: "shorten")', () => {
    const action = fieldItems(sources()).find((i) => i.value === 'action')
    expect(action?.hint).toBe('7 values')
  })

  it('hints chain, proto and interface with what is really on offer', () => {
    const items = fieldItems(sources())
    expect(items.find((i) => i.value === 'chain')?.hint).toBe('input · forward · output')
    expect(items.find((i) => i.value === 'proto')?.hint).toBe('seen so far: tcp · udp')
    expect(items.find((i) => i.value === 'interface')?.hint).toBe('seen so far: ether1 · bridge-lan')
  })

  it('says so rather than promising a list when nothing has been seen yet', () => {
    const items = fieldItems(sources({ protos: [], interfaces: [] }))
    expect(items.find((i) => i.value === 'proto')?.hint).toBe('nothing seen yet')
    expect(items.find((i) => i.value === 'interface')?.hint).toBe('nothing seen yet')
  })
})

describe('the token bar value menus (#1246)', () => {
  it('lists known devices by name and commits the id the chip resolves against', () => {
    expect(valueItems('device', sources())).toEqual([
      { value: 'dev-cam', label: 'cam-porch' },
      { value: 'dev-nas', label: 'nas' },
    ])
    expect(tokenPatch('device', 'dev-cam')).toEqual({ device: 'dev-cam' })
  })

  it('reuses the strip\'s own action list verbatim, minus its "any" entry', () => {
    expect(valueItems('action', sources()).map((i) => i.value)).toEqual([
      'accept',
      'drop',
      'reject',
      'log',
      'marked',
      'natted',
      'unknown',
    ])
    expect(valueItems('action', sources()).find((i) => i.value === 'marked')?.label).toBe('Marked (mangle)')
    expect(tokenPatch('action', 'drop')).toEqual({ action: 'drop' })
  })

  it('reuses the chain options and the persisted seen values, never a value that has not arrived', () => {
    expect(valueItems('chain', sources()).map((i) => i.value)).toEqual(['input', 'forward', 'output'])
    expect(valueItems('proto', sources()).map((i) => i.value)).toEqual(['tcp', 'udp'])
    expect(valueItems('interface', sources()).map((i) => i.value)).toEqual(['ether1', 'bridge-lan'])
    expect(valueItems('proto', sources({ protos: [] }))).toEqual([])
    expect(tokenPatch('chain', 'forward')).toEqual({ chain: 'forward' })
    expect(tokenPatch('proto', 'tcp')).toEqual({ protocol: 'tcp' })
    expect(tokenPatch('interface', 'ether1')).toEqual({ interface: 'ether1' })
  })

  it('offers port nothing to click -- it is typed, and Enter commits it', () => {
    expect(valueItems('port', sources())).toEqual([])
    expect(typedValueHint('port')).toBe('type a number, ↵ to commit')
    expect(tokenPatch('port', '8291')).toEqual({ port: '8291' })
  })

  it('gives a side one second-level menu -- scope, observed country, or typed address', () => {
    const items = valueItems('source', sources())
    expect(items).toEqual([
      { value: 'scope:internal', label: 'internal', hint: 'scope' },
      { value: 'scope:external', label: 'external', hint: 'scope' },
      { value: 'country:DE', label: '🇩🇪 DE', hint: 'country' },
    ])
    expect(typedValueHint('source')).toBe('or type a name, IP or CIDR, ↵ to commit')
    // Every part of the pick lands in the one side's fields, so the chip
    // buildFilterChips draws stays a single composite token.
    expect(tokenPatch('source', 'scope:external')).toEqual({ srcScope: 'external' })
    expect(tokenPatch('source', 'country:DE')).toEqual({ srcCountry: 'DE' })
    expect(tokenPatch('source', 'query:185.220.101.34')).toEqual({ srcQuery: '185.220.101.34' })
  })

  it('reads destination off its own side, never the source\'s', () => {
    const src = sources({ dstCountries: [{ value: 'NL', label: '🇳🇱 NL' }] })
    expect(valueItems('destination', src).map((i) => i.value)).toEqual([
      'scope:internal',
      'scope:external',
      'country:NL',
    ])
    expect(tokenPatch('destination', 'scope:internal')).toEqual({ dstScope: 'internal' })
    expect(tokenPatch('destination', 'country:NL')).toEqual({ dstCountry: 'NL' })
    expect(tokenPatch('destination', 'query:nas')).toEqual({ dstQuery: 'nas' })
  })
})

describe('what Backspace means, by which box holds the caret (#1246, owner verdict 3)', () => {
  it('deletes the character you just typed in a value box that has one', () => {
    expect(backspaceAction('port', '829')).toBe('character')
    expect(backspaceAction('source', '185.')).toBe('character')
  })

  it('never reaches past a half-typed value to the last token', () => {
    // "character you just typed" -- an empty value box drops the pending
    // field itself, which is as far back as Backspace goes there.
    expect(backspaceAction('port', '')).toBe('cancel-pending')
  })

  it('deletes the last token only from an empty free-text box', () => {
    expect(backspaceAction(null, '')).toBe('remove-last-token')
    expect(backspaceAction(null, 'iot-to-lan')).toBe('character')
  })

  it('takes the last token off the chips, and never the free-text one', () => {
    expect(lastTokenKey([])).toBeNull()
    expect(lastTokenKey([{ key: 'action', label: 'action', value: 'drop' }])).toBe('action')
    expect(
      lastTokenKey([
        { key: 'action', label: 'action', value: 'drop' },
        { key: 'chain', label: 'chain', value: 'input' },
        { key: 'rule', label: 'rule', value: 'iot-to-lan' },
      ]),
    ).toBe('chain')
    expect(lastTokenKey([{ key: 'rule', label: 'rule', value: 'iot-to-lan' }])).toBeNull()
  })
})
