<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Shared shell for the two pre-app states (setup/login) -- both are a
  // centered form, differing only in title, submit label, and what
  // happens on submit. Not a modal: this replaces App.svelte's entire
  // main content, same as Metrics being an independent view rather than
  // an overlay.
  //
  // This is the ratified door (#645, round 5's v3 amended by round 5's
  // fourth batch, restated unchanged in round 29's final walkthrough):
  // one sub-second, Orbit-speed beat -- the fall rains across the whole
  // screen behind the form, masked out of the centre so it never
  // crosses the login elements; the amber draws as a thin (1.5px) box
  // framing the wordmark; credential fields are underlined, not boxed.
  // The mockups' storyboard strip for the way out is mockup-only
  // annotation (round 23/24) -- not built here; the way out is instead
  // the same beat in reverse (reverseBeat below), no strip.
  import { authState } from '../lib/auth.svelte'

  let {
    title = '',
    subtitle = '',
    submitLabel = '',
    onsubmit,
    confirmPassword = false,
    ssoAvailable = false,
    // #646's first-run flow (scope note on #645): on a virgin instance
    // with no account yet, the door shows this same chrome but the
    // submit button's place holds an Enter button leading into the
    // untouched account-creation form -- AuthSetup.svelte toggles
    // between gate and its ordinary form, this component only renders
    // whichever is asked for.
    gate = false,
    onEnter,
    // Set by AuthLogin when this mount follows a sign-out (see
    // authState.consumeJustSignedOut()) -- plays the door's beat in
    // reverse first (the brink collapses, goes dark), then the ordinary
    // entrance below: brief, then the login, no storyboard strip.
    reverseBeat = false,
  }: {
    title?: string
    subtitle?: string
    submitLabel?: string
    onsubmit?: (username: string, password: string) => Promise<string | null>
    confirmPassword?: boolean
    // ssoAvailable: whether the backend has OIDC/SSO configured (see
    // authState.ssoAvailable) -- renders a secondary "Sign in with SSO"
    // link below the form. A plain <a>, not a button/fetch call: this
    // has to be a real top-level browser navigation for the OAuth
    // redirect to work at all, so clicking it leaves the SPA entirely.
    ssoAvailable?: boolean
    gate?: boolean
    onEnter?: () => void
    reverseBeat?: boolean
  } = $props()

  let username = $state('')
  let password = $state('')
  let passwordConfirm = $state('')
  let error = $state<string | null>(null)
  let submitting = $state(false)
  // Requires clicking through to a separate warning screen rather than
  // reacting to one click on the form -- this is a permanent, only-
  // CLI-reversible decision (see the plan's "informed choice, not
  // accidental click" requirement).

  async function handleSubmit(e: Event) {
    e.preventDefault()
    error = null

    if (confirmPassword && password !== passwordConfirm) {
      error = 'Passwords do not match.'
      return
    }

    submitting = true
    const result = await onsubmit?.(username, password)
    submitting = false
    if (result) error = result
  }
</script>

