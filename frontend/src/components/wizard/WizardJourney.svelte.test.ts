// SPDX-License-Identifier: AGPL-3.0-only
//
// #1386, the way out never played: wayOut() read the canvas/ride refs
// before the {#if wizardJourney.active} block had ever mounted them
// (mounting only follows begin('out')), so the guard always failed and
// Finish snapped straight to the fall. wizardJourney.test.ts's "the way
// out" tests only exercise the state module against a stand-in handler;
// this renders the real component and proves the journey actually
// starts -- phase reaches 'out', <body> gains the journey class, the
// canvas/ride mount, and the journey itself eventually swaps and lands.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/svelte'
import { tick } from 'svelte'
import WizardJourney from './WizardJourney.svelte'
import { wizardJourney } from '../../lib/wizardJourney.svelte'

// Same stand-in 2D context as wizardJourney.test.ts's "six beats" suite:
// accepts every call and property so the real frame loop can run under
// jsdom without a real canvas backend.
function fakeContext(): CanvasRenderingContext2D {
  const store: Record<string | symbol, unknown> = {}
  const gradient = { addColorStop: () => {} }
  return new Proxy(store, {
    get: (t, p) => (p in t ? t[p] : p === 'createLinearGradient' || p === 'createRadialGradient' ? () => gradient : () => {}),
    set: (t, p, v) => {
      t[p] = v
      return true
    },
  }) as unknown as CanvasRenderingContext2D
}

describe('WizardJourney: the way out actually plays', () => {
  beforeEach(() => {
    wizardJourney.end()
    window.matchMedia = vi.fn().mockReturnValue({ matches: false }) as unknown as typeof window.matchMedia
    vi.useFakeTimers({ toFake: ['setTimeout', 'requestAnimationFrame', 'performance'] })
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(() => fakeContext() as never)
  })

  afterEach(() => {
    wizardJourney.end()
    vi.useRealTimers()
    vi.restoreAllMocks()
    document.body.replaceChildren()
  })

  it('mounts the canvas and ride before reading them, then runs the journey to the fall', async () => {
    // The bar's wordmark Finish leaves from -- the wizard is already
    // open and sending, the way it stands when Finish is pressed.
    const wiz = document.createElement('div')
    wiz.className = 'wiz'
    const bar = document.createElement('div')
    bar.className = 'bar'
    const wm = document.createElement('span')
    wm.className = 'wm'
    wm.textContent = 'MikroView'
    bar.appendChild(wm)
    wiz.appendChild(bar)
    document.body.appendChild(wiz)

    render(WizardJourney)
    await tick()

    const swap = vi.fn()
    const after = vi.fn()

    // wizardJourney.wayOut is the same door wizardRun.finish() knocks on;
    // it only reaches this component's wayOut through wayOutHandler.
    expect(wizardJourney.wayOut(swap, after)).toBe(true)

    // begin('out') runs synchronously, mounting the canvas/ride this
    // frame -- before the two animation frames wayIn also waits out.
    expect(wizardJourney.phase).toBe('out')
    expect(document.body.classList.contains('journey')).toBe(true)
    await tick()
    expect(document.querySelector('canvas.journey-fx')).not.toBeNull()
    expect(document.querySelector('.ride')).not.toBeNull()

    // Two frames on, the journey is actually running (not bailed out).
    await vi.advanceTimersByTimeAsync(40)
    expect(wizardJourney.active).toBe(true)
    expect(swap).not.toHaveBeenCalled()

    // It plays through: the page swaps under the cover, then lands.
    await vi.advanceTimersByTimeAsync(9000)
    expect(swap).toHaveBeenCalledTimes(1)
    expect(after).toHaveBeenCalledTimes(1)
    expect(wizardJourney.active).toBe(false)
  })
})
