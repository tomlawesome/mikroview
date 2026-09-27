<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // ✓ · Where setup stands (DESIGN.md, "The steps, in detail"). A stub:
  // the step issue under #1374 ports the prototype's doneBody -- the
  // compact track, the ledger (a row per thing, green with its receipt
  // in its ink, or dashed and struck where set aside), Undo per row,
  // and "undo everything on the router first" with the forget line.
  //
  // What the shell already provides: wizardRun.evidence for the rows;
  // wizardRun.undoOpen and wizardRun.showUndoAll for the undo lines;
  // the footer's Add another router and Finish are wired.

  import { wizardRun } from '../../lib/wizardRun.svelte'

  const ev = $derived(wizardRun.evidence)
  const n = $derived(
    [ev.cert, ev.enrol, wizardRun.push ? ev.push : '', wizardRun.backup ? ev.backup : '', ev.tagged ? 'x' : ''].filter(Boolean)
      .length,
  )
  const k = $derived((wizardRun.push === false ? 1 : 0) + (wizardRun.backup === false ? 1 : 0) + (wizardRun.push !== false && !ev.tagged ? 1 : 0))
</script>

<h3>{wizardRun.name} is sending.</h3>
<p class="hint">
  {n}
  {n === 1 ? 'thing stands' : 'things stand'} on evidence{#if k}; {k} {k === 1 ? 'was' : 'were'} set aside{/if}. Finish
  takes you to the fall, with {wizardRun.name} already flowing.
</p>
<p class="note">Admin ▸ Run setup… reopens this ledger any time.</p>
