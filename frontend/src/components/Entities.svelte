<script lang="ts">
  // Admin-only: list/add/edit/remove persisted entity records (see
  // internal/entities.Entity) -- the shared foundation issue #107 lays
  // down for two sibling features (a mail-sender allowlist, UI-managed
  // IP/port/rule aliasing), neither of which exists yet. Deliberately
  // basic: a plain list plus one add/edit form, no per-type-specific
  // affordances (a "trusted sender" checkbox, a port-specific field,
  // etc.) -- those belong to whichever sibling issue actually needs
  // them, not here.
  import { entitiesState } from '../lib/entities.svelte'
  import type { Entity } from '../lib/types'

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

  function resetDraft() {
    editingKey = null
    draftType = 'host'
    draftKey = ''
    draftLabel = ''
    draftTags = ''
    error = null
  }

  function startEdit(e: Entity) {
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
    await entitiesState.remove(t.type, t.key)
    deletingKey = null
    if (editingKey?.type === t.type && editingKey?.key === t.key) resetDraft()
  }
</script>

<div class="page scrollbar">
  <p class="intro">
    Entities are shared, persisted labels/tags attached to a host or firewall rule -- friendly names editable here
    instead of only in config.yaml. This is a deliberately minimal starting point: future features (a mail-sender
    allowlist, richer IP/port/rule aliasing) will build on the same records.
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
        </datalist>
      </label>
      <label class="field">
        <span>Key</span>
        <input type="text" placeholder="192.168.1.50 or r13" bind:value={draftKey} required disabled={!!editingKey} />
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

  {#if entitiesState.list.length === 0}
    <p class="empty">No entities yet -- add one above.</p>
  {:else}
    <ul class="list">
      {#each entitiesState.list as e (e.type + ':' + e.key)}
        <li class="row">
          <span class="type">{e.type}</span>
          <span class="key">{e.key}</span>
          <span class="label">{e.label || '—'}</span>
          <span class="tags">
            {#each e.tags ?? [] as tag (tag)}
              <span class="tag">{tag}</span>
            {/each}
          </span>
          <span class="row-actions">
            <button class="edit" onclick={() => startEdit(e)}>Edit</button>
            <button class="delete" disabled={deletingKey === e.type + ':' + e.key} onclick={() => remove(e)}>
              {deletingKey === e.type + ':' + e.key ? 'Removing…' : 'Remove'}
            </button>
          </span>
        </li>
      {/each}
    </ul>
  {/if}
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

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  .cancel,
  .save,
  .edit,
  .delete {
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

  .save {
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    font-weight: 600;
  }

  .save:hover {
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
