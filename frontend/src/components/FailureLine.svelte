<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The one failure line (#1540, C4): role="alert", reading
  // "Could not <verb> <thing>: <server words>". Every screen's failed
  // load or save goes through it, so a failure announces itself the same
  // way everywhere. `said` is the server's own words from lib/api.ts,
  // shown verbatim; when the server said nothing the colon is dropped
  // rather than left dangling.
  let { verb, thing, said = '' }: { verb: string; thing: string; said?: string | null } = $props()
</script>

<p class="failure-line" role="alert">Could not {verb} {thing}{said ? `: ${said}` : ''}</p>

<style>
  .failure-line {
    color: var(--reject);
    margin: 0;
  }
</style>
