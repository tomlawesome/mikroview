// SPDX-License-Identifier: AGPL-3.0-only
//
// Thin wrapper around `document`'s visibilitychange event (#1088). Pulled
// out of App.svelte so its pause/resume-polling-in-a-background-tab logic
// can be driven by a fake document in vitest instead of the real one --
// jsdom does not implement toggling document.hidden at all.
export interface VisibilityDocument {
  readonly hidden: boolean
  addEventListener(type: 'visibilitychange', listener: () => void): void
  removeEventListener(type: 'visibilitychange', listener: () => void): void
}

// Calls `cb` with the current hidden state every time visibility changes.
// Returns an unsubscribe function; callers should invoke it from their own
// teardown (e.g. an $effect's cleanup) to avoid leaking the listener.
export function subscribe(cb: (hidden: boolean) => void, doc: VisibilityDocument = document): () => void {
  const handler = () => cb(doc.hidden)
  doc.addEventListener('visibilitychange', handler)
  return () => doc.removeEventListener('visibilitychange', handler)
}
