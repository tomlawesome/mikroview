// SPDX-License-Identifier: AGPL-3.0-only
//
// A single, app-wide transient status message (issue #439's "copied"
// feedback). There was no toast/transient-notice pattern anywhere in the
// app before this -- ConnectionBanner.svelte and the tokens-overlay
// "created" banner are both persistent until something else dismisses
// them, not "shows itself, then goes away on its own". This is
// deliberately the smallest version of that: one message at a time, no
// queue, no stacking -- a second `show()` while one is already visible
// simply replaces it and restarts the timer, which is the right
// behavior for a rapid double-copy rather than piling up messages nobody
// reads in time.
//
// Message text only, not markup or structure: every caller so far is a
// single short sentence (see CopyButton.svelte), and a queue/priority
// system would be solving a problem this app does not have yet.
class ToastState {
  message = $state<string | null>(null)

  #timer: ReturnType<typeof setTimeout> | null = null

  // 1500ms: long enough to read a two-word message, short enough that it
  // is gone well before the next click if someone copies several tokens
  // in a row.
  show(message: string, durationMs = 1500) {
    if (this.#timer !== null) clearTimeout(this.#timer)
    this.message = message
    this.#timer = setTimeout(() => {
      this.message = null
      this.#timer = null
    }, durationMs)
  }
}

export const toastState = new ToastState()
