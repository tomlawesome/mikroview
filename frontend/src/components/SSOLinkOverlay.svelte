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
  //
  // #1252, owner's ruling: none of that applies to the admin, whose
  // password survives linking (auth.Store.LinkOIDCIdentity). mikroview
  // holds exactly one admin and never authenticates to the provider on
  // its own behalf, so that account is the only thing standing between
  // a provider outage and nobody getting in at all -- SSO is added to
  // it rather than swapped for it. So the admin reads a different
  // dialog: what is gained, not what is destroyed. Warning somebody
  // about a deletion that will not happen is how real warnings stop
  // being read.
  import { authState } from '../lib/auth.svelte'
  import { startSSOLink } from '../lib/api'

  let error = $state<string | null>(null)
  let submitting = $state(false)

  // The admin is the deployment's way in when the provider is down, and
  // the only account that keeps its password through a link -- and, since
  // #1253, its second factor with it. Every other role loses both, in the
  // same store write (auth.Store.LinkOIDCIdentity).
  //
  // Derived from the role this session already carries rather than asked
  // of the server, because this warning is shown *before* the link
  // starts: handleOIDCLinkStart's response arrives only once the person
  // has already confirmed, which is too late to tell them what they are
  // agreeing to.
  const keepsPassword = $derived(authState.isAdmin)

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
    // startSSOLink answers a refusal as a string, and since api.ts's
    // send() a dropped connection too; a body that is not JSON can
    // still throw outright. Left to propagate, it escapes confirm() with
    // submitting still true: the button stays on "Redirecting…", no
    // error is shown, and there is no way to try again without
    // reloading. AuthSetup.svelte guards its own call to this function
    // for the same reason (#1218 audit finding 7); this caller was
    // missed.
    let result: { url: string } | string
    try {
      result = await startSSOLink()
    } catch (err) {
      result = err instanceof Error ? err.message : String(err)
    }
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
          this account and that identity are connected for good.
        </p>

        {#if keepsPassword}
          <div class="kept">
            <strong>Your MikroView password and second step stay.</strong>
            <p>
              You're the MikroView admin, so SSO becomes an extra way in rather than
              a replacement: your password and your authenticator app or passkey are
              what still let you in on the day your identity provider can't be
              reached. Everyone else loses both when they connect.
            </p>
          </div>
        {:else}
          <div class="warning">
            <strong>Your MikroView password and second step will be deleted.</strong>
            <p>
              Both go: the password, and the authenticator app or passkeys you use
              for the second step. This can't be undone from MikroView. After
              connecting, signing in goes through your identity provider only — and
              if you ever lose access to it, MikroView can't recover this account
              for you.
            </p>
          </div>
        {/if}

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
        <!-- The label names the consequence rather than saying "OK", so
             it cannot be clicked through unread -- and the consequence
             is not the same for the admin, so neither is the label. -->
        <button type="button" class:danger={!keepsPassword} class:go={keepsPassword} onclick={confirm} disabled={submitting}>
          {#if submitting}
            Redirecting…
          {:else if keepsPassword}
            Connect SSO and keep my password
          {:else}
            Delete my password and second step, and connect SSO
          {/if}
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

  .kept {
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 10px 12px;
    background: var(--bg-elevated);
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .kept strong {
    color: var(--fg);
  }

  .kept p {
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

  .danger {
    background: var(--reject);
    border: 1px solid var(--reject);
    color: var(--bg);
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 13px;
    font-weight: 600;
  }

  .go {
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 13px;
    font-weight: 600;
  }

  .danger:disabled,
  .go:disabled,
  .cancel:disabled {
    opacity: 0.6;
  }
</style>
