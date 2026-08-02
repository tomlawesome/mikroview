<script lang="ts">
  import { appState } from '../lib/state.svelte'

  const labels = {
    connecting: 'Connecting…',
    open: 'Live',
    closed: 'Disconnected',
  }
</script>

<span class="conn conn-{appState.connState}">
  <span class="dot"></span>
  {labels[appState.connState]}
</span>

<style>
  .conn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    font-weight: 500;
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
  }

  .conn-open {
    color: var(--accept);
  }
  .conn-open .dot {
    background: var(--accept);
    box-shadow: 0 0 6px var(--accept);
  }

  .conn-connecting {
    color: var(--drop);
  }
  .conn-connecting .dot {
    background: var(--drop);
    animation: pulse 1s ease-in-out infinite;
  }

  .conn-closed {
    color: var(--reject);
  }
  .conn-closed .dot {
    background: var(--reject);
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
