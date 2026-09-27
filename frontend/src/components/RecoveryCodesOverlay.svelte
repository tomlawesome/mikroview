<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // "New recovery codes…" from the account menu (#1331): the housekeeping
  // path for a mislaid set of ten, without touching either second
  // factor. Before this route the only way to a fresh set was removing
  // a factor and adding it back -- see the issue for why that got worse
  // once an account could hold several passkeys.
  //
  // Two screens, cut to AuthenticatorOverlay's own pattern: a password
  // gate (this replaces the existing set, so it needs the same proof as
  // turning a factor off), then the ten codes, shown once behind the
  // same explicit "I have saved these" the two factor flows already
  // use rather than the header X/Escape/backdrop every other overlay
  // answers to.
  //
  // Design lead ruling, 2026-09-26: regenerating does not end other
  // sessions -- unlike confirming a factor or removing the last one,
  // nothing about which factors protect the account has changed here,
  // so there is no stale session anywhere to challenge.
  import { ApiError, regenerateRecoveryCodes } from '../lib/api'
  import { authState } from '../lib/auth.svelte'
  import { copyToClipboard } from '../lib/clipboard'
  import { trapFocus } from '../lib/focusTrap'

  let { open = $bindable(false) }: { open?: boolean } = $props()

  type Step = 'gate' | 'codes'

  let step = $state<Step>('gate')
  let password = $state('')
  let recoveryCodes = $state<string[]>([])
  let error = $state<string | null>(null)
  let busy = $state(false)
  let codesCopied = $state(false)

  function resetFields() {
    step = 'gate'
    password = ''
    recoveryCodes = []
    error = null
    busy = false
    codesCopied = false
  }

  // Reachable everywhere except the 'codes' step, matching
  // AuthenticatorOverlay/PasskeysOverlay: the fresh ten exist in clear
  // nowhere else, so leaving that screen is only ever the explicit "I
  // have saved these".
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

  async function regenerate() {
    error = null
    busy = true
    try {
      const result = await regenerateRecoveryCodes(password)
      if (typeof result === 'string') {
        error = result
        return
      }
      recoveryCodes = result
      password = ''
      step = 'codes'
    } catch (err) {
      // This route needs an existing session before it does anything
      // else, same as the four forced-enrolment routes -- a 401 here can
      // only mean that session is gone, not a wrong password (the
      // server answers that with a plain 401 body, caught above as a
      // string, never thrown).
      if (err instanceof ApiError && err.status === 401) {
        authState.handleUnauthorized()
        return
      }
      throw err
    } finally {
      busy = false
    }
  }

  async function copyCodes() {
    codesCopied = await copyToClipboard(recoveryCodes.join('\n'))
  }

  function finish() {
    close()
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if open}
  <div class="backdrop" onclick={onBackdropClick} role="presentation">
    <div class="modal" role="dialog" aria-modal="true" aria-label="New recovery codes" tabindex="-1" use:trapFocus>
      <div class="modal-header">
        <span class="title">New recovery codes</span>
        {#if step !== 'codes'}
          <button type="button" class="close" onclick={close} disabled={busy} aria-label="Close">✕</button>
        {/if}
      </div>

      {#if step === 'gate'}
        <div class="body">
          <p>
            This replaces your existing ten recovery codes with a fresh set. Do this if you've
            lost the codes you saved -- not routinely.
          </p>
          <p class="muted">
            The old set stops working the moment the new one is shown. This doesn't sign you out
            anywhere -- only changing which factors protect your account does that.
          </p>
          <label>
            Password
            <input type="password" bind:value={password} autocomplete="current-password" />
          </label>
          {#if error}<p class="error">{error}</p>{/if}
        </div>
        <div class="actions">
          <button type="button" class="cancel" onclick={close} disabled={busy}>Cancel</button>
          <button type="button" class="confirm" disabled={busy || !password} onclick={regenerate}>
            {busy ? 'Regenerating…' : 'Regenerate codes'}
          </button>
        </div>
      {/if}

      {#if step === 'codes'}
        <div class="body">
          <p>
            Ten recovery codes -- each works once, in place of a code from your authenticator app
            or a passkey prompt, if you lose access to either. Save them somewhere safe now: this
            is the only time they are shown.
          </p>
          <div class="codes" data-testid="recovery-codes">
            {#each recoveryCodes as rc (rc)}
              <span class="rc">{rc}</span>
            {/each}
          </div>
          <div class="copyrow">
            <button type="button" class="copy" onclick={copyCodes}>{codesCopied ? 'Copied' : 'Copy all'}</button>
          </div>
          <p class="muted">Your old ten no longer work. Nothing else about your account changed.</p>
        </div>
        <div class="actions">
          <button type="button" class="confirm" onclick={finish}>I have saved these</button>
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

  /* Two columns of five reads as one block of ten, matching
     AuthenticatorOverlay/PasskeysOverlay's own codes grid. */
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

  .confirm:disabled,
  .cancel:disabled {
    opacity: 0.6;
  }
</style>
