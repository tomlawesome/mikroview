<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The engine room's two side doors (#490): who may look in (accounts)
  // and which machines may speak (API/ingest tokens). Doors govern
  // entry, not flow -- they sit beside the signal path rather than on
  // it (see docs/design/screens/settings/DESIGN.md). Successor to the
  // former Users.svelte/Tokens.svelte pages: same state modules
  // (usersState/tokensState) and the same create/remove/revoke calls,
  // reshaped into two compact door cards instead of two whole pages.
  //
  // The two doors are NOT symmetrically gated. GET /api/auth/users
  // stayed admin-only -- a deliberate, later departure from the design
  // record's original "widen users too" clause (the owner overrode it
  // mid-build; the record is being amended separately) -- so "Who may
  // look in" is admin-only: absent entirely for a viewer, never fetched
  // for one either, since a 403'd request is exactly what the rail's
  // `admin: true` convention exists to prevent. GET /api/tokens *did*
  // widen, so "Which machines may speak" stays viewer-readable, with
  // only its verbs (Mint, Revoke) gated on isAdmin.
  import { onMount } from 'svelte'
  import { usersState } from '../lib/users.svelte'
  import { tokensState } from '../lib/tokens.svelte'
  import { fetchDevices } from '../lib/api'
  import { formatHM } from '../lib/format'
  import type { Device } from '../lib/types'

  let { isAdmin }: { isAdmin: boolean } = $props()

  onMount(() => {
    // Never fetched for a viewer -- see the module doc comment above.
    if (isAdmin) usersState.refresh()
    tokensState.refresh()
    fetchDevices()
      .then((all) => {
        // Same de-dup/order rule Tokens.svelte used to apply -- an id-less
        // or duplicated device would crash the keyed {#each} below.
        const byID = new Map<string, Device>()
        for (const d of all) {
          if (d.id && !byID.has(d.id)) byID.set(d.id, d)
        }
        knownDevices = [...byID.values()].sort(
          (a, b) => Number(b.configured) - Number(a.configured) || a.id.localeCompare(b.id),
        )
      })
      .catch(() => {
        knownDevices = []
      })
  })

  // ---- Who may look in ----
  let showAddUser = $state(false)
  let newUsername = $state('')
  let newPassword = $state('')
  let userError = $state<string | null>(null)
  let addingUser = $state(false)
  // #653: which tier the new account gets. Defaults to the one every
  // account created here used to get, so the quick path is unchanged.
  let newUserRole = $state<'user' | 'viewer'>('user')

  async function submitAddUser(e: Event) {
    e.preventDefault()
    userError = null
    addingUser = true
    const err = await usersState.create(newUsername, newPassword, newUserRole)
    addingUser = false
    if (err) {
      userError = err
      return
    }
    newUsername = ''
    newPassword = ''
    newUserRole = 'user'
    showAddUser = false
  }

  async function removeUser(user: { id: string; username: string }) {
    // Names the consequence rather than asking "are you sure?" -- the
    // sessions and tokens going too is the part that isn't obvious from
    // the word "remove".
    const ok = confirm(
      `Remove "${user.username}"?\n\n` +
        `They will be signed out immediately, and any API tokens they created will stop working.`,
    )
    if (!ok) return
    const err = await usersState.remove(user.id)
    if (err) userError = err
  }

  // ---- Which machines may speak ----
  let showMintToken = $state(false)
  let newTokenName = $state('')
  let newTokenKind = $state<'api' | 'ingest'>('api')
  let newTokenDevice = $state('')
  let knownDevices = $state<Device[]>([])
  let tokenError = $state<string | null>(null)
  let minting = $state(false)
  let copied = $state(false)

  async function submitMint(e: Event) {
    e.preventDefault()
    tokenError = null
    copied = false
    if (newTokenKind === 'ingest' && !newTokenDevice) {
      tokenError = 'An ingest key needs a device -- pick the router it speaks for.'
      return
    }
    minting = true
    const err = await tokensState.create(newTokenName, newTokenKind, newTokenKind === 'ingest' ? newTokenDevice : undefined)
    minting = false
    if (err) {
      tokenError = err
      return
    }
    newTokenName = ''
    showMintToken = false
  }

  async function revokeToken(id: string) {
    if (!confirm('Revoke this key? Anything using it will immediately lose access.')) return
    const err = await tokensState.revoke(id)
    if (err) tokenError = err
  }

  async function copySecret(value: string) {
    try {
      await navigator.clipboard.writeText(value)
      copied = true
    } catch {
      // The value stays selectable in the banner regardless.
    }
  }
