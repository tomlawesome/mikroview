<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Hover-revealed per-token copy control (issue #439). Writes `value` --
  // always the RAW value a row token stands for (the IP, not a resolved
  // hostname; the port number, not the service name; the raw rule label,
  // not its friendly name; the device id, not its configured display
  // name) -- to the clipboard. `label` is a short human description used
  // in the accessible name and the title tooltip ("Copy source IP"), not
  // shown visually.
  //
  // Visibility (opacity 0 until the row is hovered or something inside it
  // has focus) is the caller's CSS, not this component's -- EventRow.svelte
  // targets `:global(.copy-btn)` from `.row:hover`/`.row:focus-within`.
  // This component renders plainly visible by default on purpose, so a
  // caller with no such hover concept (EventDetailSheet.svelte's mobile
  // sheet, which has no hover at all) gets an always-visible button for
  // free rather than having to override a default-hidden state back to
  // shown.
  import { toastState } from '../lib/toast.svelte'

  let { value, label }: { value: string; label: string } = $props()

  // navigator.clipboard.writeText requires a secure context (HTTPS or
  // localhost). mikroview serves TLS by default and needs no
  // configuration to get it (README.md), but docs/configuration.md's
  // "Running behind a reverse proxy" section explicitly documents
  // disabling it (MIKROVIEW_TLS_ENABLED=false) for an operator whose own
  // reverse proxy terminates TLS instead -- on an isolated management
  // network that is a supported deployment, not a misconfiguration, and
  // it leaves the Clipboard API unavailable. legacyCopy below (a hidden
  // textarea + document.execCommand('copy'), which carries no
  // secure-context requirement) is what keeps the glyph doing real work
  // there instead of silently failing every time it's clicked -- the
  // same "try the good path, fall back rather than go dead" shape
  // TokensOverlay.svelte's copyValue already uses for its one-time token
  // banner, just with an actual fallback instead of relying on the value
  // staying manually selectable (this control lives on text that, after
  // this issue, *is* selectable, but a control advertised as "click to
  // copy" that sometimes just does nothing is worse than one that always
  // works).
  async function copy(e: Event) {
    // Stop this reaching the row -- purely defensive, since this button
    // is always a sibling of the filter target, never nested inside it,
    // so the row's own mousedown/mouseup click-vs-drag handlers are
    // never attached to this element's ancestors either way.
    e.stopPropagation()

    let ok = false
    if (navigator.clipboard?.writeText) {
      try {
        await navigator.clipboard.writeText(value)
        ok = true
      } catch {
        ok = false
      }
    }
    if (!ok) ok = legacyCopy(value)

    toastState.show(ok ? 'Copied' : 'Copy failed')
  }

  function legacyCopy(text: string): boolean {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    // Off-screen rather than hidden -- a hidden/zero-size element cannot
    // be selected, which execCommand('copy') needs.
    ta.style.position = 'fixed'
    ta.style.top = '-1000px'
    ta.style.left = '-1000px'
    document.body.appendChild(ta)
    ta.select()
    let ok = false
    try {
      ok = document.execCommand('copy')
    } catch {
      ok = false
    }
    document.body.removeChild(ta)
    return ok
  }
</script>

<button
  type="button"
  class="copy-btn"
  onclick={copy}
  title="Copy {label}"
  aria-label="Copy {label}: {value}"
>
  ⧉
</button>

<style>
  .copy-btn {
    flex: none;
    width: 17px;
    height: 17px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0;
    font-size: 11px;
    line-height: 1;
    color: var(--fg-dim);
    background: transparent;
    border: 1px solid transparent;
    border-radius: 4px;
    cursor: pointer;
  }

  .copy-btn:hover {
    color: var(--accent);
    background: var(--accent-bg-hover);
  }

  .copy-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
</style>
