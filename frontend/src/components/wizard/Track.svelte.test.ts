// SPDX-License-Identifier: AGPL-3.0-only
//
// #1406: round-15's track.js hides the amber NOW cursor once the stage
// is 'done'. Track.svelte's nowLeft had no such condition, so the
// cursor kept sitting past the last arrival on the done step.

import { describe, expect, it } from 'vitest'
import { render } from '@testing-library/svelte'
import Track from './Track.svelte'
import type { TrackStation } from '../../lib/wizardRun'

function stations(): TrackStation[] {
  return [
    { id: 'copy', lab: 'copied', st: '14:00:00', state: 'done' },
    { id: 'cert', lab: 'certificate', st: '14:01:00', state: 'done' },
    { id: 'enrol', lab: 'enrolled', st: 'not yet', state: 'wait' },
  ]
}

describe('Track: the NOW cursor', () => {
  it('sits a step past the last arrival while the run is still watching', () => {
    render(Track, { stations: stations() })
    expect(document.querySelector('.now')).not.toBeNull()
  })

  it('is hidden on the done step, the way the prototype hides it', () => {
    render(Track, { stations: stations(), stage: 'done' })
    expect(document.querySelector('.now')).toBeNull()
  })
})
