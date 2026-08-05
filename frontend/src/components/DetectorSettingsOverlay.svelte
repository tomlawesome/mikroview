<script lang="ts">
  // Admin-only: per-detector on/off + scope restrictions (see
  // internal/detect.Scope's doc comment and docs/configuration.md's
  // "Per-detector toggles" section for exactly what each field does per
  // detector). A modal like FlagsOverlay, not a separate view -- this
  // is an occasional settings tweak, not a destination you navigate to.
  import { detectorSettingsState } from '../lib/detectorSettings.svelte'
  import type { DetectorName, DetectorScope, ListMode } from '../lib/types'

  const LABELS: Record<DetectorName, string> = {
    port_scan: 'Port scan',
    activity_spike: 'Activity spike',
    critical_port: 'Critical-port attempts',
    global_spike: 'Network-wide volume spike',
    distributed_brute_force: 'Distributed brute-force',
    outbound_anomaly: 'Outbound anomaly',
    internal_recon: 'Internal reconnaissance',
    rule_spike: 'Rule hit-rate spike',
    repeated_drops: 'Repeated drops on a port',
    low_slow_scan: 'Low-and-slow port scan',
  }

  // Which scope fields apply to each detector -- kept in sync with
  // internal/detect.Scope's doc comment. Showing a control that does
  // nothing for a given detector would be actively misleading, so the
  // form only ever renders what's meaningful.
  const SCOPE_FIELDS: Record<DetectorName, Array<'hosts' | 'ports' | 'classification' | 'rules'>> = {
    port_scan: ['hosts', 'classification', 'ports'],
    activity_spike: ['hosts', 'classification'],
    critical_port: ['hosts', 'classification', 'ports'],
    distributed_brute_force: ['hosts', 'classification', 'ports'],
    outbound_anomaly: ['hosts'],
    internal_recon: ['hosts'],
    rule_spike: ['rules'],
    repeated_drops: ['hosts', 'ports'],
    global_spike: [],
    low_slow_scan: ['hosts', 'classification', 'ports'],
  }

  let expanded = $state<DetectorName | null>(null)
  let errors = $state<Partial<Record<DetectorName, string>>>({})
  let saving = $state<Partial<Record<DetectorName, boolean>>>({})

  // Local editable copies, keyed by detector name -- edits don't touch
  // detectorSettingsState.list until Save, so closing without saving
  // discards them.
  let drafts = $state<Record<string, { hosts: string; ports: string; rules: string; hostsMode: ListMode; portsMode: ListMode; rulesMode: ListMode; classification: DetectorScope['classification'] }>>({})

  function draftFor(name: DetectorName) {
    const existing = detectorSettingsState.list.find((d) => d.name === name)
    const sc = existing?.scope ?? {}
    return {
      hosts: (sc.hosts ?? []).join(', '),
      ports: (sc.ports ?? []).join(', '),
      rules: (sc.rules ?? []).join(', '),
      hostsMode: sc.hostsMode ?? '',
      portsMode: sc.portsMode ?? '',
      rulesMode: sc.rulesMode ?? '',
      classification: sc.classification ?? '',
    }
  }

  function toggleExpanded(name: DetectorName) {
    if (expanded === name) {
      expanded = null
      return
    }
    drafts[name] = draftFor(name)
    expanded = name
  }

  function parseList(v: string): string[] {
    return v
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0)
  }

  function parsePorts(v: string): number[] {
    return parseList(v)
      .map((s) => Number(s))
      .filter((n) => Number.isInteger(n) && n > 0)
  }

  async function toggleEnabled(name: DetectorName, enabled: boolean, scope: DetectorScope) {
    saving[name] = true
    const err = await detectorSettingsState.update(name, enabled, scope)
    saving[name] = false
    errors[name] = err ?? undefined
  }

  async function saveScope(name: DetectorName) {
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
    if (!err) expanded = null
  }

  function close() {
    detectorSettingsState.open = false
    expanded = null
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') close()
  }

  function onBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) close()
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if detectorSettingsState.open}
  <div class="backdrop" onclick={onBackdropClick} role="presentation">
    <div class="modal" role="dialog" aria-modal="true" aria-label="Detector settings" tabindex="-1">
      <div class="modal-header">
        <span class="title">Detectors</span>
        <button class="close" onclick={close} aria-label="Close detector settings">✕</button>
      </div>

      <ul class="list scrollbar">
        {#each detectorSettingsState.list as d (d.name)}
          <li class="row">
            <div class="row-main">
              <label class="switch">
                <input
                  type="checkbox"
                  checked={d.enabled}
                  disabled={saving[d.name]}
                  onchange={(e) => toggleEnabled(d.name, e.currentTarget.checked, d.scope)}
                />
                <span class="name">{LABELS[d.name]}</span>
              </label>
              {#if SCOPE_FIELDS[d.name].length > 0}
                <button class="scope-toggle" onclick={() => toggleExpanded(d.name)}>
                  {expanded === d.name ? 'Hide scope' : 'Edit scope'}
                </button>
              {/if}
            </div>

            {#if errors[d.name]}
              <p class="error">{errors[d.name]}</p>
            {/if}

            {#if expanded === d.name}
              <div class="scope-form">
                {#if SCOPE_FIELDS[d.name].includes('hosts')}
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
                {#if SCOPE_FIELDS[d.name].includes('classification')}
                  <label class="field">
                    <span>Source classification</span>
                    <select bind:value={drafts[d.name].classification}>
                      <option value="">any</option>
                      <option value="internal">internal only</option>
                      <option value="external">external only</option>
                    </select>
                  </label>
                {/if}
                {#if SCOPE_FIELDS[d.name].includes('ports')}
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
                {#if SCOPE_FIELDS[d.name].includes('rules')}
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
                <div class="scope-actions">
                  <button class="cancel" onclick={() => (expanded = null)}>Cancel</button>
                  <button class="save" disabled={saving[d.name]} onclick={() => saveScope(d.name)}>
                    {saving[d.name] ? 'Saving…' : 'Save scope'}
                  </button>
                </div>
              </div>
            {/if}
          </li>
        {/each}
      </ul>
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 5vh 4vw;
    z-index: 50;
  }

  .modal {
    width: 100%;
    height: 100%;
    max-width: 640px;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 10px;
    display: flex;
    flex-direction: column;
    min-height: 0;
    box-shadow: 0 24px 60px -12px rgba(0, 0, 0, 0.5);
    overflow: hidden;
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-elevated);
    flex: none;
  }

  .title {
    font-size: 14px;
    font-weight: 600;
    color: var(--fg);
  }

  .close {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    width: 28px;
    height: 28px;
    font-size: 13px;
    line-height: 1;
  }

  .close:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    list-style: none;
    margin: 0;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .row {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 10px 12px;
  }

  .row-main {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }

  .switch {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
  }

  .name {
    font-size: 13px;
    color: var(--fg);
  }

  .scope-toggle {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 4px 10px;
    font-size: 12px;
  }

  .scope-toggle:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .error {
    margin: 6px 0 0;
    color: var(--reject);
    font-size: 12px;
  }

  .scope-form {
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 5px;
    font-size: 12px;
    color: var(--fg-muted);
  }

  .field-row {
    display: flex;
    gap: 8px;
  }

  input,
  select {
    background: var(--bg);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 6px 8px;
    font-size: 13px;
  }

  input {
    flex: 1;
    min-width: 0;
  }

  input:focus,
  select:focus {
    outline: none;
    border-color: var(--accent);
  }

  .scope-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  .cancel,
  .save {
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
</style>
