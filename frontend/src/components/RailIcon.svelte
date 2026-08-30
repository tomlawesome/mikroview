<script module lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The rail's icon set (#545, under #486). The design record is
  // deliberately silent here -- "Icon glyphs in the mockups are
  // placeholders; the icon set is an implementation asset, not part of
  // this record" (docs/design/screens/navigation/DESIGN.md) -- so this
  // file is execution, not design ratification.
  //
  // One 16x16 stroked set rather than a mix: at the 54px icons density
  // the glyph carries the whole row on its own, and solid shapes at that
  // size read as blobs. Everything inherits currentColor so the rail's
  // own hover/current/broken states need no icon-specific rules.
  //
  // The type lives in a module script because a type exported from the
  // instance script is not a module export in Svelte 5 -- importers get
  // a missing-export error rather than the union.

  export type IconName =
    | 'fall'
    | 'map'
    | 'stream'
    | 'metrics'
    | 'audit'
    | 'flags'
    | 'watchlist'
    | 'engineroom'
    | 'fleet'
    | 'entities'
    | 'setup'
    | 'account'
    | 'about'
    | 'density-wide'
    | 'density-narrow'
    | 'dock'
</script>

<script lang="ts">
  let { name }: { name: IconName } = $props()
</script>

<svg
  class="icon"
  viewBox="0 0 16 16"
  fill="none"
  stroke="currentColor"
  stroke-width="1.5"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
  focusable="false"
>
  {#if name === 'fall'}
    <!-- Time pouring downward past a band line -- the fall's own shape,
         distinct from 'stream's horizontal rows. -->
    <path d="M3 2.5v3M8 2.5v3M13 2.5v3" />
    <path d="M2 7h12" />
    <path d="M3 9v4.5M8 9v4.5M13 9v3" />
  {:else if name === 'map'}
    <!-- The topography's own shape: the router above, ribs down to the
         lanes -- dots-and-ribs, matching the shelf's mini. -->
    <circle cx="8" cy="3.5" r="2" />
    <path d="M7 5.5 C 5 8.5, 3.5 9.5, 3 12" />
    <path d="M8 5.5 V 12" />
    <path d="M9 5.5 C 11 8.5, 12.5 9.5, 13 12" />
    <circle cx="3" cy="13" r="1" />
    <circle cx="8" cy="13" r="1" />
    <circle cx="13" cy="13" r="1" />
  {:else if name === 'stream'}
    <path d="M2 4h12M2 8h8M2 12h10" />
  {:else if name === 'metrics'}
    <path d="M3 13.5V8M8 13.5V2.5M13 13.5V6" />
  {:else if name === 'audit'}
    <path d="M3.5 1.75h5.5l3.5 3.5v9h-9z" />
    <path d="M9 1.75v3.5h3.5" />
    <path d="M5.75 9h4.5M5.75 11.5h3" />
  {:else if name === 'flags'}
    <path d="M4 14.5V2" />
    <path d="M4 2.5h7.75l-1.75 2.75 1.75 2.75H4" />
  {:else if name === 'watchlist'}
    <path d="M1.5 8S4 3.5 8 3.5 14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8z" />
    <circle cx="8" cy="8" r="1.75" />
  {:else if name === 'engineroom'}
    <!-- A gear: the engine room's own glyph (#490) -- distinct from
         'setup' (the wrench-ish "Run setup…" action) so the two Admin
         rows never read as the same row twice. -->
    <circle cx="8" cy="8" r="2.25" />
    <path
      d="M8 2.25v1.7M8 12.05v1.7M13.75 8h-1.7M3.95 8h-1.7M12.16 3.84l-1.2 1.2M5.04 10.96l-1.2 1.2M12.16 12.16l-1.2-1.2M5.04 5.04l-1.2-1.2"
    />
  {:else if name === 'fleet'}
    <rect x="2" y="3" width="12" height="4" rx="1.2" />
    <rect x="2" y="9" width="12" height="4" rx="1.2" />
    <circle cx="4.6" cy="5" r="0.7" fill="currentColor" stroke="none" />
    <circle cx="4.6" cy="11" r="0.7" fill="currentColor" stroke="none" />
  {:else if name === 'entities'}
    <path d="M2.25 2.25h5.4l6.1 6.1-5.4 5.4-6.1-6.1z" />
    <circle cx="5.4" cy="5.4" r="1.1" />
  {:else if name === 'setup'}
    <path d="M13.6 8a5.6 5.6 0 1 1-1.9-4.2" />
    <path d="M13.9 2.1v3.2h-3.2" />
  {:else if name === 'account'}
    <circle cx="8" cy="5.25" r="2.75" />
    <path d="M2.5 14.25c0-3 2.5-5.25 5.5-5.25s5.5 2.25 5.5 5.25" />
  {:else if name === 'about'}
    <circle cx="8" cy="8" r="6.25" />
    <path d="M8 7.25v4M8 4.75v.01" />
    <!-- Density and dock sit next to each other in the footer, so they
         must not both read as "an arrow pointing at a bar". Density is a
         panel whose sidebar column changes width; dock is the arrow. -->
  {:else if name === 'density-wide'}
    <rect x="1.75" y="3" width="12.5" height="10" rx="1.5" />
    <path d="M7 3v10" />
    <path d="M3.4 6.25h2.2M3.4 9.75h2.2" />
  {:else if name === 'density-narrow'}
    <rect x="1.75" y="3" width="12.5" height="10" rx="1.5" />
    <path d="M5 3v10" />
    <path d="M3.4 8h0.01" />
  {:else if name === 'dock'}
    <path d="M2.25 2.75v10.5" />
    <path d="M14 8H6.5M9.5 4.75 6.25 8l3.25 3.25" />
  {/if}
</svg>

<style>
  .icon {
    width: 16px;
    height: 16px;
    flex: 0 0 16px;
  }
</style>
