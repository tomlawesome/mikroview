// SPDX-License-Identifier: AGPL-3.0-only

// #1298: a poll that was cut off is not a failure, and must not be
// reported as one.
//
// The app polls with plain `fetch()` on a `setInterval` in half a dozen
// places (Fall, Metrics, Settings, the setup wizard, App.svelte's stats
// and flags). Leave the page -- a reload, a link, closing the tab --
// and whatever was in flight is cancelled by the browser. The promise
// then rejects, and because the request is going nowhere useful nobody
// has a reason to have written a handler for it, so the rejection
// reaches the console as an error. The operator sees red text about a
// request they cancelled by navigating; the live-browser scenarios see
// it too, which is why frontend/scripts/live-browser.mjs has had to
// filter one exact Firefox string out of its console-error check.
//
// Each engine words the same cancellation differently:
//
//   Firefox    AbortError: The operation was aborted.
//              TypeError: NetworkError when attempting to fetch resource.
//   Chromium   TypeError: Failed to fetch          (usually silent)
//   WebKit     TypeError: Load failed / cancelled
//
// Only the AbortError is unambiguous. The others are word for word what
// a *genuine* network failure says as well -- the one that must keep
// surfacing, because "Waiting for events…", the fall's window message
// and App.svelte's refresh banner are all built on it. So those shapes
// count as a cancellation only once we know the request had nowhere to
// land: the page is on its way out, or the poller that sent it has been
// stopped. While the page is live and the poller is running, a failure
// is a failure and behaves exactly as it always has.

/** Reported by Firefox, Chromium and WebKit for a fetch cut short. Also
 * exactly what each says for a real connection failure, which is why
 * these are only read as a cancellation once `leaving` or the caller's
 * own `stopped` flag says the answer had nowhere to go. */
const CUT_OFF_MESSAGES = [
  'NetworkError when attempting to fetch resource.',
  'Failed to fetch',
  'Load failed',
  'The operation was aborted.',
  'cancelled',
]

// Whether this page is on its way out. Set by the guard below from the
// events the browser fires before it tears the document down, and
// cleared again on `pageshow` -- Firefox and Safari can bring a page
// back out of the back/forward cache, and a restored page is live
// again, so its failures must surface again too.
let leaving = false

/** For the guard and its tests; nothing else needs to ask. */
export function pageIsLeaving(): boolean {
  return leaving
}

/**
 * Whether a rejected fetch means "cancelled", not "failed".
 *
 * `stopped` is for a caller that knows its own poller has been torn
 * down -- an effect's cleanup has run, the view has been left -- and so
 * knows the answer it is looking at had nowhere to land either. It
 * defaults to false, so a caller that says nothing gets the strict
 * reading: only an AbortError, or a page that is unloading.
 */
export function isCancelledFetch(err: unknown, stopped = false): boolean {
  const e = err as { name?: unknown; message?: unknown; status?: unknown } | null | undefined
  if (e === null || e === undefined) return false
  // An ApiError carries the HTTP status, which means the server
  // answered: whatever went wrong, it was not a cancelled request.
  if (typeof e.status === 'number') return false
  if (e.name === 'AbortError') return true
  if (!leaving && !stopped) return false
  if (typeof e.message !== 'string') return false
  const message = e.message
  return CUT_OFF_MESSAGES.some((m) => message.includes(m))
}

/**
 * Install the one place cancellations are filtered, for the whole app.
 *
 * Two jobs. It watches for the page leaving, which is what lets
 * `isCancelledFetch` read an otherwise ambiguous message as a
 * cancellation. And it takes over reporting unhandled rejections: a
 * cancellation is dropped, everything else is handed to `console.error`
 * exactly as before. `preventDefault()` is what stops the engine
 * printing its own copy, so nothing is reported twice.
 *
 * Doing it here rather than in each poller is deliberate: there is no
 * shared read path through lib/api.ts (every GET calls `fetch` itself),
 * so a per-caller fix would be forty edits and would miss the next
 * caller written. One listener covers every fetch in the app, including
 * ones whose rejection nobody handles.
 *
 * Returns the function that uninstalls it, for tests.
 */
export function installCancellationGuard(scope: Window = window): () => void {
  const leave = () => {
    leaving = true
  }
  const arrive = () => {
    leaving = false
  }
  const onRejection = (event: PromiseRejectionEvent) => {
    event.preventDefault()
    if (isCancelledFetch(event.reason)) return
    console.error(event.reason)
  }
  const rejection = onRejection as EventListener
  scope.addEventListener('pagehide', leave)
  scope.addEventListener('beforeunload', leave)
  scope.addEventListener('pageshow', arrive)
  scope.addEventListener('unhandledrejection', rejection)
  return () => {
    scope.removeEventListener('pagehide', leave)
    scope.removeEventListener('beforeunload', leave)
    scope.removeEventListener('pageshow', arrive)
    scope.removeEventListener('unhandledrejection', rejection)
    leaving = false
  }
}
