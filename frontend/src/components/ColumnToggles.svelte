<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // #729/#1197's column-chooser checkbox list: which optional columns
  // the stream shows, Time and Rule excluded since neither is ever
  // offered as a toggle. Factored out of FilterBar.svelte's mobile
  // drawer and Whisper.svelte's desktop popover -- both carried their
  // own copy of this exact list, data and all, and only Whisper's had a
  // test covering it (#1218 audit finding 13). One component now; each
  // caller keeps its own wrapping container (FilterBar's always-open
  // drawer field, Whisper's popover and its "undo a bad drag" reset
  // button), since those differ in more than styling.
  //
  // #710: the chooser's own naming, distinct from COLUMNS' table-header
  // labels (lib/columns.svelte). A flat list read "Device column",
  // "Address column", "Address column" -- the same visible text twice,
  // once for source's address and once for destination's, with nothing
  // beside it to tell them apart. These three lists say the bare column
  // name in the strip's own mono voice, and the source/destination
  // facts (address, port, MAC) sit under a small heading naming which
  // side they belong to instead of repeating "source"/"destination" on
  // every row. Order here is the chooser's own -- COLUMNS interleaves
  // source's and destination's facts around chain/proto for a table-
  // layout reason (see its own comment) that has nothing to do with how
  // this menu groups them.
  import { columnState } from '../lib/columns.svelte'

  // touch (issue #85's 44px touch-target convention): the mobile
  // drawer's own row height and font, applied to the whole label rather
  // than just the checkbox glyph, so the tap target is the full
  // "checkbox + column name" row rather than the ~16px box. Whisper's
  // desktop popover leaves this off, the compact default. A prop rather
  // than a `.drawer` ancestor selector (which is what FilterBar.svelte
  // used before this was its own component): Svelte scopes a
  // component's styles to elements it renders itself, so a parent's own
  // `.drawer` class can no longer reach in here to change them.
  let { touch = false }: { touch?: boolean } = $props()

  interface ColumnChoice {
    key: string
    text: string
    ariaLabel: string
  }
  const PLAIN_COLUMNS: ColumnChoice[] = [
    { key: 'device', text: 'device', ariaLabel: 'Device column' },
    { key: 'action', text: 'action', ariaLabel: 'Action column' },
    { key: 'chain', text: 'chain', ariaLabel: 'Chain column' },
    { key: 'source', text: 'source', ariaLabel: 'Source column' },
    { key: 'destination', text: 'destination', ariaLabel: 'Destination column' },
    { key: 'proto', text: 'proto', ariaLabel: 'Proto column' },
    { key: 'iface', text: 'interface', ariaLabel: 'Interfaces column' },
    { key: 'nat', text: 'NAT', ariaLabel: 'NAT column' },
  ]
  const SOURCE_COLUMNS: ColumnChoice[] = [
    { key: 'srcAddr', text: 'address', ariaLabel: 'Source address column' },
    { key: 'srcPort', text: 'src port', ariaLabel: 'Source port column' },
    { key: 'mac', text: 'MAC', ariaLabel: 'Source MAC column' },
  ]
  const DEST_COLUMNS: ColumnChoice[] = [
    { key: 'dstAddr', text: 'address', ariaLabel: 'Destination address column' },
    { key: 'port', text: 'port', ariaLabel: 'Destination port column' },
  ]
</script>

{#snippet columnCheckbox(col: ColumnChoice)}
  <!-- aria-label carries the disambiguated name ("Source address
       column", not "Address column") on the input directly, which
       wins over the wrapping <label>'s own text for the accessible
       name -- so the visible word stays bare while a screen reader
       still hears which side it belongs to. -->
  <label class="col-toggle" class:touch>
    <input
      type="checkbox"
      checked={columnState.isColumnVisible(col.key)}
      onchange={() => columnState.toggleColumn(col.key)}
      aria-label={col.ariaLabel}
    />
    {col.text}
  </label>
{/snippet}

{#each PLAIN_COLUMNS as col (col.key)}
  {@render columnCheckbox(col)}
{/each}
<span class="col-group-heading" class:touch>source</span>
{#each SOURCE_COLUMNS as col (col.key)}
  {@render columnCheckbox(col)}
{/each}
<span class="col-group-heading" class:touch>destination</span>
{#each DEST_COLUMNS as col (col.key)}
  {@render columnCheckbox(col)}
{/each}

<style>
  .col-toggle {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font: 12px var(--font-mono);
    color: var(--fg-muted);
    cursor: pointer;
    white-space: nowrap;
  }

  .col-toggle:hover {
    color: var(--fg);
  }

  .col-toggle input[type='checkbox'] {
    cursor: pointer;
  }

  .col-toggle input[type='checkbox']:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  /* #710: the small heading naming which side "address"/"src port"/
     "MAC" belongs to. flex-basis: 100% starts a new line inside
     whichever flex container (FilterBar's drawer field, Whisper's
     panel) the caller wraps this in. */
  .col-group-heading {
    flex-basis: 100%;
    margin-top: 4px;
    padding-top: 6px;
    border-top: 1px solid var(--border);
    font: 500 9px var(--font-mono);
    letter-spacing: 0.1em;
    color: var(--fg-dim);
  }

  .col-toggle.touch {
    min-height: 44px;
    font: 14px var(--font-sans);
    color: var(--fg);
  }

  .col-group-heading.touch {
    font-size: 11px;
    padding-top: 10px;
  }
</style>
