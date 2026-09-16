<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // After an upgrade, the one thing the operator still has to do by hand
  // (#1240, docs/upgrades.md): paste step 1 of the setup again on each
  // router, because the wizard's script changes between versions and the
  // router does not update itself.
  //
  // In the banner stack under ConfigProblemBanner, whose structure this
  // copies: the stack is where the deck already puts something that must
  // be acted on, it pushes the content down rather than covering a
  // scene, and it survives navigation between rolls. Not a toast (it
  // would be gone before it was read) and not a modal (nothing is
  // broken).
  //
  // Calm, not loud: --accent on the raised surface, never --reject.
  // Red means something is wrong; this is an instruction after a
  // successful upgrade, and red here would teach operators to ignore
  // red.
  //
  // Unlike the config-problem banner, there is no ✕ and no per-page-view
  // dismissal: `done` is the dismissal, it says what it claims, and it
  // is recorded server-side for the whole instance, so the line is there
  // on every reload and in every admin session until somebody presses
  // it.
  import { authState } from '../lib/auth.svelte'
  import { upgradeState } from '../lib/upgrade.svelte'
  import { wizardState } from '../lib/wizard.svelte'

  // Admin sessions only, same gate as ConfigProblemBanner: the action is
  // the admin's, and a viewer can do nothing about it. An effect rather
  // than a bare call, because the role is not known yet on the first
  // paint after a reload.
  $effect(() => {
    if (authState.role === 'admin') upgradeState.ensureLoaded()
  })

  // The same in-app navigation everything else uses to reach the wizard
  // (it is a modal over whatever is on screen, not a page): open it,
  // then put it on step 1, which is the step with the script to paste.
  function openSetup() {
    wizardState.launch()
    wizardState.goTo(1)
  }
</script>

{#if authState.role === 'admin' && upgradeState.show}
  <div class="banner" role="status">
    <span class="line">{upgradeState.line}</span>
    <div class="actions">
      <button type="button" class="link" onclick={openSetup}>open setup</button>
      {#if upgradeState.offersDone}
        <button type="button" class="done" onclick={() => upgradeState.acknowledge()}>done</button>
      {/if}
    </div>
  </div>
{/if}

<style>
  .banner {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.55rem 0.9rem;
    /* The raised neutral ground with the accent ink, per the ratified
       placement note: colourway-aware, so this re-tints with the rest of
       the UI, and deliberately not the reject pair ConfigProblemBanner
       uses. */
    background: var(--bg-elevated);
    color: var(--accent);
    border-bottom: 1px solid var(--border);
    font-size: 0.8rem;
    line-height: 1.45;
  }

  .line {
    flex: 1;
    min-width: 0;
  }

  .actions {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-shrink: 0;
  }

  .link {
    background: none;
    border: none;
    padding: 0;
    color: var(--accent);
    text-decoration: underline;
    cursor: pointer;
    font: inherit;
  }

  .done {
    background: none;
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 0.15rem 0.6rem;
    color: var(--fg);
    cursor: pointer;
    font: inherit;
  }

  .link:hover {
    color: var(--fg);
  }

  .done:hover {
    color: var(--fg);
    border-color: var(--hair-2);
  }
</style>
