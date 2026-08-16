<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  import type { Action } from '../lib/types'

  let { action }: { action: Action } = $props()

  // MARKED/NATTED spell out the raw value rather than abbreviating to
  // MARK/NAT: "NAT" is already the label on the translated-address badge
  // beside this one, and two different meanings on one row is worse than
  // a slightly longer pill. Both are the same width as REJECT, so the
  // column does not change shape.
  const labels: Record<Action, string> = {
    accept: 'ACCEPT',
    drop: 'DROP',
    reject: 'REJECT',
    log: 'LOG',
    marked: 'MARKED',
    natted: 'NATTED',
    unknown: '?',
  }
</script>

<span class="badge badge-{action}">{labels[action] ?? action}</span>

<style>
  .badge {
    display: inline-block;
    font-family: var(--font-mono);
    font-size: 12.5px;
    font-weight: 700;
    letter-spacing: 0.03em;
    padding: 3px 8px;
    border-radius: 4px;
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

  .badge-log {
    color: var(--log);
    background: var(--log-bg);
  }

  .badge-marked {
    color: var(--marked);
    background: var(--marked-bg);
  }

  .badge-natted {
    color: var(--natted);
    background: var(--natted-bg);
  }

  .badge-unknown {
    color: var(--unknown);
    background: var(--unknown-bg);
  }
</style>
