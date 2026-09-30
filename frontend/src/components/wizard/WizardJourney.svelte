<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The way in, and the way out (#1386): the canvas the journey draws
  // on, the riding wordmark box, and the two choreographies -- wayIn
  // from the sign-in door into the wizard, wayOut from Finish onto the
  // fall. Ported from round-15's an.js (wayIn, wayOut); the six beats
  // themselves are lib/wizardJourney.ts's journey().
  //
  // Mounted once in App.svelte beside the wizard. The door is the real
  // AuthScreen, held over the shell (App.svelte's .door-hold) while the
  // shell loads; the wizard mounts under it, and the swap -- the door
  // dropped, the wizard shown -- happens under the canvas's cover.
  //
  // Cross-component cues are classes on <body> (journey.css): the three
  // components involved never import each other.

  import { untrack } from 'svelte'
  import { authState } from '../../lib/auth.svelte'
  import { appState } from '../../lib/state.svelte'
  import { wizardState } from '../../lib/wizard.svelte'
  import { wizardJourney } from '../../lib/wizardJourney.svelte'
  import { journey, strike } from '../../lib/wizardJourney'
  import './journey.css'

  let canvas = $state<HTMLCanvasElement | null>(null)
  let ride = $state<HTMLElement | null>(null)

  const LETTERS_A = 'MIKRO'.split('')
  const LETTERS_B = 'VIEW'.split('')

  // The way in decides once the shell has loaded under the held door:
  // the ledger (wizardState.status, read on sign-in by Wizard.svelte) and
  // the device list (appState.initialLoadDone) are what "would the
  // wizard auto-launch" needs.
  $effect(() => {
    if (!wizardJourney.pending) return
    if (authState.state !== 'authenticated') return
    if (!appState.initialLoadDone) return
    if (!wizardState.status) return
    untrack(() => {
      if (wizardJourney.decide() === 'play') wayIn()
    })
  })

  const body = () => document.body
  const raf = (f: () => void) => requestAnimationFrame(() => requestAnimationFrame(f))

  function wayIn() {
    // The wizard has just been launched (decide()); it mounts under the
    // door this frame. Two frames on, both rects can be read.
    raf(() => {
      const src = document.querySelector<HTMLElement>('.door-hold .wm-box .wm')
      const bar = document.querySelector<HTMLElement>('.wiz .bar .wm')
      if (!src || !bar || !canvas || !ride) {
        wizardJourney.end()
        return
      }
      const from = src.getBoundingClientRect()
      const to = bar.getBoundingClientRect()
      wizardJourney.begin('in')
      body().classList.add('journey', 'am')
      src.closest('.wm-box')?.classList.add('gone')
      const ok = journey({
        canvas,
        ride,
        from,
        to,
        D: window.innerHeight * 3.2,
        scale: 1,
        slide: (y) => {
          wizardJourney.slideY = y
        },
        swap: () => {
          // Under the cover: the door goes, the page stands bare.
          wizardJourney.dropDoor()
        },
        groups: [
          [4350, () => body().classList.add('ak-bar')],
          [4450, () => body().classList.add('ak-strip')],
          [
            4550,
            () => {
              wizardJourney.rows = true
              strike([...document.querySelectorAll<HTMLElement>('.wiz .rail li')], 90)
            },
          ],
          [4800, () => strike([...(document.querySelector('.wiz .body')?.children ?? [])] as HTMLElement[], 120)],
          [5350, () => strike([...document.querySelectorAll<HTMLElement>('.wiz .foot')], 0)],
        ],
        landed: () => {
          wizardJourney.landed = true
        },
        done: () => {
          body().classList.remove('journey', 'am', 'ak-bar', 'ak-strip')
          document.querySelectorAll('.strike').forEach((el) => el.classList.remove('strike'))
          wizardJourney.end()
          document.querySelector<HTMLElement>('.wiz .body input')?.focus()
        },
      })
      if (!ok) {
        // No canvas context (jsdom): the reduced path.
        body().classList.remove('journey', 'am')
        wizardJourney.end()
      }
    })
  }

  // The way out: Finish plays the same journey at three-quarters speed.
  // `swap` is what finish() used to do outright -- show the fall and
  // close the wizard -- now done under the cover. `after` runs once the
  // journey is done and the fall stands: the tour's offer (#1386) waits
  // on it rather than on a clock of its own.
  export function wayOut(swap: () => void, after: () => void): boolean {
    const bar = document.querySelector<HTMLElement>('.wiz .bar .wm')
    if (!bar || !canvas || !ride || wizardJourney.active) return false
    const from = bar.getBoundingClientRect()
    wizardJourney.begin('out')
    body().classList.add('journey', 'am')
    return journey({
      canvas,
      ride,
      from,
      to: from,
      D: window.innerHeight,
      scale: 0.78,
      slide: (y) => {
        wizardJourney.slideY = y
      },
      swap: () => {
        swap()
        body().classList.add('ak-strip')
      },
      groups: [
        [4350, () => body().classList.add('ak-bar')],
        [4450, () => strike([...document.querySelectorAll<HTMLElement>('.fall .rig g.band')] as unknown as HTMLElement[], 90)],
        [4800, () => body().classList.add('ak-fb')],
        [
          5300,
          () => {
            body().classList.remove('journey', 'am', 'ak-bar', 'ak-strip', 'ak-fb')
            document.querySelectorAll('.strike').forEach((el) => el.classList.remove('strike'))
          },
        ],
      ],
      landed: () => {
        wizardJourney.landed = true
      },
      done: () => {
        body().classList.remove('journey', 'am', 'ak-bar', 'ak-strip', 'ak-fb')
        wizardJourney.end()
        after()
      },
    })
  }

  // finish() asks for the way out through the state module rather than
  // importing this component: wizardRun.svelte.ts stays a plain module.
  $effect(() => {
    wizardJourney.wayOutHandler = wayOut
    return () => {
      wizardJourney.wayOutHandler = null
    }
  })
</script>

{#if wizardJourney.active}
  <canvas class="journey-fx" bind:this={canvas} aria-hidden="true"></canvas>
  <div class="ride" bind:this={ride} aria-hidden="true">
    <span class="wm">{#each LETTERS_A as c, i (i)}<span class="l">{c}</span>{/each}<em>{#each LETTERS_B as c, i (i)}<span class="l">{c}</span>{/each}</em></span>
  </div>
{/if}
