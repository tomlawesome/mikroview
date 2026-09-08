import { describe, expect, it } from 'vitest'
import {
  EXPECTED_LABEL,
  EXPECTED_ONE_LABEL,
  expectedAllLabel,
  expectedPlaceholder,
  expectedTargets,
  expectedWho,
  offersExpectedAll,
  type ExpectedScope,
} from './expected'

const lines = [{ key: 'a' }, { key: 'b' }, { key: 'c' }]

describe('what an expected submit covers (#1016)', () => {
  it('covers only the line named, whichever position it is in', () => {
    expect(expectedTargets({ kind: 'one', key: 'b' }, lines)).toEqual([{ key: 'b' }])
  })

  it('covers nothing when the named line is no longer listed', () => {
    // The register refreshed under an open form. Widening to the whole
    // list here would mark lines the operator never looked at, which is
    // the exact failure per-line exists to prevent.
    expect(expectedTargets({ kind: 'one', key: 'gone' }, lines)).toEqual([])
  })

  it('covers every listed line, in the card’s own order, only for the bulk scope', () => {
    expect(expectedTargets({ kind: 'all' }, lines)).toEqual(lines)
  })
})

describe('the two controls read differently (#1016)', () => {
  it('leaves the per-line control unqualified — it is the default', () => {
    expect(EXPECTED_ONE_LABEL).toBe('expected ▸')
  })

  it('makes the bulk control say how many it will mark', () => {
    expect(expectedAllLabel(4)).toBe('mark all 4 expected ▸')
    expect(expectedAllLabel(2)).toContain('2')
  })

  it('cannot be mistaken for the single-line action: it does not start with the same word', () => {
    expect(expectedAllLabel(4).startsWith(EXPECTED_ONE_LABEL.split(' ')[0])).toBe(false)
  })

  it('offers no bulk control for a single line — it would be a duplicate under a count of one', () => {
    expect(offersExpectedAll(1)).toBe(false)
    expect(offersExpectedAll(0)).toBe(false)
    expect(offersExpectedAll(2)).toBe(true)
  })
})

describe('the form says what it is about to speak for (#1016)', () => {
  const one: ExpectedScope = { kind: 'one', key: 'a' }
  const all: ExpectedScope = { kind: 'all' }

  it('asks the same question either way', () => {
    expect(EXPECTED_LABEL).toBe('EXPECTED — WHY?')
  })

  it('names the scope in the placeholder', () => {
    expect(expectedPlaceholder(one, 1)).toBe('why this line is meant to be here…')
    expect(expectedPlaceholder(all, 3)).toBe('why these 3 lines are meant to be here…')
  })

  it('says who it is recorded as and exactly what it covers', () => {
    expect(expectedWho('tom', one, 1)).toBe('as tom · this line only')
    expect(expectedWho('tom', all, 3)).toBe('as tom · all 3 of these lines')
  })

  it('falls back to "you" when the session has not named the operator yet', () => {
    expect(expectedWho('', one, 1)).toBe('as you · this line only')
  })
})
