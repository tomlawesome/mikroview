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
 */
export function registerServiceWorker(
  container: Registrar | undefined = typeof navigator === 'undefined' ? undefined : navigator.serviceWorker,
  scope: LoadListener | undefined = typeof window === 'undefined' ? undefined : window,
): void {
  if (!container || !scope) return
  scope.addEventListener('load', () => {
    void container.register(SERVICE_WORKER_URL, { scope: SERVICE_WORKER_SCOPE }).catch(() => {})
  })
}
