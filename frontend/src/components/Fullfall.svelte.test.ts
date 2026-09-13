// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { render } from '@testing-library/svelte'

import Fullfall from './Fullfall.svelte'

// The shared pre-deck rain (#1214, grown from #645's door): what a unit
// test can vouch for is the field's composition and the mask variants
// being real classes -- the mask's computed geometry and the CSP/
// Firefox stroke-collapse regression are live-door.mjs's and
// live-door-engines.mjs's to witness in a real browser.
describe('Fullfall', () => {
  it('rains forty strokes, decoration only, accept-dominant like the fall itself', () => {
    const { container } = render(Fullfall)

    const layer = container.querySelector('.fullfall')
    expect(layer?.getAttribute('aria-hidden')).toBe('true')

    const strokes = container.querySelectorAll('.fullfall i')
    expect(strokes.length).toBe(40)

    // The fall's own weather: mostly accepts, a scatter of drops, a
    // few NAT marks -- never drop-dominant, which would read as a
    // network on fire before any data exists to say so.
    const drops = container.querySelectorAll('.fullfall i.r').length
    const nats = container.querySelectorAll('.fullfall i.v').length
    const accepts = strokes.length - drops - nats
    expect(drops).toBe(9)
    expect(nats).toBe(5)
    expect(accepts).toBe(26)
  })

  it('carves the door mask by default and the attach mask when asked', () => {
    const door = render(Fullfall)
    expect(door.container.querySelector('.fullfall.door')).toBeTruthy()
    expect(door.container.querySelector('.fullfall.attach')).toBeNull()
    door.unmount()

    const attach = render(Fullfall, { props: { mask: 'attach' } })
    expect(attach.container.querySelector('.fullfall.attach')).toBeTruthy()
    expect(attach.container.querySelector('.fullfall.door')).toBeNull()
  })
})
