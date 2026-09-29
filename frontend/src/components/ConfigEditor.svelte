<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // #1347: the config editor, full-screen -- a document, like the setup
  // wizard, mounted by App.svelte over the shell (or in place of it in
  // setup-only mode). The design is the issue's "Current plan": the text
  // on the left in a plain textarea with a line-number gutter, the
  // Problems rail on the right fed by POST /api/config/validate each time
  // typing pauses, and a top bar with Carry forward, Show/Hide secrets,
  // Snapshot, Download and Close. The result is always a download;
  // MikroView never writes the config file.
  //
  // No editor library, deliberately: a textarea with wrapping off keeps
  // one text line to one screen line, which is all the gutter needs.
  import { configEditorState } from '../lib/configEditor.svelte'
  import { authState } from '../lib/auth.svelte'
  import { trapFocus } from '../lib/focusTrap'
  import PasswordAgain from './PasswordAgain.svelte'

  interface Props {
    /** The config was refused at start-up: this is the only screen, and there is no shell to close back to. */
    setupOnly?: boolean
  }

  let { setupOnly = false }: Props = $props()

  const ed = configEditorState
  const file = $derived(ed.file)
  const opened = $derived(ed.visible && !!file)

  let textarea = $state<HTMLTextAreaElement | null>(null)
  let gutter = $state<HTMLDivElement | null>(null)

  const lineNumbers = $derived(Array.from({ length: ed.text.split('\n').length }, (_, i) => i + 1))
  // The worst severity on each line, for the gutter's marks.
  const marks = $derived.by(() => {
    const m = new Map<number, string>()
    for (const p of ed.problems) {
      if (m.get(p.line) !== 'error') m.set(p.line, p.severity === 'fatal' ? 'error' : 'warning')
    }
    return m
  })
  const errorCount = $derived(ed.problems.filter((p) => p.severity === 'fatal').length)

  // --- the in-editor questions: at most one bar at a time ------------------

  interface Pending {
    message: string
    confirmLabel: string
    run: () => void
  }

  let pending = $state<Pending | null>(null)
  let askingGuess = $state(false)
  let noteOpen = $state(false)
  let note = $state('')
  let armedDelete = $state<string | null>(null)

  function clearBars() {
    pending = null
    askingGuess = false
    noteOpen = false
  }

  // --- the text and its gutter ------------------------------------------

  function onInput(e: Event) {
    ed.setText((e.currentTarget as HTMLTextAreaElement).value)
  }

  function syncGutter() {
    if (gutter && textarea) gutter.scrollTop = textarea.scrollTop
  }

  function lineHeight(el: HTMLElement): number {
    const lh = parseFloat(getComputedStyle(el).lineHeight)
    return Number.isFinite(lh) && lh > 0 ? lh : 20
  }

  /** Moves the caret to the start of a line and scrolls it into view. */
  function goToLine(line: number) {
    if (!textarea || !(line > 0)) return
    const lines = ed.text.split('\n')
    let offset = 0
    for (let i = 0; i < line - 1 && i < lines.length; i++) offset += lines[i].length + 1
    textarea.focus()
    textarea.setSelectionRange(offset, offset)
    textarea.scrollTop = Math.max(0, (line - 1) * lineHeight(textarea) - textarea.clientHeight / 3)
    syncGutter()
  }

  // --- the top bar --------------------------------------------------------

  function onCarryForward() {
    clearBars()
    if (ed.needsGuessConfirm) {
      askingGuess = true
      return
    }
    void ed.carryForward()
  }

  function confirmGuess() {
    askingGuess = false
    void ed.carryForward()
  }

  function onSecrets() {
    if (ed.revealed) ed.hideSecrets()
    else void ed.showSecrets()
  }

  function onSnapshot() {
    clearBars()
    note = ''
    noteOpen = true
  }

  async function keepSnapshot(e: Event) {
    e.preventDefault()
    if (await ed.takeSnapshot(note)) {
      noteOpen = false
      note = ''
    }
  }

  function onDownload() {
    if (!opened || ed.downloading) return
    void ed.download()
  }

  const UNSAVED = "Your changes haven't been downloaded or kept as a snapshot."

  function requestClose() {
    if (setupOnly) return
    if (ed.edited) {
      clearBars()
      pending = {
        message: `${UNSAVED} Close the editor and lose them?`,
        confirmLabel: 'Close without saving',
        run: () => ed.close(),
      }
      return
    }
    ed.close()
  }

  // --- snapshots ----------------------------------------------------------

  function whyLabel(why: string): string {
    if (why === 'before-carry-forward') return 'before Carry forward'
    if (why === 'manual') return 'kept by hand'
    return why
  }

  function whenLabel(iso: string): string {
    const t = new Date(iso)
    return Number.isNaN(t.getTime()) ? iso : t.toLocaleString()
  }

  function onLoadSnapshot(id: string) {
    clearBars()
    if (ed.edited) {
      pending = {
        message: `${UNSAVED} Loading this snapshot replaces the text in the editor. Load it anyway?`,
        confirmLabel: 'Load snapshot',
        run: () => void ed.loadSnapshot(id),
      }
      return
    }
    void ed.loadSnapshot(id)
  }

  // Round 28's arm-then-confirm (EngineRoom.svelte's remove/reset): the
  // first click arms, a second on the same button deletes, and a click
  // anywhere else disarms.
  function onDeleteClick(e: MouseEvent, id: string) {
    e.stopPropagation()
    if (armedDelete === id) {
      armedDelete = null
      void ed.deleteSnapshot(id)
      return
    }
    armedDelete = id
  }

  // --- keys ---------------------------------------------------------------

  function onKeydown(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === 's') {
      e.preventDefault()
      onDownload()
      return
    }
    if (e.key !== 'Escape') return
    if (pending || askingGuess || noteOpen) {
      e.preventDefault()
      clearBars()
      return
    }
    if (ed.passwordFor && opened) {
      e.preventDefault()
      ed.cancelPassword()
      return
    }
    if (!setupOnly) {
      e.preventDefault()
      requestClose()
    }
  }

  $effect(() => {
    if (setupOnly && !ed.file && !ed.passwordFor) ed.askToOpen()
  })
