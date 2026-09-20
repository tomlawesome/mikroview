<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // #1218: "N new settings are available" -- the setup wizard's own
  // paste-block treatment (SetupWizard.svelte's .paste/pre.script/
  // button.copy idiom), applied to whatever this version understands
  // that config.yaml does not set. Self-contained and admin-gated by
  // whoever mounts it, matching AuditLog.svelte's pattern: it fetches
  // its own data on mount rather than taking it as a prop.
  //
  // Deliberately its own component rather than folded into
  // SetupWizard.svelte or EngineRoom.svelte: this notice is not part of
  // the guided setup wizard (it fires on a version change, not a setup
  // step) and has no natural home in either file yet -- wiring it into
  // Settings is left to whoever mounts this.
  import { onMount } from 'svelte'
  import { configUpgradeState } from '../lib/configUpgrade.svelte'
  import { copyToClipboard } from '../lib/clipboard'
  import { toastState } from '../lib/toast.svelte'

  onMount(() => {
    configUpgradeState.refresh()
  })

  // Mirrors SetupWizard.svelte's own `copied` label state, keyed by
  // setting rather than a single value since more than one block is on
  // screen at once here.
  let copiedKey = $state('')

  async function copy(key: string, block: string) {
    const ok = await copyToClipboard(block)
    toastState.show(ok ? 'Copied' : 'Copy failed')
    if (!ok) return
    copiedKey = key
    setTimeout(() => {
      if (copiedKey === key) copiedKey = ''
    }, 1500)
  }

  // Plain per-visit close, not a remembered dismissal (owner ruling,
  // #1218 follow-up: "Just have a close button. It's simple.") -- local
  // state rather than configUpgradeState's, so it resets whenever this
  // component remounts, which is every time Settings is scrolled back
  // to (Deck.svelte unmounts an off-screen card's scene, deckMount.ts).
  let closed = $state(false)
</script>

<div class="config-upgrade">
  {#if closed}
    <!-- nothing: closed for this visit only -->
  {:else if configUpgradeState.error}
    <p class="empty error">Could not check for new settings: {configUpgradeState.error}</p>
  {:else if configUpgradeState.loaded && configUpgradeState.settings.length === 0}
    <p class="empty">Nothing new -- every setting this version understands is already set.</p>
  {:else if configUpgradeState.loaded}
    <div class="head">
      <p class="intro">
        {configUpgradeState.settings.length} setting{configUpgradeState.settings.length === 1 ? '' : 's'} this
        version understands {configUpgradeState.settings.length === 1 ? "isn't" : "aren't"} set in your config.yaml yet.
        Paste whichever you want into <code>config.yaml</code>, at the top level, then restart mikroview.
      </p>
      <button type="button" class="close" onclick={() => (closed = true)} aria-label="close">×</button>
    </div>
    {#each configUpgradeState.settings as setting (setting.key)}
      <div class="paste">
        <pre class="script">{setting.block}</pre>
        <button type="button" class="copy" onclick={() => copy(setting.key, setting.block)}>
          {copiedKey === setting.key ? 'copied' : 'copy'}
        </button>
      </div>
    {/each}
  {/if}
</div>

<style>
  .config-upgrade {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 14px 16px;
  }

  .head {
    display: flex;
    align-items: flex-start;
    gap: 12px;
  }

  .intro {
    margin: 0;
    max-width: 70ch;
    font-size: 13px;
    color: var(--fg-muted);
    line-height: 1.5;
  }

  .close {
    flex-shrink: 0;
    padding: 2px 8px;
    line-height: 1;
    font-size: 16px;
  }

  .intro code {
    font-family: var(--font-mono);
    color: var(--fg);
  }

  .empty {
    margin: 0;
    color: var(--fg-dim);
    font-size: 13px;
    padding: 10px 0;
  }

  .empty.error {
    color: var(--reject);
  }

  /* Same paste-block shape as SetupWizard.svelte's .paste/pre.script/
     button.copy -- the pre and its copy button read as one row, the
     button as part of the box's right edge. Not shared code: Svelte
     scopes component styles, so this is the same values re-declared
     rather than imported. */
  .paste {
    display: flex;
    align-items: stretch;
    gap: 0;
  }

  pre.script {
    flex: 1;
    min-width: 0;
    margin: 0;
    padding: 12px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-right: none;
    border-top-right-radius: 0;
    border-bottom-right-radius: 0;
    border-radius: 6px;
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.5;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    color: var(--fg);
    user-select: all;
    max-height: calc(14 * 1.5em + 24px);
    overflow-y: auto;
  }

  button {
    border-radius: 5px;
    padding: 7px 13px;
    font-size: 13px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    align-self: flex-start;
  }

  button:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  button.copy {
    align-self: stretch;
    border-top-left-radius: 0;
    border-bottom-left-radius: 0;
  }
</style>
