<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // "LIVE · 34/s" is ratified as one thing (#683, round 29): live state
  // and the arriving rate together, not a separate "17/s" reading
  // elsewhere on the bar -- on six of the seven scenes. The stream's own
  // bar (#s5) draws bare "LIVE", no rate: the whisper line right below
  // it already carries "34/s now", so `showRate` lets SceneBar suppress
  // the duplicate there without a second component. Non-open states
  // (still real -- see ws.ts's reconnect handling) keep their own words
  // rather than pretending to be live.
  import { appState } from '../lib/state.svelte'
  import { formatEps } from '../lib/format'

  let { showRate = true }: { showRate?: boolean } = $props()

  const labels = {
    connecting: 'Connecting…',
    open: 'LIVE',
    closed: 'Disconnected',
  }

  const rate = $derived(
    showRate && appState.connState === 'open' && appState.stats
      ? `${formatEps(appState.stats.eventsPerSecond)}/s`
      : '',
  )
</script>

<span class="conn conn-{appState.connState}" title={rate ? `Live -- ${rate} arriving` : undefined}>
  <span class="dot"></span>
  {labels[appState.connState]}{rate ? ` · ${rate}` : ''}
</span>

<style>
  .conn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    font-weight: 500;
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
  }

  .conn-open {
    color: var(--accept-tinted);
  }
  .conn-open .dot {
    background: var(--accept-tinted);
    box-shadow: 0 0 6px var(--accept-tinted);
  }

  .conn-connecting {
    color: var(--drop-tinted);
  }
  .conn-connecting .dot {
    background: var(--drop-tinted);
    animation: pulse 1s ease-in-out infinite;
  }

  .conn-closed {
    color: var(--reject-tinted);
  }
  .conn-closed .dot {
    background: var(--reject-tinted);
  }

  @keyframes pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.3;
    }
  }
</style>