<!-- data-void: the door is always the void (#645). The ratified scene
     -- round 29's door, "the fall rains over the whole void" -- was
     accepted against the dark ground, and the sign-in is the one screen
     whose job is that identity: night outside, the operator's own theme
     once they enter. app.css's [data-void] rule re-declares the dark
     token block on this subtree under any theme or colorway. -->
<div class="screen" class:reverse={reverseBeat} data-void>
  <!-- The fall, rained across the whole void behind the door -- never
       over the login elements: a radial mask carves the centre out of
       the layer entirely (round 5 fourth batch). Seventeen strokes,
       transform-only, is the whole cost -- no particle system. Each
       stroke's --y is its resting place under reduced motion: the rain
       hangs still instead of vanishing, so the scene keeps its texture
       when its movement is declined. -->
  <div class="fullfall" aria-hidden="true">
    <!-- The round-29 scene's own seventeen, verbatim: eleven accepts,
         four drops, two NAT marks. -->
    <i style="left: 4%; animation-delay: 0.2s; --y: 8%; opacity: 0.5"></i>
    <i style="left: 9%; animation-delay: 3.1s; --y: 64%; opacity: 0.3"></i>
    <i class="r" style="left: 15%; animation-delay: 1.6s; --y: 31%; opacity: 0.55"></i>
    <i style="left: 21%; animation-delay: 4.4s; --y: 78%; opacity: 0.4"></i>
    <i style="left: 26%; animation-delay: 2.2s; --y: 15%; opacity: 0.65"></i>
    <i class="v" style="left: 33%; animation-delay: 5.0s; --y: 52%; opacity: 0.45"></i>
    <i style="left: 38%; animation-delay: 0.9s; --y: 88%; opacity: 0.3"></i>
    <i class="r" style="left: 45%; animation-delay: 3.7s; --y: 24%; opacity: 0.4"></i>
    <i style="left: 51%; animation-delay: 1.2s; --y: 70%; opacity: 0.6"></i>
    <i style="left: 57%; animation-delay: 4.8s; --y: 41%; opacity: 0.35"></i>
    <i style="left: 63%; animation-delay: 2.7s; --y: 95%; opacity: 0.5"></i>
    <i class="v" style="left: 69%; animation-delay: 0.5s; --y: 58%; opacity: 0.4"></i>
    <i class="r" style="left: 75%; animation-delay: 3.4s; --y: 12%; opacity: 0.6"></i>
    <i style="left: 81%; animation-delay: 1.9s; --y: 83%; opacity: 0.35"></i>
    <i style="left: 86%; animation-delay: 5.3s; --y: 36%; opacity: 0.55"></i>
    <i class="r" style="left: 91%; animation-delay: 2.4s; --y: 67%; opacity: 0.35"></i>
    <i style="left: 96%; animation-delay: 4.1s; --y: 47%; opacity: 0.5"></i>
  </div>

  <div class="stack">
    <!-- The amber 1.5px box framing the wordmark (round 5 third batch,
         "v2 accepted... the amber draws as a thin box... instead of the
         underline"). Replaces the pre-door top-left corner lockup --
         this page is the door, not a scene carrying SceneBar's chrome. -->
    <div class="wm-box"><span class="wm">MIKRO<em>VIEW</em></span></div>

    {#if gate}
      <div class="col">
        <button type="button" class="submit-btn" onclick={() => onEnter?.()}>Enter</button>
      </div>
    {:else}
      <div class="col">
        {#if authState.ssoError}
          <p class="error">{authState.ssoError}</p>
        {/if}

        <form class="form-body" onsubmit={handleSubmit}>
          <!-- No heading on the door itself: the framed wordmark is the
               title (the round-29 scene carries none). Setup still
               passes one -- that form explains itself. -->
          {#if title}
            <h1>{title}</h1>
          {/if}
          {#if subtitle}
            <p class="subtitle">{subtitle}</p>
          {/if}

          <!-- "account" / "password" is the ratified scene's own copy
               (round 15 amended its "passphrase"; "account" stood), and
               it sits inside the field as the mockup draws it (owner,
               2026-08-30). The <label> stays for assistive tech,
               visually hidden -- the placeholder is presentation, not
               the accessible name. -->
          <label>
            <span class="sr-only">account</span>
            <input type="text" autocomplete="username" placeholder="account" bind:value={username} required />
          </label>

          <label>
            <span class="sr-only">password</span>
            <input type="password" autocomplete={confirmPassword ? 'new-password' : 'current-password'} placeholder="password" bind:value={password} required />
          </label>

          {#if confirmPassword}
            <label>
              <span class="sr-only">confirm password</span>
              <input type="password" autocomplete="new-password" placeholder="confirm password" bind:value={passwordConfirm} required />
            </label>
          {/if}

          {#if error}
            <p class="error">{error}</p>
          {/if}

          <button type="submit" class="submit-btn" disabled={submitting}>{submitting ? 'Please wait…' : submitLabel}</button>

          {#if ssoAvailable}
            <div class="divider"><span>or</span></div>
            <a class="sso-link" href="/api/auth/oidc/login">Sign in with SSO</a>
          {/if}
        </form>
      </div>
    {/if}
  </div>

</div>

<style>
  .screen {
    flex: 1;
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 20px;
    background: var(--bg);
    overflow: hidden;
  }

  .fullfall {
    position: absolute;
    inset: 0;
    overflow: hidden;
    pointer-events: none;
    /* The centre is carved out entirely -- the rain never crosses the
       wordmark or the form, whatever their combined height turns out to
       be (taller than the mockup's placeholder-only stack, since these
       fields keep their labels). */
    -webkit-mask: radial-gradient(ellipse 460px 380px at 50% 52%, transparent 62%, black 78%);
    mask: radial-gradient(ellipse 460px 380px at 50% 52%, transparent 62%, black 78%);
  }

  .fullfall i {
    position: absolute;
    top: -20px;
    width: 2.5px;
    height: 13px;
    border-radius: 2px;
    background: var(--fall-accept);
    animation: fall 5.5s linear infinite;
  }

  .fullfall i.r {
    background: var(--fall-drop);
  }

  .fullfall i.v {
    background: var(--fall-nat);
  }

  @keyframes fall {
    to {
      transform: translateY(106vh);
    }
  }

  .stack {
    position: relative;
    z-index: 2;
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    animation: rise 0.3s ease-out;
  }

  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(8px);
    }
  }

  .wm-box {
    display: inline-block;
    margin: 0 auto 22px;
    padding: 12px 30px;
    border: 1.5px solid var(--now);
    border-radius: 4px;
    box-shadow:
      0 0 14px color-mix(in srgb, var(--now) 22%, transparent),
      inset 0 0 10px color-mix(in srgb, var(--now) 6%, transparent);
    animation: kindle 0.5s ease-out;
  }

  .wm {
    font-size: 34px;
    font-weight: 800;
    letter-spacing: 0.04em;
    color: var(--fg);
  }

  .wm em {
    color: var(--accent);
    font-style: normal;
  }

  @keyframes kindle {
    from {
      box-shadow: none;
      border-color: transparent;
    }
  }

  /* The way out (round 5's storyboard, built without its mockup-only
     strip per round 23/24 -- just the beat, reversed): the box
     collapses and goes dark, then kindles back in, before the ordinary
     rise below plays. Transform/opacity/box-shadow only -- no layout
     property is touched. */
  .screen.reverse .wm-box {
    animation: doorway-reverse 0.55s ease-out;
  }

  @keyframes doorway-reverse {
    0% {
      opacity: 1;
      transform: scale(1);
    }
    35% {
      opacity: 0.3;
      box-shadow: none;
      border-color: transparent;
      transform: scale(0.94);
    }
    55% {
      opacity: 0;
      box-shadow: none;
      border-color: transparent;
      transform: scale(0.9);
    }
    100% {
      opacity: 1;
      transform: scale(1);
    }
  }

  .screen.reverse .stack {
    animation: rise 0.3s ease-out 0.35s both;
  }

  @media (prefers-reduced-motion: reduce) {
    /* Still rain, not no rain: reduced motion declines the falling, not
       the scene. Each stroke stops at its own --y instead of animating
       from above the frame (where the animation's absence would
       otherwise strand every stroke off-screen at top: -20px). */
    .fullfall i {
      animation: none;
      top: var(--y, 50%);
    }
    .stack,
    .wm-box,
    .screen.reverse .stack,
    .screen.reverse .wm-box {
      animation: none !important;
    }
  }

  .col {
    width: 100%;
    max-width: 260px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .form-body {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  h1 {
    margin: 0;
    font-size: 30px;
    font-weight: 600;
    letter-spacing: -0.01em;
    color: var(--fg);
    text-align: center;
  }

  .subtitle {
    margin: -6px 0 14px;
    font-size: 13px;
    color: var(--fg-dim);
    text-align: center;
  }

  label {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  /* The label exists for assistive tech only; the field's visible word
     is its placeholder, in the box as the round-29 scene draws it. */
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }

  input::placeholder {
    color: var(--fg-dim);
    opacity: 1;
  }

  /* Underline credential fields (round 5 v3, the app's input idiom
     applied to this app's own labeled fields rather than the mockup's
     placeholder-only mini-form -- #645 builds on the existing Atlas
     restyle's accessible label structure, it does not revert it). */
  input {
    background: transparent;
    border: 0;
    border-bottom: 1px solid var(--border);
    color: var(--fg);
    border-radius: 0;
    padding: 7px 4px;
    font-size: 13.5px;
    text-align: center;
  }

  input:focus {
    outline: none;
    border-bottom-color: var(--accent);
  }

  .error {
    width: 100%;
    margin: 0;
    color: var(--reject);
    font-size: 13px;
  }

  /* The round-29 scene's Enter: a quiet outlined pill, centred -- not a
     filled bar. The door's weight lives in the brink and the fall; the
     control defers to them. */
  .submit-btn {
    align-self: center;
    margin-top: 12px;
    background: transparent;
    border: 1px solid var(--hair-2);
    color: var(--accent);
    font-weight: 600;
    letter-spacing: 0.04em;
    border-radius: 999px;
    padding: 7px 30px;
    font-size: 13px;
    cursor: pointer;
  }

  .submit-btn:hover {
    border-color: var(--accent);
  }

  .submit-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .divider {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 4px;
    color: var(--fg-dim);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .divider::before,
  .divider::after {
    content: '';
    flex: 1;
    height: 1px;
    background: var(--border);
  }

  .sso-link {
    width: 100%;
    box-sizing: border-box;
    text-align: center;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 8px;
    padding: 10px;
    font-size: 14px;
    text-decoration: none;
  }

  .sso-link:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

</style>
