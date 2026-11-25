<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // ✓ · Where setup stands (DESIGN.md, "The steps, in detail"; #1385),
  // ported from the prototype's doneBody (round-15/wizard.js) and the
  // .ledger rules (round-15/wizard.css, already carried into wizard.css
  // by #1381): the lead, the compact track, a row per thing -- green
  // with its receipt in its own ink, or dashed and struck where set
  // aside -- Undo per green row, and "undo everything on the router
  // first" revealing every undo block in order, then the act that
  // forgets this router on MikroView.
  //
  // Every RouterOS line shown here comes from the server: cert/logs/
  // push/backup's Undo from POST /api/setup/commands' own Undo builders
  // (wizardRun.undoTextFor, backed by internal/routeros/commands.go's
  // Undo* functions), and the tagged-rules Undo from wizardTune.undoBlock
  // against the same pushed rule table StepTune reads -- for the rules this
  // run tagged, or, on a reopened ledger, the rules the push shows tagged
  // (taggedInPush) -- nothing here
  // hand-writes a RouterOS line (docs/decisions/observe-never-probe.md).

  import { fetchRouterRules, type RouterFilterRule, type RouterTable } from '../../lib/api'
  import { wizardRun } from '../../lib/wizardRun.svelte'
  import { taggedInPush, undoBlock } from '../../lib/wizardTune'
  import { taggedStands, type LedgerRowId } from '../../lib/wizardRun'
  import Track from './Track.svelte'

  const ev = $derived(wizardRun.evidence)
  const rows = $derived(wizardRun.ledgerRows)
  const n = $derived(rows.filter((r) => r.done).length)
  // The first-run tail's offered row is neither a proof nor set aside
  // (#1360), so it counts in neither.
  const k = $derived(rows.filter((r) => !r.done && !r.offer).length)

  // The tagged-rules row's own Undo needs the actual pushed rule table
  // to address each rule the same way the tagging block did (matcherFor,
  // by comment where it is unique, by position otherwise) -- the same
  // read StepTune makes, fetched here only once this step is reached and
  // only when there is a tagged row to undo.
  const device = $derived(wizardRun.rulesDevice)
  let table = $state<RouterTable<RouterFilterRule> | null>(null)
  $effect(() => {
    if (!device || !taggedStands(ev)) return
    let stale = false
    fetchRouterRules(device)
      .then((t) => {
        if (!stale) table = t
      })
      .catch(() => {})
    return () => {
      stale = true
    }
  })
  const tuneUndoText = $derived(
    table ? undoBlock(table.rules, wizardRun.tuneChosen.length ? wizardRun.tuneChosen : taggedInPush(table.rules)) : '',
  )

  function undoTextFor(id: LedgerRowId): string {
    return id === 'tune' ? tuneUndoText : wizardRun.undoTextFor(id)
  }

  const undoAllText = $derived(wizardRun.undoOrder.map((id) => undoTextFor(id)).join('\n'))

  let forgetting = $state(false)
  let forgetError = $state<string | null>(null)

  // Round 28's arm-then-confirm gesture (#1539 C1): the first click arms
  // it, the second forgets; a click elsewhere or Escape disarms.
  let armed = $state(false)

  function onForgetClick(e: MouseEvent) {
    e.stopPropagation()
    if (!armed) {
      armed = true
      return
    }
    armed = false
    void forget()
  }

  async function forget() {
    if (forgetting) return
    forgetting = true
    forgetError = await wizardRun.forget()
    forgetting = false
  }
</script>

<svelte:window
  onclick={() => (armed = false)}
  onkeydown={(e) => {
    if (e.key === 'Escape') armed = false
  }}
/>

<h3>{wizardRun.name} is sending.</h3>
<p class="hint">
  {n}
  {n === 1 ? 'thing stands' : 'things stand'} on evidence{#if k}; {k} {k === 1 ? 'was' : 'were'} set aside{/if}. Finish
  takes you to the fall, with {wizardRun.name} already flowing.
</p>
<Track stations={wizardRun.trackStations} stage={wizardRun.stage} compact />
<div class="ledger">
  {#each rows as r (r.t)}
    <div class="row" class:skip={!r.done && !r.offer} style:--ink={r.done && r.ink ? `var(--ink-${r.ink})` : null}>
      <span class="step-n" class:done={r.done} class:skip={!r.done && !r.offer} class:offer={r.offer}>{r.done ? '✓' : r.offer ? '+' : '–'}</span>
      <span>
        {r.t}
        <div class="r" class:done={r.done} class:skip={!r.done && !r.offer} class:offer={r.offer}>{r.r}</div>
      </span>
      {#if r.offer}
        <button type="button" class="linkish u" onclick={() => wizardRun.toBlock()}>Set it up</button>
      {:else if r.u}
        <button type="button" class="linkish u" onclick={() => wizardRun.toggleUndo(r.u as LedgerRowId)}>
          {wizardRun.undoOpen === r.u ? 'Hide' : 'Undo'}
        </button>
      {:else}
        <span></span>
      {/if}
      {#if r.u && wizardRun.undoOpen === r.u}
        <div class="undo">
          <pre>{undoTextFor(r.u as LedgerRowId)}</pre>
          <p class="note">Paste on the router. MikroView notices when the lines stop and this row goes back to waiting.</p>
        </div>
      {/if}
    </div>
  {/each}
</div>
<p class="note">
  Admin ▸ Run setup… reopens this ledger any time. To start again from nothing,
  <button type="button" class="linkish" onclick={() => wizardRun.toggleUndoAll()}>
    {wizardRun.showUndoAll ? 'hide the undo lines' : 'undo everything on the router first'}
  </button>.
</p>
{#if wizardRun.showUndoAll}
  <pre>{undoAllText}</pre>
  <p class="note">
    Then
    <button type="button" class="linkish" class:armed disabled={forgetting} onclick={onForgetClick}>
      {#if armed}confirm — the router must enrol again{:else}forget {wizardRun.name} on MikroView — its token, its enrolment and its record{/if}
    </button>
  </p>
  {#if forgetError}
    <p class="note" aria-live="polite">{forgetError}</p>
  {/if}
{/if}
