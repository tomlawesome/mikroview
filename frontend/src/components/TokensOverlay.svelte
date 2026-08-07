<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Admin-only: create/name/revoke read-only API bearer tokens (issue
  // #101) -- for a companion service (e.g. Birdcage) to pull event/flag
  // data with no browser session. Mirrors UsersOverlay's modal
  // pattern/markup.
  import { authState } from '../lib/auth.svelte'
  import { tokensState } from '../lib/tokens.svelte'

  let name = $state('')
  let error = $state<string | null>(null)
  let submitting = $state(false)
  let copied = $state(false)

  $effect(() => {
    if (authState.showTokens) {
      error = null
      tokensState.refresh()
    }
  })

  function close() {
    authState.showTokens = false
    name = ''
    error = null
    copied = false
    tokensState.clearJustCreated()
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') close()
  }

  function onBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) close()
  }

  async function handleCreate(e: Event) {
    e.preventDefault()
    error = null
    copied = false
    submitting = true
    const result = await tokensState.create(name)
    submitting = false
    if (result) {
      error = result
      return
    }
    name = ''
  }

  async function handleRevoke(id: string) {
    if (!confirm('Revoke this token? Anything using it will immediately lose access.')) return
    const result = await tokensState.revoke(id)
    if (result) error = result
  }

  async function copyValue(value: string) {
    try {
      await navigator.clipboard.writeText(value)
      copied = true
    } catch {
      // Clipboard access can fail (permissions, non-secure context) --
      // the value stays selectable/visible in the banner either way, so
      // there's still a manual fallback; nothing more to do here.
    }
  }

  function formatDateTime(iso?: string): string {
    if (!iso) return '—'
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return '—'
    return d.toLocaleString()
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if authState.showTokens}
  <div class="backdrop" onclick={onBackdropClick} role="presentation">
    <div class="modal" role="dialog" aria-modal="true" aria-label="API tokens" tabindex="-1">
      <div class="modal-header">
        <span class="title">API tokens</span>
        <button type="button" class="close" onclick={close} aria-label="Close">✕</button>
      </div>

      <div class="body">
        <p class="hint">
          Read-only bearer tokens for scripted/service access to <code>/api/events</code>,
          <code>/api/flags</code>, <code>/api/stats</code>, and <code>/api/devices</code> -- nothing else. The
          value is shown once, at creation.
        </p>

        {#if tokensState.justCreated}
          <div class="created-banner">
            <div class="created-label">
              Created "{tokensState.justCreated.name}" -- copy this now, it won't be shown again:
            </div>
            <div class="created-value-row">
              <code class="created-value">{tokensState.justCreated.value}</code>
              <button
                type="button"
                class="copy"
                onclick={() => tokensState.justCreated && copyValue(tokensState.justCreated.value ?? '')}
              >
                {copied ? 'Copied' : 'Copy'}
              </button>
            </div>
          </div>
        {/if}

        <form class="create-form" onsubmit={handleCreate}>
          <input type="text" placeholder="Token name (e.g. birdcage)" bind:value={name} required />
          <button type="submit" class="save" disabled={submitting}>{submitting ? 'Creating…' : 'Create'}</button>
        </form>

        {#if error}
          <p class="error">{error}</p>
        {/if}

        <div class="list">
          {#if tokensState.list.length === 0}
            <p class="empty">No tokens yet.</p>
          {/if}
          {#each tokensState.list as tok (tok.id)}
            <div class="row">
              <div class="row-main">
                <span class="row-name">{tok.name}</span>
                <span class="row-meta">
                  created {formatDateTime(tok.createdAt)} · last used {formatDateTime(tok.lastUsedAt)}
                </span>
              </div>
              <button type="button" class="revoke" onclick={() => handleRevoke(tok.id)}>Revoke</button>
            </div>
          {/each}
        </div>
      </div>

      <div class="actions">
        <button type="button" class="cancel" onclick={close}>Close</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 5vh 4vw;
    z-index: 50;
  }

  .modal {
    width: 100%;
    max-width: 480px;
    max-height: 85vh;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 10px;
    display: flex;
    flex-direction: column;
    box-shadow: 0 24px 60px -12px rgba(0, 0, 0, 0.5);
    overflow: hidden;
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-elevated);
    flex: none;
  }

  .title {
    font-size: 14px;
    font-weight: 600;
    color: var(--fg);
  }

  .close {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    width: 28px;
    height: 28px;
    font-size: 13px;
    line-height: 1;
  }

  .close:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .body {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 16px;
    overflow-y: auto;
  }

  .hint {
    margin: 0;
    font-size: 12px;
    color: var(--fg-muted);
    line-height: 1.5;
  }

  .hint code {
    font-size: 11px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 3px;
    padding: 1px 4px;
  }

  .created-banner {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 10px;
    border: 1px solid var(--accent);
    border-radius: 6px;
    background: var(--bg-elevated);
  }

  .created-label {
    font-size: 12px;
    color: var(--fg);
  }

  .created-value-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .created-value {
    flex: 1;
    font-size: 12px;
    word-break: break-all;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 5px;
    padding: 6px 8px;
    color: var(--fg);
    user-select: all;
  }

  .copy {
    flex: none;
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    font-weight: 600;
    border-radius: 5px;
    padding: 7px 12px;
    font-size: 12px;
  }

  .copy:hover {
    opacity: 0.9;
  }

  .create-form {
    display: flex;
    gap: 8px;
  }

  .create-form input {
    flex: 1;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 8px 10px;
    font-size: 14px;
  }

  .create-form input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .save {
    flex: none;
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 13px;
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    font-weight: 600;
  }

  .save:hover {
    opacity: 0.9;
  }

  .save:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .error {
    margin: 0;
    color: var(--reject);
    font-size: 13px;
  }

  .list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    border-top: 1px solid var(--border);
    padding-top: 10px;
  }

  .empty {
    margin: 0;
    font-size: 12px;
    color: var(--fg-muted);
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 8px 0;
  }

  .row-main {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .row-name {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg);
  }

  .row-meta {
    font-size: 11px;
    color: var(--fg-muted);
  }

  .revoke {
    flex: none;
    background: transparent;
    border: 1px solid var(--reject);
    color: var(--reject);
    border-radius: 5px;
    padding: 6px 10px;
    font-size: 12px;
  }

  .revoke:hover {
    background: var(--reject);
    color: #fff;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 16px;
    border-top: 1px solid var(--border);
    flex: none;
  }

  .cancel {
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 13px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .cancel:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }
</style>
