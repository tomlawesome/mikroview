<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Authenticator app (TOTP) second factor, from the account menu (#1249).
  //
  // Four screens, one component: status (on/off, and the way into either
  // direction), enrol (QR + the same secret as text beside it, always --
  // the issue is explicit that the text is not a fallback for a broken
  // scanner, it is shown unconditionally -- plus the code box that
  // confirms it), the ten recovery codes (shown once, closed only by the
  // explicit "I have saved these" rather than the header X/Escape/
  // backdrop every other overlay here answers to), and turning it off
  // (password-gated, like changePassword -- there is deliberately no way
  // to do this without one; a lost phone goes through the CLI recovery
  // path instead, not this dialog).
  //
  // Structure and styling deliberately mirror ChangePasswordOverlay,
  // SSOLinkOverlay and ResetCodeOverlay: the account actions look like
  // siblings.
  import { authState } from '../lib/auth.svelte'
  import { ApiError, confirmTOTP, disableTOTP, enrolTOTP } from '../lib/api'
  import { copyToClipboard } from '../lib/clipboard'
  import { trapFocus } from '../lib/focusTrap'
  import { qrCode } from '../lib/qrcode'

  // Bound to the caller's local state (#1332): AccountMenu mounts one
  // copy of this per card on the deck, and reading a shared auth flag
  // here opened every copy at once. AboutOverlay, one line above this
  // in AccountMenu, already took its open state as a bound prop -- this
  // now matches it rather than being the one overlay that didn't.
  let { open = $bindable(false) }: { open?: boolean } = $props()

  // 'on-done' is #1250's addition: reached instead of 'codes' when this
  // account's recovery codes were already minted by an earlier passkey
  // -- confirming still turns the factor on, it just has no fresh codes
  // to show (see confirm() below).
  type Step = 'status' | 'enrolling' | 'codes' | 'on-done' | 'turning-off' | 'off-done'

  let step = $state<Step>('status')
  let uri = $state('')
  let secret = $state('')
  let code = $state('')
  let recoveryCodes = $state<string[]>([])
  let password = $state('')
  let error = $state<string | null>(null)
  let busy = $state(false)
  let codesCopied = $state(false)

  // The secret lives in the URI's own query string -- extracted here
  // rather than asked for separately, so there is exactly one value the
  // server hands over and one place a mistake in reading it could hide.
  function secretFromUri(u: string): string {
    try {
      return new URL(u).searchParams.get('secret') ?? ''
    } catch {
      return ''
    }
  }

  function resetFields() {
    step = 'status'
    uri = ''
    secret = ''
    code = ''
    recoveryCodes = []
    password = ''
    error = null
    busy = false
    codesCopied = false
  }

  // Reachable from the header X, Escape and the backdrop -- everywhere
  // except the 'codes' step, which those three are not wired to at all
  // (see the markup below): the ten codes exist in clear nowhere else,
  // so leaving is only ever the explicit "I have saved these". Also
  // blocked while busy: closing mid-confirm doesn't cancel the request,
  // it only unmounts the view -- so a confirm that turns out to have
  // minted fresh recovery codes would land on a step nothing is left
  // open to show, and a reload after that loses them for good.
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

  async function startEnrol() {
    error = null
    busy = true
    // enrolTOTP throws rather than returning text on a 401 -- the same
    // session-death case AuthEnrolFactor's own forced-enrolment door
    // guards against (see enrolTOTP's comment in lib/api.ts). Routed the
    // same way here: this overlay's own header X/Escape/backdrop would
    // otherwise offer a way out that doesn't actually work, since the
    // session behind it is already gone.
    try {
      const result = await enrolTOTP()
      if (typeof result === 'string') {
        error = result
        return
      }
      uri = result.uri
      secret = secretFromUri(result.uri)
      code = ''
      step = 'enrolling'
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        authState.handleUnauthorized()
        return
      }
      throw err
    } finally {
      busy = false
    }
  }

  async function confirm() {
    error = null
    busy = true
    // Same reasoning as startEnrol above.
    try {
      const result = await confirmTOTP(code)
      if (typeof result === 'string') {
        error = result
        return
      }
      authState.hasTOTP = true
      // #1250: recovery codes are shared with passkeys and minted once, by
      // whichever factor activates first. A passkey already on this
      // account means confirmTOTP just turned the factor on with nothing
      // new to show -- the codes step exists nowhere to skip to, only the
      // one-line note that the ones already issued still cover this too.
      if (result.alreadyIssued) {
        step = 'on-done'
        return
      }
      recoveryCodes = result.recoveryCodes ?? []
      step = 'codes'
    } catch (err) {
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
    // The codes have been shown once and acknowledged -- nothing left to
    // do here but the same field reset every other close path takes.
    close()
  }

  async function turnOff() {
    error = null
    busy = true
    const err = await disableTOTP(password)
    busy = false
    if (err) {
      error = err
      return
    }
    authState.hasTOTP = false
    password = ''
    step = 'off-done'
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if open}
  <div class="backdrop" onclick={onBackdropClick} role="presentation">
    <div class="modal" role="dialog" aria-modal="true" aria-label="Authenticator app" tabindex="-1" use:trapFocus>
      <div class="modal-header">
        <span class="title">Authenticator app</span>
        {#if step !== 'codes'}
          <button type="button" class="close" onclick={close} disabled={busy} aria-label="Close">✕</button>
        {/if}
      </div>

      {#if step === 'status'}
        <div class="body">
          {#if authState.hasTOTP}
            <p>An authenticator app is protecting sign-in on this account.</p>
            <p class="muted">
              Turning it off needs your password -- the same guard as changing it.
            </p>
          {:else}
            <p>
              Add a code from an authenticator app (Google Authenticator, 1Password, Bitwarden,
              or similar) as a second step at sign-in, alongside your password.
            </p>
          {/if}
          {#if error}<p class="error">{error}</p>{/if}
        </div>
        <div class="actions">
          <button type="button" class="cancel" onclick={close} disabled={busy}>Close</button>
          {#if authState.hasTOTP}
            <button type="button" class="danger" onclick={() => ((step = 'turning-off'), (error = null))}>
              Turn off
            </button>
          {:else}
            <button type="button" class="confirm" disabled={busy} onclick={startEnrol}>
              {busy ? 'Starting…' : 'Set up authenticator app'}
            </button>
          {/if}
        </div>
      {/if}

      {#if step === 'enrolling'}
        <div class="body">
          <p>Scan this with your authenticator app, or type the secret in by hand.</p>
          <div class="enrol-row">
            <canvas use:qrCode={uri} width="176" height="176" aria-label="QR code for the authenticator secret"
            ></canvas>
            <!-- Shown as text unconditionally, not only when the QR fails --
                 #1249 is explicit that this sits beside the code always. -->
            <div class="secretcol">
              <span class="seclabel">Secret</span>
              <p class="secret" data-testid="totp-secret">{secret}</p>
            </div>
          </div>

          <label>
            Code from the app
            <input
              type="text"
              inputmode="numeric"
              autocomplete="one-time-code"
              placeholder="123456"
              bind:value={code}
            />
          </label>

          <p class="muted">
            Confirming turns this on and signs out everywhere else this account is currently
            signed in. You'll stay signed in here.
          </p>

          {#if error}<p class="error">{error}</p>{/if}
        </div>
        <div class="actions">
          <button type="button" class="cancel" onclick={close} disabled={busy}>Cancel</button>
          <button type="button" class="confirm" disabled={busy || !code} onclick={confirm}>
            {busy ? 'Confirming…' : 'Confirm'}
          </button>
        </div>
      {/if}

      {#if step === 'codes'}
        <div class="body">
          <p>
            Ten recovery codes -- each works once, in place of a code from the app, if you lose
            it. Save them somewhere safe now: this is the only time they are shown.
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
          <!-- #1250: the two overlays' one cross-reference line -- codes
               are shared, so whichever second factor is added later
               reuses these rather than minting its own. -->
          <p class="muted">Your ten recovery codes cover your authenticator app and your passkeys alike.</p>
        </div>
        <div class="actions">
          <button type="button" class="confirm" onclick={finish}>I have saved these</button>
        </div>
      {/if}

      {#if step === 'on-done'}
        <div class="body">
          <p>Authenticator app turned on.</p>
          <p class="muted">Your recovery codes were already issued, and still cover this too.</p>
        </div>
        <div class="actions">
          <button type="button" class="confirm" onclick={close}>Close</button>
        </div>
      {/if}

      {#if step === 'turning-off'}
        <div class="body">
          <div class="warning">
            <strong>This removes the second step at sign-in.</strong>
            <p>Your password alone will sign you in again after this.</p>
          </div>
          <label>
            Password
            <input type="password" bind:value={password} autocomplete="current-password" />
          </label>
          {#if error}<p class="error">{error}</p>{/if}
        </div>
        <div class="actions">
          <button type="button" class="cancel" onclick={() => ((step = 'status'), (password = ''), (error = null))} disabled={busy}>
            Cancel
          </button>
          <button type="button" class="danger" disabled={busy || !password} onclick={turnOff}>
            {busy ? 'Turning off…' : 'Turn off authenticator app'}
          </button>
        </div>
      {/if}

      {#if step === 'off-done'}
        <div class="body">
          <p>Authenticator app turned off. Signing in now needs only your password.</p>
        </div>
        <div class="actions">
          <button type="button" class="confirm" onclick={close}>Close</button>
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

  .enrol-row {
    display: flex;
    align-items: center;
    gap: 14px;
  }

  .enrol-row canvas {
    flex: none;
    width: 88px;
    height: 88px;
    border-radius: 6px;
    background: #fff;
    /* A light tile regardless of theme: the QR standard needs real
       contrast against the modules qrcode.js draws in black, which a
       dark theme's own background would not give it. */
  }

  .secretcol {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }

  .seclabel {
    font-size: 11px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .secret {
    margin: 0;
    font-family: var(--font-mono);
    font-size: 13px;
    letter-spacing: 0.04em;
    word-break: break-all;
    color: var(--fg);
    user-select: all;
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

  /* Two columns of five reads as one block of ten rather than a long
     single-file list the eye has to track down and back up while
     copying by hand. */
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
