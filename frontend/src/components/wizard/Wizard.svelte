<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The setup wizard as a full screen (#1381), replacing the modal
  // (SetupWizard.svelte, retired with this build). Built from
  // docs/design/screens/wizard/DESIGN.md, the ratified record -- where
  // it and the prototype disagree, the record wins.
  //
  // This is the shell: the bar (wordmark, chips as proofs arrive, the
  // live rate), the strip under it, the gated step rail, the body and
  // the footer. Each step's body is its own component under
  // ./wizard/, filled in by the step issues; the footer's controls are
  // wired here for every step, as the prototype's renderFoot has them.
  //
  // The model is still a claim ledger: every check is an observation --
  // MikroView never connects to a router (the AGENTS.md invariant) --
  // and the wizard is stateless beyond the evidence, so reopening shows
  // the ledger as it stands (wizardRun.begin()).
  //
  // Mounted once in App.svelte where the modal was: it covers the shell
  // while open, and the same doors open it -- auto-launch on the first
  // admin sign-in with no router sending, Admin ▸ Run setup…, Add a
  // router and Re-enrol… (all through wizardState).

  import { untrack } from 'svelte'
  import { authState } from '../../lib/auth.svelte'
  import { appState } from '../../lib/state.svelte'
  import { fallState } from '../../lib/fall.svelte'
  import { wizardState } from '../../lib/wizard.svelte'
  import { wizardJourney } from '../../lib/wizardJourney.svelte'
  import { wizardRun } from '../../lib/wizardRun.svelte'
  import { announce, chipsFor, footSpec, railRows, stageOf, stripFor, type FootAction } from '../../lib/wizardRun'
  import StepRouter from './StepRouter.svelte'
  import StepMint from './StepMint.svelte'
  import StepPaste from './StepPaste.svelte'
  import StepTune from './StepTune.svelte'
  import StepStand from './StepStand.svelte'
  import './wizard.css'

  // Steps land seconds to minutes apart (the push scheduler runs every
  // 20 minutes), so this polls rather than streaming -- and only while
  // the wizard is open.
  const POLL_MS = 5000

  const isAdmin = $derived(authState.state === 'authenticated' && authState.role === 'admin')

  // One read of the ledger on sign-in, whether or not the wizard is
  // ever opened: the surfaces that explain their own silence need the
  // marks (#490).
  $effect(() => {
    if (authState.state !== 'authenticated') return
    wizardState.refresh()
  })

  // The record's auto-launch: first admin sign-in with no router
  // sending, after the shell has painted (appState.initialLoadDone).
  $effect(() => {
    if (!isAdmin) return
    if (!appState.initialLoadDone) return
    // A sign-in's way in (#1386) decides the launch itself while it is
    // pending or playing; this rule resumes once it has let the door down.
    if (wizardJourney.pending || wizardJourney.active) return
    wizardState.maybeAutoLaunch(appState.devices.length > 0)
  })

  // The entrance: the rail, the body and the footer arrive by their own
  // transitions once the page stands (wizard.js's showWizard) -- unless
  // the way in (#1386) is driving: then the rows strike on when the
  // journey says so, and the bar's wordmark appears as the ride lands.
  let away = $state(true)
  let live = $state(false)

  // A fresh run every time the wizard opens; the answers go with the
  // walk they were given in.
  $effect(() => {
    if (!wizardState.open) {
      away = true
      live = false
      return
    }
    // untrack: begin() reads the evidence to place the run, and this
    // effect must not re-run (and re-reset the answers) on every poll.
    untrack(() => wizardRun.begin())
    if (untrack(() => wizardJourney.phase === 'in')) return
    const raf = typeof requestAnimationFrame === 'function' ? requestAnimationFrame : (f: () => void) => setTimeout(f, 0)
    raf(() => raf(() => {
      away = false
      live = true
    }))
  })

  // The way in's beats: the rail stands as its rows strike (4.55s), the
  // wordmark takes over as the ride lands (4.8s). The way out slides the
  // main away and takes the rail with it.
  $effect(() => {
    if (wizardJourney.phase !== 'in') return
    if (wizardJourney.rows) away = false
    if (wizardJourney.landed) live = true
  })
  const goingOut = $derived(wizardJourney.phase === 'out')

  // While open: the ledger, the backups and the refused senders on one
  // cadence, then the run's own bookkeeping (the live rate).
  $effect(() => {
    if (!wizardState.open) return
    wizardState.refreshBackups()
    wizardState.refreshRefused()
    const timer = setInterval(() => {
      wizardState.refresh()
      wizardState.refreshBackups()
      wizardState.refreshRefused()
      wizardRun.poll()
    }, POLL_MS)
    return () => clearInterval(timer)
  })

  // The strip's boundaries come from the pushed rule tables, the same
  // read the fall makes (MikroView's own store, never the router).
  // Asked for once per open, once the push has arrived.
  let fallAsked = $state(false)
  $effect(() => {
    if (!wizardState.open) {
      fallAsked = false
      return
    }
    if (fallAsked || !evidence.push) return
    fallAsked = true
    fallState.refresh()
  })

  const answers = $derived(wizardRun.answers)
  const evidence = $derived(wizardRun.evidence)
  const step = $derived(stageOf(answers))
  const rows = $derived(railRows(answers, evidence))
  const chips = $derived(chipsFor(answers, evidence))
  const ticks = $derived(stripFor(evidence))
  const foot = $derived(footSpec(answers, evidence))
  const announcement = $derived(announce(rows))

  function act(a: FootAction) {
    switch (a) {
      case 'router-next':
        wizardRun.routerNext()
        break
      case 'back-router':
        wizardRun.gotoStep(0)
        break
      case 'back-pass':
        wizardRun.gotoStep(1)
        break
      case 'mint':
        wizardRun.mint()
        break
      case 'to-tune':
        wizardRun.toTune()
        break
      case 'tune-skip':
        wizardRun.tuneSkip()
        break
      case 'to-done':
        wizardRun.toDone()
        break
      case 'another':
        wizardRun.another()
        break
      case 'finish':
        wizardRun.finish()
        break
      case 'none':
        break
    }
  }
</script>

{#if wizardState.open}
  <div class="page wiz" class:live>
    <div class="bar">
      <span class="wm">MIKRO<em>VIEW</em></span>
      <div class="chips">
        {#each chips as c, i (i)}
          <span class="att {c.kind}"><i></i>{c.text}</span>
        {/each}
      </div>
      <div class="right">
        {#if evidence.enrol}
          <span class="att logs"><i></i>live · {wizardRun.rate}/s</span>
          <span>{evidence.lines.toLocaleString()} lines</span>
        {:else}
          <span class="att"><i></i>no router yet</span>
        {/if}
      </div>
    </div>
    <!-- The strip: one grey tick until the router speaks, green when
         logs flow, one tick per boundary in its lane once the push
         names them. The lane colour is set the way Fall.svelte sets
         its own ticks (a style: directive, which the CSP allows). -->
    <div class="striprow">
      <div class="ovstrip" aria-hidden="true">
        {#each ticks as t, i (i)}
          <span class="ovtick" class:on={t.on} class:dark={t.dark} style:background={t.lane || null}></span>
        {/each}
      </div>
    </div>
    <!-- The rail: five rows, gated. A step ahead of the furthest reached
         is locked -- dashed number, dim, disabled, titled with the
         reason. An earlier completed step is a real button while the run
         is still yours to change; once the router is answering nothing
         goes back. Each row carries its receipt in the ink of what it
         records (--ink, read by the done rules in wizard.css). -->
    <nav class="rail" class:away={away || goingOut} aria-label="Setup steps">
      <ol>
        {#each rows as r (r.id)}
          <li>
            <button
              type="button"
              class="step-row {r.state.cls}"
              class:locked={r.locked}
              class:current={r.current}
              disabled={!r.can}
              aria-current={r.current ? 'step' : 'false'}
              aria-disabled={r.locked ? 'true' : undefined}
              title={r.locked ? 'After the step before it' : undefined}
              style:--ink={r.state.cls === 'done' && r.state.ink ? `var(--ink-${r.state.ink})` : null}
              onclick={() => wizardRun.gotoStep(rows.indexOf(r))}
            >
              <span class="step-n">{r.n}</span>
              <span class="step-text">
                <span class="step-title">{r.title}</span>
                {#if r.state.receipt}
                  <span class="step-receipt">{r.state.receipt}</span>
                {/if}
              </span>
            </button>
          </li>
        {/each}
      </ol>
    </nav>
    <div class="main" style:transform={goingOut ? `translateY(${-wizardJourney.slideY}px)` : null}>
      <div class="body" class:away>
        {#if step === 'router'}
          <StepRouter />
        {:else if step === 'pass'}
          <StepMint />
        {:else if step === 'paste'}
          <StepPaste />
        {:else if step === 'tune'}
          <StepTune />
        {:else}
          <StepStand />
        {/if}
      </div>
      <div class="foot" class:away>
        {#if foot.left}
          <button type="button" class:primary={foot.left.primary} disabled={foot.left.disabled} onclick={() => act(foot.left!.action)}>
            {foot.left.label}
          </button>
        {/if}
        <span class="fhint">{foot.hint}</span>
        {#each foot.right as b (b.action + b.label)}
          <button type="button" class:primary={b.primary} disabled={b.disabled} onclick={() => act(b.action)}>
            {b.label}
          </button>
        {/each}
      </div>
    </div>
  </div>
  <p class="wiz-sr-only" role="status">{announcement}</p>
{/if}
