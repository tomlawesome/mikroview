<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Opening the router-backup drop box (#1361): the one dialog both
  // doors share -- Settings → router backups' "drop box: closed · open…"
  // row (RouterBackups.svelte) and the wizard's Back up nightly choice
  // (wizard/StepRouter.svelte). The ratified design names one dialog
  // and one endpoint for both, so the copy and the call live here once.
  //
  // A password re-check (owner's ruling 3a -- closing needs none, and
  // is not this component's business), the trust caveat every SFTP
  // push already carries, and the HTTPS-only alternative that needs no
  // open port at all (docs/routeros-setup.md, 7c-ii). The server's own
  // words come back on a refusal (wrong password, port already in use)
  // and the dialog stays open; on success the new state goes up to the
  // host, read from the server's answer, never from what was asked for.
  //
  // `look` is which host's form grammar to wear: Settings' dashed
  // underline inputs and link-buttons, or the wizard's boxed inputs and
  // outlined buttons (wizard.css). Same markup, same words, same acts.

  import { setRouterBackupSwitch } from '../lib/api'
  import type { RouterBackupSwitchState } from '../lib/types'

  let {
    look = 'settings',
    onopened,
    oncancel,
  }: {
    look?: 'settings' | 'wizard'
    onopened: (state: RouterBackupSwitchState) => void
    oncancel: () => void
  } = $props()

  let password = $state('')
  let submitting = $state(false)
  let error = $state<string | null>(null)

  async function submit() {
    if (!password || submitting) return
    submitting = true
    error = null
    const result = await setRouterBackupSwitch(true, password)
    submitting = false
    if (typeof result === 'string') {
      error = result
      return
    }
    password = ''
    onopened(result)
  }

  function cancel() {
    password = ''
    error = null
    oncancel()
  }
</script>

<div class="dbox {look}">
  <p class="caveat">
    RouterOS never checks who it is sending to — anyone on the path between your router and mikroview could read the
    backup and the ingest token. Only open this on a network you trust, or use the HTTPS-only alternative
    (docs/routeros-setup.md, section 7c-ii), which needs no open port at all.
  </p>
  <label class="lab">
    your password
    <input
      type="password"
      autocomplete="current-password"
      disabled={submitting}
      bind:value={password}
      onkeydown={(e) => {
        if (e.key === 'Enter') submit()
      }}
    />
  </label>
  {#if error}<p class="err" role="alert">{error}</p>{/if}
  <span class="acts">
    <button type="button" class="act" disabled={submitting} onclick={cancel}>cancel</button>
    <button type="button" class="act go" disabled={submitting || !password} onclick={submit}>
      {submitting ? 'opening…' : 'open'}
    </button>
  </span>
</div>

<style>
  .dbox {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .caveat {
    margin: 0;
  }

  .lab {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .err {
    margin: 0;
  }

  .acts {
    display: flex;
    gap: 8px;
  }

  /* Settings' grammar (RouterBackups.svelte's .pform): full width under
     both columns of the group, dashed-underline mono input, link-style
     acts on the right, the caveat as the group's italic hint. */
  .settings {
    grid-column: 1 / -1;
    padding: 10px 0 2px;
    margin-top: 4px;
    border-top: 1px solid var(--border);
  }

  .settings .caveat {
    max-width: 480px;
    font-size: 11.5px;
    font-style: italic;
    color: var(--fg-dim);
  }

  .settings .lab {
    font-size: 11px;
    color: var(--fg-dim);
    max-width: 360px;
  }

  .settings input {
    background: transparent;
    border: 0;
    border-bottom: 1px dashed var(--border);
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg);
    padding: 3px 0;
    outline: none;
  }

  .settings input:focus {
    border-bottom-color: var(--accent);
  }

  .settings .err {
    font-size: 11.5px;
    color: var(--reject);
  }

  .settings .acts {
    justify-content: flex-end;
  }

  .settings .act {
    background: none;
    border: none;
    padding: 0;
    font-size: inherit;
    color: var(--accent);
    cursor: pointer;
    text-decoration: underline;
    text-decoration-color: transparent;
  }

  .settings .act:hover {
    text-decoration-color: currentColor;
  }

  .settings .act:disabled {
    cursor: default;
    opacity: 0.6;
  }

  /* The wizard's grammar (wizard.css): the caveat as muted running text
     -- the caution bar is already on the step's own "drop box is
     closed" box above it, and two bars stacked read as clutter -- the
     boxed input the form's other fields wear (wizard.css styles the
     input and the buttons themselves, by tag), the acts left-aligned
     under it as the footer's own buttons are. */
  .wizard {
    align-self: stretch;
    max-width: 480px;
  }

  .wizard .caveat {
    padding: 0 12px;
    color: var(--fg-muted);
    font-size: 12.5px;
    line-height: 1.55;
  }

  .wizard .lab {
    font-size: 12px;
    color: var(--fg-dim);
  }

  .wizard input {
    width: 320px;
  }

  .wizard .err {
    font-size: 12.5px;
    color: var(--warn);
  }

  .wizard .act.go:not(:disabled) {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--bg);
    font-weight: 600;
  }
</style>
