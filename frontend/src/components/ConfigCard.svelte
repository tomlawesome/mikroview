<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // #1347: the Engine Room's Config card -- the running config file at a
  // glance and the door into the config editor. Admin-only: EngineRoom
  // mounts it under isAdmin, and every route it reads is admin-only.
  //
  // Two columns in EngineRoom's .stsection.wide grid, the same as
  // DiskControl: a sentence on the left, the rows on the right in the
  // .orow grammar. What the file says about itself (its path, header and
  // schema) comes from GET /api/config/editor/summary, an ungated read
  // fetched on mount -- nothing in it is secret, only facts about the
  // file -- so the card shows it before the editor has ever been opened.
  // Once the editor has been opened, its own answer (configEditorState.file)
  // takes over, since it is at least as fresh.
  import { onMount } from 'svelte'
  import { configEditorState } from '../lib/configEditor.svelte'
  import PasswordAgain from './PasswordAgain.svelte'

  onMount(() => {
    void configEditorState.refreshSnapshots()
    void configEditorState.refreshSummary()
  })

  let asking = $state(false)
  let opening = $state(false)
  let openError = $state<string | null>(null)

  const file = $derived(configEditorState.file ?? configEditorState.summary)
  const fileLine = $derived.by(() => {
    if (file) return null
    if (configEditorState.summaryError) return 'unknown — the server did not answer'
    return '…'
  })

  function startOpen() {
    asking = true
    openError = null
  }

  function cancelOpen() {
    asking = false
    openError = null
  }

  async function submit(password: string) {
    opening = true
    openError = null
    try {
      const err = await configEditorState.open(password)
      if (err) {
        openError = err
        return
      }
      asking = false
    } finally {
      opening = false
    }
  }

  const snapshotLine = $derived.by(() => {
    if (configEditorState.snapshotsError) return 'unknown — the server did not answer'
    if (!configEditorState.snapshotsLoaded) return '…'
    const n = configEditorState.snapshots.length
    return n === 0 ? 'none yet' : `${n} kept (the last ${configEditorState.snapshotsKeep} are kept)`
  })
</script>

<div class="wleft">
  <p class="oghint">
    Check and update config.yaml here. MikroView checks it as you type and gives you the result as a download;
    it never changes the file on disk itself.
  </p>
</div>

<div class="wrows">
  <div class="orow">
    <span>file</span>
    <span class="ov" class:dim={!file}>{file ? file.path : fileLine}</span>
  </div>
  <div class="orow">
    <span>written by</span>
    <span class="ov" class:dim={!file}>
      {#if !file}
        {fileLine}
      {:else if file.header}
        {file.header.writtenBy}
      {:else}
        no header — written before v0.7
      {/if}
    </span>
  </div>
  <div class="orow">
    <span>schema</span>
    <span class="ov" class:dim={!file}>
      {#if !file}
        {fileLine}
      {:else if file.header}
        {file.header.schema} · this version reads {file.runningSchema}
      {:else}
        looks like {file.schemaGuess} · this version reads {file.runningSchema}
      {/if}
    </span>
  </div>
  <div class="orow">
    <span>snapshots</span>
    <span class="ov" class:dim={configEditorState.snapshots.length === 0}>{snapshotLine}</span>
  </div>
  <div class="orow action">
    {#if asking}
      <PasswordAgain
        prompt="Enter your password again to open the config editor."
        submitLabel="Open"
        busy={opening}
        error={openError}
        onsubmit={submit}
        oncancel={cancelOpen}
      />
    {:else}
      <span>editor</span>
      <span class="ov"><button type="button" class="olink" onclick={startOpen}>Open the editor</button></span>
    {/if}
  </div>
</div>

<style>
  .wleft {
    grid-column: 1;
    min-width: 0;
  }

  .wrows {
    grid-column: 2;
    min-width: 0;
  }

  @media (max-width: 1100px) {
    .wleft,
    .wrows {
      grid-column: 1;
    }
  }

  .oghint {
    margin: 2px 0 0;
    font-size: 11.5px;
    font-style: italic;
    color: var(--fg-dim);
    line-height: 1.5;
  }

  .orow {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 14px;
    padding: 7px 0;
    font-size: 12px;
  }

  .orow + .orow {
    margin-top: 3px;
  }

  .orow > span:first-child {
    color: var(--fg-dim);
    flex: none;
  }

  .orow .ov {
    color: var(--fg-muted);
    text-align: right;
    overflow-wrap: anywhere;
  }

  .orow .ov.dim {
    color: var(--fg-dim);
  }

  .orow.action {
    justify-content: flex-end;
  }

  .orow.action > span:first-child {
    margin-right: auto;
  }

  .olink {
    background: none;
    border: none;
    padding: 0;
    font-size: inherit;
    color: var(--accent);
    cursor: pointer;
    text-decoration: underline;
    text-decoration-color: transparent;
  }

  .olink:hover {
    text-decoration-color: currentColor;
  }
</style>
