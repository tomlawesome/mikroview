// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi } from 'vitest'
import { subscribe, type VisibilityDocument } from './visibility'

// A fake document standing in for the real one (#1088): jsdom does not
// implement toggling document.hidden, so App.svelte's pause/resume-on-
// visibilitychange logic is only testable if it goes through this
// seam rather than the global `document` directly.
function fakeDocument(initialHidden: boolean): VisibilityDocument & { fire(hidden: boolean): void } {
  let hidden = initialHidden
  const listeners = new Set<() => void>()
  return {
    get hidden() {
      return hidden
    },
    addEventListener(_type, listener) {
      listeners.add(listener)
    },
    removeEventListener(_type, listener) {
      listeners.delete(listener)
    },
    fire(next: boolean) {
      hidden = next
      for (const l of listeners) l()
    },
  }
}

describe('visibility subscribe', () => {
  it('calls back with the current hidden state on each change', () => {
    const doc = fakeDocument(false)
    const cb = vi.fn()
    subscribe(cb, doc)

    doc.fire(true)
    expect(cb).toHaveBeenLastCalledWith(true)

    doc.fire(false)
    expect(cb).toHaveBeenLastCalledWith(false)
    expect(cb).toHaveBeenCalledTimes(2)
  })

  it('stops calling back once unsubscribed', () => {
    const doc = fakeDocument(false)
    const cb = vi.fn()
    const unsubscribe = subscribe(cb, doc)

    doc.fire(true)
    expect(cb).toHaveBeenCalledTimes(1)

    unsubscribe()
    doc.fire(false)
    expect(cb).toHaveBeenCalledTimes(1)
  })

  it('supports independent subscribers', () => {
    const doc = fakeDocument(false)
    const first = vi.fn()
    const second = vi.fn()
    subscribe(first, doc)
    const unsubscribeSecond = subscribe(second, doc)

    doc.fire(true)
    expect(first).toHaveBeenCalledTimes(1)
    expect(second).toHaveBeenCalledTimes(1)

    unsubscribeSecond()
    doc.fire(false)
    expect(first).toHaveBeenCalledTimes(2)
    expect(second).toHaveBeenCalledTimes(1)
  })
})
