// SPDX-License-Identifier: AGPL-3.0-only
//
// The one register for what the operator reads when a request fails.
// Kept out of api.ts so a store can use it while a test mocks the api
// module whole.

// #1162: what the operator reads when a request fails.
//
// The throwing functions below used to carry only their own name and a
// status ("mark as expected: setFlagVerdict: 403", "Could not load the
// window: fetchEventsWindow: 500") while the server had already sent
// words for it -- "user role required", "flag not found" -- in the body,
// which was thrown away. putHostMark has forwarded that body all along;
// this is the same move, with a fallback in words rather than a bare
// number for the refusals that carry no body at all (the CSRF gate's
// 403, a proxy's 502).
//
// The body is only forwarded when it reads like a message: mikroview's
// own handlers answer with one short line (internal/api's httpError),
// while an HTML error page from something in front of it is not
// something to paste into a toast.
export function wordsForStatus(status: number): string {
  if (status === 401) return 'your session has expired — sign in again'
  if (status === 403) return 'you are not allowed to do that'
  if (status === 404) return 'the server has no record of that'
  if (status === 409) return 'something else changed it first — reload and try again'
  if (status === 429) return 'too many requests just now — try again in a moment'
  if (status >= 500) return `the server could not do that (${status})`
  return `the server refused that (${status})`
}

// The one sentence for "no answer came at all" (#1540): the browser's own
// "Failed to fetch" and the connection-dropped refusal of send() both read
// as this, so it reads the same on every screen. Lower case after the
// "Could not <verb> <thing>:" lead-in of the shared failure line.
export const NO_ANSWER = "can't reach MikroView — check the connection and try again"

// #1540: what the operator reads for a caught error. The client's own
// texts become plain words -- "fetchHostDossier: 503" (a function name and
// a status) takes wordsForStatus's register, and the browser's "Failed to
// fetch" (a TypeError, no response) takes NO_ANSWER. The server's own
// words pass through verbatim.
export function plainWords(err: unknown): string {
  if (err instanceof TypeError) return NO_ANSWER
  const text = err instanceof Error ? err.message : String(err ?? '')
  const bare = /^[A-Za-z]\w*: (\d{3})$/.exec(text)
  if (bare) return wordsForStatus(Number(bare[1]))
  return text
}

// The one lead-in (#1540, C4): "Could not <verb> <thing>: <words>". Stores
// that hold a failure for a screen build it here, so the screen shows the
// store's text as it is and the FailureLine's own wording is matched.
export function couldNot(verb: string, thing: string, said: unknown): string {
  const words = plainWords(said)
  return `Could not ${verb} ${thing}${words ? `: ${words}` : ''}`
}
