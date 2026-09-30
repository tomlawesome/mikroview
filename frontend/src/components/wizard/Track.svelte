<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The router's turn track (DESIGN.md, "The router's turn"; #1383),
  // ported from docs/design/screens/wizard/round-15/track.js's render():
  // one wire from Copy to Done, a station per proof, the wire's segments
  // colouring up to the last arrival. CSS is wizard.css's already-ported
  // .track rules (the ink stays --accept throughout -- round 9's "the
  // track alone stays green").
  //
  // track.js travels its amber NOW cursor a fraction of the way to the
  // next station, timed from the scenario's own scripted arrival times.
  // MikroView has no such expected-arrival instant for a real router --
  // the claim ledger never estimates what has not happened -- so this
  // places the cursor a fixed, small step past the last arrival instead
  // of animating a fraction it cannot honestly compute.

  import type { TrackStation } from '../../lib/wizardRun'

  const { stations, compact = false }: { stations: TrackStation[]; compact?: boolean } = $props()

  const n = $derived(stations.length)
  const lastDone = $derived(stations.reduce((a, x, i) => (x.state === 'done' ? i : a), -1))
  const centre = (i: number) => ((i + 0.5) / n) * 100
  const segs = $derived(
    Array.from({ length: Math.max(0, lastDone) }, (_, i) => ({
      left: centre(i),
      width: centre(i + 1) - centre(i),
    })),
  )
  const nowLeft = $derived(
    lastDone >= 0 && lastDone < n - 1 ? centre(lastDone) + (centre(lastDone + 1) - centre(lastDone)) * 0.12 : null,
  )
</script>

<div class="track" class:compact aria-label="What has arrived">
  <div class="wire"></div>
  {#each segs as seg, i (i)}
    <div class="seg" style:left="{seg.left}%" style:width="{seg.width}%"></div>
  {/each}
  {#if nowLeft !== null}
    <div class="now" style:left="calc({nowLeft}% - 1px)"></div>
  {/if}
  {#each stations as x (x.id)}
    <div class="stn {x.state}">
      <div class="dot"></div>
      <div class="lab">{x.lab}</div>
      {#if !compact}
        <div class="st">{x.st}</div>
      {/if}
    </div>
  {/each}
</div>
