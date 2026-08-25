<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // The inline name editor (issue #413). Single instance mounted once at
  // the app root (see App.svelte), driven by lib/nameEditor.svelte.ts's
  // singleton -- same family as IpLookupPopover / RouterLookupPopover,
  // and fixed-positioned from the trigger's own screen coordinates for
  // the same reason: an absolutely-positioned popover anchored inside
  // the table would be clipped by LiveTable's scrolling body.
  //
  // An anchored popover rather than swapping the token's text for an
  // input. The design record rejected in-cell editing outright: the cell
  // is ellipsized and about 120px wide inside a grid whose rows move, so
  // it guarantees a layout jump, and Enter/blur inside it is ambiguous
  // against click-to-filter.
  //
  // The one thing this component must never do is present a writable
  // field for an edit that would be discarded. On a router-named token
  // there is no input at all -- see the refusal branch below, and
  // nameEditorState.editable for the gate itself.
  import { nameEditorState as st } from '../lib/nameEditor.svelte'
  import { trapFocus } from '../lib/focusTrap'

  // `popover` carries the shared chrome the lookup popovers already
  // define for themselves; `name-editor` is what tells this one apart
  // from them, for anything (a live-check scenario, an assistive tool)
  // that needs to name *this* dialog rather than whichever popover
  // happens to be open.
  const POPOVER_WIDTH = 300

  let popoverEl: HTMLDivElement | undefined = $state()
  let inputEl: HTMLInputElement | undefined = $state()

  function onDocClick(e: MouseEvent) {
    if (popoverEl && !popoverEl.contains(e.target as Node)) st.close()
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') st.close()
  }

  $effect(() => {
    if (!st.anchor) return
    // Deferred past the current click's bubble phase -- the click on the
    // pencil that opened this would otherwise still be on its way up to
    // `document` when the listener attaches, closing what it just
    // opened. Same reasoning as IpLookupPopover.svelte.
    const timer = setTimeout(() => document.addEventListener('click', onDocClick))
    return () => {
      clearTimeout(timer)
      document.removeEventListener('click', onDocClick)
    }
  })

  // Focus moves to the input as soon as the provenance answer allows one
  // to exist. trapFocus (used on the whole dialog below) focuses the
  // first focusable child on mount, but at mount time this is still
  // loading and the input is not in the DOM yet, so the field itself
  // needs claiming when it appears.
  $effect(() => {
    if (st.editable && inputEl) inputEl.focus()
  })

  const style = $derived.by(() => {
    const a = st.anchor
    if (!a) return ''
    const x = Math.min(a.x, window.innerWidth - POPOVER_WIDTH - 12)
    const y = Math.min(a.y + 6, window.innerHeight - 220)
    return `left: ${Math.max(8, x)}px; top: ${Math.max(8, y)}px`
  })

  function onInputKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault()
      void st.save()
    }
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if st.anchor}
  <div
    bind:this={popoverEl}
    class="popover name-editor"
    {style}
    role="dialog"
    aria-label={st.title}
    tabindex="-1"
    use:trapFocus
  >
    <div class="header">
      <span class="title">{st.title}</span>
      <button class="close" onclick={() => st.close()} aria-label="Close">✕</button>
    </div>

    <!-- The identity line: the raw value is always visible, whatever
         else is or is not shown, so a bad rename can never hide what the
         row is actually about. -->
    <div class="identity">{st.identityLine}</div>

    {#if st.loading}
      <div class="status">Checking where this name comes from…</div>
    {:else if st.error}
      <div class="status error">{st.error}</div>
    {:else if !st.editable}
      <!-- The gate (owner ruling, 2026-08-22). RouterOS keeps winning,
           so there is no field here to type a discarded name into --
           just what supplies the name and where to change it. -->
      <p class="refusal">{st.refusal}</p>
    {:else}
      <label class="field">
        <span class="field-label">Display name</span>
        <input
          bind:this={inputEl}
          bind:value={st.draft}
          type="text"
          spellcheck="false"
          autocomplete="off"
          onkeydown={onInputKeydown}
        />
      </label>
      <div class="hint">Leave empty to show the raw value again.</div>
      <div class="scope">{st.scopeLine}</div>
    {/if}

    <!-- Standing subtext, shown in every state including the refusal:
         it is what stops "display name" being read as "rename the
         thing". Filters, groups and copies never move off the raw
         value. -->
    <div class="subtext">Display only. Filters, groups and copies keep using the raw value.</div>

    <div class="actions">
      {#if st.editable}
        <button class="save" onclick={() => st.save()} disabled={st.saving}>
          {st.saving ? 'Saving…' : 'Save'}
        </button>
      {/if}
      <button class="cancel" onclick={() => st.close()}>
        {st.editable ? 'Cancel' : 'Close'}
      </button>
    </div>
  </div>
{/if}

<style>
  .popover {
    position: fixed;
    width: 300px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 7px;
    padding: 10px 12px;
    box-shadow: 0 12px 32px -8px rgba(0, 0, 0, 0.4);
    z-index: 41;
    font-size: 13px;
  }

  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 8px;
  }

  .title {
    font-weight: 600;
    color: var(--fg);
  }

  .close {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    width: 20px;
    height: 20px;
    font-size: 11px;
    line-height: 1;
  }

  .close:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .identity {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg-muted);
    margin-bottom: 8px;
    overflow-wrap: anywhere;
  }

  .status {
    color: var(--fg-dim);
    padding: 4px 0;
  }

  .status.error {
    color: var(--reject);
  }

  /* Deliberately styled as a notice, not as a disabled input: a greyed
     box with no words reads as something broken, where this is the
     product working exactly as decided. */
  .refusal {
    margin: 0 0 8px;
    padding: 8px;
    border: 1px solid var(--border);
    border-left: 3px solid var(--reject);
    border-radius: 5px;
    color: var(--fg-muted);
    line-height: 1.45;
  }

  .field {
    display: block;
    margin-bottom: 6px;
  }

  .field-label {
    display: block;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--fg-dim);
    margin-bottom: 3px;
  }

  .field input {
    width: 100%;
    box-sizing: border-box;
    padding: 5px 7px;
    font: inherit;
    color: var(--fg);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 5px;
  }

  .field input:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -1px;
  }

  .hint,
  .scope {
    font-size: 12px;
    color: var(--fg-dim);
    margin-bottom: 6px;
  }

  .subtext {
    font-size: 11px;
    color: var(--fg-dim);
    border-top: 1px solid var(--border);
    padding-top: 6px;
    margin-top: 2px;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 6px;
    margin-top: 8px;
  }

  .actions button {
    font: inherit;
    font-size: 12px;
    padding: 4px 10px;
    border-radius: 5px;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--fg-muted);
    cursor: pointer;
  }

  .actions .save {
    border-color: var(--accent);
    color: var(--accent);
  }

  .actions .save:disabled {
    opacity: 0.6;
    cursor: default;
  }

  .actions button:hover:not(:disabled) {
    color: var(--fg);
    border-color: var(--fg-muted);
  }
</style>
