<!-- SPDX-License-Identifier: AGPL-3.0-only -->
<script lang="ts">
  // Uptime's home is the account menu's foot, beside the version:
  // "0.9 · AGPL-3.0 · up 12 d 4 h" (round 37's account menu, accepted by
  // the owner 2026-09-02; drawn in round-38's the-whole.html as
  // `.whomenu .ver`). It is mikroview's own fact about itself, which is
  // what the About row already carries -- not a status readout the scene
  // bar has to keep showing, which is where #444 had put it and where
  // round 30 then drew nothing, leaving this component mounted nowhere.
  //
  // Days and hours only: "a ticking second is a clock, not a fact". The
  // counter in uptime.svelte.ts still advances every second -- this reads
  // just the two units off it, so the string changes once an hour and a
  // menu held open does not twitch.
  import { uptimeState } from '../lib/uptime.svelte'
  import { formatUptimeDaysHours } from '../lib/format'

  uptimeState.start()
</script>

{#if uptimeState.seconds !== null}
  <span class="uptime" title="How long the mikroview server has been running (since its last restart)"
    >up {formatUptimeDaysHours(uptimeState.seconds)}</span
  >
{/if}

<style>
  /* Font and colour are the foot's, not this component's: the drawing
     draws one `.ver` line and this is the tail of it, so it inherits
     rather than restating the type. */
  .uptime {
    white-space: nowrap;
  }
</style>
