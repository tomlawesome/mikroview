<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // The pencil (issue #413), in the slot #439 reserved for it: the same
  // hover/focus reveal as the copy glyph, immediately after it, on
  // label-bearing tokens only. A sibling of the click-to-filter target
  // rather than something inside it, exactly like CopyButton -- so
  // plain click still filters, drag still selects, and nothing
  // collides.
  //
  // Admins only, and absent rather than disabled for everyone else --
  // see nameEditorState.available, which is also what EventRow asks
  // before building one of these at all.
  //
  // Visibility on desktop (opacity 0 until the row is hovered or holds
  // focus) is EventRow's CSS targeting `.edit-btn`, the same way it
  // handles `.copy-btn` -- see CopyButton.svelte for why the default
  // here is plainly visible instead.
  import { nameEditorState, type EditableTokenType } from '../lib/nameEditor.svelte'

  let {
    type,
    // The RAW value this token stands for -- the IP, the port number as
    // a string, the raw log prefix. Never the displayed label: the
    // entity is keyed by the raw value, and `label` below is only for
    // the accessible name.
    value,
    // Which device the traffic was seen on. Scopes the router-pushed
    // name layer, which exists for hosts only (see
    // internal/naming.Resolver.Host); '' for ports and rules.
    device = '',
    label,
  }: { type: EditableTokenType; value: string; device?: string; label: string } = $props()

  let btnEl: HTMLButtonElement | undefined = $state()

  function onClick(e: Event) {
    // Defensive, matching CopyButton: this is always a sibling of the
    // filter target, never nested inside it, so the row's own
    // click-vs-drag handlers are not on this element's ancestors.
    e.stopPropagation()
    if (!btnEl) return
    nameEditorState.open(type, value, device, btnEl.getBoundingClientRect())
  }
</script>

{#if nameEditorState.available}
  <button
    bind:this={btnEl}
    type="button"
    class="edit-btn"
    onclick={onClick}
    title="Edit name for {label}"
    aria-label="Edit name for {label}"
  >
    ✎
  </button>
{/if}

<style>
  .edit-btn {
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

  .edit-btn:hover {
    color: var(--accent);
    background: var(--accent-bg-hover);
  }

  .edit-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
</style>
