<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Admin-only entity management (see internal/entities.Entity, issue
  // #107) -- persisted (type, key) -> label/tags records covering hosts,
  // rules, and (issue #109) ports. Two halves:
  //
  //  1. "Named entities": every persisted record, with the type/key/tags
  //     add-or-edit form #107 shipped, plus inline label editing (click
  //     a label to rename it in place without opening the form).
  //  2. "Discovered": hosts/rules/ports seen in live data that have no
  //     entity yet -- mirroring internal/device.Registry's own
  //     "auto-discovered, shown even before configured" pattern, so a
  //     user can name something without already knowing its raw IP/
  //     rule-label/port number. See lib/discoveredEntities.ts for how
  //     each of the three is derived.
  import { onMount } from 'svelte'
  import { entitiesState } from '../lib/entities.svelte'
  import { appState } from '../lib/state.svelte'
  import { fetchRules } from '../lib/api'
  import { discoverHosts, discoverPorts, discoverRules } from '../lib/discoveredEntities'
  import { formatRelative } from '../lib/format'
  import type { Entity, RuleUsage } from '../lib/types'

  // '' means "not currently editing" -- the add form and the edit form
  // are the same form (Upsert already treats create/replace as one
  // operation), so there's only ever one draft in flight.
  let editingKey = $state<{ type: string; key: string } | null>(null)

  let draftType = $state('host')
  let draftKey = $state('')
  let draftLabel = $state('')
  let draftTags = $state('')

  let error = $state<string | null>(null)
  let saving = $state(false)
  let deletingKey = $state<string | null>(null)

  // rulesUsage backs the "discovered rules" section -- GET /api/rules'
  // full history (issue #103's internal/rules.Store), fetched once per
  // panel open the same way entitiesState.refresh() is triggered by
  // the rail's Entities item (this component is unmounted/remounted on
  // every view toggle, so onMount firing once per open is exactly right).
  let rulesUsage = $state<RuleUsage[]>([])
  let rulesError = $state(false)

  onMount(() => {
    fetchRules()
      .then((r) => (rulesUsage = r))
      .catch(() => (rulesError = true))
  })

  const discoveredRules = $derived(discoverRules(rulesUsage, entitiesState.list))
  const discoveredHosts = $derived(discoverHosts(appState.events, entitiesState.list))
  const discoveredPorts = $derived(discoverPorts(appState.events, entitiesState.list))

  function resetDraft() {
    editingKey = null
    draftType = 'host'
    draftKey = ''
    draftLabel = ''
    draftTags = ''
    error = null
  }

  function startEdit(e: Entity) {
    cancelInline()
    editingKey = { type: e.type, key: e.key }
    draftType = e.type
    draftKey = e.key
    draftLabel = e.label ?? ''
    draftTags = (e.tags ?? []).join(', ')
    error = null
  }

  function parseTags(v: string): string[] {
    return v
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0)
  }

  async function submit(e: Event) {
    e.preventDefault()
    error = null
    saving = true
    const err = await entitiesState.upsert({
      type: draftType.trim(),
      key: draftKey.trim(),
      label: draftLabel.trim(),
      tags: parseTags(draftTags),
    })
    saving = false
    if (err) {
      error = err
      return
    }
    resetDraft()
  }

  async function remove(t: Entity) {
    deletingKey = t.type + ':' + t.key
    // Same contract as submit() above: the error comes back as text, so
    // ignoring it made a failed delete indistinguishable from one that
    // worked.
    error = await entitiesState.remove(t.type, t.key)
    deletingKey = null
    if (editingKey?.type === t.type && editingKey?.key === t.key) resetDraft()
  }

  // ---- Inline label editing -----------------------------------------
  // Shared by both halves of this view: a named entity's label cell
  // (rename in place), and a discovered row's "Name it" affordance
  // (create a new entity with just a label, no tags). Only one row can
  // be mid-edit at a time -- same single-draft-in-flight reasoning
  // editingKey above already follows for the full form.
  let inlineKey = $state<string | null>(null)
  let inlineDraft = $state('')
  let inlineSaving = $state(false)
  let inlineError = $state<string | null>(null)

  function rowKey(type: string, key: string): string {
    return type + ':' + key
  }

  function startInline(type: string, key: string, currentLabel: string) {
    resetDraft() // mutually exclusive with the full add/edit form
    inlineKey = rowKey(type, key)
    inlineDraft = currentLabel
    inlineError = null
  }

  function cancelInline() {
    inlineKey = null
    inlineError = null
  }

  async function saveInline(type: string, key: string, tags: string[] = []) {
    inlineError = null
    inlineSaving = true
    // inlineKey is cleared *before* the await, not after (#384).
    //
    // upsert() refreshes the whole list before it returns, so the newly
    // named entity is already in entitiesState.list by the time we get
    // control back. Its row key in "Named entities" is the same
    // type:key that was just being edited down in "Discovered" -- so a
    // still-set inlineKey makes that brand-new row render the inline
    // input, focusOnMount focuses it, and the browser scrolls the row
    // (near the top of the page) into view. Clearing afterwards
    // unmounts the input again, so the only trace left is the
    // operator's scroll position, thrown away on every add -- which is
    // precisely the workflow this view is for.
    const wasEditing = inlineKey
    inlineKey = null
    const err = await entitiesState.upsert({ type, key, label: inlineDraft.trim(), tags })
    inlineSaving = false
    if (err) {
      // Nothing was created, so the only row matching this key is the
      // one the operator is already looking at -- reopening its editor
      // puts the cursor back where the error needs fixing.
      inlineKey = wasEditing
      inlineError = err
    }
  }

  function onInlineKeydown(e: KeyboardEvent, type: string, key: string, tags: string[] = []) {
    if (e.key === 'Enter') {
      e.preventDefault()
      saveInline(type, key, tags)
    } else if (e.key === 'Escape') {
      cancelInline()
    }
  }

  // A plain HTML `autofocus` attribute trips svelte's a11y-autofocus
  // warning; this action gets the same "ready to type immediately"
  // behavior without it, and re-runs correctly since the input this is
  // attached to only exists (is (re)mounted) while inlineKey matches.
  function focusOnMount(node: HTMLInputElement) {
    node.focus()
  }
