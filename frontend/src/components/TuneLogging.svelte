<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Tune logging (#435): the config annotation helper. Its own surface,
  // not a sixth wizard step (the issue's decision 2) -- reached from the
  // wizard's finish screen and from the topography's coverage lens on a
  // dark pair, and built to recur (#895 later adds a second way in). The
  // operator uploads their router's `/export hide-sensitive`, and gets
  // it back with logging switched on for every rule that crosses a dark
  // connection -- their exact config, changed only in its logging
  // attributes -- or the equivalent `set` commands to paste individually.
  //
  // The invariant this page exists to keep honest: the upload never
  // leaves the browser except in the two POSTs it drives
  // (fetchTuneLoggingAnalyse/fetchTuneLoggingRender, lib/api.ts). Nothing
  // here writes it to storage, a log, or anywhere that outlives this
  // component -- exportText is plain component state, gone the moment
  // this unmounts, which is what the ephemerality note below states in
  // the operator's own words.
  //
  // Outside the deck deliberately (see App.svelte's DECK_VIEWS): a
  // workflow stepped into and left, not a dashboard to swipe among --
  // the same operate-page shape Fleet.svelte has always used.
  import { appState } from '../lib/state.svelte'
  import { policyState } from '../lib/policy.svelte'
  import { coverageState } from '../lib/coverage.svelte'
  import { tuneLoggingNavState } from '../lib/tuneLoggingNav.svelte'
  import { fetchTuneLoggingAnalyse, fetchTuneLoggingRender } from '../lib/api'
  import { copyToClipboard } from '../lib/clipboard'
  import { downloadText } from '../lib/export'
  import { counterText, darkBoundaryKeys, groupRules, initialSelection, waitingMessage } from '../lib/tuneLogging'
  import type { TuneLoggingAnalyseResponse, TuneLoggingRenderResponse, TuneLoggingRule } from '../lib/types'

  // The pushed tables the dark-boundary set is computed from -- the same
  // two refreshes Topography.svelte runs, so the "dark" this page sends
  // the server never disagrees with what the coverage lens painted.
  $effect(() => {
    policyState.refresh()
    coverageState.refresh()
  })

  let device = $state('')
  // The pair that prompted this visit, from the topography's coverage
  // lens (contract §6: "passes that pair's key so it is pre-selected").
  // Read once on mount and cleared -- see tuneLoggingNav.svelte.ts's own
  // doc comment for why this is a separate slot from topologyNav's.
  let preselectedBoundary = $state<string | null>(null)

  $effect(() => {
    const pending = tuneLoggingNavState.consume()
    if (!pending) return
    device = pending.device
    preselectedBoundary = pending.boundaryKey
  })

  // Device pick is only shown when there is a real choice to make
  // (contract §6: "device pick (if >1)"); the only known device is
  // picked for the operator otherwise.
  $effect(() => {
    if (!device && appState.devices.length === 1) device = appState.devices[0].id
  })

  let exportText = $state('')

  let analysing = $state(false)
  let analyseError = $state<string | null>(null)
  let result = $state<TuneLoggingAnalyseResponse | null>(null)
  let selected = $state<Set<number>>(new Set())
  // The non-dark group starts collapsed (contract §6): "the rest shown
  // collapsed, unticked".
  let showOther = $state(false)

  let rendering = $state(false)
  let renderError = $state<string | null>(null)
  let renderResult = $state<TuneLoggingRenderResponse | null>(null)
  // Whether the rendered result has been downloaded or copied at least
  // once -- what the beforeunload guard below reads. Cleared whenever a
  // fresh render arrives, since that is a new unsaved result.
  let resultSaved = $state(false)
  let copied = $state('')

  // A new or edited export invalidates whatever was derived from the
  // old one -- an analyse result, a render, and the guard around it all
  // describe text that is no longer what is in the box.
  function resetDownstream() {
    result = null
    analyseError = null
    selected = new Set()
    showOther = false
    renderResult = null
    renderError = null
    resultSaved = false
  }

  async function onFile(e: Event) {
    const input = e.currentTarget as HTMLInputElement
    const file = input.files?.[0]
    if (!file) return
    exportText = await file.text()
    resetDownstream()
    // Cleared so choosing the same file twice (after editing it outside
    // the browser) still fires a change event.
    input.value = ''
  }

  // darkBoundaries mirrors Topography.svelte's own coverageOf: logged ->
  // observed, declared -> quiet, neither -> dark. Estate-wide, the same
  // scope the coverage lens itself draws in today.
  const darkBoundaries = $derived(darkBoundaryKeys(policyState.edges, new Set(coverageState.byKey.keys())))

  async function analyse() {
    if (!device || !exportText.trim() || analysing) return
    analysing = true
    analyseError = null
    const res = await fetchTuneLoggingAnalyse({ device, export: exportText, darkBoundaries })
    analysing = false
    if (typeof res === 'string') {
      analyseError = res
      return
    }
    result = res
    selected = initialSelection(res.rules)
  }

  const grouped = $derived(result ? groupRules(result.rules) : { dark: [] as TuneLoggingRule[], other: [] as TuneLoggingRule[] })

  function toggle(id: number) {
    const next = new Set(selected)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    selected = next
  }

  async function render() {
    if (!device || selected.size === 0 || rendering) return
    rendering = true
    renderError = null
    const res = await fetchTuneLoggingRender({ device, export: exportText, selected: [...selected] })
    rendering = false
    if (typeof res === 'string') {
      renderError = res
      return
    }
    renderResult = res
    resultSaved = false
  }

  // Download first, copy second (the record, contract §6): the page's
  // own command snippets overwrite the clipboard, so the file -- which
  // does not -- is the button that comes first.
  function download() {
    if (!renderResult) return
    downloadText(`${device}-logging.rsc`, renderResult.annotated)
    resultSaved = true
  }

  async function copy(text: string, label: string) {
    const ok = await copyToClipboard(text)
    if (ok) {
      copied = label
      setTimeout(() => (copied = ''), 1500)
      resultSaved = true
    }
  }

  function onBeforeUnload(e: BeforeUnloadEvent) {
    e.preventDefault()
    e.returnValue = ''
  }

  // The guard (issue's own invariant list, "Warn on leave"): set only
  // while a rendered result exists that has been neither downloaded nor
  // copied, cleared the instant either happens. No `beforeunload` guard
  // existed anywhere in the frontend before this -- see lib/export.ts's
  // download-a-blob precedent this page's own download() reuses.
  $effect(() => {
    if (!renderResult || resultSaved) return
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  })
</script>

{#snippet ruleRow(r: TuneLoggingRule)}
  {@const ct = result ? counterText(r, result.observing.since) : null}
  <label class="rule-row" class:highlight={preselectedBoundary !== null && r.boundary === preselectedBoundary}>
    <input type="checkbox" checked={selected.has(r.id)} onchange={() => toggle(r.id)} />
    <span class="rule-main">
      <span class="rule-title">
        {r.chain} · {r.action} · {r.inInterface || 'any'} → {r.outInterface || 'any'}{r.comment ? ` — ${r.comment}` : ''}
      </span>
      {#if ct}<span class="rule-counter">{ct}</span>{/if}
    </span>
  </label>
{/snippet}

<div class="page scrollbar op-page">
  <div class="opwrap">
    <div class="opanel">
      <div class="og">
        <h3>tune logging — coverage-complete, from your own export</h3>

        <!-- The ephemerality sentence, verbatim from the issue body. -->
        <p class="note ephemeral">
          Your config is never stored — it runs through memory, and once you leave this page it is gone.
        </p>

        {#if appState.devices.length === 0}
          <p class="empty">No routers known yet — finish setup first, and this fills in on its own.</p>
        {:else}
          {#if appState.devices.length > 1}
            <div class="field">
              <label for="tl-device">Router</label>
              <select id="tl-device" bind:value={device}>
                <option value="" disabled>Which router is this export from?…</option>
                {#each appState.devices as d (d.id)}
                  <option value={d.id}>{d.name && d.name !== d.id ? `${d.name} (${d.id})` : d.id}</option>
                {/each}
              </select>
            </div>
          {/if}

          <div class="field">
            <label for="tl-export">The router's export (<code>/export hide-sensitive</code>)</label>
            <input id="tl-file" type="file" accept=".rsc,.txt,text/plain" onchange={onFile} />
            <textarea
              id="tl-export"
              class="export-box"
              rows="8"
              placeholder="paste the export here, or choose a file above…"
              bind:value={exportText}
              oninput={resetDownstream}
            ></textarea>
          </div>

          {#if analyseError}<p class="load-error">{analyseError}</p>{/if}

          <button
            type="button"
            class="primary"
            onclick={analyse}
            disabled={!device || !exportText.trim() || analysing}
          >
            {analysing ? 'Analysing…' : 'Analyse'}
          </button>

          {#if result?.rejected}
            <p class="load-error">{result.rejected.reason}</p>
          {:else if result && !result.ready}
            <p class="observation waiting">
              <span class="dot" aria-hidden="true"></span>
              {waitingMessage(result.observing.hours)}
            </p>
          {:else if result}
            <div class="rules">
              <h4>crosses a dark connection — ticked by default</h4>
              {#if grouped.dark.length === 0}
                <p class="note">No forward rule crosses a dark connection here.</p>
              {/if}
              {#each grouped.dark as r (r.id)}
                {@render ruleRow(r)}
              {/each}

              {#if grouped.other.length > 0}
                <button type="button" class="ghost" onclick={() => (showOther = !showOther)}>
                  {showOther ? 'Hide' : 'Show'} the other {grouped.other.length} rule{grouped.other.length === 1
                    ? ''
                    : 's'}
                </button>
                {#if showOther}
                  {#each grouped.other as r (r.id)}
                    {@render ruleRow(r)}
                  {/each}
                {/if}
              {/if}
            </div>

            {#if renderError}<p class="load-error">{renderError}</p>{/if}
            <button type="button" class="primary" onclick={render} disabled={rendering || selected.size === 0}>
              {rendering ? 'Rendering…' : `Render (${selected.size} selected)`}
            </button>

            {#if renderResult}
              {@const rr = renderResult}
              <div class="render-result">
                <p class="note">
                  {rr.changed} rule{rr.changed === 1 ? '' : 's'} changed.
                </p>
                <!-- Download first, copy second -- the page's own copy
                     buttons overwrite the clipboard, so the file (which
                     does not) is the one to reach for first. -->
                <button type="button" class="primary" onclick={download}>
                  Download {device}-logging.rsc
                </button>
                <button type="button" onclick={() => copy(rr.annotated, 'annotated')}>
                  {copied === 'annotated' ? 'Copied' : 'Copy the annotated export'}
                </button>
                <pre>{rr.commands}</pre>
                <button type="button" onclick={() => copy(rr.commands, 'commands')}>
                  {copied === 'commands' ? 'Copied' : 'Copy'}
                </button>
              </div>
            {/if}
          {/if}
        {/if}
      </div>
    </div>
  </div>
</div>

<style>
  /* The deck's operate-page frame (Fleet.svelte's own fields). */
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px 16px 24px;
  }

  .op-page .opwrap {
    display: flex;
    justify-content: center;
  }

  .op-page .opanel {
    width: 100%;
    max-width: 900px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .og h3 {
    margin: 0 0 6px;
    font-size: 10px;
    font-weight: 650;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .og {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .note {
    margin: 0;
    font-size: 12.5px;
    line-height: 1.6;
    color: var(--fg-muted);
  }

  /* The wizard's own amber-left-rule register (SetupWizard.svelte's
     .note.below-minimum), reused here for the ephemerality statement --
     the loudest thing on this page, without a second colour. */
  .note.ephemeral {
    border-left: 3px solid var(--log);
    padding-left: 10px;
    color: var(--fg);
  }

  .empty {
    color: var(--fg-dim);
    font-size: 13px;
    padding: 10px 0;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .field label {
    font-size: 12.5px;
    color: var(--fg-muted);
  }

  select {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 7px 10px;
    font-size: 13px;
    align-self: flex-start;
  }

  .export-box {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 6px;
    padding: 10px 12px;
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.5;
    resize: vertical;
  }

  code {
    font-size: 11.5px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 3px;
    padding: 1px 4px;
  }

  button {
    align-self: flex-start;
    border-radius: 5px;
    padding: 7px 13px;
    font-size: 13px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  button:hover:not(:disabled) {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  button.primary {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--bg);
    font-weight: 600;
  }

  button:disabled {
    opacity: 0.5;
  }

  .load-error {
    margin: 0;
    color: var(--reject);
    font-size: 13px;
  }

  .observation {
    margin: 0;
    align-self: stretch;
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    line-height: 1.5;
    padding: 9px 12px;
    border-radius: 6px;
    border: 1px dashed var(--border);
    color: var(--fg-muted);
  }

  .dot {
    width: 7px;
    height: 7px;
    flex: none;
    border-radius: 50%;
    background: var(--fg-muted);
    animation: pulse 1.8s ease-in-out infinite;
  }

  @keyframes pulse {
    0%,
    100% {
      opacity: 0.35;
    }
    50% {
      opacity: 1;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .dot {
      animation: none;
      opacity: 0.7;
    }
  }

  .rules {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .rules h4 {
    margin: 0;
    font-size: 11px;
    font-weight: 650;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .rule-row {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 8px 10px;
    cursor: pointer;
  }

  .rule-row.highlight {
    border-color: var(--accent);
  }

  .rule-row input {
    margin-top: 2px;
  }

  .rule-main {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
  }

  .rule-title {
    font-size: 13px;
    color: var(--fg);
  }

  .rule-counter {
    font-size: 11.5px;
    color: var(--fg-muted);
    font-family: var(--font-mono);
  }

  pre {
    margin: 0;
    align-self: stretch;
    padding: 12px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 6px;
    font-size: 12.5px;
    line-height: 1.6;
    overflow-x: auto;
    white-space: pre;
    color: var(--fg);
    user-select: all;
  }

  .render-result {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
    border-top: 1px solid var(--border);
    padding-top: 12px;
  }
</style>
