<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Admin-only: list, add and remove accounts (issue #133). #548's page,
  // successor to UsersOverlay.svelte -- the modal retired wholesale (see
  // the PR notes: it opened only from an admin-only rail row, so this
  // page carries the same reach today). Mirrors Tokens.svelte's page
  // shape, which itself mirrors this one's former modal pattern.
  //
  // Two things this deliberately cannot do. It never creates an admin --
  // mikroview has exactly one, and the server refuses a request for a
  // second. And it never removes the admin: moving that role is CLI-only
  // and recovery-key gated (`mikroview -transfer-admin`), so a
  // compromised admin session cannot hand ownership to an attacker or
  // demote the real admin out of their own deployment.
  import { onMount } from 'svelte'
  import { usersState } from '../lib/users.svelte'
  import PageHeader from './PageHeader.svelte'

  let username = $state('')
  let password = $state('')
  let error = $state<string | null>(null)
  let submitting = $state(false)

  onMount(() => {
    usersState.refresh()
  })

  async function handleCreate(e: Event) {
    e.preventDefault()
    error = null
    submitting = true
    const result = await usersState.create(username, password)
    submitting = false
    if (result) {
      error = result
      return
    }
    username = ''
    password = ''
  }

  async function handleDelete(user: { id: string; username: string }) {
    // Names the consequences rather than asking "are you sure?": the
    // sessions and tokens going too is the part that isn't obvious from
    // the word "delete".
    const ok = confirm(
      `Delete "${user.username}"?\n\n` +
        `They will be signed out immediately, and any API tokens they created will stop working.`,
    )
    if (!ok) return
    const result = await usersState.remove(user.id)
    if (result) error = result
  }

  function formatDateTime(iso?: string): string {
    if (!iso) return '—'
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return '—'
    return d.toLocaleString()
  }
</script>

<div class="page scrollbar">
  <!-- No readOnly chip: this page stays admin-only in the rail (GET
       /api/auth/users is server-gated the same way -- see
       internal/api/auth.go's callerIsAdmin check), so a viewer never
       reaches it -- see the #548 PR notes on the open question of
       whether it should become viewer-readable per the design record. -->
  <PageHeader title="Users" />

  <p class="hint">
    Accounts added here can see everything MikroView shows, but can't change settings, manage
    accounts, or issue API tokens. The admin role can't be granted from here — moving it is a
    command-line step that needs a recovery key.
  </p>

  <form class="create-form" onsubmit={handleCreate}>
    <input type="text" placeholder="Username" autocomplete="off" bind:value={username} required />
    <input type="password" placeholder="Password" autocomplete="new-password" bind:value={password} required />
    <button type="submit" class="save" disabled={submitting}>{submitting ? 'Adding…' : 'Add'}</button>
  </form>

  {#if error}
    <p class="error">{error}</p>
  {/if}

  <div class="list">
    {#each usersState.list as user (user.id)}
      <div class="row">
        <div class="row-main">
          <span class="row-name">
            {user.username}
            {#if user.role === 'admin'}<span class="badge admin">admin</span>{/if}
            {#if user.sso}<span class="badge sso">SSO</span>{/if}
          </span>
          <span class="row-meta">
            added {formatDateTime(user.createdAt)} · last signed in {formatDateTime(user.lastLogin)}
          </span>
        </div>
        {#if user.role === 'admin'}
          <span class="row-note" title="Transfer the admin role from the command line first">can't be removed</span>
        {:else}
          <button type="button" class="revoke" onclick={() => handleDelete(user)}>Delete</button>
        {/if}
      </div>
    {/each}
  </div>
</div>

<style>
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    max-width: 640px;
  }

  .hint {
    margin: 0;
    font-size: 12px;
    color: var(--fg-muted);
    line-height: 1.5;
  }

  .create-form {
    display: flex;
    gap: 8px;
  }

  .create-form input {
    flex: 1;
    min-width: 0;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 5px;
    padding: 7px 9px;
    color: var(--fg);
    font-size: 13px;
  }

  .create-form input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .save {
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 13px;
    font-weight: 600;
    flex: none;
  }

  .save:disabled {
    opacity: 0.6;
  }

  .error {
    margin: 0;
    font-size: 12px;
    color: var(--reject);
  }

  .list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 8px 10px;
    background: var(--bg-elevated);
  }

  .row-main {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .row-name {
    font-size: 13px;
    color: var(--fg);
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .badge {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    border-radius: 3px;
    padding: 1px 5px;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .badge.admin {
    border-color: var(--accent);
    color: var(--accent);
  }

  .row-meta {
    font-size: 11px;
    color: var(--fg-muted);
  }

  .row-note {
    font-size: 11px;
    color: var(--fg-muted);
    flex: none;
  }

  .revoke {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 5px 10px;
    font-size: 12px;
    flex: none;
  }

  .revoke:hover {
    color: var(--reject);
    border-color: var(--reject);
  }
</style>
