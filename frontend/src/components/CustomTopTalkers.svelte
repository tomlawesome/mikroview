<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // No wrapping element -- each widget card (and the trailing add-trigger)
  // becomes its own sibling in Dashboard.svelte's grid, the same as the
  // fixed BarList panels around it.
  import { topTalkerWidgetsState } from '../lib/topTalkers.svelte'
  import CustomTopTalkerCard from './CustomTopTalkerCard.svelte'
  import AddTopTalkerWidget from './AddTopTalkerWidget.svelte'
</script>

{#each topTalkerWidgetsState.widgets as widget (widget.id)}
  <CustomTopTalkerCard {widget} />
{/each}
<AddTopTalkerWidget />
