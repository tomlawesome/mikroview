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
  import { CopyFlash } from '../lib/copyFlash.svelte'
  // The secure-context fallback lives in lib/clipboard.ts, shared with
  // the metrics Table's own copy control (#488) so there is one answer
  // to "what happens with TLS terminated upstream", not two.
  import { copyToClipboard } from '../lib/clipboard'

  let { value, label }: { value: string; label: string } = $props()

  const flash = new CopyFlash()
  const flipped = $derived(flash.label('copy', ''))

  async function copy(e: Event) {
    // Stop this reaching the row -- purely defensive, since this button
    // is always a sibling of the filter target, never nested inside it,
    // so the row's own mousedown/mouseup click-vs-drag handlers are
    // never attached to this element's ancestors either way.
    e.stopPropagation()
    await flash.run('copy', () => copyToClipboard(value))
  }
</script>

<button
  type="button"
  class="copy-btn"
  class:flipped={flipped !== ''}
  onclick={copy}
  title="Copy {label}"
  aria-label={flipped || `Copy ${label}: ${value}`}
>
  {flipped || '⧉'}
</button>

<style>
  .copy-btn {
    flex: none;
    min-width: 17px;
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

  .copy-btn.flipped {
    padding: 0 4px;
    white-space: nowrap;
    color: var(--accent);
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
