<!-- SPDX-License-Identifier: AGPL-3.0-only -->
<script lang="ts">
  // A dev-only contact sheet for the city's device library (#864): every
  // symbol, at the three city camera stops, resting and flagged.
  //
  // It exists to answer the two questions the issue's done-when asks --
  // does each symbol read as its type at street, and as a distinct
  // silhouette at city -- without needing a whole city to look at one.
  // It is deliberately not reachable from the app: nothing links here,
  // and its only entry point is dev/city-devices.html, which is not an
  // input to the production build. The reviewed captures are
  // docs/design/screens/city/devices-{city,district,street}.png.
  import CityDeviceDefs from './CityDeviceDefs.svelte'
  import {
    DEVICE_KINDS,
    DEVICE_KIND_LABEL,
    IK,
    SREF,
    VK,
    deviceScale,
    deviceStampAttrs,
    deviceTop,
  } from '../lib/city/devices'

  // The camera scale at each stop, taken from round 40's scenes: survey
  // S 5.9, a gated community around S 11, street S 17.
  interface Band {
    id: string
    stop: string
    S: number
    cols: number
    note: string
  }

  const BANDS: Band[] = [
    {
      id: 'city',
      stop: 'city',
      S: 5.9,
      cols: 11,
      note: 'the whole estate from above — each shape has to hold as a silhouette',
    },
    {
      id: 'district',
      stop: 'district',
      S: 11,
      cols: 6,
      note: 'one gated community and its gates',
    },
    {
      id: 'street',
      stop: 'street',
      S: 17,
      cols: 6,
      note: 'buildings with their labels — each shape has to read as its own type',
    },
  ]

  // One ordinary host's footprint. Routers and gateways get larger ones
  // in the real city; the point here is comparing shapes, not sizes.
  const FOOTPRINT = 4.6

  // The district inks the app already owns, cycled so it is visible that
  // one symbol serves every VLAN.
  const INKS = [
    'var(--lane-lan)',
    'var(--lane-srv)',
    'var(--lane-iot)',
    'var(--lane-guest)',
    'var(--accent)',
  ]

  const LU = IK * SREF
  const LV = VK * SREF
  const TALLEST = Math.max(...DEVICE_KINDS.map((k) => deviceTop(k)))

  interface Sheet {
    prefix: string
    width: number
    height: number
    dw: number
    dh: number
    scale: number
    cells: { kind: (typeof DEVICE_KINDS)[number]; x: number; y: number; ink: string; name: string }[]
  }

  function sheet(band: Band, flagged: boolean): Sheet {
    const scale = deviceScale(FOOTPRINT, band.S)
    const dw = (scale * LU) / 0.74
    const dh = (scale * LV) / 0.74
    const cellW = Math.round(2 * dw + 30)
    const above = TALLEST * scale + 16
    const rowH = Math.round(above + dh + 34)
    const rows = Math.ceil(DEVICE_KINDS.length / band.cols)
    return {
      prefix: `dl-${band.id}-${flagged ? 'flag' : 'rest'}`,
      width: cellW * band.cols,
      height: rowH * rows,
      dw,
      dh,
      scale,
      cells: DEVICE_KINDS.map((kind, i) => ({
        kind,
        x: (i % band.cols) * cellW + cellW / 2,
        y: Math.floor(i / band.cols) * rowH + above,
        ink: INKS[i % INKS.length],
        name: DEVICE_KIND_LABEL[kind] + (flagged ? ', flagged' : ''),
      })),
    }
  }
</script>

<div class="gallery">
  <header>
    <h1>The city — device library</h1>
    <p>
      Eleven symbols, drawn once for a footprint of radius 1 and stamped at each camera stop. The
      body is the district's ink; lights, lenses and screens are the signal ink, which a flagged
      device switches to the alarm ink. A wrong shape here is a labelling defect, never a data
      claim.
    </p>
  </header>

  {#each BANDS as band (band.id)}
    <section data-band={band.id}>
      <h2>{band.stop}</h2>
      <p class="note">{band.note}</p>
      {#each [false, true] as flagged (flagged)}
        {#if flagged}<h3>flagged</h3>{/if}
        {@const s = sheet(band, flagged)}
        <div class="sheet">
          <svg
            width={s.width}
            height={s.height}
            viewBox="0 0 {s.width} {s.height}"
            role="list"
            aria-label="{band.stop} stop, {flagged ? 'flagged' : 'resting'}"
          >
            <CityDeviceDefs prefix={s.prefix} />
            {#each s.cells as cell (cell.kind)}
              <g role="listitem" aria-label={cell.name}>
                <title>{cell.name}</title>
                <!-- The footprint the symbol was drawn for. The plinth
                     that really carries it is #867's, not this slice's. -->
                <path
                  transform="translate({cell.x} {cell.y})"
                  d="M0 {-s.dh}L{s.dw} 0L0 {s.dh}L{-s.dw} 0Z"
                  fill="var(--accent)"
                  fill-opacity="0.06"
                  stroke="var(--accent)"
                  stroke-opacity="0.28"
                  stroke-width="1"
                />
                <use
                  {...deviceStampAttrs(cell.kind, s.prefix, {
                    ink: cell.ink,
                    flagged,
                    scale: s.scale,
                    x: cell.x,
                    y: cell.y,
                  })}
                />
                <text x={cell.x} y={cell.y + s.dh + 20} text-anchor="middle" class="cap">
                  {cell.kind}
                </text>
              </g>
            {/each}
          </svg>
        </div>
      {/each}
    </section>
  {/each}
</div>

<style>
  .gallery {
    padding: 24px 28px 48px;
    background: var(--bg);
    color: var(--fg);
  }

  header p {
    max-width: 62ch;
    color: var(--fg-muted);
    font-size: 0.85rem;
    line-height: 1.5;
  }

  h1 {
    font-size: 1.1rem;
    margin: 0 0 8px;
  }

  h2 {
    font-size: 0.72rem;
    letter-spacing: 0.16em;
    text-transform: uppercase;
    color: var(--accent);
    margin: 0 0 4px;
  }

  h3 {
    font-size: 0.62rem;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-muted);
    margin: 14px 0 4px;
  }

  section {
    margin-top: 30px;
    padding-top: 18px;
    border-top: 1px solid var(--border);
  }

  .note {
    color: var(--fg-muted);
    font-size: 0.78rem;
    margin: 0 0 10px;
  }

  .sheet {
    overflow-x: auto;
  }

  .cap {
    fill: var(--fg-muted);
    font-size: 10px;
    font-family: var(--font-mono, ui-monospace, monospace);
    letter-spacing: 0.04em;
  }
</style>
