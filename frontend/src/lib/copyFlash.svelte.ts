// SPDX-License-Identifier: AGPL-3.0-only
//
// The one copy acknowledgement (#1542): the control that was clicked flips
// its own label to "copied" / "copy failed" for a moment, then returns.
// No toast. A modal overlay passes sentence-case words (#1541).

const HOLD_MS = 1500

export class CopyFlash {
  key = $state<string | null>(null)
  ok = $state(false)
  #timer: ReturnType<typeof setTimeout> | undefined

  /** Copy `text`, flash the outcome on the control named `key`. Returns ok. */
  async run(key: string, copy: () => Promise<boolean>): Promise<boolean> {
    const ok = await copy()
    this.key = key
    this.ok = ok
    if (this.#timer) clearTimeout(this.#timer)
    this.#timer = setTimeout(() => {
      if (this.key === key) this.key = null
    }, HOLD_MS)
    return ok
  }

  /** The label for `key`: the flipped word while flashing, else `idle`. */
  label(
    key: string,
    idle: string,
    words: { copied: string; failed: string } = { copied: 'copied', failed: 'copy failed' },
  ): string {
    if (this.key !== key) return idle
    return this.ok ? words.copied : words.failed
  }
}
