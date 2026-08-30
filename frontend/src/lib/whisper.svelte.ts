// SPDX-License-Identifier: AGPL-3.0-only
//
// Reactive state behind the whisper's click-to-seek and fence controls
// (issue #644, round-22/round-29's ratified "the whisper commands the
// stream"). Deliberately not part of appState.filters: neither seeking
// nor fencing is a filter -- both are display-only lenses over the same
// buffer, the same relationship appState.frozenPool/streamHeld already
// has to Autoscroll-off. A module singleton, not component-local state,
// so it survives the Stream card unmounting when the deck rolls to
// another scene (Deck.svelte only mounts the centred card and its
// neighbours).
import { appState } from './state.svelte'

class WhisperState {
  fenceOn = $state(false)
  // The first of the fence's two clicks, in ms -- null once idle, or
  // once the second click has closed a range.
  fenceFirst = $state<number | null>(null)
  // The closed fence range, in ms, end-exclusive -- null means "no
  // fence applied", the one thing LiveTable/EventRow need to decide
  // what to dim.
  fenceRange = $state<{ start: number; end: number } | null>(null)
  // The single minute (bucket start, ms) the whisper's stat line
  // reports from a plain, non-fence click -- null means "the rolling
  // window", the default.
  seekMs = $state<number | null>(null)

  // fenceOn flips the mode; a fence already drawn is cleared with it --
  // turning fencing off is "back to the rolling window", not "keep the
  // last range dimmed but stop being able to redraw it".
  toggleFence() {
    this.fenceOn = !this.fenceOn
    this.fenceFirst = null
    if (!this.fenceOn) this.fenceRange = null
  }

  // One call per curve click, at the minute (bucket start, ms) actually
  // hit. Fencing consumes two calls per range; plain mode seeks on
  // every call.
  clickMinute(minuteMs: number) {
    if (this.fenceOn) {
      if (this.fenceFirst === null) {
        this.fenceFirst = minuteMs
        this.fenceRange = null
      } else {
        const start = Math.min(this.fenceFirst, minuteMs)
        const end = Math.max(this.fenceFirst, minuteMs) + 60_000
        this.fenceRange = { start, end }
        this.fenceFirst = null
      }
      return
    }
    this.seekMs = minuteMs
    // Round 22's own ratified line: "click to seek... autoscroll off" --
    // flips the real toggle rather than a copy of its state, so the
    // scene bar's own Autoscroll button stays the single source of
    // truth for whether the stream is held.
    appState.autoscroll = false
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
