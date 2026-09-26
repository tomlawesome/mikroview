<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // #1373: what else this router last reported sending logs to
  // MikroView from -- another action, or a RouterOS built-in (memory,
  // remote, disk, echo) repointed here -- left running by a setup
  // MikroView's own wizard has since moved past. Shared by Fleet.svelte
  // and Entities.svelte's two router-card loops so the description and
  // the fix commands cannot read differently on one card than another
  // (the same reason lib/fleet.ts itself exists).
  //
  // canFix gates the paste-ready commands, not the description: the
  // fact that a router is still receiving logs from an old action is
  // true whoever is looking, but only an admin can act on it -- the
  // same admin-only line Re-enrol… already draws on these cards.
  import type { LoggingLeftover } from '../lib/types'
  import { leftoverFixCommands } from '../lib/fleet'
  import CopyButton from './CopyButton.svelte'

  let { leftovers, canFix }: { leftovers: LoggingLeftover[]; canFix: boolean } = $props()
</script>

{#if leftovers.length > 0}
  {#each leftovers as l (l.name)}
    <div class="frow dim">{l.description}</div>
  {/each}
  {#if canFix}
    <div class="leftover-fix">
      <pre class="mono scrollbar">{leftoverFixCommands(leftovers)}</pre>
      <div class="leftover-fix-actions">
        <span class="dim">Paste into the router's terminal</span>
        <CopyButton value={leftoverFixCommands(leftovers)} label="the logging cleanup commands" />
      </div>
    </div>
  {/if}
{/if}

<style>
  .leftover-fix {
    margin-top: 2px;
  }

  .leftover-fix pre {
    margin: 0;
    padding: 6px 8px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 4px;
    font-size: 0.72rem;
    line-height: 1.35;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 120px;
    overflow-y: auto;
  }

  .leftover-fix-actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
    margin-top: 2px;
    font-size: 0.72rem;
    color: var(--fg-dim);
  }
</style>
