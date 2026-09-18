<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Log every rule (#435, renamed from "Tune logging" by #1134): the
  // config annotation helper. Its own surface, not a sixth wizard step
  // (the issue's decision 2) -- reached from the wizard's finish screen
  // and from the topography's coverage lens on a dark pair, and built to
  // recur (#895 later adds a second way in). The operator hands over
  // their router's `/export hide-sensitive`, and gets it back with
  // logging switched on for every rule that crosses a dark connection --
  // their exact config, changed only in its logging attributes -- or the
  // equivalent `set` commands to paste individually.
  //
  // #1134 is the owner's ruling on three faults found in v0.5.1, and it
  // is what this file's shape now answers to:
  //
  // - A page nobody could leave. It rendered outside the deck, whose
  //   roll rail is the app's navigation, so it had none. It is a deck
  //   card now (lib/deckCards.ts's `log-every-rule`), taking the deck's
  //   shell exactly as Entities and Settings do -- not a second shell
  //   invented for it. The old "workflow you step into and leave"
  //   reading is superseded: a workflow with no way out is a trap.
  // - A raw <input type="file"> beside a bare textarea. One drop zone
  //   now: drop a file on it, click it to browse, or paste -- the same
  //   control for all three, with the native file control kept but never
  //   shown.
  // - It never said what it was for. The lead sentence, verbatim from
  //   the ruling, is the first thing on the page, and the never-stored
  //   line is a footnote under the drop zone rather than the headline.
  //
  // The invariant this page exists to keep honest: the export never
  // leaves the browser except in the two POSTs it drives
  // (fetchTuneLoggingAnalyse/fetchTuneLoggingRender, lib/api.ts). Nothing
  // here writes it to storage or a log -- the pasted export and whatever
  // Analyse/Render made of it live in lib/logEveryRuleWork.svelte.ts, a
  // module-lifetime store rather than this component's own state, so
  // scrolling the deck away and back (Deck.svelte unmounts an
  // off-screen card's scene) does not throw the operator's work away.
  // It is still gone the moment the tab closes or reloads, which is
  // what the ephemerality note below states in the operator's own
  // words.
  //
  // The `TuneLogging*` names it imports keep theirs: they mirror the two
  // /api/tune-logging endpoints, and #1134 leaves those paths alone.
  import { appState } from '../lib/state.svelte'
  import { policyState } from '../lib/policy.svelte'
  import { coverageState } from '../lib/coverage.svelte'
  import { logEveryRuleNavState } from '../lib/logEveryRuleNav.svelte'
  import { logEveryRuleWorkState as work } from '../lib/logEveryRuleWork.svelte'
  import { fetchTuneLoggingAnalyse, fetchTuneLoggingRender } from '../lib/api'
  import { copyToClipboard } from '../lib/clipboard'
  import { downloadText } from '../lib/export'
  import {
    countFilterRules,
    counterText,
    darkBoundaryKeys,
    everyPacketNote,
    exportProblem,
    groupRules,
    initialSelection,
    waitingMessage,
  } from '../lib/logEveryRule'
  import type { TuneLoggingRule } from '../lib/types'

  // The pushed tables the dark-boundary set is computed from -- the same
  // two refreshes Topography.svelte runs, so the "dark" this page sends
  // the server never disagrees with what the coverage lens painted.
  $effect(() => {
    policyState.refresh()
    coverageState.refresh()
  })

  // The pair that prompted this visit, from the topography's coverage
  // lens (contract §6: "passes that pair's key so it is pre-selected").
  // Read once on mount and cleared -- see tuneLoggingNav.svelte.ts's own
  // doc comment for why this is a separate slot from topologyNav's.
  let preselectedBoundary = $state<string | null>(null)

  $effect(() => {
    const pending = logEveryRuleNavState.consume()
    if (!pending) return
    // Arriving for a different router than the one whose work is still
    // held. That work outlives this component now (see
    // logEveryRuleWork.svelte.ts), so without this the operator would
    // see the previous router's pasted export, and the rules analysed
    // from it, sitting under the new router's name -- and Render would
    // pair this device with that export, producing an .rsc built from
    // the wrong router's config. Same device, different boundary, is a
    // second look at work in progress and is kept.
    if (pending.device !== work.device) work.reset()
    work.device = pending.device
    preselectedBoundary = pending.boundaryKey
  })

  // Device pick is only shown when there is a real choice to make
  // (contract §6: "device pick (if >1)"); the only known device is
  // picked for the operator otherwise.
  $effect(() => {
    if (!work.device && appState.devices.length === 1) work.device = appState.devices[0].id
  })

  let dragging = $state(false)
  let fileInput = $state<HTMLInputElement | null>(null)
  const ruleCount = $derived(work.exportText ? countFilterRules(work.exportText) : 0)
  // #1186: what is wrong with the text in the zone, said here rather
  // than left for Analyse to answer with the under-24h waiting line.
  const problem = $derived(exportProblem(work.exportText))

  let analysing = $state(false)
  let analyseError = $state<string | null>(null)

  let rendering = $state(false)
  let renderError = $state<string | null>(null)
  let copied = $state('')

  // The one intake. Every door into the drop zone -- a dropped file, a
  // chosen file, a paste -- ends here, so a second export always lands
  // in exactly the state the first one did.
  function take(text: string, name: string) {
    work.exportText = text
    work.exportName = name
    work.resetDownstream()
    analyseError = null
    renderError = null
  }

  async function onFile(e: Event) {
    const input = e.currentTarget as HTMLInputElement
    const file = input.files?.[0]
    if (!file) return
    take(await file.text(), file.name)
    // Cleared so choosing the same file twice (after editing it outside
    // the browser) still fires a change event.
    input.value = ''
  }

  async function onDrop(e: DragEvent) {
    e.preventDefault()
    dragging = false
    const file = e.dataTransfer?.files?.[0]
    if (file) {
      take(await file.text(), file.name)
      return
    }
    // Dragging a selection rather than a file: the text is the export
    // just as much as a file's contents are.
    const text = e.dataTransfer?.getData('text/plain')
    if (text) take(text, 'dragged text')
  }

  // Paste is listened for on the window, not on the zone: the zone's
  // click opens the file browser, so there is no way to focus it first
  // and "paste into it" the way an input would allow. Anything already
  // typing into a field of its own keeps its own paste -- including a
  // neighbouring deck card's, since the deck mounts more than the card
  // you are on.
  function onPaste(e: ClipboardEvent) {
    const target = e.target as HTMLElement | null
    const tag = target?.tagName
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target?.isContentEditable) return
    const text = e.clipboardData?.getData('text/plain')
    if (!text?.trim()) return
    e.preventDefault()
    take(text, 'pasted export')
  }

  $effect(() => {
    window.addEventListener('paste', onPaste)
    return () => window.removeEventListener('paste', onPaste)
  })

  // darkBoundaries mirrors Topography.svelte's own coverageOf: logged ->
  // observed, declared -> quiet, neither -> dark. Estate-wide, the same
  // scope the coverage lens itself draws in today.
  const darkBoundaries = $derived(darkBoundaryKeys(policyState.edges, new Set(coverageState.byKey.keys())))

  async function analyse() {
    if (!work.device || !work.exportText.trim() || problem || analysing) return
    analysing = true
    analyseError = null
    const res = await fetchTuneLoggingAnalyse({ device: work.device, export: work.exportText, darkBoundaries })
    analysing = false
    if (typeof res === 'string') {
      analyseError = res
      return
    }
    work.result = res
    work.selected = initialSelection(res.rules)
  }

  const grouped = $derived(work.result ? groupRules(work.result.rules) : { dark: [] as TuneLoggingRule[], other: [] as TuneLoggingRule[] })

  function toggle(id: number) {
    const next = new Set(work.selected)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    work.selected = next
  }

  async function render() {
    if (!work.device || work.selected.size === 0 || rendering) return
    rendering = true
    renderError = null
    const res = await fetchTuneLoggingRender({ device: work.device, export: work.exportText, selected: [...work.selected] })
    rendering = false
    if (typeof res === 'string') {
      renderError = res
      return
    }
    work.renderResult = res
    work.resultSaved = false
  }

  // Download first, copy second (the record, contract §6): the page's
  // own command snippets overwrite the clipboard, so the file -- which
  // does not -- is the button that comes first.
  function download() {
    if (!work.renderResult) return
    downloadText(`${work.device}-logging.rsc`, work.renderResult.annotated)
    work.resultSaved = true
  }

  async function copy(text: string, label: string) {
    const ok = await copyToClipboard(text)
    if (ok) {
      copied = label
      setTimeout(() => (copied = ''), 1500)
      work.resultSaved = true
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
    if (!work.renderResult || work.resultSaved) return
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  })
</script>

