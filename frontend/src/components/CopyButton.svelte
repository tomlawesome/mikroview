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
  // The secure-context fallback lives in lib/clipboard.ts, shared with
  // the metrics Table's own copy control (#488) so there is one answer
  // to "what happens with TLS terminated upstream", not two.
  import { copyToClipboard } from '../lib/clipboard'

  let { value, label }: { value: string; label: string } = $props()

  async function copy(e: Event) {
    // Stop this reaching the row -- purely defensive, since this button
    // is always a sibling of the filter target, never nested inside it,
    // so the row's own mousedown/mouseup click-vs-drag handlers are
    // never attached to this element's ancestors either way.
    e.stopPropagation()
    toastState.show((await copyToClipboard(value)) ? 'Copied' : 'Copy failed')
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
