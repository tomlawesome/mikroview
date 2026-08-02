<script lang="ts">
  import type { Action } from '../lib/types'

  let { action }: { action: Action } = $props()

  const labels: Record<Action, string> = {
    accept: 'ACCEPT',
    drop: 'DROP',
    reject: 'REJECT',
    log: 'LOG',
    unknown: '?',
  }
</script>

<span class="badge badge-{action}">{labels[action] ?? action}</span>

<style>
  .badge {
    display: inline-block;
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.03em;
    padding: 2px 7px;
    border-radius: 3px;
    line-height: 1.5;
    white-space: nowrap;
  }

  .badge-accept {
    color: var(--accept);
    background: var(--accept-bg);
  }

  .badge-drop {
    color: var(--drop);
    background: var(--drop-bg);
  }

  .badge-reject {
    color: var(--reject);
    background: var(--reject-bg);
  }

  .badge-log,
  .badge-unknown {
    color: var(--unknown);
    background: var(--unknown-bg);
  }
</style>
