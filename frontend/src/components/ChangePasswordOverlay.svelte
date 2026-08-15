<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Change your own password (#294 item 4).
  //
  // Before this there was no way to do it from the interface at all:
  // it meant `mikroview -recover-admin-account`, which needs host
  // access. So someone who suspected their password was known could not
  // act on it, and could not end other sessions either -- which is why
  // changing it here is also "sign out everywhere", and why the dialog
  // says so rather than leaving it as a surprise.
  //
  // Structure and styling deliberately mirror SSOLinkOverlay: they are
  // the two account actions in the same menu, and looking like siblings
  // is worth more than either being individually prettier.
  import { authState } from '../lib/auth.svelte'
  import { changePassword } from '../lib/api'

  let currentPassword = $state('')
  let newPassword = $state('')
  let confirmPassword = $state('')
  let error = $state<string | null>(null)
  let done = $state(false)
  let submitting = $state(false)

  function close() {
    authState.showChangePassword = false
    currentPassword = ''
    newPassword = ''
    confirmPassword = ''
    error = null
    done = false
    submitting = false
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') close()
  }

  function onBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) close()
  }

  async function submit() {
    error = null
    // Checked here as well as server-side: the server never sees the
    // confirmation field, so this particular mistake can only be caught
    // in the browser.
    if (newPassword !== confirmPassword) {
      error = "The two new passwords don't match."
      return
    }
    submitting = true
    const err = await changePassword(currentPassword, newPassword)
    submitting = false
    if (err) {
      error = err
      return
    }
    done = true
    currentPassword = ''
    newPassword = ''
    confirmPassword = ''
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if authState.showChangePassword}
  <div class="backdrop" onclick={onBackdropClick} role="presentation">
    <div class="modal" role="dialog" aria-modal="true" aria-label="Change password" tabindex="-1">
      <div class="modal-header">
        <span class="title">Change your password</span>
        <button type="button" class="close" onclick={close} aria-label="Close">✕</button>
      </div>

      {#if done}
        <div class="body">
          <p>Your password has been changed.</p>
          <p class="muted">
            You're still signed in here. Anywhere else this account was signed in has
            been signed out — if you did this because you thought someone else had
            your password, that has now ended their access too.
          </p>
        </div>
        <div class="actions">
          <button type="button" class="cancel" onclick={close}>Close</button>
        </div>
      {:else}
        <div class="body">
          <label>
            Current password
            <input type="password" bind:value={currentPassword} autocomplete="current-password" />
          </label>
          <label>
            New password
            <input type="password" bind:value={newPassword} autocomplete="new-password" />
          </label>
          <label>
            New password again
            <input type="password" bind:value={confirmPassword} autocomplete="new-password" />
          </label>

          <p class="muted">
            Everywhere else this account is signed in will be signed out. You'll stay
            signed in here.
          </p>

          {#if error}
            <p class="error">{error}</p>
          {/if}
        </div>

        <div class="actions">
          <button type="button" class="cancel" onclick={close} disabled={submitting}>Cancel</button>
          <button
            type="button"
            class="confirm"
            onclick={submit}
            disabled={submitting || !currentPassword || !newPassword || !confirmPassword}
          >
            {submitting ? 'Changing…' : 'Change password'}
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
    max-width: 440px;
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



  .muted {
    color: var(--fg-muted);
    font-size: 12px;
  }

  .error {
    color: var(--reject);
    font-size: 12px;
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

  /* Not SSOLinkOverlay's red: deleting your password to move to SSO is
     irreversible, changing it is not. Using the same colour for both
     would spend the warning where it is not needed and blunt it where
     it is. */
  .confirm {
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 13px;
    font-weight: 600;
  }

  .confirm:disabled,
  .cancel:disabled {
    opacity: 0.6;
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
</style>
