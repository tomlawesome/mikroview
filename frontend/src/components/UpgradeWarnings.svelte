<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Issue #1344: a router that upgrades past a RouterOS release with a
  // breaking change got no warning -- the one note that existed
  // (7.24.3's certificate-store change) was tied to the exact 7.24.3
  // row and never rendered. This component is the setup wizard's
  // rendering of internal/routeros.Upgrades: one block per catalogue
  // entry, naming every connected router (and the operator's own pick)
  // whose version is at or past that entry's From.
  //
  // A separate component, not inline in SetupWizard.svelte, so the
  // follow-up issue (showing this beside the drop-list setup card and
  // the attach journey, neither of which fetches setup commands today)
  // is a one-line add rather than a copy of this markup.
  import { prose, UPGRADE_STEP_LABELS } from '../lib/setupsteps'
  import type { SetupCommandsResponse } from '../lib/types'

  let { commands }: { commands: SetupCommandsResponse } = $props()

  interface UpgradeBlock {
    id: string
    from: string
    heading: string
    body: string
    names: string[]
    stepLabels: string[]
  }

  const blocks = $derived.by((): UpgradeBlock[] => {
    const out: UpgradeBlock[] = []
    for (const u of commands.routeros.upgrades) {
      const names = commands.routers
        .filter((r) => r.upgrades.includes(u.id))
        .map((r) => `${r.name} (${r.routerosVersion})`)
      if (commands.picked && commands.picked.upgrades.includes(u.id)) {
        names.push(`Your picked version (${commands.picked.version})`)
      }
      if (names.length === 0) continue
      out.push({
        id: u.id,
        from: u.from,
        heading: u.heading,
        body: u.body,
        names,
        stepLabels: u.steps.map((key) => UPGRADE_STEP_LABELS[key] ?? key),
      })
    }
    return out
  })
</script>

{#each blocks as b (b.id)}
  <p class="note upgrade" role="note" data-upgrade={b.id}>
    <strong>{prose(b.names)} {b.names.length === 1 ? 'runs' : 'run'} RouterOS {b.from} or later.</strong>
    {b.heading} {b.body}
    Affects {prose(b.stepLabels)}.
  </p>
{/each}
