<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Every scene's own bar, under the Atlas identity (owner, 2026-08-29:
  // pages are the site -- no persistent chrome). Carries the wordmark,
  // the page's view switcher, the live+rate reading, the flag/watch
  // markers and the account chip -- round 30's ratified content (#683,
  // #700). No scene name and no strap: the owner struck both on every
  // deck in turn and then ratified it in words (#697, 2026-08-31, "I
  // meant all... No page heading, no strap"), and round 30 deleted
  // `.scname`/`.epi` from its own stylesheet so nobody restores them
  // from it. The rail names the card you are on; the bar does not.
  //
  // Where the heading stood, the switchers ride instead: metrics'
  // seismograph/register/table and the docket's flags/watchlist/audit
  // log. Round 30 draws them absolutely positioned at the bar's own
  // line, just right of the wordmark; here the bar is a real flex row,
  // so they simply follow the wordmark and land in the same place.
  // ported field-for-field from docs/design/concepts/round-29/the-
  // whole.html rather than approximated.
  //
  // The stream's own filter box/spans (#s5's `.filterline`) do NOT ride
  // here (#697, round 30's "the top is a flow column": chrome, then the
  // filter, then the bar, then the whisper, then the lines -- each its
  // own row, not folded into this one). They moved to FilterBar.svelte,
  // which sits below this bar for the stream -- see #700/#691. Everything
  // else the old toolbar held (eps, buffer%, the max-age selector,
  // Autoscroll/Pause/Group/Clear) is off the bar entirely -- the
  // mockup's own `.scenebar` markup for #s5 does not draw them, so they
  // are gaps, recorded on the issue, not homed here.
  //
  // Inside the deck every card carries its own bar, so the scene named
  // here is the card's own, passed as a prop; outside the deck (the
  // operate pages) it defaults to the current view.
  import { appState, type View } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { METRICS_VIEWS, metricsPref } from '../lib/metrics.svelte'
  import ConnectionIndicator from './ConnectionIndicator.svelte'
  import AlarmCluster from './AlarmCluster.svelte'
  import AccountMenu from './AccountMenu.svelte'

  let { scene = null }: { scene?: View | null } = $props()
  const view = $derived(scene ?? appState.view)

  // The docket's three tabs, mirroring Docket.svelte's own derivation so
  // the bar and the pane can never disagree about which tab is on: an
  // absent tier falls back to flags rather than showing a tab nobody can
  // reach (#653 -- absent, not disabled, as everywhere else).
  const isAdmin = $derived(authState.role === 'admin')
  const canEdit = $derived(authState.canEdit)
  const isDocket = $derived(view === 'flags' || view === 'watchlist' || view === 'audit')
  const docketTab = $derived.by(() => {
    if (appState.view === 'watchlist' && canEdit) return 'watchlist'
    if (appState.view === 'audit' && isAdmin) return 'audit'
    return 'flags'
  })
</script>

<div class="scene-bar">
  <span class="wm">MIKRO<em>VIEW</em></span>
  {#if view === 'metrics'}
    <span class="switch" role="group" aria-label="Metrics view">
      {#each METRICS_VIEWS as option (option.value)}
        <button
          class="sw"
          class:on={metricsPref.view === option.value}
          aria-pressed={metricsPref.view === option.value}
          title={option.title}
          onclick={() => metricsPref.setView(option.value)}>{option.label}</button
        >
      {/each}
    </span>
  {:else if isDocket}
    <!-- No counts under these labels (round 30): they were tried inline
         and beneath and both were called clumsy, and the counts already
         live in this same bar's flag and watcher marks. -->
    <span class="switch" role="tablist" aria-label="The docket">
      <button
        class="sw"
        class:on={docketTab === 'flags'}
        role="tab"
        aria-selected={docketTab === 'flags'}
        onclick={() => (appState.view = 'flags')}>flags</button
      >
      {#if canEdit}
        <button
          class="sw"
          class:on={docketTab === 'watchlist'}
          role="tab"
          aria-selected={docketTab === 'watchlist'}
          onclick={() => (appState.view = 'watchlist')}>watchlist</button
        >
      {/if}
      {#if isAdmin}
        <button
          class="sw"
          class:on={docketTab === 'audit'}
          role="tab"
          aria-selected={docketTab === 'audit'}
          onclick={() => (appState.view = 'audit')}>audit log</button
        >
      {/if}
    </span>
  {/if}

  <div class="status-cluster">
    <ConnectionIndicator showRate={view !== 'live'} />
    <AlarmCluster />
    <AccountMenu />
  </div>
</div>

<style>
  .scene-bar {
    display: flex;
    align-items: baseline;
    gap: 16px;
    padding: 12px 20px 4px;
    flex-wrap: wrap;
  }
  .wm {
    font-size: 13px;
    font-weight: 800;
    letter-spacing: 0.22em;
    color: var(--fg-dim);
  }
  .wm em {
    color: var(--accent);
    font-style: normal;
  }
  /* The switchers, from round 30's `.dtabs`/`.mviews`: quiet sans
     labels, the active one in full ink over an accent rule. */
  .switch {
    display: flex;
    gap: 16px;
    align-items: baseline;
  }
  .sw {
    background: transparent;
    border: none;
    padding: 0 0 2px;
    font: 500 11px var(--font-sans);
    color: var(--fg-dim);
    cursor: pointer;
    border-bottom: 1px solid transparent;
  }
  .sw:hover {
    color: var(--fg);
  }
  .sw.on {
    color: var(--fg);
    border-bottom-color: var(--accent);
  }

  .status-cluster {
    margin-left: auto;
    display: flex;
    gap: 16px;
    align-items: center;
  }
</style>
