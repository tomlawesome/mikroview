<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Step 4 · Tag firewall rules (#1384; DESIGN.md, "The steps, in
  // detail"), ported from the prototype's tuneBody. The lead counts what
  // the push read; the rule list (chain · action · rule · why it matters
  // · prefix) proposes the rules that log nothing on a boundary nobody
  // watches, a checkbox each and every one ticked to start; the tagging
  // block below it (or "# nothing ticked"); Copy — N rules, "Safe to
  // paste again; it sets, never adds."; and the observation line: quiet
  // until Copy, waiting after, counting once the first new prefix lands.
  //
  // The proposal is read from MikroView's own copy of the pushed rule
  // table (fetchRouterRules -- the store, never the router), and the
  // block is printed for the operator to paste: nothing here runs it.
  // Once copied, the run counts decoded lines upward from that moment
  // (wizardRun.markTuneCopied), so `tagged` is the first new prefix
  // landing, not the old ones. Skip this step and Next are the footer's
  // (Wizard.svelte); the rail's receipt is wizardRun.ts's rowState.

  import { fetchRouterRules, type RouterFilterRule, type RouterTable } from '../../lib/api'
  import { copyToClipboard } from '../../lib/clipboard'
  import { wizardState } from '../../lib/wizard.svelte'
  import { wizardRun } from '../../lib/wizardRun.svelte'
  import { proposeRules, tagBlock, type TuneRule } from '../../lib/wizardTune'

  // The router this run is about; a walk with no router record yet reads
  // the first device whose push carried a rule table.
  const device = $derived(
    wizardState.ledgerDevice ||
      wizardState.status?.devices.find((d) => d.pushedKinds && 'filter-rule' in d.pushedKinds)?.device ||
      '',
  )

  let table = $state<RouterTable<RouterFilterRule> | null>(null)
  let chosen = $state<Set<number>>(new Set())

  // One read of the rule table per visit to the step; the proposal is
  // fixed from there, and the ticks start all on (the prototype's
  // chosen set).
  $effect(() => {
    if (!device) return
    let stale = false
    fetchRouterRules(device)
      .then((t) => {
        if (stale) return
        table = t
        chosen = new Set(proposeRules(t.rules).rules.map((r) => r.ordinal))
      })
      .catch(() => {
        if (!stale) table = { available: false, rules: [] }
      })
    return () => {
      stale = true
    }
  })

  const ev = $derived(wizardRun.evidence)
  const proposal = $derived(proposeRules(table?.rules ?? []))
  const chosenRules = $derived(proposal.rules.filter((r) => chosen.has(r.ordinal)))
  const block = $derived(table ? tagBlock(table.rules, chosenRules) : '')
  const copied = $derived(wizardRun.tuneCopied)

  function toggle(r: TuneRule) {
    if (copied) return
    const next = new Set(chosen)
    if (next.has(r.ordinal)) next.delete(r.ordinal)
    else next.add(r.ordinal)
    chosen = next
  }

  // Copy: what is on the clipboard is decided here, whether or not the
  // browser's own write succeeds -- the block stays selectable either
  // way -- and the count starts from this moment.
  async function copy() {
    if (copied || chosenRules.length === 0) return
    wizardRun.tuneChosen = chosenRules
    wizardRun.markTuneCopied(chosenRules.length)
    await copyToClipboard(block)
  }
</script>

{#if wizardRun.push === false || (table && !table.available)}
  <h3>Tag firewall rules</h3>
  <p class="hint">Needs the push — the rule table is what proposes the rules to tag.</p>
{:else if !table}
  <h3>Tag firewall rules</h3>
{:else}
  <h3>{proposal.rules.length} rules log nothing.</h3>
  <p class="hint">
    The push read {proposal.total} rules; {proposal.alreadyLog} already log. These {proposal.rules.length} sit on boundaries
    nobody watches — a rule, but no line. Tick the ones to tag; this is the only second paste.
  </p>
  <div class="rules">
    <div class="h"><span></span><span>chain</span><span>action</span><span>rule</span><span>why it matters</span><span>prefix</span></div>
    {#each proposal.rules as r (r.ordinal)}
      <label class:lit={ev.tagged && chosen.has(r.ordinal)}>
        <input type="checkbox" checked={chosen.has(r.ordinal)} disabled={copied} onchange={() => toggle(r)} />
        <span class="mono">{r.chain}</span>
        <span class="act {r.action}">{r.action}</span>
        <span>{r.comment || `rule ${r.ordinal}`}</span>
        <span class="why">{r.why}</span>
        <span class="pre">{r.prefix}</span>
      </label>
    {/each}
  </div>
  <pre class="script tagblock" aria-label="Tagging block">{#if block}{block}{:else}<span class="sec"># nothing ticked</span>{/if}</pre>
  <div class="copyrow">
    <button type="button" class="primary" disabled={copied || chosenRules.length === 0} onclick={copy}>
      {copied ? 'Copied' : `Copy — ${chosenRules.length} rule${chosenRules.length === 1 ? '' : 's'}`}
    </button>
    <span class="note">Safe to paste again; it sets, never adds.</span>
  </div>
  {#if ev.tagged}
    <div class="obs counting">
      {wizardRun.chosenCount} rules logging · first new line {wizardRun.tunedAt}<small>{ev.lines.toLocaleString()} lines</small>
    </div>
  {:else if copied}
    <div class="obs waiting">Waiting for the first line carrying a new prefix.</div>
  {:else}
    <div class="obs quiet">Nothing to wait for until you paste — the rule table is already here.</div>
  {/if}
{/if}
