// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
  conditionFrom,
  inkFor,
  narrowFields,
  narrowValues,
  fieldSpec,
  parseValue,
  valueText,
  verbFor,
} from './conditionTokens'

// #829. The value's shape picks the operator, and this is the part of the
// editor most able to be wrong without anything noticing: an address list
// stored as an equals against a comma-joined string is a detector that
// lists, looks configured, and matches nothing forever.
describe('the value shape picks the operator', () => {
  it('reads a slash as a range of addresses', () => {
    expect(parseValue('sourceAddress', '10.0.70.0/24')).toEqual({
      operator: 'inCIDR',
      values: ['10.0.70.0/24'],
    })
  })

  it('reads a dash as a low and a high', () => {
    expect(parseValue('destinationPort', '1-1024')).toEqual({
      operator: 'inRange',
      values: ['1', '1024'],
    })
  })

  it('takes the en dash the drawing renders as well as the hyphen anyone types', () => {
    // valueText writes a saved range with an en dash, so a token picked
    // back up has to parse as the range it was.
    expect(parseValue('destinationPort', '1 – 1024')).toEqual({
      operator: 'inRange',
      values: ['1', '1024'],
    })
  })

  it('reads a clock span with the same grammar as a port range', () => {
    expect(parseValue('timeOfDay', '22:00-06:00')).toEqual({
      operator: 'inRange',
      values: ['22:00', '06:00'],
    })
  })

  it('reads commas as a list, trimming what was typed around them', () => {
    expect(parseValue('protocol', 'tcp, udp')).toEqual({
      operator: 'inSet',
      values: ['tcp', 'udp'],
    })
  })

  it('reads a class word as a classification, not as an address spelled internal', () => {
    expect(parseValue('destinationAddress', 'internal')).toEqual({
      operator: 'matchesClassification',
      values: ['internal'],
    })
  })

  it('reads a lone value as an equals', () => {
    expect(parseValue('action', 'drop')).toEqual({ operator: 'equals', values: ['drop'] })
  })

  it('turns a not prefix into the negative form', () => {
    expect(parseValue('action', 'not drop')).toEqual({ operator: 'notEquals', values: ['drop'] })
    expect(parseValue('protocol', 'not tcp, udp')).toEqual({
      operator: 'notInSet',
      values: ['tcp', 'udp'],
    })
  })

  it('refuses a not the engine has no operator for, rather than dropping it', () => {
    // There is no notInCIDR and no notInRange. Silently storing inCIDR
    // would store the opposite of what was asked for, which is the worst
    // of the three possible answers.
    const got = parseValue('sourceAddress', 'not 10.0.70.0/24')
    expect(got).toHaveProperty('error')
  })

  it('refuses a shape the field does not take', () => {
    // A port has no classification, so `internal` on one is not a
    // classification -- it falls through to an equals, which the engine
    // then refuses as an unparseable port. Reported here rather than on
    // save.
    const got = parseValue('timeOfDay', '22:00')
    expect(got).toHaveProperty('error')
  })

  it('keeps a pair a pair, however many commas it has', () => {
    // An address list is [router, list], not a set of two values.
    expect(parseValue('addressListMembership', 'router1, blocked')).toEqual({
      operator: 'equals',
      values: ['router1', 'blocked'],
    })
  })

  it('says nothing is there yet rather than building an empty condition', () => {
    expect(parseValue('action', '   ')).toHaveProperty('error')
  })
})

// #1076. An identity's pair is [mac, ip], but unlike addressListMembership's
// [router, list] the engine (matchlog.Identity.Empty) only refuses an
// identity with *neither* side -- a MAC-only or IP-only device still names
// something. The bar's parser has to accept the one-sided shapes the engine
// already does, not just the two-non-empty-parts shape every other pair
// takes.
describe('a source identity keeps an empty side, unlike other pairs', () => {
  it('takes a mac with no address', () => {
    expect(parseValue('sourceIdentity', 'aa:bb:cc:dd:ee:ff,')).toEqual({
      operator: 'equals',
      values: ['aa:bb:cc:dd:ee:ff', ''],
    })
  })

  it('takes an address with no mac', () => {
    expect(parseValue('sourceIdentity', ',10.0.0.5')).toEqual({
      operator: 'equals',
      values: ['', '10.0.0.5'],
    })
  })

  it('refuses both sides empty', () => {
    expect(parseValue('sourceIdentity', ',')).toEqual({
      error: 'identity takes mac,ip -- either side may be left empty, not both',
    })
  })

  it('refuses no comma at all', () => {
    expect(parseValue('sourceIdentity', 'aa:bb:cc:dd:ee:ff')).toEqual({
      error: 'identity takes mac,ip -- either side may be left empty, not both',
    })
  })

  it('refuses three parts', () => {
    expect(parseValue('sourceIdentity', 'aa:bb:cc:dd:ee:ff,10.0.0.5,extra')).toEqual({
      error: 'identity takes mac,ip -- either side may be left empty, not both',
    })
  })

  it('round-trips a one-sided identity through valueText back into parseValue', () => {
    const macOnly = { field: 'sourceIdentity', operator: 'equals', values: ['aa:bb:cc:dd:ee:ff', ''] }
    expect(valueText(macOnly)).toBe('aa:bb:cc:dd:ee:ff,')
    expect(parseValue('sourceIdentity', valueText(macOnly))).toEqual({
      operator: 'equals',
      values: ['aa:bb:cc:dd:ee:ff', ''],
    })

    const ipOnly = { field: 'sourceIdentity', operator: 'equals', values: ['', '10.0.0.5'] }
    expect(valueText(ipOnly)).toBe(',10.0.0.5')
    expect(parseValue('sourceIdentity', valueText(ipOnly))).toEqual({
      operator: 'equals',
      values: ['', '10.0.0.5'],
    })
  })
})

