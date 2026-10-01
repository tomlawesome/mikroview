// SPDX-License-Identifier: AGPL-3.0-only
//
// #1314: registering the service worker must not leave the promise
// without a handler.
//
// The rejection this is about cannot be caught anywhere else. Firefox
// rejects an in-flight registration with InvalidStateError once the
// document stops being the current active one, and by then the window
// is gone -- so no `unhandledrejection` listener runs, #1298's guard
// never sees it, and the engine prints it to the console on its own.
// The only thing that stops it being reported is the promise already
// having a rejection handler, which is what the second test below
// pins. Node's own unhandled-rejection reporting is no use for that:
// the runner installs its own handling, so a promise left bare inside a
// test looks the same as one that was caught. Watching the promise
// itself is the check that actually distinguishes them.

import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  activateWaitingWorker,
  registerServiceWorker,
  SERVICE_WORKER_SCOPE,
  SERVICE_WORKER_URL,
  type ControllerWatcher,
  type LoadListener,
  type Registrar,
} from './serviceWorker'

/** A window that only remembers the `load` listener and can fire it. */
function fakeWindow(): LoadListener & { fireLoad: () => void } {
  let onLoad: (() => void) | null = null
  return {
    addEventListener: ((type: string, listener: EventListenerOrEventListenerObject) => {
      if (type === 'load') onLoad = listener as () => void
    }) as LoadListener['addEventListener'],
    fireLoad: () => onLoad?.(),
  }
}

/**
 * A rejected registration that reports whether the caller attached a
 * rejection handler to it.
 *
 * The underlying promise is caught here first, so the test itself never
 * leaves a bare rejection behind whichever way the assertion goes.
 */
