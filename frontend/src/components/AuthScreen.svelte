<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Shared shell for the two pre-app states (setup/login) -- both are a
  // centered card with a username/password form, differing only in
  // title, submit label, and what happens on submit. Not a modal: this
  // replaces App.svelte's entire main content, same as Metrics being an
  // independent view rather than an overlay.
  import LogoLockup from './LogoLockup.svelte'
  import { authState } from '../lib/auth.svelte'

  let {
    title,
    subtitle,
    submitLabel,
    onsubmit,
    confirmPassword = false,
    ssoAvailable = false,
  }: {
    title: string
    subtitle: string
    submitLabel: string
    onsubmit: (username: string, password: string) => Promise<string | null>
    confirmPassword?: boolean
    // ssoAvailable: whether the backend has OIDC/SSO configured (see
    // authState.ssoAvailable) -- renders a secondary "Sign in with SSO"
    // link below the form. A plain <a>, not a button/fetch call: this
    // has to be a real top-level browser navigation for the OAuth
    // redirect to work at all, so clicking it leaves the SPA entirely.
    ssoAvailable?: boolean
  } = $props()

  let username = $state('')
  let password = $state('')
  let passwordConfirm = $state('')
  let error = $state<string | null>(null)
  let submitting = $state(false)
  // Requires clicking through to a separate warning screen rather than
  // reacting to one click on the form -- this is a permanent, only-
  // CLI-reversible decision (see the plan's "informed choice, not
  // accidental click" requirement).

  async function handleSubmit(e: Event) {
    e.preventDefault()
    error = null

    if (confirmPassword && password !== passwordConfirm) {
      error = 'Passwords do not match.'
      return
    }

    submitting = true
    const result = await onsubmit(username, password)
    submitting = false
    if (result) error = result
  }

</script>

<div class="screen">
  <div class="card">
    <LogoLockup size={26} />

    {#if authState.ssoError}
      <p class="error">{authState.ssoError}</p>
    {/if}

    <form class="form-body" onsubmit={handleSubmit}>
      <h1>{title}</h1>
      <p class="subtitle">{subtitle}</p>

      <label>
        <span>Username</span>
        <input type="text" autocomplete="username" bind:value={username} required />
      </label>

      <label>
        <span>Password</span>
        <input type="password" autocomplete={confirmPassword ? 'new-password' : 'current-password'} bind:value={password} required />
      </label>

      {#if confirmPassword}
        <label>
          <span>Confirm password</span>
          <input type="password" autocomplete="new-password" bind:value={passwordConfirm} required />
        </label>
      {/if}

      {#if error}
        <p class="error">{error}</p>
      {/if}

      <button type="submit" disabled={submitting}>{submitting ? 'Please wait…' : submitLabel}</button>

      {#if ssoAvailable}
        <div class="divider"><span>or</span></div>
        <a class="sso-link" href="/api/auth/oidc/login">Sign in with SSO</a>
      {/if}
    </form>

  </div>
</div>

<style>
  .screen {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 20px;
  }

  .card {
    width: 100%;
    max-width: 340px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 32px 28px;
  }

  .form-body {
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
  }

  h1 {
    margin: 4px 0 0;
    font-size: 16px;
    color: var(--fg);
  }

  .subtitle {
    margin: 0 0 8px;
    font-size: 13px;
    color: var(--fg-muted);
    text-align: center;
  }



  label {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 5px;
    font-size: 12px;
    color: var(--fg-muted);
  }

  input {
    background: var(--bg);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 9px 10px;
    font-size: 14px;
  }

  input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .error {
    width: 100%;
    margin: 0;
    color: var(--reject);
    font-size: 13px;
  }

  button {
    width: 100%;
    margin-top: 6px;
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    font-weight: 600;
    border-radius: 5px;
    padding: 10px;
    font-size: 14px;
  }

  button:hover {
    opacity: 0.9;
  }

  button:disabled {
    opacity: 0.5;
    cursor: default;
  }


  .divider {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 4px;
    color: var(--fg-dim);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .divider::before,
  .divider::after {
    content: '';
    flex: 1;
    height: 1px;
    background: var(--border);
  }

  .sso-link {
    width: 100%;
    box-sizing: border-box;
    text-align: center;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 10px;
    font-size: 14px;
    text-decoration: none;
  }

  .sso-link:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }





</style>
