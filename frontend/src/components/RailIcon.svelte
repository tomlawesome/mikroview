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
    | 'stream'
    | 'metrics'
    | 'audit'
    | 'flags'
    | 'detectors'
    | 'watchlist'
    | 'users'
    | 'tokens'
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
  {#if name === 'stream'}
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
  {:else if name === 'detectors'}
    <circle cx="8" cy="8" r="1.4" fill="currentColor" stroke="none" />
    <path d="M5.2 10.8a4 4 0 0 1 0-5.6M10.8 5.2a4 4 0 0 1 0 5.6" />
    <path d="M3.1 12.9a7 7 0 0 1 0-9.8M12.9 3.1a7 7 0 0 1 0 9.8" />
  {:else if name === 'watchlist'}
    <path d="M1.5 8S4 3.5 8 3.5 14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8z" />
    <circle cx="8" cy="8" r="1.75" />
  {:else if name === 'users'}
    <circle cx="6" cy="5.5" r="2.5" />
    <path d="M1.5 14c0-2.5 2-4.5 4.5-4.5s4.5 2 4.5 4.5" />
    <path d="M11 3.6a2.4 2.4 0 0 1 0 3.8M12.4 9.8A4.6 4.6 0 0 1 14.5 14" />
  {:else if name === 'tokens'}
    <circle cx="5" cy="8" r="3" />
    <path d="M8 8h6.5M12.6 8v2.4M10.6 8v1.8" />
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