// A verb taken off the list before anything was typed only ever wins over
// a bare single value -- the one shape that says nothing about which
// comparison was meant.
describe('a verb chosen off the list', () => {
  it('wins over a bare value', () => {
    expect(conditionFrom('protocol', 'tcp', 'inSet')).toEqual({
      field: 'protocol',
      operator: 'inSet',
      values: ['tcp'],
    })
  })

  it('loses to a decisive shape, because the text is what was last said', () => {
    expect(conditionFrom('sourceAddress', '10.0.70.0/24', 'equals')).toEqual({
      field: 'sourceAddress',
      operator: 'inCIDR',
      values: ['10.0.70.0/24'],
    })
  })

  it('is ignored where the field does not offer it', () => {
    expect(conditionFrom('timeOfDay', '22:00-06:00', 'equals')).toEqual({
      field: 'timeOfDay',
      operator: 'inRange',
      values: ['22:00', '06:00'],
    })
  })
})

// The verb the unfinished token shows updates as the value is typed --
// the token says `between` the moment a dash appears, which is what
// replaced round 7's breadcrumb.
describe('the verb the half-written token shows', () => {
  it('follows the shape as it is typed', () => {
    expect(verbFor('destinationPort', '')).toBe('is')
    expect(verbFor('destinationPort', '1-')).toBe('between')
    expect(verbFor('destinationPort', '1-1024')).toBe('between')
    expect(verbFor('destinationPort', '22, 23')).toBe('one of')
  })

  it('falls back to a chosen verb while the value is still nothing', () => {
    expect(verbFor('destinationPort', '', 'inRange')).toBe('between')
  })
})

// #829's "typing narrows". Fourteen fields become two, and the words
// someone reaches for are not always the words on the row: `dst` has to
// find both `destination` and `port`.
describe('typing narrows', () => {
  it('offers every field with nothing typed', () => {
    expect(narrowFields('').length).toBe(15)
  })

  it('narrows dst to the destination pair', () => {
    const got = narrowFields('dst').map((f) => f.label)
    expect(got).toEqual(['destination', 'port'])
  })

  it('matches the label as well as the aliases', () => {
    expect(narrowFields('chain').map((f) => f.label)).toEqual(['chain'])
  })

  it('narrows to nothing rather than falling back to everything', () => {
    // An empty list closes the list. Falling back to all fifteen would
    // say "no matches" by showing every row, which reads as a bug.
    expect(narrowFields('zzz')).toEqual([])
  })

  it('offers the field verbs before anything is typed, and values after', () => {
    const port = fieldSpec('destinationPort')!
    expect(narrowValues(port, '').map((r) => r.value)).toContain('between')
    expect(narrowValues(port, '1-').map((r) => r.value)).toEqual(['1-1024', '1-65535'])
  })
})

// Round 8: the value is bold text in the ink the app already gives that
// thing -- no new colour is minted, so the dataviz validator has nothing
// new to check.
describe('the ink a value wears', () => {
  it('gives an action the ink its badge already has', () => {
    expect(inkFor({ field: 'action', operator: 'equals', values: ['drop'] })).toBe('var(--drop)')
  })

  it('gives a classification the accept green wherever it appears', () => {
    expect(
      inkFor({ field: 'destinationAddress', operator: 'matchesClassification', values: ['internal'] }),
    ).toBe('var(--accept)')
  })

  it('gives a port the accent and an address the stream ink', () => {
    expect(inkFor({ field: 'destinationPort', operator: 'equals', values: ['22'] })).toBe(
      'var(--accent)',
    )
    expect(inkFor({ field: 'sourceAddress', operator: 'inCIDR', values: ['10.0.0.0/8'] })).toBe(
      'var(--fg)',
    )
  })
})

describe('a saved condition reads back as it was written', () => {
  it('draws a range with the en dash and a list with commas', () => {
    expect(valueText({ field: 'destinationPort', operator: 'inRange', values: ['1', '1024'] })).toBe(
      '1 – 1024',
    )
    expect(valueText({ field: 'protocol', operator: 'inSet', values: ['tcp', 'udp'] })).toBe(
      'tcp, udp',
    )
  })
})
