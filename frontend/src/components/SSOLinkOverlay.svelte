<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Connect the signed-in account to SSO (issue #133 Part 4).
  //
  // This exists because the operation is irreversible from inside
  // mikroview and destroys a credential. The warning comes before the
  // confirm, not after it, and the confirm button says what happens
  // rather than "OK" -- the same reasoning SECURITY.md applies to the
  // "skip auth" choice, which is likewise a permanent decision rather
  // than a default someone falls into.
  import { authState } from '../lib/auth.svelte'
  import { startSSOLink } from '../lib/api'

  let error = $state<string | null>(null)
  let submitting = $state(false)

  function close() {
    authState.showSSOLink = false
    error = null
    submitting = false
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') close()
  }

  function onBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) close()
  }

  async function confirm() {
    error = null
    submitting = true
    const result = await startSSOLink()
    if (typeof result === 'string') {
      error = result
      submitting = false
      return
    }
    // Full navigation, not fetch: the provider needs to see the browser
    // so it can show its own sign-in page and set its own cookies.
    // submitting stays true -- the page is on its way out.
    location.href = result.url
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if authState.showSSOLink}
  <div class="backdrop" onclick={onBackdropClick} role="presentation">
    <div class="modal" role="dialog" aria-modal="true" aria-label="Connect SSO" tabindex="-1">
      <div class="modal-header">
        <span class="title">Connect SSO to your account</span>
        <button type="button" class="close" onclick={close} aria-label="Close">✕</button>
      </div>

      <div class="body">
        <p>
          You'll be sent to your identity provider to sign in. When you come back,
          this account will use SSO from then on.
        </p>

        <div class="warning">
          <strong>Your MikroView password will be deleted.</strong>
          <p>
            This can't be undone from MikroView. After connecting, signing in goes
            through your identity provider only — and if you ever lose access to it,
            MikroView can't recover this account for you.
          </p>
        </div>

        <p class="muted">
          You'll stay signed in here. Anywhere else you're signed in will be
          signed out.
        </p>

        {#if error}
          <p class="error">{error}</p>
        {/if}
      </div>

      <div class="actions">
        <button type="button" class="cancel" onclick={close} disabled={submitting}>Cancel</button>
        <button type="button" class="danger" onclick={confirm} disabled={submitting}>
          {submitting ? 'Redirecting…' : 'Delete my password and connect SSO'}
        </button>
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

  .danger {
    background: var(--reject);
    border: 1px solid var(--reject);
    color: var(--bg);
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 13px;
    font-weight: 600;
  }

  .danger:disabled,
  .cancel:disabled {
    opacity: 0.6;
  }
</style>
