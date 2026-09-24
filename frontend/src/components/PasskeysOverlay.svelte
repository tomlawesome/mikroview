<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Passkeys, the second factor #1250 adds alongside #1249's authenticator
  // app -- cut to AuthenticatorOverlay's pattern (docs/plans/passkeys-
  // second-factor.md's own instruction): same modal chrome, same
  // password-gated removal beat, same shared recovery codes.
  //
  // Five states: list (every registered passkey, with rename/remove),
  // adding (name it, then the browser's own prompt), codes (the ten
  // recovery codes, shown once, the same as the authenticator app's --
  // or, when a factor already minted them, a one-line note that the
  // existing ones still stand rather than a blank grid), removing
  // (password confirm, the same beat as turning the authenticator app
  // off), and unavailable -- shown instead of all of the above whenever
  // this deployment can't offer a passkey right now, or this browser is
  // not at the address they were made for. The row that opens this
  // overlay is never hidden (AccountMenu.svelte); this is where it says
  // why.
  import { authState } from '../lib/auth.svelte'
  import { trapFocus } from '../lib/focusTrap'
  import { copyToClipboard } from '../lib/clipboard'
  import { registerPasskey } from '../lib/passkeys.svelte'
  import { ApiError, fetchPasskeys, renamePasskey, disablePasskey } from '../lib/api'
  import { appState } from '../lib/state.svelte'
  import { formatDayMonth, formatRelative } from '../lib/format'
  import type { PasskeySummary } from '../lib/types'

  let { open = $bindable(false) }: { open?: boolean } = $props()

  type Step = 'list' | 'adding' | 'codes' | 'added-done' | 'removing'

  let step = $state<Step>('list')
  let passkeys = $state<PasskeySummary[]>([])
  let listError = $state<string | null>(null)
  let loadingList = $state(false)

  let newName = $state('')
  let addError = $state<string | null>(null)
  let addBusy = $state(false)
  let recoveryCodes = $state<string[]>([])
  let codesCopied = $state(false)

  let renamingId = $state<string | null>(null)
  let renameValue = $state('')
  let renameError = $state<string | null>(null)

  let removingId = $state<string | null>(null)
  let removePassword = $state('')
  let removeError = $state<string | null>(null)
  let removeBusy = $state(false)

  // Whichever server status makes passkeys unusable here -- absent
  // (null) is "carry on to the list". Computed from authState alone: the
  // four copy blocks below key off the server's own `status` plus a
  // client-side origin comparison, never a guess.
  const unavailableReason = $derived.by((): 'unset' | 'ip' | 'insecure' | 'wrong-address' | null => {
    if (authState.passkeyStatus !== 'ready') return authState.passkeyStatus
    if (location.origin !== authState.passkeyOrigin) return 'wrong-address'
    return null
  })

  async function loadList() {
    listError = null
    loadingList = true
    const result = await fetchPasskeys()
    loadingList = false
    if (typeof result === 'string') {
      listError = result
      return
    }
    passkeys = result
  }

  // Fetched fresh every time this opens, the same reasoning as
  // AboutOverlay's version fetch: this is the one place anyone looks to
  // see the current list, and a passkey added or removed from a
  // different session must not be shown stale here.
  $effect(() => {
    if (open && !unavailableReason) void loadList()
  })

  function resetFields() {
    step = 'list'
    newName = ''
    addError = null
    addBusy = false
    recoveryCodes = []
    codesCopied = false
    renamingId = null
    renameValue = ''
    renameError = null
    removingId = null
    removePassword = ''
    removeError = null
    removeBusy = false
  }

  // Reachable everywhere except the 'codes' step, matching
  // AuthenticatorOverlay: the ten codes exist in clear nowhere else, so
  // leaving is only ever the explicit acknowledgement. Also blocked
  // while a request is in flight (addBusy/removeBusy), for the same
  // reason: closing mid-request only unmounts the view, it doesn't
  // cancel it -- an add that turns out to mint fresh recovery codes
  // would land on a step nothing is left open to show, lost for good on
  // the next reload.
  const busy = $derived(addBusy || removeBusy)

  function close() {
    open = false
    resetFields()
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && step !== 'codes' && !busy) close()
  }

  function onBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget && step !== 'codes' && !busy) close()
  }

  function startAdding() {
    addError = null
    newName = ''
    step = 'adding'
  }

  async function submitAdd() {
    addError = null
    addBusy = true
    // registerPasskey forwards beginPasskeyRegistration/
    // finishPasskeyRegistration's own 401 throw unchanged -- the same
    // session-death case AuthEnrolFactor's own forced-enrolment door
    // guards against (see beginPasskeyRegistration's comment in
    // lib/api.ts). Routed the same way here: this overlay's own header
    // X/Escape/backdrop would otherwise offer a way out that doesn't
    // actually work, since the session behind it is already gone.
    try {
      const result = await registerPasskey(newName.trim())
      if (typeof result === 'string') {
        addError = result
        return
      }
      passkeys = [...passkeys, result.passkey]
      authState.passkeyCount += 1
      if (result.recoveryCodes) {
        recoveryCodes = result.recoveryCodes
        step = 'codes'
      } else {
        step = 'added-done'
      }
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        authState.handleUnauthorized()
        return
      }
      throw err
    } finally {
      addBusy = false
    }
  }

  async function copyCodes() {
    codesCopied = await copyToClipboard(recoveryCodes.join('\n'))
  }

  function finishCodes() {
    close()
  }

  function beginRename(row: PasskeySummary) {
    renamingId = row.id
    renameValue = row.name
    renameError = null
  }

  function cancelRename() {
    renamingId = null
    renameValue = ''
    renameError = null
  }

  async function submitRename() {
    if (!renamingId) return
    const id = renamingId
    const name = renameValue.trim()
    renameError = null
    const err = await renamePasskey(id, name)
    if (err) {
      renameError = err
      return
    }
    passkeys = passkeys.map((p) => (p.id === id ? { ...p, name } : p))
    renamingId = null
    renameValue = ''
  }

  function beginRemove(row: PasskeySummary) {
    removingId = row.id
    removePassword = ''
    removeError = null
    step = 'removing'
  }

  function cancelRemove() {
    removingId = null
    removePassword = ''
    removeError = null
    step = 'list'
  }

  async function confirmRemove() {
    if (!removingId) return
    removeError = null
    removeBusy = true
    try {
      const result = await disablePasskey(removingId, removePassword)
      if (typeof result === 'string') {
        removeError = result
        return
      }
      // #1253: this was the account's last second factor -- the server
      // already ended every session on it, this browser's included.
      // signOutAfterFactorRemoved() is logout()'s own local half, not a
      // repeat of a server call that would only 401 against a session
      // already gone; the ordinary return-to-list below never runs for
      // this case, since there is no session left to show that list in.
      if (result.signedOut) {
        await authState.signOutAfterFactorRemoved()
        return
      }
      passkeys = passkeys.filter((p) => p.id !== removingId)
      authState.passkeyCount = Math.max(0, authState.passkeyCount - 1)
      removingId = null
      removePassword = ''
      step = 'list'
    } finally {
      removeBusy = false
    }
  }

  const removingRow = $derived(passkeys.find((p) => p.id === removingId))
  // #1253: removing this passkey leaves the account with no second
  // factor at all iff it is the only passkey left and there is no
  // authenticator app either. Drives the 'removing' step's wording --
  // the three true outcomes (still another passkey, still the
  // authenticator app, or signed out now) rather than the previous
  // "if it was the only one" hedge that never said what actually happens.
  const otherPasskeysRemain = $derived(passkeys.length > 1)
  const isLastFactor = $derived(passkeys.length <= 1 && !authState.hasTOTP)
