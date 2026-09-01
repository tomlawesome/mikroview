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
  /* #685: the ratified round-29 table (the-whole.html, #s5) draws these
     small and flat -- `.badge { font: 700 10px var(--mono); border-radius:
     4px; padding: 0 6px; letter-spacing: 0.06em; }` -- against rows that
     "go quiet, no full-row action washes". This was bigger (12.5px, 3px
     8px padding) and read as the loudest thing on the page; matched to
     the mockup's own numbers. Colors are untouched -- the accept/drop/
     reject/nat tokens already carry the same 0.12-alpha wash the mockup
     uses. */
  .badge {
    display: inline-block;
    font: 700 10px var(--font-mono);
    letter-spacing: 0.06em;
    padding: 0 6px;
    border-radius: 4px;
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
