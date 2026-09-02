<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // #640's ledger: every expectation this deployment has recorded --
  // "this much of this, from this host, is normal here".
  //
  // It sits on the watchers station under the bench, which is the
  // owner's placement decision on #640 (2026-09-02): the detectors
  // above, what they have been told to expect below. The two halves
  // answer the same question from opposite ends -- what is being
  // watched, and what has been carved out of it -- and reading one
  // without the other tells you only half of why a flag is or is not
  // on the card.
  //
  // Every row is also an argument for its own existence: the absorbed
  // count is how many firings it has suppressed, so an expectation
  // that has absorbed nothing in months is visibly not earning its
  // place and can be forgotten from here.
  import { fetchExpectations, forgetExpectation } from '../lib/api'
  import { detectorSettingsState } from '../lib/detectorSettings.svelte'
  import { formatDayMonth } from '../lib/format'
  import type { Exclusion } from '../lib/types'

  // canEdit is the station's own tier gate (#653), passed straight
  // through: a viewer reads the ledger -- an expectation is the reason
  // a firing it might have seen is absent -- but has no Forget button
  // at all. Hidden, never disabled, the grammar the bench's run/pause
  // tick set.
  let { canEdit }: { canEdit: boolean } = $props()

  let rows = $state<Exclusion[]>([])
  let loadError = $state<string | null>(null)
  // Keyed by expectation id rather than held as one message for the
  // section: a refusal belongs beside the row whose button produced
  // it, and a single shared slot would attribute the second row's
  // failure to the first.
  let rowError = $state<Record<string, string>>({})
  let forgetting = $state<Record<string, boolean>>({})

  async function load() {
    try {
      rows = await fetchExpectations()
      loadError = null
    } catch (e) {
      // The list is the whole section, so a failure to read it is
      // stated rather than shown as an empty ledger -- "nothing is
      // expected here" and "we could not ask" are opposite facts and
      // must not render the same.
      loadError = e instanceof Error ? e.message : String(e)
    }
  }

  // Once on mount, and again after every successful forget. The
  // refetch is not cosmetic: Absorbed counts move whenever traffic
  // does, so the list the operator is looking at after pruning one row
  // should be the server's, not the client's guess at it.
  $effect(() => {
    void load()
  })

  async function forget(id: string) {
    forgetting[id] = true
    const failure = await forgetExpectation(id)
    forgetting[id] = false
    if (failure) {
      rowError[id] = failure
      return
    }
    rowError[id] = ''
    await load()
  }

  // The detector's display name where the definitions list has one --
  // an expectation's type is the definition id (flagID's own first
  // half), so a row reads "Port scan" rather than "port_scan" as soon
  // as the bench above it has loaded. Falls back to the id, which is
  // still a true answer, rather than blanking the column.
  function detectorName(type: string): string {
    return detectorSettingsState.list.find((d) => d.name === type)?.label ?? type
  }

  // "any size" is not a formatting nicety: a detector that declares no
  // size records a size-less expectation, which absorbs that host on
  // that detector outright. Rendering an absent size as "up to 0"
  // would say the exact opposite of what the entry does.
  function sizeFact(e: Exclusion): string {
    return e.size === undefined || e.size === null ? 'any size' : `up to ${e.size}`
  }
</script>

<section class="expectations">
  <h3>What it has been told to expect</h3>

  {#if loadError}
    <p class="error">{loadError}</p>
  {:else if rows.length === 0}
    <p class="empty">Nothing yet — every Expected verdict on the Flags card records one here.</p>
  {:else}
    <ul class="rows">
      {#each rows as e (e.id)}
        <li class="erow">
          <span class="line">
            <span class="name">{detectorName(e.type)}</span>
            <span class="dot">·</span>
            <span class="target">{e.target}</span>
            <span class="dot">·</span>
            <span class="fact">{sizeFact(e)}</span>
            <span class="dot">·</span>
            <span class="fact">absorbed {e.absorbed ?? 0}</span>
            {#if e.since}
              <span class="dot">·</span>
              <span class="fact">since {formatDayMonth(e.since)}</span>
            {/if}
            {#if canEdit}
              <button
                type="button"
                class="forget"
                disabled={forgetting[e.id]}
                aria-label="Forget the expectation for {e.target}"
                onclick={() => forget(e.id)}
              >
                {forgetting[e.id] ? 'forgetting…' : 'Forget'}
              </button>
            {/if}
          </span>
          {#if rowError[e.id]}
            <p class="error">{rowError[e.id]}</p>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  /* The same top rule and rhythm the bench above uses, so the ledger
     reads as the station's next section rather than a panel dropped
     onto it. */
  .expectations {
    margin-top: 12px;
    padding-top: 8px;
    border-top: 1px solid var(--border);
  }

  h3 {
    margin: 0 0 6px;
  }

  .empty {
    margin: 0;
    font-size: 12px;
    color: var(--fg-muted);
  }

  .rows {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .erow {
    font-size: 12px;
  }

  .line {
    display: flex;
    align-items: baseline;
    gap: 6px;
    flex-wrap: wrap;
  }

  .name {
    color: var(--fg);
    font-weight: 600;
  }

  .target {
    font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
    font-size: 10.5px;
    color: var(--fg-dim);
  }

  .dot,
  .fact {
    color: var(--fg-muted);
  }

  /* Same ink as the bench's own quiet actions: forgetting an
     expectation only ever re-arms detection, so it is not a
     destructive control and is not dressed as one. */
  .forget {
    background: transparent;
    border: 1px solid transparent;
    border-radius: 5px;
    padding: 0 6px;
    font-size: 11.5px;
    color: var(--fg-muted);
  }

  .forget:hover:not(:disabled) {
    color: var(--accent);
  }

  .forget:disabled {
    opacity: 0.6;
    cursor: default;
  }

  .error {
    margin: 4px 0 0;
    color: var(--reject);
    font-size: 11.5px;
  }
</style>