function watchedRejection(reason: unknown) {
  const settled = Promise.reject(reason)
  settled.catch(() => {})
  let handled = false
  const promise = {
    then(onFulfilled?: unknown, onRejected?: unknown) {
      if (onRejected) handled = true
      return settled.then(onFulfilled as never, onRejected as never)
    },
    catch(onRejected?: unknown) {
      handled = true
      return settled.catch(onRejected as never)
    },
    finally(onFinally?: () => void) {
      return settled.finally(onFinally)
    },
  } as unknown as Promise<ServiceWorkerRegistration>
  return { promise, wasHandled: () => handled }
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('registerServiceWorker (#1314)', () => {
  it('registers the generated worker at the site root, once the page has loaded', () => {
    const register = vi.fn(() => Promise.resolve({} as ServiceWorkerRegistration))
    const win = fakeWindow()

    registerServiceWorker({ register } as Registrar, win)
    expect(register, 'nothing is registered before load').not.toHaveBeenCalled()

    win.fireLoad()
    expect(register).toHaveBeenCalledWith(SERVICE_WORKER_URL, { scope: SERVICE_WORKER_SCOPE })
    expect(SERVICE_WORKER_URL).toBe('/sw.js')
    expect(SERVICE_WORKER_SCOPE).toBe('/')
  })

  it('attaches a rejection handler, so a refused registration is never reported', async () => {
    // Exactly what Firefox rejects with once the document the
    // registration started on is no longer the current active one.
    const refusal = new DOMException(
      'An attempt was made to use an object that is not, or is no longer, usable',
      'InvalidStateError',
    )
    const watched = watchedRejection(refusal)
    const win = fakeWindow()

    registerServiceWorker({ register: () => watched.promise } as Registrar, win)
    win.fireLoad()

    expect(watched.wasHandled(), 'the registration promise was left bare').toBe(true)
    await Promise.resolve()
  })

  it('says nothing about a refused registration either', async () => {
    // A failed registration costs installability and offline, both of
    // which this app barely uses, and the operator has no action to
    // take -- so it is swallowed rather than logged.
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const win = fakeWindow()
    const watched = watchedRejection(new Error('nope'))

    registerServiceWorker({ register: () => watched.promise } as Registrar, win)
    win.fireLoad()
    await new Promise((r) => setTimeout(r, 0))

    expect(error).not.toHaveBeenCalled()
    expect(warn).not.toHaveBeenCalled()
  })

  it('does nothing at all where the browser has no service workers', () => {
    const win = fakeWindow()
    expect(() => registerServiceWorker(undefined, win)).not.toThrow()
    expect(() => win.fireLoad()).not.toThrow()
  })

  // #1363: main.ts hands this to freshnessState so a later upgrade can
  // ask the worker to take over before reloading.
  it('resolves with the registration once one lands', async () => {
    const registration = {} as ServiceWorkerRegistration
    const win = fakeWindow()

    const result = registerServiceWorker({ register: () => Promise.resolve(registration) } as Registrar, win)
    win.fireLoad()

    expect(await result).toBe(registration)
  })

  it('resolves with undefined where registration failed', async () => {
    const watched = watchedRejection(new Error('nope'))
    const win = fakeWindow()

    const result = registerServiceWorker({ register: () => watched.promise } as Registrar, win)
    win.fireLoad()

    expect(await result).toBeUndefined()
  })
})

/** A worker whose `state` can be driven, remembering the one message
 * handler activateWaitingWorker cares about. */
function fakeWorker(initialState: ServiceWorkerState): ServiceWorker & { setState: (s: ServiceWorkerState) => void } {
  let state = initialState
  let onStateChange: (() => void) | null = null
  return {
    get state() {
      return state
    },
    postMessage: vi.fn(),
    addEventListener: ((type: string, listener: EventListenerOrEventListenerObject) => {
      if (type === 'statechange') onStateChange = listener as () => void
    }) as ServiceWorker['addEventListener'],
    removeEventListener: vi.fn(),
    setState(s: ServiceWorkerState) {
      state = s
      onStateChange?.()
    },
  } as unknown as ServiceWorker & { setState: (s: ServiceWorkerState) => void }
}

/** A container that remembers the `controllerchange` listener and can fire it. */
function fakeContainer(): ControllerWatcher & { fireControllerChange: () => void } {
  const listeners = new Set<EventListenerOrEventListenerObject>()
  return {
    addEventListener: ((_type: string, listener: EventListenerOrEventListenerObject) => {
      listeners.add(listener)
    }) as ControllerWatcher['addEventListener'],
    removeEventListener: ((_type: string, listener: EventListenerOrEventListenerObject) => {
      listeners.delete(listener)
    }) as ControllerWatcher['removeEventListener'],
    fireControllerChange: () => {
      for (const l of listeners) (l as () => void)()
    },
  }
}

describe('activateWaitingWorker (#1363)', () => {
  it('resolves straightaway where nothing is waiting or installing', async () => {
    const container = fakeContainer()
    await expect(activateWaitingWorker(undefined, container)).resolves.toBeUndefined()
    await expect(
      activateWaitingWorker({ waiting: null, installing: null } as unknown as ServiceWorkerRegistration, container),
    ).resolves.toBeUndefined()
  })

  it('calls registration.update() before looking for a worker, per the ratified design', async () => {
    const container = fakeContainer()
    const registration = {
      waiting: null,
      installing: null,
      update: vi.fn(() => Promise.resolve()),
    } as unknown as ServiceWorkerRegistration

    await activateWaitingWorker(registration, container)

    expect(registration.update).toHaveBeenCalledOnce()
  })

  it('finds a worker that only registration.update() turns up -- not just whatever was already waiting or installing', async () => {
    const worker = fakeWorker('installed')
    const container = fakeContainer()
    const registration = {
      waiting: null,
      installing: null,
      // Simulates the real update() algorithm: the browser only starts
      // installing a new worker as a side effect of the update check
      // this call performs, so `installing` is unset until it resolves.
      update: vi.fn(() => {
        ;(registration as unknown as { installing: unknown }).installing = worker
        return Promise.resolve()
      }),
    } as unknown as ServiceWorkerRegistration

    const settled = activateWaitingWorker(registration, container)
    await Promise.resolve()
    await Promise.resolve()
    expect(worker.postMessage).toHaveBeenCalledWith({ type: 'SKIP_WAITING' })

    container.fireControllerChange()
    await settled
  })

  it('does not reject when registration.update() itself rejects (offline, a mid-flight navigation)', async () => {
    const container = fakeContainer()
    const registration = {
      waiting: null,
      installing: null,
      update: vi.fn(() => Promise.reject(new Error('offline'))),
    } as unknown as ServiceWorkerRegistration

    await expect(activateWaitingWorker(registration, container)).resolves.toBeUndefined()
  })

  it('asks an already-installed waiting worker to skip waiting, and resolves once it takes control', async () => {
    const worker = fakeWorker('installed')
    const container = fakeContainer()
    const registration = { waiting: worker, installing: null } as unknown as ServiceWorkerRegistration

    const settled = activateWaitingWorker(registration, container)
    let resolved = false
    void settled.then(() => (resolved = true))
    await Promise.resolve()

    expect(worker.postMessage).toHaveBeenCalledWith({ type: 'SKIP_WAITING' })
    expect(resolved).toBe(false)

    container.fireControllerChange()
    await settled
    expect(resolved).toBe(true)
  })

  it('waits for an installing worker to reach installed before asking it to skip waiting', async () => {
    const worker = fakeWorker('installing')
    const container = fakeContainer()
    const registration = { waiting: null, installing: worker } as unknown as ServiceWorkerRegistration

    const settled = activateWaitingWorker(registration, container)
    await Promise.resolve()
    expect(worker.postMessage).not.toHaveBeenCalled()

    worker.setState('installed')
    expect(worker.postMessage).toHaveBeenCalledWith({ type: 'SKIP_WAITING' })

    container.fireControllerChange()
    await settled
  })

  it('resolves anyway if no worker ever takes control, rather than hanging the one automatic reload forever', async () => {
    vi.useFakeTimers()
    try {
      const worker = fakeWorker('installed')
      const container = fakeContainer()
      const registration = { waiting: worker, installing: null } as unknown as ServiceWorkerRegistration

      const settled = activateWaitingWorker(registration, container, 3000)
      let resolved = false
      void settled.then(() => (resolved = true))

      await vi.advanceTimersByTimeAsync(2999)
      expect(resolved).toBe(false)
      await vi.advanceTimersByTimeAsync(1)
      expect(resolved).toBe(true)
    } finally {
      vi.useRealTimers()
    }
  })
})
