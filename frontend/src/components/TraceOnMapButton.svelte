<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Small per-line trigger beside the Interfaces cell (#1018, round 53):
  // draws this one logged line's single hop on the topography map -- the
  // lane it came in on, the rule that decided, the lane it left on.
  //
  // Same shape as IpInvestigateButton / PortInvestigateButton /
  // RouterRuleButton next door, and the same `.investigate` class, so
  // the stream keeps one vocabulary for "there is more to see about this
  // cell" rather than growing a second one. A token, not a sentence
  // (owner, 2026-09-08, on the host card's own version of this): "it
  // should offer a simple trace button, not a sentence".
  //
  // Rendered only where the router logged a boundary at all. Without one
  // there is no hop to draw, and a button that always answered "nothing
  // matches" would be a promise the row cannot keep.
  import { appState } from '../lib/state.svelte'
  import { topologyNavState } from '../lib/topologyNav.svelte'

  let { id, label }: { id: number; label: string } = $props()

  function onClick() {
    topologyNavState.requestTrace({ event: id })
    appState.view = 'topography'
  }
</script>

<button class="investigate" onclick={onClick} title="Trace {label} on the map" aria-label="Trace {label} on the map">
  ⌖
</button>

<style>
  /* Mirrors IpInvestigateButton's own trigger exactly, less the italic
     serif `i`: this one's glyph is a mark, not a letter. */
  .investigate {
    flex: none;
    width: 15px;
    height: 15px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0;
    font-family: var(--font-sans);
    font-size: 10px;
    font-weight: 700;
    line-height: 1;
    color: var(--accent);
    background: transparent;
    border: 1px solid var(--accent);
    border-radius: 50%;
    cursor: pointer;
  }

  .investigate:hover {
    background: var(--accent-bg-hover);
  }
</style>
