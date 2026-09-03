<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Multi-router-fleet health view (issue #98), reshaped onto the deck's
  // own clothes for #657/#706: every known device (both configured, from
  // config.yaml's `devices` list, and auto-discovered -- seen on the
  // wire but not yet added there), with the server-computed
  // live/stale/never-seen status GET /api/devices reports.
  //
  // #647 folded the old table into Entities' card for the user/admin
  // tiers, leaving this component reachable only from the phone-width
  // bottom bar -- but #657's ratified matrix keeps Fleet itself
  // viewer-visible ("a stale router is *why* the log looks wrong")
  // while ruling Entities and Settings out of a viewer's navigation
  // entirely, so this is a deck card again for a viewer (deckCards.ts's
  // `fleet` key). Being a card of round 30's deck, it wears the deck's
  // identity, not the retired operate-page frame it was born in:
  //
  // - The same router cards as Entities' leading row (#675/#718), not
  //   the old flat table -- deviceState/ratePerSecond live in
  //   lib/fleet.ts so the two surfaces literally share the vocabulary
  //   and cannot drift. A viewer's Fleet and a user's Entities describe
  //   the same routers in the same voice.
  // - No page heading and no strap (#697, "I meant all... No page
  //   heading, no strap"); the row keeps only the .og h3 label, the
  //   same one Entities prints over the same cards.
  // - No berth, no add-router affordance of any kind: adding a router
  //   is a change, and #657's grammar is absent, never disabled. This
  //   card exists so a viewer can read why the log looks wrong -- a
  //   quiet router is presented as a fact to read, not a fault to fix.
  // - Status is a mark plus a written label (#616: never colour alone),
  //   and a router with an active silence flag carries a real link into
  //   the docket's flags tab -- the one place a viewer can take that
  //   reading further.
  //
  // Fleet never carried a readOnly chip (#548/#490's grammar): the view
  // has no edit affordance for anyone, admin included, so there is no
  // distinction to declare.
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { formatRelative } from '../lib/format'
  import { deviceState, multihomedEcho, sortedDevices, ratePerSecond } from '../lib/fleet'
  import GhostRows from './GhostRows.svelte'

  const rows = $derived(sortedDevices(appState.devices))

  // True when this device has an active (unacknowledged) device_silence
  // flag -- distinct from status === 'stale': the flag only exists for a
  // *configured* device that was active and went quiet past
  // deviceStaleAfter, while `status` also covers auto-discovered devices
  // and a shorter/different threshold could in principle apply (today
  // they share deviceStaleAfter, but the API doesn't guarantee that).
  function hasActiveSilenceFlag(deviceId: string): boolean {
    return flagsState.list.some((f) => f.type === 'device_silence' && f.target === deviceId && !f.cleared)
  }

  // The flag chip is a real door, not a decoration: the flags tab is
  // where a viewer reads what the silence means, and they can reach it.
  // Setting the view rolls the deck there (Deck.svelte's own effect);
  // at phone width the same assignment lands on the docket card.
  function openFlags() {
    appState.view = 'flags'
  }

  // Mirrors LiveTable's own emptyState derived (#549): zero devices is
  // either "the app's one loadInitial() call hasn't come back yet" or
  // "it has, and mikroview has never seen a device" -- the second is
  // first-run's sharpest client-side signal, since seeing a device is
  // exactly what running setup produces. See appState.initialLoadDone's
  // doc comment for why that flag, rather than rows.length or
  // fetchFailed alone, is what tells the two apart.
  const emptyState = $derived.by((): { kind: 'ghost' } | { kind: 'text'; text: string } => {
    if (!appState.initialLoadDone) return { kind: 'ghost' }
    return {
      kind: 'text',
      text:
        authState.role === 'admin'
          ? 'No RouterOS devices seen yet — your account menu ▸ Run setup… to point one at mikroview.'
          : 'No RouterOS devices seen yet. Ask an administrator to run setup.',
    }
  })
</script>

<div class="page scrollbar op-page">
  <div class="opwrap"><div class="opanel">
    <div class="og">
      <h3>routers — every one that pushes here</h3>
      {#if rows.length === 0}
        {#if emptyState.kind === 'ghost'}
          <GhostRows label="Loading devices…" rows={4} />
        {:else}
          <div class="empty">{emptyState.text}</div>
        {/if}
      {:else}
        <div class="fcards">
          {#each rows as d (d.id)}
            {@const st = deviceState(d, appState.now)}
            <div class="fcard" class:live={d.status === 'live'}>
              <div class="fhead"><b>{d.name}</b><span class="fstate {st.cls}">{st.mark} {st.text}</span></div>
              <div class="frow">
                {d.routerosVersion ? `RouterOS ${d.routerosVersion}` : 'RouterOS version not yet reported'}
              </div>
              {#if d.status === 'live'}
                <div class="frow">
                  {ratePerSecond(appState.events, d.id, appState.now)} events/s now · {d.eventCount} event{d.eventCount === 1 ? '' : 's'} ever
                </div>
              {:else if d.status === 'never_seen'}
                <div class="frow dim">never heard from yet</div>
              {:else}
                <div class="frow dim">
                  last heard {formatRelative(d.lastSeen, appState.now)} — quiet is a fact, not a fault
                </div>
              {/if}
              {#if multihomedEcho(d)}
                <!-- The source-address split's echo (#442): the wizard's
                     step 2 owns the diagnosis and the command; this card
                     only says the pair is visible and where the fix is. -->
                <div class="frow dim">{multihomedEcho(d)}</div>
              {/if}
              {#if d.sourceIp}
                <div class="frow dim">syslog from <span class="mono">{d.sourceIp}</span></div>
              {/if}
              {#if !d.configured}
                <div class="frow dim">seen on the wire, not in the <span class="mono">devices</span> config</div>
              {/if}
              {#if hasActiveSilenceFlag(d.id)}
                <button type="button" class="flag-door" onclick={openFlags} aria-label="Read the silence flag for {d.name} in the docket">
                  ⚑ silence flagged — read it in the docket
                </button>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div></div>
</div>

<style>
  /* The deck's own frame (#675/#718, same fields as Entities.svelte):
     no bordered panel around already-bordered cards, label but no
     heading, cards on the glass. Svelte scopes styles per component, so
     these are the ported fields, with lib/fleet.ts holding the logic
     that must not fork. */
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px 16px 24px;
  }

  .op-page .opwrap {
    display: flex;
    justify-content: center;
  }

  .op-page .opanel {
    width: 100%;
    max-width: 1500px;
  }

  .og {
    margin-bottom: 20px;
  }

  .og h3 {
    margin: 0 0 6px;
    font-size: 10px;
    font-weight: 650;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .empty {
    color: var(--fg-dim);
    font-size: 13px;
    padding: 10px 0;
  }

  .fcards {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: 14px;
  }

  .fcard {
    background: var(--glass);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 16px 20px;
    font-size: 12.5px;
    color: var(--fg-muted);
  }

  .fcard.live {
    border-color: var(--hair-2);
  }

  .fhead {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    margin-bottom: 8px;
    gap: 10px;
  }

  .fhead b {
    font-size: 15px;
    color: var(--fg);
  }

  .fstate {
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.08em;
    white-space: nowrap;
  }

  .fstate.ok {
    color: var(--accept);
  }

  .fstate.quiet {
    color: var(--fg-dim);
  }

  .fcard .frow {
    padding: 3px 0;
  }

  .frow.dim {
    color: var(--fg-dim);
  }

  .mono {
    font-family: var(--font-mono);
    font-size: 11.5px;
  }

  /* The silence flag's door into the docket: mark + words (#616), a
     bordered chip rather than bare text so it reads as pressable, in
     the reject family because that is the flag palette's own colour for
     an uncleared flag -- but never colour alone. */
  .flag-door {
    margin-top: 8px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: var(--reject-bg);
    color: var(--reject);
    border: 1px solid var(--reject);
    border-radius: 6px;
    padding: 3px 8px;
    font-size: 11.5px;
    font-weight: 600;
    cursor: pointer;
  }

  .flag-door:hover {
    filter: brightness(1.15);
  }

  .flag-door:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
</style>