</script>

<div class="doors">
  <span class="doorlbl">The side doors — who and what may come in</span>

  {#if isAdmin}
    <!-- Admin-only door, absent entirely for a viewer -- not collapsed,
         not shown empty, not explained. GET /api/auth/users stayed
         admin-only (see the module doc comment above), so there is
         nothing here a viewer's session could even fetch. -->
    <section class="door">
      <div class="dhead">
        <span class="dname">Who may look in</span>
        <span class="dwhat">users</span>
      </div>

      <ul class="rows">
        {#each usersState.list as user (user.id)}
          <li class="row">
            <span class="who">{user.username}</span>
            {#if user.role === 'admin'}<span class="chip admin">admin</span>{/if}
            <!-- #653: a viewer and a user look identical otherwise, and
                 which one someone is is the fact an admin came here to
                 check. Only the read-only tier is marked -- "can change
                 things" is the ordinary case and needs no label. -->
            {#if user.role === 'viewer'}<span class="chip">view only</span>{/if}
            {#if user.sso}<span class="chip sso">sso</span>{/if}
            <span class="fact">
              <span class="tick"></span>
              {user.lastLogin ? `signed in ${formatHM(user.lastLogin)}` : 'never signed in'}
            </span>
            {#if user.role === 'admin'}
              <span class="note" title="Transfer the admin role from the command line first">console-only</span>
            {:else}
              <button type="button" class="verb" onclick={() => removeUser(user)}>Remove</button>
            {/if}
          </li>
        {/each}
      </ul>

      {#if userError}<p class="error">{userError}</p>{/if}

      {#if showAddUser}
        <form class="inline-form" onsubmit={submitAddUser}>
          <input type="text" placeholder="Username" autocomplete="off" bind:value={newUsername} required />
          <input type="password" placeholder="Password" autocomplete="new-password" bind:value={newPassword} required />
          <!-- #653: without this the viewer tier has no way in from the
               UI at all. Worded as what they may do rather than as a
               role name, the same way the key kind above is. -->
          <select bind:value={newUserRole} aria-label="What they may do">
            <option value="user">Can change things</option>
            <option value="viewer">Can only look</option>
          </select>
          <div class="form-actions">
            <button type="button" class="cancel" onclick={() => (showAddUser = false)}>Cancel</button>
            <button type="submit" class="verb save" disabled={addingUser}>{addingUser ? 'saving…' : 'Let them in'}</button>
          </div>
        </form>
      {:else}
        <button type="button" class="verb footer-action" onclick={() => (showAddUser = true)}>+ Let someone in</button>
      {/if}
    </section>
  {/if}

  <section class="door">
    <div class="dhead">
      <span class="dname">Which machines may speak</span>
      <span class="dwhat">tokens</span>
    </div>

    {#if tokensState.justCreated}
      <div class="secretbanner">
        <div class="sb-text">
          <strong>Copy it now — shown once.</strong>
          <code class="sk">{tokensState.justCreated.value}</code>
        </div>
        <button type="button" class="fbtn" onclick={() => tokensState.justCreated && copySecret(tokensState.justCreated.value ?? '')}>
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
    {/if}

    <ul class="rows">
      {#each tokensState.list as tok (tok.id)}
        <li class="row">
          <span class="who">{tok.name}</span>
          <!-- An ingest key names the device it speaks for, not just its
               kind: with two routers pushing, "ingest" alone does not say
               which key belongs to which, and that is the fact an
               operator revokes on. -->
          <span class="chip" class:ingest={tok.kind === 'ingest'}>
            {tok.kind === 'ingest' ? `ingest: ${tok.device ?? 'unscoped'}` : 'read'}
          </span>
          <span class="fact">
            <span class="tick"></span>
            {tok.lastUsedAt ? `spoke ${formatHM(tok.lastUsedAt)}` : 'never spoke'}
          </span>
          {#if isAdmin}
            <button type="button" class="verb" onclick={() => revokeToken(tok.id)}>Revoke</button>
          {/if}
        </li>
      {/each}
    </ul>

    {#if tokenError}<p class="error">{tokenError}</p>{/if}

    {#if isAdmin}
      {#if showMintToken}
        <form class="inline-form" onsubmit={submitMint}>
          <input type="text" placeholder="Key name (e.g. birdcage)" bind:value={newTokenName} required />
          <select bind:value={newTokenKind} aria-label="Key kind">
            <option value="api">Read-only</option>
            <option value="ingest">Ingest</option>
          </select>
          {#if newTokenKind === 'ingest'}
            {#if knownDevices.length > 0}
              <select bind:value={newTokenDevice} required aria-label="Device this key speaks for">
                <option value="" disabled>Device this key speaks for…</option>
                {#each knownDevices as d (d.id)}
                  <option value={d.id}>{d.name && d.name !== d.id ? `${d.name} (${d.id})` : d.id}</option>
                {/each}
              </select>
            {:else}
              <p class="hint">No devices known yet -- one appears here once it sends syslog.</p>
            {/if}
          {/if}
          <div class="form-actions">
            <button type="button" class="cancel" onclick={() => (showMintToken = false)}>Cancel</button>
            <button type="submit" class="verb save" disabled={minting}>{minting ? 'saving…' : 'Mint'}</button>
          </div>
        </form>
      {:else}
        <button type="button" class="verb footer-action" onclick={() => (showMintToken = true)}>+ Mint a key</button>
      {/if}
    {/if}
  </section>
</div>

<style>
  .doors {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 16px;
    min-width: 0;
  }

  .doorlbl {
    font-size: 10.5px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--fg-dim);
    font-weight: 650;
  }

  .door {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 12px 14px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .dhead {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }

  .dname {
    font-size: 13px;
    font-weight: 650;
    color: var(--fg);
  }

  .dwhat {
    font-size: 10px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .rows {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 0;
    border-bottom: 1px solid var(--border);
    font-size: 12.5px;
  }

  .row:last-child {
    border-bottom: none;
  }

  .who {
    color: var(--fg);
    font-weight: 600;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chip {
    flex: none;
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--fg-muted);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1px 6px;
  }

  .chip.admin,
  .chip.ingest {
    border-color: var(--accent);
    color: var(--accent);
  }

  .fact {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 5px;
    color: var(--fg-dim);
    font-size: 11px;
    font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
    white-space: nowrap;
    overflow: hidden;
  }

  .tick {
    flex: none;
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--now);
  }

  .verb {
    flex: none;
    background: transparent;
    border: none;
    color: var(--accent);
    font-size: 11.5px;
    padding: 0;
  }

  .verb:hover {
    text-decoration: underline;
  }

  .note {
    flex: none;
    font-size: 11px;
    color: var(--fg-dim);
  }

  .footer-action {
    text-align: left;
  }

  .error {
    margin: 0;
    font-size: 11.5px;
    color: var(--reject);
  }

  .inline-form {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding-top: 6px;
    border-top: 1px solid var(--border);
  }

  .inline-form input,
  .inline-form select {
    background: var(--bg);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 6px 8px;
    font-size: 12.5px;
  }

  .hint {
    margin: 0;
    font-size: 11px;
    color: var(--fg-muted);
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  .cancel,
  .save {
    border-radius: 5px;
    padding: 5px 12px;
    font-size: 12px;
  }

  .cancel {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .save {
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    font-weight: 600;
  }

  .save:disabled {
    opacity: 0.6;
    cursor: default;
  }

  .secretbanner {
    display: flex;
    align-items: center;
    gap: 10px;
    border: 1px solid var(--border);
    border-left: 2px solid var(--now);
    border-radius: 8px;
    background: color-mix(in srgb, var(--now) 10%, transparent);
    padding: 8px 10px;
    font-size: 11.5px;
    color: var(--fg-muted);
  }

  .sb-text {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
  }

  .sb-text strong {
    color: var(--fg);
  }

  .sk {
    font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
    color: var(--fg);
    word-break: break-all;
  }

  .fbtn {
    flex: none;
    margin-left: auto;
    font-size: 11px;
    color: var(--accent);
    border: 1px solid var(--border);
    border-radius: 7px;
    padding: 4px 10px;
    background: transparent;
  }

  .fbtn:hover {
    border-color: var(--accent);
  }
</style>
