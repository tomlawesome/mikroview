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
       the layer entirely (round 5 fourth batch). Fourteen strokes,
       transform-only, is the whole cost -- no particle system. -->
  <div class="fullfall" aria-hidden="true">
    <i style="left: 4%; animation-delay: 0.2s; opacity: 0.5"></i>
    <i style="left: 11%; animation-delay: 3.1s; opacity: 0.3"></i>
    <i class="r" style="left: 18%; animation-delay: 1.6s; opacity: 0.55"></i>
    <i style="left: 25%; animation-delay: 4.4s; opacity: 0.4"></i>
    <i style="left: 32%; animation-delay: 2.2s; opacity: 0.65"></i>
    <i class="v" style="left: 40%; animation-delay: 5.0s; opacity: 0.45"></i>
    <i style="left: 47%; animation-delay: 0.9s; opacity: 0.3"></i>
    <i class="r" style="left: 55%; animation-delay: 3.7s; opacity: 0.4"></i>
    <i style="left: 62%; animation-delay: 1.2s; opacity: 0.6"></i>
    <i style="left: 69%; animation-delay: 4.8s; opacity: 0.35"></i>
    <i class="v" style="left: 76%; animation-delay: 2.7s; opacity: 0.4"></i>
    <i class="r" style="left: 83%; animation-delay: 0.5s; opacity: 0.6"></i>
    <i style="left: 90%; animation-delay: 3.4s; opacity: 0.35"></i>
    <i style="left: 96%; animation-delay: 1.9s; opacity: 0.5"></i>
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
               (round 15 amended its "passphrase"; "account" stood).
               Labels stay real <label>s, quieted to the scene's idiom --
               #645 builds on the Atlas restyle's accessible structure,
               it does not revert it. -->
          <label>
            <span>account</span>
            <input type="text" autocomplete="username" bind:value={username} required />
          </label>

          <label>
            <span>password</span>
            <input type="password" autocomplete={confirmPassword ? 'new-password' : 'current-password'} bind:value={password} required />
          </label>

          {#if confirmPassword}
            <label>
              <span>confirm password</span>
              <input type="password" autocomplete="new-password" bind:value={passwordConfirm} required />
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
    -webkit-mask: radial-gradient(ellipse 520px 480px at 50% 50%, transparent 60%, black 78%);
    mask: radial-gradient(ellipse 520px 480px at 50% 50%, transparent 60%, black 78%);
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
    padding: 10px 26px;
    border: 1.5px solid var(--now);
    border-radius: 4px;
    box-shadow:
      0 0 14px color-mix(in srgb, var(--now) 22%, transparent),
      inset 0 0 10px color-mix(in srgb, var(--now) 6%, transparent);
    animation: kindle 0.5s ease-out;
  }

  .wm {
    font-size: 26px;
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
    .fullfall {
      display: none;
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
    max-width: 300px;
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

  /* The scene's quiet idiom: lowercase, dim, centred over a centred
     underline -- the round-29 door's composition, kept as a visible
     label rather than the mockup's vanishing placeholder. */
  label span {
    font-size: 12px;
    letter-spacing: 0.02em;
    color: var(--fg-dim);
    text-align: center;
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
    padding: 9px 2px;
    font-size: 14px;
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
    margin-top: 10px;
    min-width: 118px;
    background: transparent;
    border: 1px solid var(--hair-2);
    color: var(--fg);
    font-weight: 500;
    border-radius: 999px;
    padding: 8px 30px;
    font-size: 13.5px;
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
