<script lang="ts">
  // SSH/Telnet/control-port tracking tab (issue #34): generalizes to
  // whichever ports internal/detect.Config.CriticalPorts is actually
  // configured with (see lib/criticalPorts.svelte.ts), not hardcoded to
  // just SSH/Telnet. Deliberately independent of the live view's global
  // FilterBar/appState.filters -- same reasoning CustomTopTalkers
  // widgets already use their own filter objects -- since this tab's
  // "destination port is any one of several configured control ports"
  // match is an OR appState.filters.port (a single value) can't express.
  import { appState } from '../lib/state.svelte'
  import { criticalPortsState } from '../lib/criticalPorts.svelte'
  import { isPublicIp } from '../lib/format'
  import { topNBy } from '../lib/topN'
  import BarList from './BarList.svelte'
  import LiveTable from './LiveTable.svelte'

  criticalPortsState.ensureLoaded()

  let scopeFilter = $state<'any' | 'internal' | 'external'>('any')
  let actionFilter = $state<'any' | 'accepted' | 'denied'>('any')

  // Mirrors internal/detect.isTrackableConnState -- without it, a busy
  // accepted service's own return traffic would swamp this table the
  // same way it would the fast port-scan detector. Mirrors
  // critical_port's own DstPort match: an *attempt against* a control
  // port, not any traffic that happens to touch one on either side.
  const controlPortEvents = $derived(
    appState.ageFilteredEvents().filter((e) => {
      if (!e.dstPort || !criticalPortsState.ports.includes(e.dstPort)) return false
      if (e.connState && e.connState !== 'new') return false
      if (scopeFilter !== 'any') {
        if (!e.srcIp) return false
        const external = isPublicIp(e.srcIp)
        if (scopeFilter === 'internal' && external) return false
        if (scopeFilter === 'external' && !external) return false
      }
      if (actionFilter === 'accepted' && e.action !== 'accept') return false
      if (actionFilter === 'denied' && e.action !== 'drop' && e.action !== 'reject') return false
      return true
    }),
  )

  const TOP_N = 10
  const portRows = $derived(topNBy(controlPortEvents, (e) => (e.dstPort ? String(e.dstPort) : undefined), TOP_N))
  const sourceRows = $derived(topNBy(controlPortEvents, (e) => e.srcIp, TOP_N))
</script>

<div class="control-ports">
  <div class="toolbar-row">
    <div class="toggle-group" role="group" aria-label="Filter by source scope">
      <button class:active={scopeFilter === 'any'} onclick={() => (scopeFilter = 'any')}>All sources</button>
      <button class:active={scopeFilter === 'internal'} onclick={() => (scopeFilter = 'internal')}>Internal</button>
      <button class:active={scopeFilter === 'external'} onclick={() => (scopeFilter = 'external')}>External</button>
    </div>
    <div class="toggle-group" role="group" aria-label="Filter by outcome">
      <button class:active={actionFilter === 'any'} onclick={() => (actionFilter = 'any')}>All attempts</button>
      <button class:active={actionFilter === 'accepted'} onclick={() => (actionFilter = 'accepted')}>Accepted</button>
      <button class:active={actionFilter === 'denied'} onclick={() => (actionFilter = 'denied')}>Denied</button>
    </div>
    <span class="count">{controlPortEvents.length} attempt{controlPortEvents.length === 1 ? '' : 's'}</span>
  </div>

  <div class="summary">
    <BarList title="Attempts by port" rows={portRows} emptyMessage="No control-port attempts observed yet" />
    <BarList title="Top sources" rows={sourceRows} emptyMessage="No control-port attempts observed yet" />
  </div>

  <LiveTable events={controlPortEvents} emptyMessage="No control-port attempts observed yet." />
</div>

<style>
  .control-ports {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 10px;
    min-height: 0;
  }

  .toolbar-row {
    display: flex;
    align-items: center;
    gap: 14px;
    flex-wrap: wrap;
  }

  .toggle-group {
    display: flex;
    gap: 6px;
  }

  .toggle-group button {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 6px 11px;
    font-size: 12px;
  }

  .toggle-group button:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .toggle-group button.active {
    color: var(--accent);
    border-color: var(--accent);
    background: var(--accent-bg);
  }

  .count {
    margin-left: auto;
    font-size: 12px;
    color: var(--fg-dim);
    font-variant-numeric: tabular-nums;
  }

  .summary {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: 10px;
  }
</style>
