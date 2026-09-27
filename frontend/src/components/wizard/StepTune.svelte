<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Step 4 · Tag firewall rules (DESIGN.md, "The steps, in detail"). A
  // stub: the step issue under #1374 ports the prototype's tuneBody --
  // the rule list proposed from the push (chain · action · rule · why
  // it matters · prefix, a checkbox each), the tagging block, "Copy — N
  // rules", and the observation line (quiet until Copy, waiting after,
  // counting once the first new prefix lands).
  //
  // What the shell already provides: wizardRun.markTuneCopied(n) starts
  // the count; wizardRun.evidence.tagged is the first new line landing;
  // the footer's Skip this step and Next are wired.

  import { wizardRun } from '../../lib/wizardRun.svelte'
</script>

{#if wizardRun.push === false}
  <h3>Tag firewall rules</h3>
  <p class="hint">Needs the push — the rule table is what proposes the rules to tag.</p>
{:else}
  <h3>Tag firewall rules</h3>
  <p class="hint">Tick the ones to tag; this is the only second paste.</p>
  <div class="obs quiet">Nothing to wait for until you paste — the rule table is already here.</div>
{/if}
