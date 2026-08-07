<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Admin-only: create an additional account. Self-registration only
  // ever creates the first (super-admin) account -- see
  // docs/configuration.md's "Authentication" section -- this is the
  // only way to add users after that.
  import { authState } from '../lib/auth.svelte'

  let username = $state('')
  let password = $state('')
  let role = $state<'admin' | 'user'>('user')
  let error = $state<string | null>(null)
  let submitting = $state(false)

  function close() {
    authState.showAddUser = false
    username = ''
    password = ''
    role = 'user'
    error = null
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') close()
  }

  function onBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) close()
  }

  async function handleSubmit(e: Event) {
    e.preventDefault()
    error = null
    submitting = true
    const result = await authState.createUser(username, password, role)
    submitting = false
    if (result) {
      error = result
      return
    }
    close()
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if authState.showAddUser}
  <div class="backdrop" onclick={onBackdropClick} role="presentation">
    <div class="modal" role="dialog" aria-modal="true" aria-label="Add user" tabindex="-1">
      <form onsubmit={handleSubmit}>
        <div class="modal-header">
          <span class="title">Add user</span>
          <button type="button" class="close" onclick={close} aria-label="Close">✕</button>
        </div>

        <div class="body">
          <label>
            <span>Username</span>
            <input type="text" autocomplete="username" bind:value={username} required />
          </label>
          <label>
            <span>Password</span>
            <input type="password" autocomplete="new-password" bind:value={password} required />
          </label>
          <label>
            <span>Role</span>
            <select bind:value={role}>
              <option value="user">User</option>
              <option value="admin">Admin</option>
            </select>
          </label>

          {#if error}
            <p class="error">{error}</p>
          {/if}
        </div>

        <div class="actions">
          <button type="button" class="cancel" onclick={close}>Cancel</button>
          <button type="submit" class="save" disabled={submitting}>{submitting ? 'Creating…' : 'Create user'}</button>
        </div>
      </form>
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
    max-width: 360px;
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
  }

  label {
    display: flex;
    flex-direction: column;
    gap: 5px;
    font-size: 12px;
    color: var(--fg-muted);
  }

  input,
  select {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 8px 10px;
    font-size: 14px;
  }

  input:focus,
  select:focus {
    outline: none;
    border-color: var(--accent);
  }

  .error {
    margin: 0;
    color: var(--reject);
    font-size: 13px;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 16px;
    border-top: 1px solid var(--border);
  }

  .cancel,
  .save {
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 13px;
  }

  .cancel {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .cancel:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .save {
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
</style>
