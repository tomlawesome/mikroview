<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // #646 beat 3, "Attach": the journey's own scene, right after the
  // admin account is made. Same void identity as the door (#645) --
  // this is still before any data exists to show -- but not the door
  // itself; AuthSetup.svelte's onEnter already ran.
  //
  // The two lines are the real ones: steps.syslog.commands from POST
  // /api/setup/commands (#436 moved the RouterOS syntax server-side) is
  // exactly what step 2 of the full wizard renders, and what
  // docs/routeros-setup.md documents as "2. Forward firewall log events
  // to it" -- never invented copy. Round 27's verdict, verbatim: "one
  // account kept here, then the two RouterOS lines to paste... that is
  // the whole of setup, for now."
  import { authState } from '../lib/auth.svelte'
  import { wizardState } from '../lib/wizard.svelte'
  import { journeyState } from '../lib/journey.svelte'

  let copied = $state(false)

  // wizardState.status carries the instance's own syslog port -- the
  // same field step 2 of the full wizard reads. SetupWizard's own effect
  // also refreshes this on becoming authenticated, but this beat can
  // render first, so it asks for itself rather than assuming a race it
  // wins. Fetching the commands is guarded on their absence rather than
  // repeated every render -- this beat needs no token, no picked
  // version, nothing that would call for a second fetch later.
  $effect(() => {
    if (!wizardState.status) wizardState.refresh()
  })

  $effect(() => {
    if (wizardState.status && !wizardState.commands) wizardState.refreshCommands()
  })

  const commands = $derived(wizardState.commands?.steps.syslog.commands ?? '')

  async function copy() {
    if (!commands) return
    try {
      await navigator.clipboard.writeText(commands)
      copied = true
      setTimeout(() => (copied = false), 1500)
    } catch {
      // Clipboard access can fail (permissions, non-secure context) --
      // the block stays selectable either way.
    }
  }
</script>

<div class="attach-screen" data-void>
  <div class="stack">
    <div class="wm-box"><span class="wm">MIKRO<em>VIEW</em></span></div>
    <p class="account">Signed in as <b>{authState.username}</b> — the account you just made.</p>
    <p class="lead">Two lines on the router, and mikroview starts hearing it:</p>
    {#if commands}
      <pre class="code">{commands}</pre>
      <button type="button" class="copy" onclick={copy}>{copied ? 'Copied' : 'Copy'}</button>
    {:else}
      <p class="waiting">Fetching this instance's own address…</p>
    {/if}
    <p class="note">That is the whole of setup, for now — paste and move on. Mikroview never connects to the router.</p>
    <button type="button" class="continue" onclick={() => journeyState.fromAttach()}>Continue</button>
  </div>
</div>

<style>
  .attach-screen {
    flex: 1;
    min-height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 20px;
    background: var(--bg);
  }

  .stack {
    width: 100%;
    max-width: 420px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    text-align: center;
  }

  .wm-box {
    display: inline-block;
    margin-bottom: 6px;
    padding: 10px 24px;
    border: 1.5px solid var(--now);
    border-radius: 4px;
    box-shadow:
      0 0 14px color-mix(in srgb, var(--now) 22%, transparent),
      inset 0 0 10px color-mix(in srgb, var(--now) 6%, transparent);
  }

  .wm {
    font-size: 26px;
    font-weight: 800;
    letter-spacing: 0.04em;
    color: var(--fg);
  }

  .wm em {
    color: var(--accent);
    font-style: normal;
  }

  .account {
    margin: 0;
    font-size: 13px;
    color: var(--fg-muted);
  }

  .account b {
    color: var(--fg);
    font-weight: 600;
  }

  .lead {
    margin: 0;
    font-size: 14px;
    color: var(--fg);
  }

  .code {
    width: 100%;
    box-sizing: border-box;
    margin: 0;
    padding: 12px 14px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    color: var(--fg-muted);
    font-family: var(--font-mono);
    font-size: 11.5px;
    line-height: 1.6;
    text-align: left;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .waiting {
    margin: 4px 0;
    font-size: 12px;
    color: var(--fg-dim);
  }

  .copy {
    align-self: flex-end;
    padding: 5px 14px;
    font-size: 11.5px;
    font-weight: 600;
    color: var(--fg-muted);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    cursor: pointer;
  }

  .copy:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .note {
    margin: 2px 0 0;
    font-size: 11.5px;
    color: var(--fg-dim);
  }

  .continue {
    margin-top: 10px;
    padding: 7px 30px;
    font: 600 13px var(--font-sans);
    letter-spacing: 0.04em;
    color: var(--accent);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    cursor: pointer;
  }

  .continue:hover {
    border-color: var(--accent);
  }
</style>