</script>

{#snippet discoveredRow(type: string, item: { key: string; lastSeen: string })}
  {@const rk = rowKey(type, item.key)}
  <li class="row discovered">
    <span class="type">{type}</span>
    <span class="key">{item.key}</span>
    {#if inlineKey === rk}
      <input
        class="inline-input"
        type="text"
        placeholder="friendly name"
        bind:value={inlineDraft}
        use:focusOnMount
        onkeydown={(e) => onInlineKeydown(e, type, item.key)}
      />
      <span class="row-actions">
        <button class="cancel" onclick={cancelInline}>Cancel</button>
        <button class="save" disabled={inlineSaving} onclick={() => saveInline(type, item.key)}>
          {inlineSaving ? 'Saving…' : 'Save'}
        </button>
      </span>
    {:else}
      <span class="label unnamed">not yet named</span>
      <span class="row-actions">
        <button class="name-it" onclick={() => startInline(type, item.key, '')}>Name it</button>
      </span>
    {/if}
    <span class="seen">last seen {formatRelative(item.lastSeen, appState.now)}</span>
  </li>
  {#if inlineKey === rk && inlineError}
    <p class="error">{inlineError}</p>
  {/if}
{/snippet}

<div class="page scrollbar">
  <p class="intro">
    Entities are shared, persisted labels/tags attached to a host, port, or firewall rule -- friendly names editable
    here instead of only in config.yaml. <strong>Discovered</strong> below lists hosts/rules/ports seen in live
    traffic that don't have a label yet, so you don't need to already know a raw IP, rule label, or port number to
    name it.
  </p>

  <form class="form" onsubmit={submit}>
    <div class="form-title">{editingKey ? `Editing ${editingKey.type}:${editingKey.key}` : 'Add entity'}</div>
    <div class="form-row">
      <label class="field">
        <span>Type</span>
        <input type="text" list="entity-types" placeholder="host" bind:value={draftType} required disabled={!!editingKey} />
        <datalist id="entity-types">
          <option value="host"></option>
          <option value="rule"></option>
          <option value="port"></option>
        </datalist>
      </label>
      <label class="field">
        <span>Key</span>
        <input
          type="text"
          placeholder="192.168.1.50, r13, or 8291"
          bind:value={draftKey}
          required
          disabled={!!editingKey}
        />
      </label>
      <label class="field">
        <span>Label</span>
        <input type="text" placeholder="friendly name" bind:value={draftLabel} />
      </label>
      <label class="field grow">
        <span>Tags (comma-separated)</span>
        <input type="text" placeholder="trusted-mail-sender" bind:value={draftTags} />
      </label>
    </div>
    {#if error}
      <p class="error">{error}</p>
    {/if}
    <div class="form-actions">
      {#if editingKey}
        <button type="button" class="cancel" onclick={resetDraft}>Cancel</button>
      {/if}
      <button type="submit" class="save" disabled={saving}>
        {saving ? 'Saving…' : editingKey ? 'Save changes' : 'Add entity'}
      </button>
    </div>
  </form>

  <section class="section">
    <h3 class="section-title">Named entities</h3>
    {#if entitiesState.list.length === 0}
      <p class="empty">No entities yet -- add one above, or name something discovered below.</p>
    {:else}
      <ul class="list">
        {#each entitiesState.list as e (e.type + ':' + e.key)}
          {@const rk = rowKey(e.type, e.key)}
          <li class="row">
            <span class="type">{e.type}</span>
            <span class="key">{e.key}</span>
            {#if inlineKey === rk}
              <input
                class="inline-input"
                type="text"
                placeholder="friendly name"
                bind:value={inlineDraft}
                use:focusOnMount
                onkeydown={(ev) => onInlineKeydown(ev, e.type, e.key, e.tags ?? [])}
              />
            {:else}
              <button
                class="label label-btn"
                onclick={() => startInline(e.type, e.key, e.label ?? '')}
                title="Click to edit label"
              >
                {e.label || '— click to name —'}
              </button>
            {/if}
            <span class="tags">
              {#each e.tags ?? [] as tag (tag)}
                <span class="tag">{tag}</span>
              {/each}
            </span>
            <span class="row-actions">
              {#if inlineKey === rk}
                <button class="cancel" onclick={cancelInline}>Cancel</button>
                <button class="save" disabled={inlineSaving} onclick={() => saveInline(e.type, e.key, e.tags ?? [])}>
                  {inlineSaving ? 'Saving…' : 'Save'}
                </button>
              {:else}
                <button class="edit" onclick={() => startEdit(e)}>Edit</button>
                <button class="delete" disabled={deletingKey === rk} onclick={() => remove(e)}>
                  {deletingKey === rk ? 'Removing…' : 'Remove'}
                </button>
              {/if}
            </span>
          </li>
          {#if inlineKey === rk && inlineError}
            <p class="error">{inlineError}</p>
          {/if}
        {/each}
      </ul>
    {/if}
  </section>

  <section class="section">
    <h3 class="section-title">Discovered rules</h3>
    <p class="section-intro">
      Every rule label mikroview has ever seen fire, that doesn't have a label yet.
      {#if rulesError}<span class="fetch-error">Couldn't load rule history.</span>{/if}
    </p>
    {#if discoveredRules.length === 0}
      <p class="empty">Nothing discovered yet.</p>
    {:else}
      <ul class="list">
        {#each discoveredRules as item (item.key)}
          {@render discoveredRow('rule', item)}
        {/each}
      </ul>
    {/if}
  </section>

  <section class="section">
    <h3 class="section-title">Discovered hosts</h3>
    <p class="section-intro">
      Source/destination IPs seen in recent traffic that don't have a label yet (limited to what's currently loaded
      in this browser tab).
    </p>
    {#if discoveredHosts.length === 0}
      <p class="empty">Nothing discovered yet.</p>
    {:else}
      <ul class="list">
        {#each discoveredHosts as item (item.key)}
          {@render discoveredRow('host', item)}
        {/each}
      </ul>
    {/if}
  </section>

  <section class="section">
    <h3 class="section-title">Discovered ports</h3>
    <p class="section-intro">
      Source/destination ports seen in recent traffic that don't have a label yet (limited to what's currently loaded
      in this browser tab).
    </p>
    {#if discoveredPorts.length === 0}
      <p class="empty">Nothing discovered yet.</p>
    {:else}
      <ul class="list">
        {#each discoveredPorts as item (item.key)}
          {@render discoveredRow('port', item)}
        {/each}
      </ul>
    {/if}
  </section>
</div>

<style>
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .intro {
    margin: 0;
    max-width: 80ch;
    font-size: 13px;
    color: var(--fg-muted);
  }

  .form {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .form-title {
    font-size: 12px;
    font-weight: 600;
    color: var(--fg-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .form-row {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 5px;
    font-size: 12px;
    color: var(--fg-muted);
    flex: 1 1 140px;
    min-width: 120px;
  }

  .field.grow {
    flex: 2 1 220px;
  }

  input {
    background: var(--bg);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 6px 8px;
    font-size: 13px;
  }

  input:focus {
    outline: none;
    border-color: var(--accent);
  }

  input:disabled {
    opacity: 0.6;
  }

  .error {
    margin: 0;
    color: var(--reject);
    font-size: 12px;
  }

  .fetch-error {
    color: var(--reject);
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  .section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .section-title {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    color: var(--fg);
  }

  .section-intro {
    margin: 0;
    font-size: 12px;
    color: var(--fg-muted);
  }

  .cancel,
  .save,
  .edit,
  .delete,
  .name-it {
    border-radius: 5px;
    padding: 6px 12px;
    font-size: 12px;
  }

  .cancel {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .cancel:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .save,
  .name-it {
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    font-weight: 600;
  }

  .save:hover,
  .name-it:hover {
    opacity: 0.9;
  }

  .save:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .empty {
    margin: 0;
    font-size: 13px;
    color: var(--fg-muted);
    font-style: italic;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .row {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 10px 14px;
    display: flex;
    align-items: center;
    gap: 14px;
    flex-wrap: wrap;
  }

  .row.discovered {
    border-style: dashed;
  }

  .type {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--fg-muted);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 2px 6px;
    flex: none;
  }

  .key {
    font-family: var(--font-mono, monospace);
    font-size: 13px;
    color: var(--fg);
    flex: 1 1 160px;
    min-width: 100px;
  }

  .label {
    font-size: 13px;
    color: var(--fg-muted);
    flex: 1 1 160px;
    min-width: 100px;
  }

  .label-btn {
    text-align: left;
    background: transparent;
    border: 1px dashed transparent;
    border-radius: 4px;
    padding: 2px 4px;
    margin: -2px -4px;
    cursor: text;
  }

  .label-btn:hover {
    border-color: var(--border);
    color: var(--fg);
  }

  .label.unnamed {
    font-style: italic;
    opacity: 0.7;
  }

  .inline-input {
    flex: 1 1 160px;
    min-width: 100px;
  }

  .seen {
    font-size: 11px;
    color: var(--fg-dim, var(--fg-muted));
    flex: none;
  }

  .tags {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    flex: 1 1 160px;
    min-width: 100px;
  }

  .tag {
    font-size: 11px;
    color: var(--fg-muted);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 2px 8px;
  }

  .row-actions {
    display: flex;
    gap: 6px;
    flex: none;
    margin-left: auto;
  }

  .edit {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .edit:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .delete {
    background: transparent;
    border: 1px solid var(--reject);
    color: var(--reject);
  }

  .delete:hover {
    opacity: 0.85;
  }

  .delete:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
