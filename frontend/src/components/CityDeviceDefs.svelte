<!-- SPDX-License-Identifier: AGPL-3.0-only -->
<script lang="ts">
  // The city's device library as an SVG <defs> block (#864): every
  // symbol once, under this scene's own prefix, so a <use> never has to
  // reach across SVG roots (which does not work).
  //
  // Every city SVG puts one of these inside itself and then stamps
  // devices with <use {...deviceStampAttrs(kind, prefix, ...)} />.
  import { DEVICE_KINDS, DEVICE_LIBRARY, deviceSymbolId } from '../lib/city/devices'

  interface Props {
    /** Unique per SVG root, so two scenes on a page cannot collide. */
    prefix: string
  }

  const { prefix }: Props = $props()
</script>

<defs>
  {#each DEVICE_KINDS as kind (kind)}
    <g id={deviceSymbolId(prefix, kind)}>
      {#each DEVICE_LIBRARY[kind].parts as part, i (i)}
        {#if part.shape === 'path'}
          <path
            d={part.d}
            fill={part.fill}
            fill-opacity={part.fillOpacity}
            stroke={part.stroke}
            stroke-opacity={part.strokeOpacity}
            stroke-width={part.strokeWidth}
            stroke-linecap={part.strokeLinecap}
          />
        {:else if part.shape === 'circle'}
          <circle
            cx={part.cx}
            cy={part.cy}
            r={part.r}
            fill={part.fill}
            fill-opacity={part.fillOpacity}
            stroke={part.stroke}
            stroke-opacity={part.strokeOpacity}
            stroke-width={part.strokeWidth}
          />
        {:else}
          <ellipse
            cx={part.cx}
            cy={part.cy}
            rx={part.rx}
            ry={part.ry}
            fill={part.fill}
            fill-opacity={part.fillOpacity}
            stroke={part.stroke}
            stroke-opacity={part.strokeOpacity}
            stroke-width={part.strokeWidth}
          />
        {/if}
      {/each}
    </g>
  {/each}
</defs>
