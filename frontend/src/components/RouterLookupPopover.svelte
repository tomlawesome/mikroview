<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Single instance mounted once at the app root (see App.svelte), same
  // fixed-position-from-trigger-coordinates approach as IpLookupPopover /
  // PortLookupPopover, driven by lib/routerLookup.svelte.ts's singleton.
  //
  // Renders the pushed rule/NAT data from mikroview's own store (issue
  // #186 step 4). For a filter rule there are three honest states beyond
  // loading/error, and they are deliberately distinct: the device never
  // pushed a table ("no data yet" -- with a pointer at what enables it),
  // the table exists but no rule carries this prefix (prefix resolution
  // is the operator's convention, see #186 step 4c), and one-or-more
  // matches (a shared prefix legitimately resolves to several rules).
  //
  // For NAT there are two *modes*, and #445's central requirement is
  // that they never share a rendering. A logged translation names its
  // rule and states a fact. An unlogged one cannot name anything, so it
  // shows the table partitioned by what the event rules out, with the
  // reason against every exclusion. Those are different claims, and a
  // reader who mistook the second for the first would have been misled
  // by this component -- so the mode is said in the header and again in
  // a text chip, never left as a footnote to be skimmed past. The chip
  // is accent-coloured in logged mode, but the distinction it carries is
  // the word, never the colour.
  import { routerLookupState as st, natChip, natTitle } from '../lib/routerLookup.svelte'
  import { appState } from '../lib/state.svelte'
  import RouterNatLookup from './RouterNatLookup.svelte'

  const POPOVER_WIDTH = 320

  let popoverEl: HTMLDivElement | undefined = $state()

  function onDocClick(e: MouseEvent) {
    if (popoverEl && !popoverEl.contains(e.target as Node)) st.close()
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') st.close()
  }

  $effect(() => {
    if (!st.anchor) return
    // Deferred past the current click's bubble phase -- same reasoning
    // as IpLookupPopover.svelte.
    const timer = setTimeout(() => document.addEventListener('click', onDocClick))
    return () => {
      clearTimeout(timer)
      document.removeEventListener('click', onDocClick)
    }
  })

  // Hold-while-open (#413's decision, shared by #445 and #439's lookup
  // popovers): this popover is anchored to a row, and under newest-at-top
  // that row slides down the screen as events arrive. The hold is taken
  // in an effect keyed on the anchor rather than inside the store's
  // open/close, so the release travels with the same lifecycle that took
  // it -- including an unmount, which no explicit close() would run.
  $effect(() => {
    if (!st.anchor) return
    appState.holdStream()
    return () => appState.releaseStream()
  })

  const style = $derived.by(() => {
    const a = st.anchor
    if (!a) return ''
    const x = Math.min(a.x, window.innerWidth - POPOVER_WIDTH - 12)
    const y = Math.min(a.y + 6, window.innerHeight - 80)
    return `left: ${Math.max(8, x)}px; top: ${y}px`
  })

  const title = $derived(
    st.mode === 'rule'
      ? `Rules with log-prefix “${st.ruleLabel}”`
      : natTitle(st.device, st.natMode),
  )

  const chip = $derived(natChip(st.natMode))
</script>

<svelte:window onkeydown={onKeydown} />

