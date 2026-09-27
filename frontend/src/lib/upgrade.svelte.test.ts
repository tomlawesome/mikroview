// SPDX-License-Identifier: AGPL-3.0-only
//
// upgradeState.line's wording, direct: #1240's design ruling is read
// out of these two branches in UpgradeNotice.svelte's own test, but
// neither pins the fallback string against the rest of the file's rule
// (2026-09-18 audit, stage 5 finding 26a) -- that the step is always
// named from setupsteps.ts's own TITLES, never spelled out by number,
// because the walking order is exactly the thing that moved once
// already (#1284) and broke a "step 2" sentence just like this one.

import { beforeEach, describe, expect, it } from 'vitest'

import { TITLES } from './setupsteps'
import { upgradeState, type Upgrade } from './upgrade.svelte'

function upgrade(over: Partial<Upgrade> = {}): Upgrade {
  return {
    previous: 'v0.4.0',
    current: 'v0.5.0',
    acknowledged: false,
    routers: { behind: 0, total: 0, reported: 0, staleLogging: 0 },
    ...over,
  }
}

beforeEach(() => {
  upgradeState.upgrade = null
})

describe('upgradeState.line', () => {
  it('names the certificate step from TITLES, not a step number, once routers are counted', () => {
    upgradeState.upgrade = upgrade({ routers: { behind: 2, total: 3, reported: 3, staleLogging: 0 } })
    expect(upgradeState.line).toContain(TITLES.ca)
    expect(upgradeState.line).not.toMatch(/step \d/)
  })

  // The fallback -- no routers declared to count yet -- is `ad6323fd`'s
  // bug a third time: this branch still names the step by number
  // instead of reading TITLES.ca the way its sibling two lines above
  // already does.
  it('names the same step from TITLES when there is nothing yet to count', () => {
    upgradeState.upgrade = upgrade()
    expect(upgradeState.line).toContain(TITLES.ca)
    expect(upgradeState.line).not.toMatch(/step \d/)
  })

  // #1373: named beside the crossing, singular and plural.
  it('names a router with an old logging setup, singular', () => {
    upgradeState.upgrade = upgrade({ routers: { behind: 0, total: 1, reported: 1, staleLogging: 1 } })
    expect(upgradeState.line).toContain('1 router has an old logging setup')
  })

  it('names routers with an old logging setup, plural', () => {
    upgradeState.upgrade = upgrade({ routers: { behind: 0, total: 3, reported: 3, staleLogging: 2 } })
    expect(upgradeState.line).toContain('2 routers have an old logging setup')
  })

  it('says nothing about logging leftovers when there are none', () => {
    upgradeState.upgrade = upgrade({ routers: { behind: 0, total: 1, reported: 1, staleLogging: 0 } })
    expect(upgradeState.line).not.toContain('logging setup')
  })
})
