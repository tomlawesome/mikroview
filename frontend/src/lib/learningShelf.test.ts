// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { anyBaselineWarming } from './learningShelf'

// #768: the shelf's warming state is now one field on the GET
// /api/flags response, not a projection of the definitions catalogue.
// What the field *means* is unchanged (#642's ruling, amendment 2) and
// is pinned server-side in internal/api/flags_test.go; what this pins
// is the one distinction the frontend still owns -- a server that did
// not answer is not a server that said "no".
describe('anyBaselineWarming (#642, moved to the flags response by #768)', () => {
  it('reports true when the flags response says a baseline is warming', () => {
    expect(anyBaselineWarming({ baselinesWarming: true })).toBe(true)
  })

  it('reports false when the flags response says nothing is warming', () => {
    expect(anyBaselineWarming({ baselinesWarming: false })).toBe(false)
  })

  it('reports false when the server did not answer -- absence is not a claim', () => {
    expect(anyBaselineWarming({})).toBe(false)
    expect(anyBaselineWarming({ baselinesWarming: undefined })).toBe(false)
  })
})
