<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Step 3 · Paste once, and the router's turn (DESIGN.md, "The steps,
  // in detail"). A stub: the step issue under #1374 ports the
  // prototype's pasteBody (the block in a folded pre, Copy, the token
  // line with Reroll, the waiting observation) and watchBody (the
  // track from track.js, the latest arrival, the refused-sender and
  // ahead-of-review boxes, the stream of lines).
  //
  // What the shell already provides: wizardRun.markCopied() starts the
  // router's turn; wizardRun.evidence carries every proof the track
  // and the observation line read; the footer's Next is disabled until
  // everything chosen has arrived (wizardRun.arrivedAll).

  import { wizardRun } from '../../lib/wizardRun.svelte'

  const arrived = $derived(wizardRun.arrivedAll)
</script>

{#if wizardRun.stage === 'paste'}
  <h3>One paste.</h3>
  <p class="hint">
    The block, in order, and the enrol line last. Into the router’s <b>terminal</b> (WinBox ▸ New Terminal, or ssh), not
    a script.
  </p>
  <div class="obs waiting">Nothing has arrived yet. Copy, paste, and this line will say what the router did.</div>
{:else if arrived}
  <h3>{wizardRun.name} is sending.</h3>
  <p class="hint">Everything you chose has arrived; each station stands on evidence.</p>
{:else}
  <h3>The router’s turn.</h3>
  <p class="hint">
    The block is on the router. Each part answers in its own time — every station that lights is something that
    arrived.
  </p>
  <div class="obs waiting">Copied. Waiting for the router — the certificate fetch comes first.</div>
{/if}
