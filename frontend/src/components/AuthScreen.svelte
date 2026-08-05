<script lang="ts">
  // Shared shell for the two pre-app states (setup/login) -- both are a
  // centered card with a username/password form, differing only in
  // title, submit label, and what happens on submit. Not a modal: this
  // replaces App.svelte's entire main content, same as Metrics being an
  // independent view rather than an overlay.
  import LogoLockup from './LogoLockup.svelte'

  let {
    title,
    subtitle,
    submitLabel,
    onsubmit,
    confirmPassword = false,
    skipLabel,
    skipHint,
    onskip,
  }: {
    title: string
    subtitle: string
    submitLabel: string
    onsubmit: (username: string, password: string) => Promise<string | null>
    confirmPassword?: boolean
    // skipLabel/skipHint/onskip: an optional secondary "continue without
    // an account" action (see AuthSetup.svelte) -- absent entirely for
    // the login screen, which has no equivalent choice.
    skipLabel?: string
    skipHint?: string
    onskip?: () => Promise<string | null>
  } = $props()

  let username = $state('')
  let password = $state('')
  let passwordConfirm = $state('')
  let error = $state<string | null>(null)
  let submitting = $state(false)
  // Requires an explicit second click past a visible explanation --
  // this is a permanent, only-CLI-reversible decision, so it shouldn't
  // be reachable by one accidental click (see the plan's "informed
  // choice, not accidental click" requirement).
  let confirmingSkip = $state(false)

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

  async function handleSkip() {
    if (!onskip) return
    error = null
    submitting = true
    const result = await onskip()
    submitting = false
    if (result) error = result
  }
</script>

<div class="screen">
  <form class="card" onsubmit={handleSubmit}>
    <LogoLockup size={26} />
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

    {#if onskip}
      <div class="skip">
        {#if !confirmingSkip}
          <button type="button" class="link" onclick={() => (confirmingSkip = true)} disabled={submitting}>
            {skipLabel}
          </button>
        {:else}
          <p class="skip-hint">{skipHint}</p>
          <div class="skip-actions">
            <button type="button" class="link" onclick={() => (confirmingSkip = false)} disabled={submitting}>
              Cancel
            </button>
            <button type="button" class="danger-link" onclick={handleSkip} disabled={submitting}>
              {submitting ? 'Please wait…' : 'Yes, continue without an account'}
            </button>
          </div>
        {/if}
      </div>
    {/if}
  </form>
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

  .skip {
    width: 100%;
    margin-top: 4px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
  }

  .link {
    background: none;
    border: none;
    width: auto;
    margin: 0;
    padding: 0;
    color: var(--fg-muted);
    font-size: 12px;
    text-decoration: underline;
  }

  .link:hover {
    opacity: 0.85;
    color: var(--fg);
  }

  .skip-hint {
    width: 100%;
    margin: 0;
    font-size: 12px;
    line-height: 1.5;
    color: var(--fg-muted);
    text-align: center;
  }

  .skip-actions {
    display: flex;
    gap: 14px;
    align-items: center;
  }

  .danger-link {
    background: none;
    border: none;
    width: auto;
    margin: 0;
    padding: 0;
    color: var(--reject);
    font-size: 12px;
    font-weight: 600;
    text-decoration: underline;
  }

  .danger-link:hover {
    opacity: 0.85;
  }

  .danger-link:disabled,
  .link:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
