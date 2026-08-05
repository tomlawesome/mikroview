<script lang="ts">
  // The reputation-info rows shared by IpLookupPopover (a live, on-demand
  // lookup) and Flags.svelte's expanded detail (a snapshot captured at
  // raise time) -- same ReputationResult shape either way, so the
  // rendering only needs to live in one place.
  import type { ReputationResult } from '../lib/types'

  let { result }: { result: ReputationResult } = $props()

  const hasIntel = $derived(
    result.abuseScore != null ||
      result.totalReports != null ||
      !!result.isp ||
      !!result.countryCode ||
      !!result.ports?.length ||
      !!result.hostnames?.length ||
      !!result.vulns?.length,
  )
</script>

{#if !hasIntel}
  <div class="status">No intel found for this IP</div>
{:else}
  <div class="rows">
    {#if result.abuseScore != null}
      <div class="row">
        <span class="label">Abuse score</span>
        <span class="value" class:high={result.abuseScore >= 50}>{result.abuseScore}/100</span>
      </div>
    {/if}
    {#if result.totalReports != null}
      <div class="row">
        <span class="label">Reports</span>
        <span class="value">{result.totalReports}</span>
      </div>
    {/if}
    {#if result.isp}
      <div class="row">
        <span class="label">ISP</span>
        <span class="value">{result.isp}</span>
      </div>
    {/if}
    {#if result.countryCode}
      <div class="row">
        <span class="label">Country</span>
        <span class="value">{result.countryCode}</span>
      </div>
    {/if}
    {#if result.ports?.length}
      <div class="row">
        <span class="label">Open ports</span>
        <span class="value">{result.ports.join(', ')}</span>
      </div>
    {/if}
    {#if result.hostnames?.length}
      <div class="row">
        <span class="label">Hostnames</span>
        <span class="value">{result.hostnames.join(', ')}</span>
      </div>
    {/if}
    {#if result.vulns?.length}
      <div class="row">
        <span class="label">Vulns</span>
        <span class="value">{result.vulns.join(', ')}</span>
      </div>
    {/if}
  </div>
{/if}

<style>
  .status {
    color: var(--fg-dim);
    padding: 4px 0;
    font-size: 13px;
  }

  .rows {
    display: flex;
    flex-direction: column;
    gap: 5px;
    font-size: 13px;
  }

  .row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 10px;
  }

  .label {
    color: var(--fg-muted);
    flex: none;
  }

  .value {
    color: var(--fg);
    text-align: right;
    overflow-wrap: anywhere;
  }

  .value.high {
    color: var(--reject);
    font-weight: 600;
  }
</style>
