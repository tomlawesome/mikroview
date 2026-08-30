<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The watchers station, opened (#490) -- the detector bench. Successor
  // to the former Detectors.svelte page (see git history): same data
  // (detectorSettingsState, lib/detectorCopy.ts's hand-written copy) and
  // the same on/off + scope-editing behaviour, reshaped into one row per
  // detector rather than a card grid, since it now unfolds inside the
  // station instead of occupying a whole page.
  //
  // Mounted only while the watchers station is the one open (see
  // EngineRoom.svelte) -- there is no standalone route for this any
  // more.
  import { detectorSettingsState } from '../lib/detectorSettings.svelte'
  import {
    DETECTORS,
    SCOPE_FIELDS,
    draftFrom,
    learningSummary,
    parseList,
    parsePorts,
    scopeSummary,
    type DetectorDraft,
  } from '../lib/detectorCopy'
  import type { DetectorScope } from '../lib/types'

  let { isAdmin }: { isAdmin: boolean } = $props()

  // Which detector's scope form is open, if any -- independent of which
  // station is open (EngineRoom.svelte's own expandedStation), since a
  // viewer never reaches this at all (no knob ink, see below) and an
  // admin can only ever be editing one detector's scope at a time.
  let editingScope = $state<string | null>(null)
  let drafts = $state<Record<string, DetectorDraft>>({})
  let errors = $state<Partial<Record<string, string>>>({})
  let saving = $state<Partial<Record<string, boolean>>>({})

  function toggleScopeForm(name: string, scope: DetectorScope) {
    if (editingScope === name) {
      editingScope = null
      return
    }
    drafts[name] = draftFrom(scope)
    editingScope = name
  }

  async function toggleEnabled(name: string, enabled: boolean, scope: DetectorScope) {
    saving[name] = true
    const err = await detectorSettingsState.update(name, enabled, scope)
    saving[name] = false
    errors[name] = err ?? undefined
  }

  async function saveScope(name: string) {
    const d = drafts[name]
    const existing = detectorSettingsState.list.find((x) => x.name === name)
    const scope: DetectorScope = {
      hosts: parseList(d.hosts),
      hostsMode: d.hostsMode,
      ports: parsePorts(d.ports),
      portsMode: d.portsMode,
      rules: parseList(d.rules),
      rulesMode: d.rulesMode,
      classification: d.classification,
    }
    saving[name] = true
    const err = await detectorSettingsState.update(name, existing?.enabled ?? true, scope)
    saving[name] = false
    errors[name] = err ?? undefined
    if (!err) editingScope = null
  }
</script>

<ul class="bench">
  {#each detectorSettingsState.list as d (d.name)}
    {@const info = DETECTORS[d.name] ?? { label: d.label, explanation: d.description ?? '' }}
    {@const fields = SCOPE_FIELDS[d.name] ?? []}
    {@const learning = learningSummary(d.learning)}
    <li class="row">
      <div class="line">
        {#if isAdmin}
          <input
            type="checkbox"
            class="cbx"
            aria-label="{info.label} runs"
            checked={d.enabled}
            disabled={saving[d.name]}
            onchange={(e) => toggleEnabled(d.name, e.currentTarget.checked, d.scope)}
          />
        {/if}
        <span class="name">{info.label}</span>
        <span class="id">{d.name}</span>
        <span class="dash">—</span>
        {#if fields.length === 0}
          <span class="scope-fact">{scopeSummary(d.scope)}</span>
        {:else if isAdmin}
          <button type="button" class="scope-knob" onclick={() => toggleScopeForm(d.name, d.scope)}>
            {scopeSummary(d.scope)}
          </button>
        {:else}
          <span class="scope-fact">{scopeSummary(d.scope)}</span>
        {/if}
        <span class="state" class:paused={!d.enabled}>
          <span class="dot"></span>
          {saving[d.name] ? 'saving…' : d.enabled ? 'running' : 'paused'}
        </span>
      </div>

      {#if learning}
        <p class="learning">{learning}</p>
      {/if}

      {#if errors[d.name]}
        <p class="error">{errors[d.name]}</p>
      {/if}

      {#if isAdmin && editingScope === d.name}
        <div class="scope-form">
          {#if info.scopeNote}
            <p class="note"><strong>What this restricts:</strong> {info.scopeNote}</p>
          {/if}
          {#if info.example}
            <p class="example"><strong>Example:</strong> {info.example}</p>
          {/if}
          {#if fields.includes('hosts')}
            <label class="field">
              <span>Hosts (comma-separated IPs or CIDRs)</span>
              <div class="field-row">
                <select bind:value={drafts[d.name].hostsMode}>
                  <option value="">no restriction</option>
                  <option value="allow">allow only</option>
                  <option value="deny">deny</option>
                </select>
                <input type="text" placeholder="192.168.1.50, 203.0.113.0/24" bind:value={drafts[d.name].hosts} />
              </div>
            </label>
          {/if}
          {#if fields.includes('classification')}
            <label class="field">
              <span>Source classification</span>
              <select bind:value={drafts[d.name].classification}>
                <option value="">any</option>
                <option value="internal">internal only</option>
                <option value="external">external only</option>
              </select>
            </label>
          {/if}
          {#if fields.includes('ports')}
            <label class="field">
              <span>Ports (comma-separated)</span>
              <div class="field-row">
                <select bind:value={drafts[d.name].portsMode}>
                  <option value="">no restriction</option>
                  <option value="allow">allow only</option>
                  <option value="deny">deny</option>
                </select>
                <input type="text" placeholder="22, 3389" bind:value={drafts[d.name].ports} />
              </div>
            </label>
          {/if}
          {#if fields.includes('rules')}
            <label class="field">
              <span>Rule labels (comma-separated)</span>
              <div class="field-row">
                <select bind:value={drafts[d.name].rulesMode}>
                  <option value="">no restriction</option>
                  <option value="allow">allow only</option>
                  <option value="deny">deny</option>
                </select>
                <input type="text" placeholder="r13" bind:value={drafts[d.name].rules} />
              </div>
            </label>
          {/if}
          <div class="actions">
            <button type="button" class="cancel" onclick={() => (editingScope = null)}>Cancel</button>
            <button type="button" class="save" disabled={saving[d.name]} onclick={() => saveScope(d.name)}>
              {saving[d.name] ? 'saving…' : 'Save scope'}
            </button>
          </div>
        </div>
      {/if}
    </li>
  {/each}
</ul>

<style>
  .bench {
    list-style: none;
    margin: 8px 0 0;
    padding-top: 8px;
    border-top: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .row {
    font-size: 12px;
  }

  .line {
    display: flex;
    align-items: baseline;
    gap: 8px;
    flex-wrap: wrap;
  }

  .cbx {
    align-self: center;
    /* Never a flex item that grows: the checkbox is a fixed mark at the
       head of the row, and the row's alignment depends on it. */
    flex: none;
  }

  .name {
    color: var(--fg);
    font-weight: 600;
  }

  .id {
    font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
    font-size: 10.5px;
    color: var(--fg-dim);
  }

  .dash {
    color: var(--fg-dim);
  }

  .scope-fact {
    color: var(--fg-muted);
  }

  .scope-knob {
    color: var(--fg);
    font-weight: 600;
    /* border: none first -- app.css resets a button's font but not its
       border, so setting only the dashed underline left Chromium's
       default button border boxing in what the record calls the admin's
       ink: a dashed underline under a value, not a button. */
    border: none;
    border-bottom: 1px dashed var(--accent);
    background: transparent;
    padding: 0;
  }

  .scope-knob:hover {
    color: var(--accent);
  }

  .state {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    color: var(--fg-muted);
    white-space: nowrap;
  }

  .state .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accept);
  }

  .state.paused {
    color: var(--fg-dim);
  }

  .state.paused .dot {
    background: transparent;
    border: 1px solid var(--fg-dim);
  }

  .error {
    margin: 4px 0 0;
    color: var(--reject);
    font-size: 11.5px;
  }

  /* No colour carries meaning here on its own (#639) -- the wording
     already states which of the five states this is, so this is styled
     identically to any other secondary fact line rather than given a
     status colour that would just repeat the words. */
  .learning {
    margin: 4px 0 0;
    color: var(--fg-muted);
    font-size: 11.5px;
  }

  .scope-form {
    margin-top: 6px;
    padding: 10px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .note,
  .example {
    margin: 0;
    font-size: 11.5px;
    color: var(--fg-muted);
    line-height: 1.5;
  }

  .example {
    color: var(--fg);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 11.5px;
    color: var(--fg-muted);
  }

  .field-row {
    display: flex;
    gap: 6px;
  }

  /* Scoped to the scope form. As bare element selectors these also hit
     the bench's run/pause checkbox -- which is an <input> on the same
     row -- and `flex: 1` made it absorb the row's free space, so every
     detector name started at a different x depending on how long it
     was. The bench read as ragged for that reason alone. */
  .scope-form input,
  .scope-form select {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 5px 7px;
    font-size: 12px;
  }

  .scope-form input {
    flex: 1;
    min-width: 0;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  .cancel,
  .save {
    border-radius: 5px;
    padding: 5px 10px;
    font-size: 12px;
  }

  .cancel {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .save {
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    font-weight: 600;
  }

  .save:disabled {
    opacity: 0.6;
    cursor: default;
  }
</style>