{#if st.anchor}
  <div bind:this={popoverEl} class="popover" {style} role="dialog" aria-label={title}>
    <div class="header">
      <span class="title">{title}</span>
      {#if st.mode === 'nat' && !st.loading && !st.error}
        <span class="chip" class:logged={st.natMode === 'logged'}>{chip}</span>
      {/if}
      <button class="close" onclick={() => st.close()} aria-label="Close">✕</button>
    </div>

    <!-- The NAT half is delegated whole -- loading, error and
         never-pushed included -- so the popover and the mobile sheet
         cannot end up wording the same state two different ways. -->
    {#if st.mode === 'nat'}
      <RouterNatLookup />
    {:else if st.loading}
      <div class="status">Loading…</div>
    {:else if st.error}
      <div class="status error">{st.error}</div>
    {:else if !st.available}
      <div class="status">
        No rule table pushed by “{st.device}” yet — this data arrives via the RouterOS push
        integration, not syslog.
      </div>
    {:else if st.rules.length === 0}
      <div class="status">
        No rule in the pushed table ({st.tableSize} rules) carries the log-prefix “{st.ruleLabel}”.
        Prefixes are set per rule in RouterOS (<code>log-prefix=</code>).
      </div>
    {:else}
      <div class="entries">
        {#each st.rules as r (r.ordinal)}
          <div class="entry">
            <div class="entry-header">
              <span class="ordinal">#{r.ordinal}</span>
              <span class="chain">{r.chain}</span>
              <span class="badge action-{r.action}">{r.action}</span>
            </div>
            {#if r.comment}
              <div class="comment">{r.comment}</div>
            {:else}
              <div class="comment dim">no comment set on this rule</div>
            {/if}
            {#if r.srcAddressList}
              <div class="detail">src-address-list: {r.srcAddressList}</div>
            {/if}
          </div>
        {/each}
      </div>
      <div class="footnote">
        Numbered as RouterOS numbers them — “go look at rule {st.rules[0].ordinal} in RouterOS”.
      </div>
    {/if}
  </div>
{/if}

<style>
  .popover {
    position: fixed;
    width: 320px;
    max-height: 340px;
    overflow-y: auto;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 7px;
    padding: 10px 12px;
    box-shadow: 0 12px 32px -8px rgba(0, 0, 0, 0.4);
    z-index: 40;
    font-size: 13px;
  }

  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 8px;
  }

  .title {
    font-weight: 600;
    color: var(--fg);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* The mode announcement. Bordered and coloured, but every bit of the
     distinction it carries is in its text -- an operator who cannot tell
     the accent from the muted border still reads "logged" or "not
     logged". */
  .chip {
    flex: none;
    margin-left: auto;
    font-family: var(--font-mono);
    font-size: 10px;
    letter-spacing: 0.02em;
    padding: 1px 6px;
    border-radius: 4px;
    color: var(--fg-muted);
    border: 1px solid var(--border);
    white-space: nowrap;
  }

  .chip.logged {
    color: var(--accent);
    border-color: var(--accent);
  }

  .close {
    flex: none;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    width: 20px;
    height: 20px;
    font-size: 11px;
    line-height: 1;
  }

  .close:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .status {
    color: var(--fg-dim);
    padding: 4px 0;
  }

  .status.error {
    color: var(--reject);
  }

  .status code {
    font-family: var(--font-mono);
    font-size: 12px;
  }


  .entries {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .entry + .entry {
    border-top: 1px solid var(--border);
    padding-top: 8px;
  }

  .entry-header {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }

  .ordinal {
    font-family: var(--font-mono);
    font-weight: 700;
    color: var(--fg);
  }

  .chain {
    font-family: var(--font-mono);
    color: var(--fg-muted);
    flex: 1;
  }


  .badge {
    flex: none;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.02em;
    padding: 1px 6px;
    border-radius: 4px;
    color: var(--fg-muted);
    border: 1px solid var(--border);
  }

  .badge.action-drop,
  .badge.action-reject {
    color: var(--reject);
    border-color: var(--reject);
  }

  .badge.action-accept {
    color: var(--accept);
    border-color: var(--accept);
  }

  .comment {
    color: var(--fg);
    margin-top: 3px;
    overflow-wrap: anywhere;
  }

  .comment.dim {
    color: var(--fg-dim);
    font-style: italic;
  }

  .detail {
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--fg-dim);
    margin-top: 2px;
    overflow-wrap: anywhere;
  }


  .footnote {
    margin-top: 10px;
    padding-top: 8px;
    border-top: 1px solid var(--border);
    color: var(--fg-dim);
    font-size: 11.5px;
  }

</style>
