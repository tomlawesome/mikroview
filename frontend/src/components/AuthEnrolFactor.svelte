<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // The forced-enrolment door (#1336, round 61's ratified "two keys" --
  // markup and CSS ported from docs/design/concepts/round-61/
  // two-keys.html, owner acceptance 2026-09-23): the ratified door
  // opened out into a short staged walk. A mono thread under the
  // wordmark names the whole of what stands between you and the app --
  // choose · prove it · keep the codes · enter -- so being stopped
  // reads as being three small steps deep, not locked out. The choice
  // is two keys side by side; proving is a two-column stage, QR left,
  // code right; the ten recovery codes close only on the explicit
  // "I have saved these" (the overlays' own no-escape rule). There is
  // deliberately no way out -- no cancel, no skip, no sign-out, no SSO
  // -- and the mono caption says so: the server 403s everything but the
  // four enrolment routes while this state holds (internal/api/auth.go's
  // secondFactorEnrolPaths), so a way out here would be a lie.
  //
  // Enrolment logic and copy are AuthenticatorOverlay's and
  // PasskeysOverlay's own, reached through the same lib calls
  // (enrolTOTP/confirmTOTP/registerPasskey/qrCode/copyToClipboard) --
  // the overlays themselves are untouched and keep serving the account
  // menu (#1332 is that menu's own open defect; nothing here reaches
  // into it).
  import { authState } from '../lib/auth.svelte'
  import { enrolTOTP, confirmTOTP } from '../lib/api'
  import { registerPasskey } from '../lib/passkeys.svelte'
  import { copyToClipboard } from '../lib/clipboard'
  import { qrCode } from '../lib/qrcode'
  import Fullfall from './Fullfall.svelte'

  // The mockup's five scenes fold to four stages: 'first' and
  // 'no-passkey' are the same choose stage, differing only in whether
  // the passkey key is live -- a runtime fact here (passkeyUsable
  // below), two hand-drawn scenes there.
  type Stage = 'choose' | 'totp' | 'passkey' | 'codes'

  let stage = $state<Stage>('choose')
  let uri = $state('')
  let secret = $state('')
  let code = $state('')
  let name = $state('')
  let recoveryCodes = $state<string[]>([])
  let codesCopied = $state(false)
  let error = $state<string | null>(null)
  let busy = $state(false)

  // The design's rule (PasskeysOverlay's own, restated by round 61):
  // passkeys unusable is a named state, never a hidden row -- the key
  // stays visible, disabled, with one line why. Same deployment/address
  // check as the overlay's unavailableReason, collapsed to the mockup's
  // one drawn line.
  const passkeyUsable = $derived(
    authState.passkeyStatus === 'ready' && location.origin === authState.passkeyOrigin,
  )

  // AuthenticatorOverlay's own extraction, repeated rather than exported
  // from that component's instance scope: the secret lives in the URI's
  // own query string -- one value the server hands over, one place a
  // mistake in reading it could hide.
  function secretFromUri(u: string): string {
    try {
      return new URL(u).searchParams.get('secret') ?? ''
    } catch {
      return ''
    }
  }

  // The mockup draws the secret in groups of four ("GQ4T MNZV ..."), for
  // reading while typing it in by hand -- display only, the QR carries
  // the raw value.
  function groupSecret(s: string): string {
    return s.match(/.{1,4}/g)?.join(' ') ?? s
  }

  async function chooseTOTP() {
    error = null
    busy = true
    const result = await enrolTOTP()
    busy = false
    if (typeof result === 'string') {
      error = result
      return
    }
    uri = result.uri
    secret = secretFromUri(result.uri)
    code = ''
    stage = 'totp'
  }

  function choosePasskey() {
    error = null
    name = ''
    stage = 'passkey'
  }

  // The door opens: re-read the session, which now reports
  // mustEnrolSecondFactor false, and App.svelte plays its ordinary
  // entrance -- the app is simply there, no extra confirmation screen
  // (round 61's own words).
  async function enter() {
    await authState.check()
  }

  async function confirm(e: Event) {
    e.preventDefault()
    if (!code) return
    error = null
    busy = true
    const result = await confirmTOTP(code)
    busy = false
    if (typeof result === 'string') {
      error = result
      return
    }
    authState.hasTOTP = true
    // Codes are minted once, by whichever factor activates first
    // (#1250). On this door that is almost always now -- but an account
    // whose factor was cleared after its codes were issued gets
    // alreadyIssued instead, with nothing new to show. The design draws
    // no scene for that, so the door simply opens.
    if (result.alreadyIssued) {
      await enter()
      return
    }
    recoveryCodes = result.recoveryCodes ?? []
    stage = 'codes'
  }

  async function addPasskey(e: Event) {
    e.preventDefault()
    error = null
    busy = true
    const result = await registerPasskey(name.trim())
    busy = false
    if (typeof result === 'string') {
      error = result
      return
    }
    authState.passkeyCount += 1
    if (result.recoveryCodes) {
      recoveryCodes = result.recoveryCodes
      stage = 'codes'
      return
    }
    // Same already-issued case as confirm() above.
    await enter()
  }

  async function copyCodes() {
    codesCopied = await copyToClipboard(recoveryCodes.join('\n'))
  }

  function finish() {
    void enter()
  }
