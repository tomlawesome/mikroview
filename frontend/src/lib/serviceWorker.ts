// SPDX-License-Identifier: AGPL-3.0-only

// #1314: the app registers its own service worker, so that the
// registration promise has somewhere to fail.
//
// vite-plugin-pwa's generated `registerSW.js` was doing it, and its one
// line ends without a `catch`:
//
//   window.addEventListener('load', () =>
//     navigator.serviceWorker.register('/sw.js', { scope: '/' }))
//
// Firefox rejects a registration that is still in flight the moment the
// document stops being the current active one -- reload or navigate
// soon enough after first load and the registration started on `load`
// is still running. The rejection is `InvalidStateError: An attempt was
// made to use an object that is not, or is no longer, usable`, and
// nothing in the app can hear it: the window an `unhandledrejection`
// would be dispatched on has already gone, so #1298's cancellation
// guard never sees it either, and the engine prints it straight to the
// console. Measured against a real instance under Firefox: reload 100ms
// after load and it appears on 8 loads out of 8; at 300ms on an idle
// host, never. That is why it shows up as a rare failure on a
// four-shard gate -- a loaded host is the slow-registration case -- and
// why Chromium, which reports nothing here, stays quiet.
//
// Attaching a handler is the whole fix: a rejected promise that has one
// is never reported. The same measurement with a `catch` in place logs
// nothing at any gap.
//
// This has to live in the app rather than be fixed where it happened,
// because `registerSW.js` is generated: `injectRegister: false` in
// vite.config.ts hands the job over here. The URL and scope are the
// plugin's own defaults for the worker it generates (`dist/sw.js`,
// served from the site root).

/** Where the generated worker is served from, and what it controls. */
export const SERVICE_WORKER_URL = '/sw.js'
export const SERVICE_WORKER_SCOPE = '/'

/** Just the part of ServiceWorkerContainer this needs, so a test can pass a stub. */
export type Registrar = Pick<ServiceWorkerContainer, 'register'>

/** Just the part of Window this needs, for the same reason. */
export type LoadListener = Pick<Window, 'addEventListener'>

/**
 * registerServiceWorker installs the PWA worker once the page has
 * loaded, exactly as the plugin's injected script did, and swallows a
 * registration that never lands.
 *
 * Swallowed rather than reported, on purpose. A failed registration
 * costs the install-as-an-app and offline behaviour and nothing else --
 * the app itself is already running, fetches straight from the server
 * and does not cache any live data (see vite.config.ts) -- so there is
 * no operator action to prompt and nothing to say. docs/configuration.md
 * already covers the one case an operator can act on, a certificate the
 * browser does not trust.
 *
 * Resolves with the registration on success, or `undefined` either where
 * there is nothing to register or the registration itself failed --
 * #1363's freshness check holds onto it (main.ts) so a later upgrade can
 * ask the waiting worker to take over before reloading into it. The
 * `.then(_, _)` below is the one place that has to touch the raw
 * registration promise directly: it is both this function's success
 * path and the rejection handler the comment above swallows failure
 * with, so a caller here never has two independent listeners racing to
 * decide whether Firefox's mid-flight rejection was ever reported.
 */
export function registerServiceWorker(
  container: Registrar | undefined = typeof navigator === 'undefined' ? undefined : navigator.serviceWorker,
  scope: LoadListener | undefined = typeof window === 'undefined' ? undefined : window,
): Promise<ServiceWorkerRegistration | undefined> {
  if (!container || !scope) return Promise.resolve(undefined)
  return new Promise((resolve) => {
    scope.addEventListener('load', () => {
      container.register(SERVICE_WORKER_URL, { scope: SERVICE_WORKER_SCOPE }).then(
        (registration) => resolve(registration),
        () => resolve(undefined),
      )
    })
  })
}

/** Just the part of ServiceWorkerContainer #1363's handover needs, so a test can pass a stub. */
export type ControllerWatcher = Pick<ServiceWorkerContainer, 'addEventListener' | 'removeEventListener' | 'controller'>

/**
 * activateWaitingWorker is the handover the design calls for before a
 * safe reload: `registration.update()` first, so a worker this tab's
 * registration has not polled for yet (the freshness check can fire long
 * before the browser's own 24-hour update cycle would) is actually found,
 * then ask whatever is waiting (or still installing) to take over, and
 * resolve once it has taken control *and* finished activating -- so the
 * reload that follows is served by the new worker rather than the one
 * this tab loaded with.
 *
 * Not at `controllerchange` alone (#1421): that fires as soon as
 * activation starts, while the worker is still `activating` and running
 * its own activate step. Chromium holds a navigation started then until
 * activation finishes; WebKit lets it through, and the reloaded page is
 * left holding a copy of the worker that reads `activating` for good --
 * no `statechange` ever reaches it, although the worker itself finished
 * activating moments later. Waiting here for `activated` means the
 * reload never starts mid-activation, in any engine.
 *
 * Resolves anyway after `timeoutMs` if no worker ever takes control or
 * finishes activating -- `update()` found nothing new, the worker never
 * reaches `installed` or `activated`, or a browser that never fires
 * `controllerchange` for a case this hasn't seen -- so a real version
 * mismatch still reloads rather than hanging the one automatic attempt
 * forever on a promise that will never settle. `update()` rejecting
 * (offline, a mid-flight navigation) is swallowed the same way -- the
 * handover is a best effort, not a precondition for the reload that
 * follows it.
 */
export async function activateWaitingWorker(
  registration: ServiceWorkerRegistration | undefined,
  container: ControllerWatcher | undefined = typeof navigator === 'undefined' ? undefined : navigator.serviceWorker,
  timeoutMs = 3000,
): Promise<void> {
  if (!registration || !container) return

  try {
    await registration.update?.()
  } catch {
    // Best effort -- see the doc comment above.
  }

  const worker = registration.waiting ?? registration.installing
  if (!worker) return

  return new Promise((resolve) => {
    let settled = false
    let controlling: ServiceWorker | null = null
    const finish = () => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      container.removeEventListener('controllerchange', onControllerChange)
      controlling?.removeEventListener('statechange', onActivation)
      resolve()
    }
    // `redundant` counts as done too: a worker that will never reach
    // `activated` leaves nothing more worth waiting for.
    const onActivation = () => {
      if (controlling?.state === 'activated' || controlling?.state === 'redundant') finish()
    }
    const onControllerChange = () => {
      if (controlling) return
      controlling = container.controller ?? worker
      controlling.addEventListener('statechange', onActivation)
      onActivation()
    }
    const timer = setTimeout(finish, timeoutMs)
    container.addEventListener('controllerchange', onControllerChange)

    const skipWaiting = () => worker.postMessage({ type: 'SKIP_WAITING' })
    if (worker.state === 'installed') {
      skipWaiting()
    } else {
      worker.addEventListener('statechange', function onStateChange() {
        if (worker.state !== 'installed') return
        worker.removeEventListener('statechange', onStateChange)
        skipWaiting()
      })
    }
  })
}
