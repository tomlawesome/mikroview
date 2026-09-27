<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // #1347: the config editor's "your password again" field -- the inline
  // re-entry PasskeysOverlay.svelte uses to remove a passkey (a labelled
  // password input, the action disabled until something is typed, the
  // server's refusal shown underneath), shared by the Engine Room's
  // Config card (to open the editor) and the editor itself (when the
  // 15-minute unlock has lapsed), so both ask in the same words and the
  // same shape.
  interface Props {
    prompt: string
    submitLabel: string
    busy?: boolean
    error?: string | null
    onsubmit: (password: string) => void | Promise<void>
    oncancel?: () => void
  }

  let { prompt, submitLabel, busy = false, error = null, onsubmit, oncancel }: Props = $props()

  let password = $state('')

  async function submit(e: Event) {
    e.preventDefault()
    if (!password || busy) return
    const typed = password
    password = ''
    await onsubmit(typed)
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && oncancel) {
      e.stopPropagation()
      oncancel()
    }
  }
</script>

<form class="again" onsubmit={submit}>
  <p class="prompt">{prompt}</p>
  <div class="line">
    <label>
      <span>Password</span>
      <!-- svelte-ignore a11y_autofocus -->
      <input
        type="password"
        bind:value={password}
        autocomplete="current-password"
        autofocus
        disabled={busy}
        onkeydown={onKeydown}
      />
    </label>
    <button type="submit" class="go" disabled={busy || !password}>{busy ? 'Checking…' : submitLabel}</button>
    {#if oncancel}
      <button type="button" class="cancel" onclick={oncancel} disabled={busy}>Cancel</button>
    {/if}
  </div>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
</form>

<style>
  .again {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 0;
  }

  .prompt {
    margin: 0;
    font-size: 12px;
    color: var(--fg-muted);
    line-height: 1.5;
  }

  .line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }

  label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--fg-dim);
  }

  input {
    font: 12px var(--font-mono);
    color: var(--fg);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 4px 8px;
    width: 18ch;
  }

  input:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  button {
    border-radius: 5px;
    padding: 4px 11px;
    font-size: 12px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    cursor: pointer;
  }

  button:hover:not(:disabled) {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  button:disabled {
    opacity: 0.6;
    cursor: default;
  }

  button.go {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 45%, transparent);
  }

  .error {
    margin: 0;
    font-size: 12px;
    color: var(--reject);
  }
</style>