{#snippet ruleRow(r: TuneLoggingRule)}
  {@const ct = work.result ? counterText(r, work.result.observing.since) : null}
  {@const epn = everyPacketNote(r)}
  <label class="rule-row" class:highlight={preselectedBoundary !== null && r.boundary === preselectedBoundary}>
    <input type="checkbox" checked={work.selected.has(r.id)} onchange={() => toggle(r.id)} />
    <span class="rule-main">
      <span class="rule-title">
        {r.chain} · {r.action} · {r.inInterface || 'any'} → {r.outInterface || 'any'}{r.comment ? ` — ${r.comment}` : ''}
      </span>
      <!-- #1230: said beside the rule rather than only in the docs, and
           this rule starts unticked whatever its boundary says. -->
      {#if epn}<span class="rule-warning">{epn}</span>{/if}
      {#if ct}<span class="rule-counter">{ct}</span>{/if}
    </span>
  </label>
{/snippet}

<div class="page scrollbar op-page">
  <div class="opwrap">
    <div class="opanel">
      <div class="og">
        <h3>log every rule</h3>

        <!-- The lead sentence, verbatim from #1134's ruling, and the
             first thing on the page: what you put in, what you get
             back, and what happens to it. -->
        <p class="lead">
          Drop in your router's export (<code>/export hide-sensitive</code>). You get it back with logging switched on
          for every firewall rule that is not logging yet, ready to paste into the router. Nothing you paste is stored.
        </p>

        <!-- #1186: the 24-hour gate, said before the operator hands
             anything over. It used to appear only after Analyse, as
             "watching for 0 hours" -- which arrives as a refusal, in a
             place where the operator has already done the work. -->
        <p class="note">
          Which rules are worth logging is worked out from 24 hours of watching the router. Until
          that has passed, this page can only say how far along it is.
        </p>

        {#if appState.devices.length === 0}
          <p class="empty">No routers known yet — finish setup first, and this fills in on its own.</p>
        {:else}
          {#if appState.devices.length > 1}
            <div class="field">
              <label for="ler-device">Router</label>
              <select id="ler-device" bind:value={work.device}>
                <option value="" disabled>Which router is this export from?…</option>
                {#each appState.devices as d (d.id)}
                  <option value={d.id}>{d.name && d.name !== d.id ? `${d.name} (${d.id})` : d.id}</option>
                {/each}
              </select>
            </div>
          {/if}

          <!-- One control for all three ways in (#1134). The native
               file control is kept -- it is the only way to open the
               browser's own file chooser -- but never shown; the zone
               clicks it. -->
          <div class="field">
            <button
              type="button"
              class="drop"
              class:dragging
              class:filled={work.exportText.trim().length > 0}
              onclick={() => fileInput?.click()}
              ondragenter={(e) => {
                e.preventDefault()
                dragging = true
              }}
              ondragover={(e) => {
                e.preventDefault()
                dragging = true
              }}
              ondragleave={() => (dragging = false)}
              ondrop={onDrop}
            >
              {#if work.exportText.trim()}
                <span class="drop-picked">{work.exportName}</span>
                <span class="drop-sub">
                  {ruleCount} firewall rule{ruleCount === 1 ? '' : 's'} in it — drop, click or paste another to replace
                  it
                </span>
              {:else}
                <span class="drop-picked">Drop the export here</span>
                <span class="drop-sub">or click to choose a file, or paste it</span>
              {/if}
            </button>
            <input
              bind:this={fileInput}
              id="ler-file"
              class="sr-only"
              type="file"
              accept=".rsc,.txt,text/plain"
              tabindex="-1"
              aria-hidden="true"
              onchange={onFile}
            />

            <!-- #1186: text that cannot be an export is said so here,
                 under the zone that took it, rather than after a round
                 trip that could only answer with "watching for 0
                 hours". The Analyse button below is held until it is
                 sorted -- there is nothing for the server to find in
                 text with no firewall rules in it. -->
            {#if problem}
              <p class="load-error">{problem}</p>
            {/if}

            <!-- The ephemerality sentence, verbatim from #435's issue
                 body. #1134 moved it here, under the zone, as the
                 footnote it always was rather than the headline.
                 #895 reworded it once scheduled backups existed: "never
                 stored" was true of this helper and only ever meant
                 this helper, and leaving it at that would have read as
                 a promise about a backup that is stored. So the
                 sentence now says which of the two it is about, and
                 names the other. -->
            <p class="note ephemeral">
              This helper stores nothing you paste — it runs through memory, and once you leave this page it is
              gone. Scheduled backups are a different thing: those are kept, with their secrets removed as they
              arrive, and you can annotate one from here too.
            </p>
          </div>

          {#if analyseError}<p class="load-error">{analyseError}</p>{/if}

          <button
            type="button"
            class="primary"
            onclick={analyse}
            disabled={!work.device || !work.exportText.trim() || problem !== null || analysing}
          >
            {analysing ? 'Analysing…' : 'Analyse'}
          </button>

          {#if work.result?.rejected}
            <p class="load-error">{work.result.rejected.reason}</p>
          {:else if work.result && !work.result.ready}
            <p class="observation waiting">
              <span class="dot" aria-hidden="true"></span>
              {waitingMessage(work.result.observing.hours)}
            </p>
          {:else if work.result}
            <div class="rules">
              <h4>crosses a dark connection — ticked by default</h4>
              {#if grouped.dark.length === 0}
                <p class="note">No forward rule crosses a dark connection here.</p>
              {/if}
              {#each grouped.dark as r (r.id)}
                {@render ruleRow(r)}
              {/each}

              {#if grouped.other.length > 0}
                <button type="button" class="ghost" onclick={() => (work.showOther = !work.showOther)}>
                  {work.showOther ? 'Hide' : 'Show'} the other {grouped.other.length} rule{grouped.other.length === 1
                    ? ''
                    : 's'}
                </button>
                {#if work.showOther}
                  {#each grouped.other as r (r.id)}
                    {@render ruleRow(r)}
                  {/each}
                {/if}
              {/if}
            </div>

            {#if renderError}<p class="load-error">{renderError}</p>{/if}
            <button type="button" class="primary" onclick={render} disabled={rendering || work.selected.size === 0}>
              {rendering ? 'Rendering…' : `Render (${work.selected.size} selected)`}
            </button>

            {#if work.renderResult}
              {@const rr = work.renderResult}
              <div class="render-result">
                <p class="note">
                  {rr.changed} rule{rr.changed === 1 ? '' : 's'} changed.
                </p>
                <!-- Download first, copy second -- the page's own copy
                     buttons overwrite the clipboard, so the file (which
                     does not) is the one to reach for first. -->
                <button type="button" class="primary" onclick={download}>
                  Download {work.device}-logging.rsc
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

  /* The lead sentence: page ink, a size up from the notes around it,
     because it is what the page is for. */
  .lead {
    margin: 0;
    max-width: 62ch;
    font-size: 14px;
    line-height: 1.65;
    color: var(--fg);
  }

  /* A footnote since #1134, not the headline it used to be: the quiet
     register of every other .note, one size down again. */
  .note.ephemeral {
    font-size: 12px;
    color: var(--fg-dim);
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

  /* The drop zone (#1134). The dashed edge is the app's own
     "something belongs here and does not yet" mark -- Entities' empty
     third slot and AddTopTalkerWidget's berth wear the same one -- on
     the elevated surface every other input here sits on. It is a real
     <button> so the keyboard reaches it, Enter opens the file browser
     and the a11y rules are satisfied without a role attribute. */
  .drop {
    align-self: stretch;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 5px;
    min-height: 112px;
    padding: 18px 16px;
    border: 1px dashed var(--border);
    border-radius: 8px;
    background: var(--bg-elevated);
    cursor: pointer;
    text-align: center;
    transition:
      border-color 0.15s,
      color 0.15s;
  }

  /* :not(:disabled) only to outweigh the generic button:hover rule
     below, which would otherwise win on specificity and grey the edge
     back down. */
  .drop:hover:not(:disabled),
  .drop.dragging:not(:disabled) {
    border-color: var(--accent);
  }

  /* Solid once it is holding something: the border stops asking. */
  .drop.filled:not(:disabled) {
    border-style: solid;
    border-color: var(--accent);
  }

  .drop-picked {
    font-size: 13.5px;
    font-weight: 600;
    color: var(--fg);
  }

  .drop.filled .drop-picked {
    font-family: var(--font-mono);
    font-weight: 500;
  }

  .drop-sub {
    font-size: 12px;
    color: var(--fg-muted);
  }

  /* The native file control, kept for the browser's own chooser and
     never shown (#1134). Not display:none: a hidden-by-display input
     cannot be clicked open in every engine. */
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
    border: 0;
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

  /* #1230: the one line on this page that is a caution rather than a
     fact about the rule, so it is the only one that takes --warn. */
  .rule-warning {
    font-size: 11.5px;
    color: var(--warn);
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
