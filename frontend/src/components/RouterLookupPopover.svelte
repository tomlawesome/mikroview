<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Single instance mounted once at the app root (see App.svelte), same
  // fixed-position-from-trigger-coordinates approach as IpLookupPopover /
  // PortLookupPopover, driven by lib/routerLookup.svelte.ts's singleton.
  //
  // Renders the pushed rule/NAT data from mikroview's own store (issue
  // #186 step 4). Three honest states beyond loading/error, and they are
  // deliberately distinct: the device never pushed a table ("no data
  // yet" -- with a pointer at what enables it), the table exists but no
  // rule carries this prefix (prefix resolution is the operator's
  // convention, see #186 step 4c), and one-or-more matches (a shared
  // prefix legitimately resolves to several rules).
  import { routerLookupState as st } from '../lib/routerLookup.svelte'

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

  const style = $derived.by(() => {
    const a = st.anchor
    if (!a) return ''
    const x = Math.min(a.x, window.innerWidth - POPOVER_WIDTH - 12)
    const y = Math.min(a.y + 6, window.innerHeight - 80)
    return `left: ${Math.max(8, x)}px; top: ${y}px`
  })

  const title = $derived(
    st.mode === 'rule' ? `Rules with log-prefix “${st.ruleLabel}”` : `NAT table — ${st.device}`,
  )
</script>

<svelte:window onkeydown={onKeydown} />

{#if st.anchor}
  <div bind:this={popoverEl} class="popover" {style} role="dialog" aria-label={title}>
    <div class="header">
      <span class="title">{title}</span>
      <button class="close" onclick={() => st.close()} aria-label="Close">✕</button>
    </div>

    {#if st.loading}
      <div class="status">Loading…</div>
    {:else if st.error}
      <div class="status error">{st.error}</div>
    {:else if !st.available}
      <div class="status">
        No {st.mode === 'rule' ? 'rule' : 'NAT'} table pushed by “{st.device}” yet — this data
        arrives via the RouterOS push integration, not syslog.
      </div>
    {:else if st.mode === 'rule' && st.rules.length === 0}
      <div class="status">
        No rule in the pushed table ({st.tableSize} rules) carries the log-prefix “{st.ruleLabel}”.
        Prefixes are set per rule in RouterOS (<code>log-prefix=</code>).
      </div>
    {:else if st.mode === 'rule'}
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
    {:else if st.natRules.length === 0}
      <!-- The rule mode has had an empty state since it was written;
           this one did not (#267, Uncertain), so a router that pushed a
           NAT table with no rules in it -- entirely ordinary, plenty of
           routers do no NAT -- got an empty box and a footnote
           explaining how to read rules that are not there. -->
      <div class="status">
        “{st.device}” has pushed its NAT table and it is empty — no NAT rules are configured on
        that router.
      </div>
    {:else}
      <div class="entries nat">
        {#each st.natRules as r (r.ordinal)}
          <div class="entry">
            <div class="entry-header">
              <span class="ordinal">#{r.ordinal}</span>
              <span class="chain">{r.chain}</span>
              <span class="badge">{r.action}</span>
            </div>
            {#if r.comment}
              <div class="comment">{r.comment}</div>
            {/if}
          </div>
        {/each}
      </div>
      <div class="footnote">
        The full pushed NAT table — a log line shows the translation result, never which rule
        performed it, so match it up by eye.
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
  }

  .footnote {
    margin-top: 10px;
    padding-top: 8px;
    border-top: 1px solid var(--border);
    color: var(--fg-dim);
    font-size: 11.5px;
  }
</style>
