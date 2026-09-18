<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // The one-time code an admin has just minted for somebody else
  // (#1251), shown once and never again.
  //
  // mikroview sends no mail, so this dialog is the whole delivery
  // mechanism: what is on screen here is what the admin reads out, and
  // closing it is the end of the code's existence in clear. Nothing
  // stores it -- not this component, not usersState, not the server,
  // which kept only its hash. A second reset mints another and kills
  // this one, which is the recovery path for closing the dialog too
  // early.
  //
  // Structure and styling deliberately mirror ChangePasswordOverlay and
  // SSOLinkOverlay: the account actions look like siblings.
  import { copyToClipboard } from '../lib/clipboard'
  import { trapFocus } from '../lib/focusTrap'
  import type { PasswordResetCode } from '../lib/types'

  let { reset, onclose }: { reset: PasswordResetCode; onclose: () => void } = $props()

  let copied = $state(false)

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') onclose()
  }

  function onBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) onclose()
  }

  async function copy() {
    copied = await copyToClipboard(reset.code)
  }
</script>

<svelte:window onkeydown={onKeydown} />

<div class="backdrop" onclick={onBackdropClick} role="presentation">
  <div
    class="modal"
    role="dialog"
    aria-modal="true"
    aria-label="One-time password reset code"
    tabindex="-1"
    use:trapFocus
  >
    <div class="modal-header">
      <span class="title">One-time code for {reset.username}</span>
      <button type="button" class="close" onclick={onclose} aria-label="Close">✕</button>
    </div>

    <div class="body">
      <p class="code" data-testid="reset-code">{reset.code}</p>
      <div class="copyrow">
        <button type="button" class="copy" onclick={copy}>{copied ? 'Copied' : 'Copy code'}</button>
      </div>

      <p>
        Give this to them in person or over a call you trust; it stops working in 24
        hours or on first use.
      </p>
      <p class="muted">
        This is the only time it is shown. {reset.username}'s old password has already
        stopped working and they have been signed out everywhere. If you lose the code,
        reset again — that issues a new one and kills this one.
      </p>
    </div>

    <div class="actions">
      <button type="button" class="confirm" onclick={onclose}>Done</button>
    </div>
  </div>
</div>

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

  /* Monospaced and spaced out: this is dictated aloud as often as it is
     copied, and the grouping is what makes that possible. */
  .code {
    font-family: var(--mono, ui-monospace, SFMono-Regular, Menlo, monospace);
    font-size: 20px;
    letter-spacing: 0.12em;
    text-align: center;
    user-select: all;
    padding: 12px 8px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-elevated);
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

  .muted {
    color: var(--fg-muted);
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

  .confirm {
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 13px;
    font-weight: 600;
  }
</style>
