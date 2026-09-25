// SPDX-License-Identifier: AGPL-3.0-only
//
// #1298: the console must stay quiet about a poll the operator
// cancelled by navigating, and must stay just as loud about one that
// genuinely failed while the page was live.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installCancellationGuard, isCancelledFetch, pageIsLeaving } from './cancelled'

let uninstall: () => void

/** One unhandled rejection, as the browser delivers it. jsdom has no
 * PromiseRejectionEvent constructor, and the guard only reads `reason`
 * and calls `preventDefault`, so a plain cancelable Event carrying a
 * reason is the same thing from its point of view. */
function rejectUnhandled(reason: unknown) {
  const event = new Event('unhandledrejection', { cancelable: true }) as Event & { reason?: unknown }
  event.reason = reason
  window.dispatchEvent(event)
  return event
}

/** What Firefox rejects a fetch with when a navigation cuts it off. */
function firefoxNetworkError(): TypeError {
  return new TypeError('NetworkError when attempting to fetch resource.')
}

function abortError(): Error {
  const err = new Error('The operation was aborted.')
  err.name = 'AbortError'
  return err
}

beforeEach(() => {
  uninstall = installCancellationGuard()
})

afterEach(() => {
  uninstall()
  vi.restoreAllMocks()
})

describe('the cancellation guard (#1298)', () => {
  it('says nothing about a fetch aborted out from under a poller', () => {
    const reported = vi.spyOn(console, 'error').mockImplementation(() => {})

    const event = rejectUnhandled(abortError())

    expect(reported).not.toHaveBeenCalled()
    expect(event.defaultPrevented).toBe(true)
  })

  it('still reports a genuine failure while the page is live', () => {
    const reported = vi.spyOn(console, 'error').mockImplementation(() => {})
    const failure = firefoxNetworkError()

    rejectUnhandled(failure)

    expect(reported).toHaveBeenCalledWith(failure)
  })

  it('says nothing about the same shape once the page is on its way out', () => {
    const reported = vi.spyOn(console, 'error').mockImplementation(() => {})

    window.dispatchEvent(new Event('pagehide'))
    rejectUnhandled(firefoxNetworkError())

    expect(pageIsLeaving()).toBe(true)
    expect(reported).not.toHaveBeenCalled()
  })

  it('is loud again for a page the browser brought back from its cache', () => {
    const reported = vi.spyOn(console, 'error').mockImplementation(() => {})

    window.dispatchEvent(new Event('pagehide'))
    window.dispatchEvent(new Event('pageshow'))
    rejectUnhandled(firefoxNetworkError())

    expect(pageIsLeaving()).toBe(false)
    expect(reported).toHaveBeenCalledTimes(1)
  })

  it('reports an error the app itself threw, cancellation or no', () => {
    const reported = vi.spyOn(console, 'error').mockImplementation(() => {})
    const bug = new TypeError('x is not a function')

    window.dispatchEvent(new Event('pagehide'))
    rejectUnhandled(bug)

    expect(reported).toHaveBeenCalledWith(bug)
  })
})

describe('isCancelledFetch', () => {
  it('reads an ambiguous message as a cancellation for a poller that has stopped', () => {
    expect(isCancelledFetch(firefoxNetworkError())).toBe(false)
    expect(isCancelledFetch(firefoxNetworkError(), true)).toBe(true)
  })

  it('never reads a refusal the server answered with as a cancellation', () => {
    const refused = Object.assign(new Error('Failed to fetch'), { status: 503 })
    expect(isCancelledFetch(refused, true)).toBe(false)
  })

  it('takes an AbortError whatever the poller is doing', () => {
    expect(isCancelledFetch(abortError())).toBe(true)
  })
})