</script>

<svelte:window onkeydown={onKeydown} onclick={() => (armedDelete = null)} />

<div
  class="ce"
  role="dialog"
  aria-modal="true"
  aria-labelledby="config-editor-title"
  tabindex="-1"
  use:trapFocus
>
  {#if setupOnly}
    <p class="banner" role="status">
      MikroView started in setup-only mode because the config file was refused. Fix it here, download it, put it in
      place and restart.
    </p>
  {/if}

  <header class="bar">
    <div class="title">
      <h2 id="config-editor-title">Config editor</h2>
      {#if file}
        <span class="facts">
          <code>{file.path}</code>
          ·
          {#if file.header}
            schema {file.header.schema}, written by {file.header.writtenBy}
          {:else}
            no header — written before v0.7 · looks like schema {file.schemaGuess}
          {/if}
          · this MikroView is {file.runningVersion}, schema {file.runningSchema}
        </span>
      {/if}
      {#if opened && ed.changedSinceStart}
        <span class="changed">The file has changed on disk since MikroView started; this is the file as it is now.</span>
      {/if}
    </div>
    <div class="actions">
      {#if opened}
        <button type="button" onclick={onCarryForward} disabled={ed.carrying}>
          {ed.carrying ? 'Carrying forward…' : 'Carry forward'}
        </button>
        <button type="button" onclick={onSecrets} disabled={ed.revealing}>
          {ed.revealed ? 'Hide secrets' : ed.revealing ? 'Showing…' : 'Show secrets'}
        </button>
        <button type="button" onclick={onSnapshot} disabled={ed.snapshotting}>Snapshot</button>
        <button type="button" class="primary" onclick={onDownload} disabled={ed.downloading}>
          {ed.downloading ? 'Downloading…' : 'Download'}
        </button>
      {/if}
      {#if setupOnly}
        <button type="button" onclick={() => void authState.logout()}>Sign out</button>
      {:else}
        <button type="button" onclick={requestClose}>Close</button>
      {/if}
    </div>
  </header>

  {#if ed.passwordFor && opened}
    <div class="strip">
      <PasswordAgain
        prompt={ed.passwordFor === 'reveal'
          ? 'It has been more than 15 minutes since you entered your password. Enter it again to show the secrets.'
          : 'It has been more than 15 minutes since you entered your password. Enter it again to download.'}
        submitLabel="Continue"
        busy={ed.unlocking}
        error={ed.passwordError}
        onsubmit={(pw) => ed.submitPassword(pw)}
        oncancel={() => ed.cancelPassword()}
      />
    </div>
  {/if}

  {#if askingGuess && file}
    <div class="strip ask" role="alertdialog" aria-label="Confirm the guessed schema">
      <p>
        This file has no header, so MikroView guessed which settings it was written for: it looks like schema
        {file.schemaGuess}. Carry it forward from schema {file.schemaGuess}?
      </p>
      <button type="button" class="primary" onclick={confirmGuess}>Carry forward from schema {file.schemaGuess}</button>
      <button type="button" onclick={() => (askingGuess = false)}>Cancel</button>
    </div>
  {/if}

  {#if noteOpen}
    <form class="strip ask" onsubmit={keepSnapshot}>
      <p>Keep a snapshot of the text as it is now. MikroView keeps the last five.</p>
      <label>
        <span>Note (optional)</span>
        <!-- svelte-ignore a11y_autofocus -->
        <input type="text" bind:value={note} maxlength="200" autofocus />
      </label>
      <button type="submit" class="primary" disabled={ed.snapshotting}>
        {ed.snapshotting ? 'Keeping…' : 'Keep snapshot'}
      </button>
      <button type="button" onclick={() => (noteOpen = false)}>Cancel</button>
    </form>
  {/if}

  {#if pending}
    <div class="strip ask" role="alertdialog" aria-label="Confirm">
      <p>{pending.message}</p>
      <button
        type="button"
        class="danger"
        onclick={() => {
          const run = pending?.run
          pending = null
          run?.()
        }}>{pending.confirmLabel}</button
      >
      <button type="button" onclick={() => (pending = null)}>Keep editing</button>
    </div>
  {/if}

  {#if ed.error}
    <p class="msg error" role="alert">{ed.error}</p>
  {:else if ed.notice}
    <p class="msg" role="status">{ed.notice}</p>
  {/if}

  {#if !opened}
    <div class="gate">
      {#if ed.passwordFor === 'open'}
        <PasswordAgain
          prompt="Enter your password again to open the config editor."
          submitLabel="Open"
          busy={ed.unlocking}
          error={ed.passwordError}
          onsubmit={(pw) => ed.submitPassword(pw)}
        />
      {/if}
    </div>
  {:else}
    <div class="body">
      <div class="editor">
        <div class="gutter" bind:this={gutter} aria-hidden="true">
          {#each lineNumbers as n (n)}
            <div class="ln" class:error={marks.get(n) === 'error'} class:warning={marks.get(n) === 'warning'}>{n}</div>
          {/each}
        </div>
        <textarea
          bind:this={textarea}
          value={ed.text}
          oninput={onInput}
          onscroll={syncGutter}
          wrap="off"
          spellcheck="false"
          autocomplete="off"
          autocapitalize="off"
          aria-label="config.yaml"
        ></textarea>
      </div>

      <aside class="rail" aria-label="Problems and snapshots">
        <section class="problems">
          {#if ed.changes.length > 0}
            <h3>What Carry forward changed ({ed.changes.length})</h3>
            <ul class="changes">
              {#each ed.changes as c, i (i)}
                <li>
                  <button type="button" class="row" onclick={() => goToLine(c.line)} disabled={!(c.line > 0)}>
                    <span class="kind">{c.kind}</span>
                    <code>{c.key}</code>
                    {#if c.to}{' '}<span class="to">→ <code>{c.to}</code></span>{/if}
                    {#if c.note}<span class="note">{c.note}</span>{/if}
                  </button>
                </li>
              {/each}
            </ul>
          {/if}

          <h3>
            Problems
            <span class="count">
              {#if ed.checking}checking…{:else if !ed.checkError}{ed.problems.length === 0
                  ? 'none'
                  : `${ed.problems.length}${errorCount > 0 ? ` · ${errorCount} must be fixed` : ''}`}{/if}
            </span>
          </h3>
          {#if ed.checkError}
            <p class="quiet error">Could not check the text: {ed.checkError}</p>
          {:else if ed.problems.length === 0 && !ed.checking}
            <p class="quiet">No problems found. MikroView would start with this file.</p>
          {/if}
          <ul class="plist">
            {#each ed.problems as p, i (i)}
              <li>
                <button type="button" class="row {p.severity === 'fatal' ? 'error' : 'warning'}" onclick={() => goToLine(p.line)}>
                  <span class="where">line {p.line}</span>
                  <span class="sev">{p.severity === 'fatal' ? 'must fix' : 'warning'}</span>
                  <span class="what">{p.message}</span>
                </button>
              </li>
            {/each}
          </ul>
        </section>

        <section class="snaps">
          <h3>Snapshots</h3>
          {#if ed.snapshotsError}
            <p class="quiet error">Could not list the snapshots: {ed.snapshotsError}</p>
          {:else if ed.snapshotsLoaded && ed.snapshots.length === 0}
            <p class="quiet">None yet. Snapshot keeps a copy of the text; Carry forward keeps one of the file before it changes anything.</p>
          {/if}
          <ul class="slist">
            {#each ed.snapshots as s (s.id)}
              <li class="snap">
                <div class="sline">
                  <span>{whenLabel(s.when)}</span>
                  <span class="dim">· {s.by} · {whyLabel(s.why)} · schema {s.schema}{s.version ? ` · ${s.version}` : ''}</span>
                </div>
                {#if s.note}<div class="snote">{s.note}</div>{/if}
                <div class="sacts">
                  <button type="button" class="olink" onclick={() => void ed.downloadSnapshot(s.id)}>Download</button>
                  <button type="button" class="olink" onclick={() => onLoadSnapshot(s.id)}>Load into editor</button>
                  <button
                    type="button"
                    class="olink del"
                    class:armed={armedDelete === s.id}
                    onclick={(e) => onDeleteClick(e, s.id)}
                  >
                    {armedDelete === s.id ? 'Delete for good?' : 'Delete'}
                  </button>
                </div>
              </li>
            {/each}
          </ul>
        </section>
      </aside>
    </div>
  {/if}
</div>

<style>
  .ce {
    position: fixed;
    inset: 0;
    z-index: 70;
    display: flex;
    flex-direction: column;
    background: var(--bg);
    color: var(--fg);
    min-height: 0;
  }

  .ce:focus {
    outline: none;
  }

  .banner {
    margin: 0;
    padding: 10px 16px;
    font-size: 13px;
    line-height: 1.5;
    color: var(--fg);
    background: var(--drop-bg);
    border-bottom: 1px solid color-mix(in srgb, var(--drop) 45%, transparent);
  }

  .bar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px 18px;
    padding: 10px 16px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-elevated);
  }

  .title {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
    flex: 1 1 320px;
  }

  h2 {
    margin: 0;
    font-size: 15px;
    font-weight: 600;
  }

  .facts {
    font-size: 12px;
    color: var(--fg-muted);
    overflow-wrap: anywhere;
  }

  .facts code {
    font-family: var(--font-mono);
    color: var(--fg);
  }

  .changed {
    font-size: 12px;
    color: var(--drop);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  button {
    border-radius: 5px;
    padding: 6px 12px;
    font-size: 13px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    cursor: pointer;
  }

  button:hover:not(:disabled) {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  button:disabled {
    opacity: 0.6;
    cursor: default;
  }

  button.primary {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 45%, transparent);
  }

  button.danger {
    color: var(--reject);
    border-color: color-mix(in srgb, var(--reject) 45%, transparent);
  }

  .strip {
    padding: 10px 16px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-elevated);
  }

  .strip.ask {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px 10px;
  }

  .strip.ask p {
    margin: 0;
    flex: 1 1 320px;
    font-size: 13px;
    line-height: 1.5;
  }

  .strip label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--fg-dim);
  }

  .strip input {
    font: 12px var(--font-sans);
    color: var(--fg);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 4px 8px;
    width: 28ch;
  }

  .msg {
    margin: 0;
    padding: 8px 16px;
    font-size: 12.5px;
    color: var(--fg-muted);
    border-bottom: 1px solid var(--border);
  }

  .msg.error,
  .quiet.error {
    color: var(--reject);
  }

  .gate {
    padding: 24px 16px;
    max-width: 640px;
  }

  .body {
    flex: 1;
    display: grid;
    grid-template-columns: minmax(0, 1fr) 380px;
    min-height: 0;
  }

  .editor {
    display: flex;
    min-width: 0;
    min-height: 0;
    border-right: 1px solid var(--border);
  }

  /* The gutter and the textarea share one line height and one top
     padding, so line n of the text sits beside number n; the gutter
     follows the textarea's scroll (syncGutter) and has spare room at
     the bottom for the textarea's horizontal scrollbar. */
  .gutter,
  textarea {
    font-family: var(--font-mono);
    font-size: 12.5px;
    line-height: 20px;
    padding-top: 10px;
  }

  .gutter {
    flex: none;
    overflow: hidden;
    padding-bottom: 40px;
    padding-left: 8px;
    padding-right: 8px;
    min-width: 4ch;
    text-align: right;
    color: var(--fg-dim);
    background: var(--bg-elevated);
    user-select: none;
  }

  .ln {
    height: 20px;
  }

  .ln.error {
    color: var(--reject);
    box-shadow: inset -2px 0 0 var(--reject);
  }

  .ln.warning {
    color: var(--drop);
    box-shadow: inset -2px 0 0 var(--drop);
  }

  textarea {
    flex: 1;
    min-width: 0;
    margin: 0;
    padding-left: 12px;
    padding-right: 12px;
    padding-bottom: 10px;
    border: none;
    border-radius: 0;
    resize: none;
    white-space: pre;
    overflow: auto;
    color: var(--fg);
    background: var(--bg);
    tab-size: 2;
  }

  textarea:focus-visible {
    outline: 2px solid color-mix(in srgb, var(--accent) 60%, transparent);
    outline-offset: -2px;
  }

  .rail {
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--bg-elevated);
  }

  .problems {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 10px 14px;
  }

  .snaps {
    flex: none;
    max-height: 42%;
    overflow-y: auto;
    padding: 10px 14px 14px;
    border-top: 1px solid var(--border);
  }

  h3 {
    margin: 4px 0 8px;
    font-size: 12px;
    font-weight: 600;
    color: var(--fg-muted);
    display: flex;
    align-items: baseline;
    gap: 8px;
  }

  .count {
    font-weight: 400;
    color: var(--fg-dim);
  }

  .quiet {
    margin: 0 0 8px;
    font-size: 12px;
    color: var(--fg-dim);
    line-height: 1.5;
  }

  ul {
    list-style: none;
    margin: 0 0 12px;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  button.row {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 2px 8px;
    width: 100%;
    text-align: left;
    padding: 6px 8px;
    font-size: 12px;
    line-height: 1.45;
    border-color: transparent;
    color: var(--fg);
  }

  button.row:hover:not(:disabled) {
    background: var(--bg-hover);
    border-color: var(--border);
  }

  button.row:disabled {
    opacity: 1;
  }

  .row code {
    font-family: var(--font-mono);
    font-size: 11.5px;
  }

  .kind,
  .where {
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--fg-dim);
  }

  .sev {
    font-size: 11px;
    color: var(--drop);
  }

  .row.error .sev {
    color: var(--reject);
  }

  .note,
  .what {
    flex-basis: 100%;
    color: var(--fg-muted);
  }

  .snap {
    padding: 6px 0;
    font-size: 12px;
    border-bottom: 1px solid var(--border);
  }

  .sline .dim {
    color: var(--fg-dim);
  }

  .snote {
    margin-top: 2px;
    color: var(--fg-muted);
    font-style: italic;
  }

  .sacts {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    margin-top: 4px;
  }

  .olink {
    background: none;
    border: none;
    padding: 0;
    font-size: 12px;
    color: var(--accent);
    cursor: pointer;
    text-decoration: underline;
    text-decoration-color: transparent;
  }

  .olink:hover:not(:disabled) {
    text-decoration-color: currentColor;
    color: var(--accent);
  }

  .olink.del.armed {
    color: var(--reject);
  }

  @media (max-width: 860px) {
    .body {
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: minmax(0, 1fr) minmax(0, 40%);
    }

    .editor {
      border-right: none;
      border-bottom: 1px solid var(--border);
    }
  }
</style>
