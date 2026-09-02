// SPDX-License-Identifier: AGPL-3.0-only
//
// Reactive state behind the whisper's click-to-seek and drag-to-fence
// controls (issue #644, elegant-fence redraw #717). Deliberately not
// part of appState.filters: neither seeking nor fencing is a filter --
// both are display-only lenses over the same buffer, the same
// relationship appState.frozenPool/streamHeld already has to
// Autoscroll-off. A module singleton, not component-local state, so it
// survives the Stream card unmounting when the deck rolls to another
// scene (Deck.svelte only mounts the centred card and its neighbours).
//
// #717 replaced the old "arm with a button, then two clicks" fence with
// "click seeks, drag fences" -- the gesture and the bucket-index math it
// needs live in Whisper.svelte (it alone knows where the curve's pixels
// fall), so this file keeps only the two facts anything downstream
// needs: the seeked minute and the closed fence range.
import { appState } from './state.svelte'

class WhisperState {
  // The closed fence range, in ms, end-exclusive -- null means "no
  // fence applied", the one thing LiveTable/EventRow need to decide
  // what to dim.
  fenceRange = $state<{ start: number; end: number } | null>(null)
  // The single minute (bucket start, ms) the whisper's stat line
  // reports from a plain click -- null means "the rolling window", the
  // default.
  seekMs = $state<number | null>(null)

  // A plain click on the curve: seeks to that minute, and cancels any
  // fence already drawn -- fencing and seeking are mutually exclusive,
  // and a click clearing a stale fence (rather than requiring a
  // separate "clear" control) is the whole of #717's clearing story.
  seek(minuteMs: number) {
    this.seekMs = minuteMs
    this.fenceRange = null
    // Round 22's own ratified line: "click to seek... autoscroll off" --
    // flips the real toggle rather than a copy of its state, so the
    // `following` pill on the whisper's own line (Whisper.svelte's hand,
    // rounds 36-38) stays the single source of truth for whether the
    // stream is held, and is the way back -- see resumeFollowing.
    appState.autoscroll = false
  }

  // A closed range, from either a mouse drag or the keyboard's two
  // Enter presses. Order-independent -- the caller need not know which
  // edge came first.
  //
  // Stops the stream following for the same reason a seek does (round
  // 36: "a seek or a fence turns it off"): a fence is a window on the
  // past, and lines arriving now would keep scrolling the reader away
  // from the range they just drew.
  setFenceRange(aMs: number, bMs: number) {
    this.fenceRange = { start: Math.min(aMs, bMs), end: Math.max(aMs, bMs) }
    appState.autoscroll = false
  }

  clearFence() {
    this.fenceRange = null
  }

  // The way back, and the whole of #749: unmounting the only writer of
  // `autoscroll` left seeking able to turn following off with nothing
  // in the app able to turn it on again, so a seek held the stream for
  // the rest of the session and only a reload recovered it.
  //
  // Following again is not just the toggle: the cursor and the fence
  // are the *reason* it went off, so re-following clears both rather
  // than leaving the stream chasing new lines through a window drawn
  // over old ones (round 36: "clicking it follows again, clears the
  // cursor and the window").
  resumeFollowing() {
    this.seekMs = null
    this.fenceRange = null
    appState.autoscroll = true
  }

  // Turning it off by hand, from the same pill. Leaves any cursor or
  // fence exactly as drawn -- this says "stop moving", not "forget
  // where I was looking".
  stopFollowing() {
    appState.autoscroll = false
  }

  get following(): boolean {
    return appState.autoscroll
  }

  // Whether a row's receipt time falls outside the active fence --
  // always false with no fence set, so callers can use this
  // unconditionally rather than guarding on fenceRange themselves.
  dimmed(receivedAt: number): boolean {
    const r = this.fenceRange
    return r !== null && (receivedAt < r.start || receivedAt >= r.end)
  }
}

export const whisperState = new WhisperState()