</script>

<svelte:window onkeydown={onKeydown} />

{#if open}
  <div class="backdrop" onclick={onBackdropClick} role="presentation">
    <div class="modal" role="dialog" aria-modal="true" aria-label="Passkeys" tabindex="-1" use:trapFocus>
      <div class="modal-header">
        <span class="title">Passkeys</span>
        {#if step !== 'codes'}
          <button type="button" class="close" onclick={close} disabled={busy} aria-label="Close">✕</button>
        {/if}
      </div>

      {#if unavailableReason}
        <div class="body">
          {#if unavailableReason === 'unset'}
            <p>
              Passkeys need MikroView to have a web address. A passkey is tied to a domain name --
              that is how WebAuthn works, so a bare IP address can never hold one. Set
              <code>publicUrl</code> in your config (or <code>MIKROVIEW_PUBLIC_URL</code>) to the
              https address you reach MikroView on and restart. Your authenticator app works
              everywhere, IP addresses included.
            </p>
          {:else if unavailableReason === 'ip'}
            <p>
              <code>publicUrl</code> is set to an IP address, and browsers refuse to create a passkey
              for an IP. Set <code>publicUrl</code> in your config (or
              <code>MIKROVIEW_PUBLIC_URL</code>) to the https address you reach MikroView on and
              restart. Your authenticator app works everywhere, IP addresses included.
            </p>
          {:else if unavailableReason === 'insecure'}
            <p>
              <code>publicUrl</code> is set to an http address. Browsers only offer passkeys over
              https. Set <code>publicUrl</code> in your config (or <code>MIKROVIEW_PUBLIC_URL</code>)
              to the https address you reach MikroView on and restart. Your authenticator app works
              everywhere, IP addresses included.
            </p>
          {:else}
            <p>
              You're viewing MikroView at {location.origin}, but passkeys live at
              <a href={authState.passkeyOrigin}>{authState.passkeyOrigin}</a>. Open MikroView there
              to add or use one. Your authenticator app works from either address.
            </p>
          {/if}
        </div>
        <div class="actions">
          <button type="button" class="cancel" onclick={close}>Close</button>
        </div>
      {:else if step === 'list'}
        <div class="body">
          {#if loadingList}
            <p class="muted">Loading…</p>
          {:else if listError}
            <p class="error">{listError}</p>
          {:else if passkeys.length === 0}
            <p>
              Add a passkey -- a fingerprint, face or device PIN -- as a second step at sign-in,
              alongside your password.
            </p>
          {:else}
            <div class="pk-list">
              {#each passkeys as row (row.id)}
                <div class="pk-row">
                  {#if renamingId === row.id}
                    <div class="pk-rename">
                      <input
                        type="text"
                        aria-label={`rename ${row.name}`}
                        bind:value={renameValue}
                        onkeydown={(e) => {
                          if (e.key === 'Enter') submitRename()
                          if (e.key === 'Escape') cancelRename()
                        }}
                      />
                      <button type="button" class="pk-icon" onclick={submitRename} aria-label="Save name">✓</button>
                      <button type="button" class="pk-icon" onclick={cancelRename} aria-label="Cancel rename">✕</button>
                    </div>
                    {#if renameError}<p class="error">{renameError}</p>{/if}
                  {:else}
                    <div class="pk-main">
                      <span class="pk-name">{row.name}</span>
                      {#if row.stale}
                        <span class="pk-stale">made for {row.rpId} — won't work here</span>
                      {/if}
                    </div>
                    <span class="pk-meta">
                      added {formatDayMonth(row.createdAt)}
                      {#if row.lastUsedAt}· last used {formatRelative(row.lastUsedAt, appState.now)}{/if}
                    </span>
                    <span class="pk-actions">
                      {#if !row.stale}
                        <button type="button" class="pk-icon" onclick={() => beginRename(row)} aria-label={`Rename ${row.name}`}>
                          ✎
                        </button>
                      {/if}
                      <button type="button" class="pk-remove" onclick={() => beginRemove(row)}>Remove</button>
                    </span>
                  {/if}
                </div>
              {/each}
            </div>
          {/if}
          {#if addError}<p class="error">{addError}</p>{/if}
        </div>
        <div class="actions">
          <button type="button" class="cancel" onclick={close}>Close</button>
          <button type="button" class="confirm" onclick={startAdding}>Add passkey</button>
        </div>
      {:else if step === 'adding'}
        <div class="body">
          {#if addBusy}
            <p>Waiting for your browser…</p>
          {:else}
            <p>Give it a name -- something that will remind you which device it's on.</p>
            <label>
              Name
              <input type="text" placeholder="this laptop" bind:value={newName} />
            </label>
          {/if}
          {#if addError}<p class="error">{addError}</p>{/if}
        </div>
        <div class="actions">
          <button type="button" class="cancel" onclick={() => (step = 'list')} disabled={addBusy}>Cancel</button>
          <button type="button" class="confirm" disabled={addBusy} onclick={submitAdd}>
            {addBusy ? 'Waiting…' : 'Continue'}
          </button>
        </div>
      {:else if step === 'codes'}
        <div class="body">
          <p>
            Ten recovery codes -- each works once, in place of a passkey or an authenticator app
            code, if you lose access to both. Save them somewhere safe now: this is the only time
            they are shown.
          </p>
          <div class="codes" data-testid="recovery-codes">
            {#each recoveryCodes as rc (rc)}
              <span class="rc">{rc}</span>
            {/each}
          </div>
          <div class="copyrow">
            <button type="button" class="copy" onclick={copyCodes}>{codesCopied ? 'Copied' : 'Copy all'}</button>
          </div>
          <p class="muted">
            Every other session this account was signed in on has been ended.
          </p>
          <p class="muted">Your ten recovery codes cover your authenticator app and your passkeys alike.</p>
        </div>
        <div class="actions">
          <button type="button" class="confirm" onclick={finishCodes}>I have saved these</button>
        </div>
      {:else if step === 'added-done'}
        <div class="body">
          <p>Passkey added.</p>
          <p class="muted">Your recovery codes were already issued, and still cover this too.</p>
        </div>
        <div class="actions">
          <button type="button" class="confirm" onclick={() => (step = 'list')}>Close</button>
        </div>
      {:else if step === 'removing'}
        <div class="body">
          <div class="warning">
            <strong>This removes "{removingRow?.name ?? 'this passkey'}".</strong>
            {#if isLastFactor}
              <p>This is the only second step this account has -- removing it signs you out
                now, and you'll set up a second step again the next time you sign in.</p>
            {:else if otherPasskeysRemain}
              <p>Your other passkeys will still be asked for at sign-in.</p>
            {:else}
              <p>Your authenticator app will still be asked for at sign-in.</p>
            {/if}
          </div>
          <label>
            Password
            <input type="password" bind:value={removePassword} autocomplete="current-password" />
          </label>
          {#if removeError}<p class="error">{removeError}</p>{/if}
        </div>
        <div class="actions">
          <button type="button" class="cancel" onclick={cancelRemove} disabled={removeBusy}>Cancel</button>
          <button type="button" class="danger" disabled={removeBusy || !removePassword} onclick={confirmRemove}>
            {removeBusy ? 'Removing…' : 'Remove passkey'}
          </button>
        </div>
      {/if}
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
    max-width: 460px;
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
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    font-size: 13px;
    line-height: 1.55;
    color: var(--fg);
  }

  .body p {
    margin: 0;
  }

  .body code {
    font-family: var(--font-mono);
    font-size: 12px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 1px 4px;
  }

  .muted {
    color: var(--fg-muted);
    font-size: 12px;
  }

  .error {
    color: var(--reject);
    font-size: 12px;
  }

  .warning {
    border: 1px solid var(--reject);
    border-radius: 6px;
    padding: 10px 12px;
    background: var(--row-reject-bg);
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .warning strong {
    color: var(--reject);
  }

  .warning p {
    margin: 0;
  }

  .modal label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 12px;
    color: var(--fg-muted);
  }

  .modal input {
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 5px;
    padding: 7px 9px;
    color: var(--fg);
    font-size: 13px;
    font-family: inherit;
  }

  .modal input:focus {
    outline: none;
    border-color: var(--accent);
  }

  /* Two columns of five, matching AuthenticatorOverlay's own recovery
     codes grid -- reads as one block of ten rather than a long
     single-file list. */
  .codes {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 6px 14px;
    padding: 10px 12px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-elevated);
  }

  .rc {
    font-family: var(--font-mono);
    font-size: 13px;
    letter-spacing: 0.03em;
    color: var(--fg);
    user-select: all;
  }

  .copyrow {
    display: flex;
    justify-content: center;
  }

  .copy {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 6px 12px;
    font-size: 12px;
  }

  .copy:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .pk-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .pk-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 10px;
    padding: 8px 10px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-elevated);
  }

  .pk-main {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1 1 auto;
    min-width: 0;
  }

  .pk-name {
    font-size: 13px;
    color: var(--fg);
    overflow-wrap: anywhere;
  }

  .pk-stale {
    font-size: 11px;
    color: var(--fg-dim);
  }

  .pk-meta {
    font-size: 11px;
    color: var(--fg-dim);
    flex: none;
  }

  .pk-actions {
    display: flex;
    align-items: center;
    gap: 4px;
    flex: none;
  }

  .pk-rename {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 1;
  }

  .pk-rename input {
    flex: 1;
  }

  .pk-icon {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    width: 24px;
    height: 24px;
    font-size: 12px;
    line-height: 1;
  }

  .pk-icon:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .pk-remove {
    background: transparent;
    border: 0;
    color: var(--reject);
    font-size: 12px;
    text-decoration: underline;
    text-underline-offset: 2px;
    padding: 2px;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 16px;
    border-top: 1px solid var(--border);
    background: var(--bg-elevated);
  }

  .cancel {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 13px;
  }

  .cancel:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .confirm {
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 13px;
    font-weight: 600;
  }

  .danger {
    background: var(--reject);
    border: 1px solid var(--reject);
    color: var(--bg);
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 13px;
    font-weight: 600;
  }

  .confirm:disabled,
  .danger:disabled,
  .cancel:disabled {
    opacity: 0.6;
  }
</style>