</script>

<!-- data-void, same as AuthScreen: the door is always the void (#645);
     the mockup's :root palette is the void token block. -->
<main class="door" data-void>
  <div class="beat">
    <Fullfall mask="enrol" />

    <div class="stack">
      <div class="wm">MIKRO<em>VIEW</em></div>

      <div class="thread" aria-label="Steps: choose, prove it, keep the codes, enter">
        {#if stage === 'choose'}
          <div class="thr"><span class="on">choose</span><span class="sep">·</span>prove it<span class="sep">·</span>keep the codes<span class="sep">·</span>enter</div>
        {:else if stage === 'totp' || stage === 'passkey'}
          <div class="thr">choose<span class="sep">·</span><span class="on">prove it</span><span class="sep">·</span>keep the codes<span class="sep">·</span>enter</div>
        {:else}
          <div class="thr">choose<span class="sep">·</span>prove it<span class="sep">·</span><span class="on">keep the codes</span><span class="sep">·</span>enter</div>
        {/if}
      </div>

      {#if stage === 'choose'}
        <section
          class="state"
          aria-label={passkeyUsable
            ? 'Add a second step: choose a kind'
            : 'Add a second step: passkeys unavailable at this address'}
        >
          <h1>Add a second step</h1>
          <p class="subtitle">Your password was right. Signing in here needs a second
            step as well, and this account doesn&rsquo;t have one yet &mdash; pick a
            key, prove it works, and you&rsquo;re in.</p>
          <div class="keys">
            <button type="button" class="key" onclick={chooseTOTP} disabled={busy}>
              <span class="glyph" aria-hidden="true"><span class="codeface">428 116</span></span>
              <span class="cname">Authenticator app</span>
              <span class="cdesc">a code from an app on your phone &mdash; Google
                Authenticator, 1Password, Bitwarden, or similar. Works anywhere,
                IP addresses included.</span>
              <span class="go">set it up &#9656;</span>
            </button>
            {#if passkeyUsable}
              <button type="button" class="key" onclick={choosePasskey} disabled={busy}>
                <span class="glyph" aria-hidden="true">
                  <svg viewBox="0 0 34 34">
                    <path d="M17 30c-3-4-5-8-5-13a5 5 0 0 1 10 0c0 3 .6 6 2 9"/>
                    <path d="M9 22c-1.3-2.3-2-4.6-2-7a10 10 0 0 1 17-7"/>
                    <path d="M28 13c.6 1.2 1 2.6 1 4 0 2.5-.3 5-1 7"/>
                  </svg>
                </span>
                <span class="cname">Passkey</span>
                <span class="cdesc">your fingerprint, face or device PIN &mdash; the
                  browser holds the key, nothing to install.</span>
                <span class="go">set it up &#9656;</span>
              </button>
            {:else}
              <span class="key off" aria-disabled="true">
                <span class="glyph" aria-hidden="true">
                  <svg viewBox="0 0 34 34">
                    <path d="M17 30c-3-4-5-8-5-13a5 5 0 0 1 10 0c0 3 .6 6 2 9"/>
                    <path d="M9 22c-1.3-2.3-2-4.6-2-7a10 10 0 0 1 17-7"/>
                    <path d="M28 13c.6 1.2 1 2.6 1 4 0 2.5-.3 5-1 7"/>
                  </svg>
                </span>
                <span class="cname">Passkey</span>
                <span class="cdesc">passkeys need a web address, and you&rsquo;re viewing
                  MikroView at {location.host} &mdash; your authenticator app works
                  everywhere.</span>
              </span>
            {/if}
          </div>
          {#if error}<p class="error">{error}</p>{/if}
          <p class="noexit">no skipping this one &mdash; every account here needs a
            second step before the door opens.</p>
        </section>
      {:else if stage === 'totp'}
        <section class="state" aria-label="Authenticator app: scan and confirm">
          <h1>Prove it works</h1>
          <p class="subtitle">Scan this with your authenticator app, or type the
            secret in by hand &mdash; then enter the code it shows.</p>
          <div class="prove">
            <div class="half">
              <div class="qr" role="img" aria-label="QR code for the authenticator secret">
                <canvas use:qrCode={uri} width="176" height="176"></canvas>
              </div>
              <div class="seclabel">Secret</div>
              <div class="secret" data-testid="totp-secret">{groupSecret(secret)}</div>
            </div>
            <div class="divide" aria-hidden="true"></div>
            <form class="half cred" onsubmit={confirm} aria-label="Confirm the authenticator app">
              <label class="seclabel" for="enrol-code">Code from the app</label>
              <input
                id="enrol-code"
                type="text"
                inputmode="numeric"
                autocomplete="one-time-code"
                placeholder="123456"
                bind:value={code}
              />
              <p class="aside">Confirming turns this on and signs out everywhere
                else this account is currently signed in. You&rsquo;ll stay signed
                in here.</p>
              {#if error}<p class="error">{error}</p>{/if}
              <button class="enter" type="submit" disabled={busy || !code}>
                {busy ? 'Confirming…' : 'Confirm'}
              </button>
            </form>
          </div>
          {#if passkeyUsable}
            <!-- Disabled while confirm() is in flight, same as the
                 choose-stage keys above -- otherwise a stale confirm
                 result can land after the click has already moved the
                 stage to 'passkey'. -->
            <button class="link-btn" type="button" onclick={choosePasskey} disabled={busy}>
              Use a passkey instead
            </button>
          {/if}
        </section>
      {:else if stage === 'passkey'}
        <section class="state" aria-label="Passkey: name it, then the browser asks">
          <h1>Prove it works</h1>
          <p class="subtitle">Give your passkey a name &mdash; something that will
            remind you which device it&rsquo;s on. Your browser asks for the
            fingerprint, face or PIN itself.</p>
          <form class="cred pkform" onsubmit={addPasskey} aria-label="Add a passkey">
            <label class="seclabel" for="enrol-name">Name</label>
            <input id="enrol-name" type="text" placeholder="this laptop" bind:value={name} />
            <p class="aside">Adding it turns this on and signs out everywhere else
              this account is currently signed in. You&rsquo;ll stay signed in here.</p>
            {#if error}<p class="error">{error}</p>{/if}
            <div class="center">
              <button class="enter" type="submit" disabled={busy}>
                {busy ? 'Waiting…' : 'Add passkey'}
              </button>
            </div>
          </form>
          <!-- Same reason as the passkey link above: chooseTOTP awaits
               enrolTOTP(), so a second click before that settles must
               not be possible. -->
          <button class="link-btn" type="button" onclick={chooseTOTP} disabled={busy}>
            Use an authenticator app instead
          </button>
        </section>
      {:else}
        <section class="state" aria-label="Recovery codes, shown once">
          <h1>Keep the codes</h1>
          <p class="subtitle">Ten recovery codes &mdash; each works once, in place of
            your second step, if you lose it. Save them somewhere safe now: this is
            the only time they are shown.</p>
          <div class="codesblock" data-testid="recovery-codes">
            {#each recoveryCodes as rc (rc)}
              <span class="rc">{rc}</span>
            {/each}
          </div>
          <button class="copy" type="button" onclick={copyCodes}>{codesCopied ? 'Copied' : 'Copy all'}</button>
          <p class="aside">Your ten recovery codes cover your authenticator app and
            your passkeys alike.</p>
          <button class="enter" type="button" onclick={finish}>I have saved these</button>
        </section>
      {/if}
    </div>
  </div>
</main>

<style>
  /* two-keys.html's own CSS, token-mapped to the void block app.css's
     [data-void] re-declares (--void→--bg, --raised→--bg-elevated,
     --hair→--border, --ink→--fg, --ink-2→--fg-muted, --ink-3→--fg-dim;
     --hair-2/--accent/--now keep their names). The mockup's body wash
     rides here too: .screen elsewhere paints flat --bg over it, and the
     mockup draws the glow. */
  .door {
    flex: 1;
    display: flex;
    flex-direction: column;
    position: relative;
    overflow: hidden;
    background:
      radial-gradient(1100px 600px at 75% -15%, rgba(80, 115, 205, 0.07), transparent 60%),
      var(--bg);
    color: var(--fg);
    font-size: 13.5px;
    line-height: 1.5;
  }

  .beat {
    flex: 1;
    display: grid;
    place-items: center;
    position: relative;
    padding: 20px;
  }

  .stack {
    text-align: center;
    width: 100%;
    max-width: 560px;
    position: relative;
    z-index: 2;
    animation: rise 0.3s ease-out;
  }

  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(8px);
    }
  }

  .wm {
    display: inline-block;
    font-size: 34px;
    font-weight: 800;
    letter-spacing: 0.04em;
    padding: 12px 30px;
    border: 1.5px solid var(--now);
    border-radius: 4px;
    box-shadow:
      0 0 14px color-mix(in srgb, var(--now) 22%, transparent),
      inset 0 0 10px color-mix(in srgb, var(--now) 6%, transparent);
    animation: kindle 0.5s ease-out;
    margin-bottom: 16px;
  }

  .wm em {
    font-style: normal;
    color: var(--accent);
  }

  @keyframes kindle {
    from {
      box-shadow: none;
      border-color: transparent;
    }
  }

  /* The thread: the walk's whole shape, in the app's caption voice. */
  .thread {
    font: 11px var(--font-mono);
    color: var(--fg-dim);
    letter-spacing: 0.06em;
    margin-bottom: 26px;
  }

  .thread .sep {
    padding: 0 7px;
    opacity: 0.6;
  }

  .thread .on {
    color: var(--fg);
    border-bottom: 1px solid var(--accent);
    padding-bottom: 1px;
  }

  h1 {
    margin: 0 0 8px;
    font-size: 22px;
    font-weight: 600;
    letter-spacing: -0.01em;
  }

  .subtitle {
    font-size: 13px;
    color: var(--fg-muted);
    margin: 0 auto 20px;
    max-width: 440px;
  }

  /* The two keys: side by side, whole card the control. */
  .keys {
    display: flex;
    gap: 14px;
    justify-content: center;
    align-items: stretch;
  }

  .key {
    flex: 1 1 0;
    max-width: 250px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 18px 16px 16px;
    border: 1px solid var(--hair-2);
    border-radius: 12px;
    text-decoration: none;
    text-align: center;
    background: color-mix(in srgb, var(--bg-elevated) 35%, transparent);
    color: inherit;
    font: inherit;
  }

  button.key:hover,
  button.key:focus-visible {
    border-color: var(--accent);
    outline: none;
  }

  button.key:focus-visible {
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 35%, transparent);
  }

  .glyph {
    height: 44px;
    display: grid;
    place-items: center;
  }

  .glyph .codeface {
    font: 600 21px var(--font-mono);
    letter-spacing: 0.18em;
    color: var(--accent);
  }

  .glyph svg {
    width: 34px;
    height: 34px;
    stroke: var(--accent);
    fill: none;
    stroke-width: 1.6;
    stroke-linecap: round;
  }

  .key .cname {
    font-size: 14px;
    font-weight: 600;
    color: var(--fg);
  }

  .key .cdesc {
    font-size: 11.5px;
    color: var(--fg-muted);
    line-height: 1.45;
  }

  .key .go {
    margin-top: auto;
    font: 10.5px var(--font-mono);
    color: var(--fg-dim);
    letter-spacing: 0.06em;
    padding-top: 8px;
  }

  button.key:hover .go {
    color: var(--accent);
  }

  .key.off {
    border-color: var(--border);
    background: transparent;
  }

  /* The mockup's span.key rule also dims .codeface, but the one key
     that can be off is the passkey, whose glyph is the SVG -- that
     selector would be dead code here. */
  .key.off .cname,
  .key.off .cdesc {
    color: var(--fg-dim);
  }

  .key.off .glyph svg {
    stroke: var(--fg-dim);
    opacity: 0.6;
  }

  .noexit {
    margin: 20px 0 0;
    font: 10.5px/1.6 var(--font-mono);
    color: var(--fg-dim);
    letter-spacing: 0.02em;
  }

  /* Stage two: the proving ground -- what you scan on the left, what
     you type on the right, one hairline between. */
  .prove {
    display: flex;
    gap: 26px;
    align-items: center;
    justify-content: center;
    text-align: left;
  }

  .prove .half {
    flex: 0 1 250px;
  }

  .prove .divide {
    align-self: stretch;
    width: 1px;
    background: var(--border);
  }

  .qr {
    width: 120px;
    height: 120px;
    border-radius: 6px;
    background: #fff;
    padding: 7px;
    margin-bottom: 10px;
  }

  .qr canvas {
    display: block;
    width: 100%;
    height: 100%;
  }

  .seclabel {
    display: block;
    font-size: 10px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  .secret {
    font-family: var(--font-mono);
    font-size: 12px;
    letter-spacing: 0.04em;
    color: var(--fg);
    margin-top: 3px;
    max-width: 230px;
  }

  .cred input {
    display: block;
    width: 100%;
    padding: 7px 4px;
    margin-top: 4px;
    background: transparent;
    border: 0;
    border-bottom: 1px solid var(--hair-2);
    border-radius: 0;
    color: var(--fg);
    font-size: 13.5px;
    font-family: var(--font-sans);
    outline: none;
  }

  .cred input::placeholder {
    color: var(--fg-dim);
    opacity: 1;
  }

  .cred input:focus {
    border-bottom-color: var(--accent);
  }

  .aside {
    font-size: 11.5px;
    color: var(--fg-dim);
    margin: 12px 0 0;
    line-height: 1.5;
  }

  .enter {
    margin-top: 16px;
    padding: 7px 30px;
    font-size: 13px;
    font-weight: 600;
    font-family: var(--font-sans);
    letter-spacing: 0.04em;
    color: var(--accent);
    background: transparent;
    border: 1px solid var(--hair-2);
    border-radius: 999px;
    cursor: pointer;
  }

  .enter:hover,
  .enter:focus-visible {
    border-color: var(--accent);
    outline: none;
  }

  /* Not drawn (the mockup is static); the door's own disabled idiom,
     AuthScreen's .submit-btn:disabled. */
  .enter:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .link-btn {
    display: inline-block;
    margin-top: 12px;
    font-size: 12px;
    color: var(--fg-dim);
    text-decoration: underline;
    text-underline-offset: 2px;
    background: transparent;
    border: 0;
    padding: 0;
  }

  .link-btn:hover,
  .link-btn:focus-visible {
    color: var(--fg-muted);
    outline: none;
  }

  .codesblock {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 6px 22px;
    padding: 12px 18px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-elevated);
    text-align: left;
    width: fit-content;
    margin: 0 auto 10px;
  }

  .rc {
    font-family: var(--font-mono);
    font-size: 12.5px;
    letter-spacing: 0.03em;
    color: var(--fg);
    user-select: all;
  }

  .copy {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 5px 12px;
    font-size: 11.5px;
    cursor: pointer;
  }

  .copy:hover,
  .copy:focus-visible {
    color: var(--fg);
    border-color: var(--hair-2);
    outline: none;
  }

  /* Not drawn (the mockup is static); the door's own error idiom. */
  .error {
    margin: 10px 0 0;
    color: var(--reject);
    font-size: 12px;
  }

  /* The passkey form's inline sizing, moved out of style attributes:
     the app's CSP (default-src 'self') refuses those (see Fullfall's
     own header comment). */
  .pkform {
    max-width: 260px;
    margin: 0 auto;
    text-align: left;
  }

  .center {
    text-align: center;
  }

  @media (max-width: 620px) {
    .keys {
      flex-direction: column;
      align-items: center;
    }

    .key {
      width: 100%;
      max-width: 300px;
    }

    .prove {
      flex-direction: column;
      gap: 16px;
      text-align: center;
    }

    .prove .divide {
      display: none;
    }

    .prove .half {
      flex: none;
      width: 100%;
      max-width: 280px;
    }

    .qr {
      margin: 0 auto 10px;
    }

    .secret {
      margin-left: auto;
      margin-right: auto;
    }

    .thread {
      font-size: 10px;
    }

    .thread .sep {
      padding: 0 4px;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    /* The rain's own still-rain rule lives in Fullfall.svelte; this
       block declines only this screen's entrance beats. */
    .stack,
    .wm {
      animation: none !important;
    }
  }
</style>
