<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The guided RouterOS setup wizard (#320). Admin-only, matching
  // GET /api/setup/status's own gate.
  //
  // Two things make this more than a copy of the documentation:
  //
  // 1. Every command is emitted with the operator's real values already
  //    in it -- the address their browser is using, the syslog port this
  //    instance actually listens on, and a token this page just minted.
  //    A placeholder left in a saved script fails much later, somewhere
  //    else, and that is a real failure this replaces.
  // 2. Each step reports whether it actually landed, from what arrived
  //    on mikroview's side. Mikroview never connects to a router, so
  //    every check here is an observation, never a poll.

  import { onDestroy } from 'svelte'
  import { createToken, fetchDevices, fetchSetupStatus } from '../lib/api'
  import type { Device, SetupStatus } from '../lib/types'
  import {
    caStep,
    caTrustCommands,
    deviceStanza,
    instanceAddress,
    pushScript,
    ruleTaggingCommands,
    rulesStep,
    pushStep,
    scheduleCommands,
    syslogCommands,
    syslogStep,
    undeclaredDevices,
  } from '../lib/setupsteps'

  let status = $state<SetupStatus | null>(null)
  let devices = $state<Device[]>([])
  let error = $state<string | null>(null)
  let token = $state('')
  let tokenError = $state<string | null>(null)
  let minting = $state(false)
  let tokenDevice = $state('')
  let copied = $state('')

  const address = instanceAddress(window.location)

  async function refresh() {
    try {
      const [s, d] = await Promise.all([fetchSetupStatus(), fetchDevices()])
      status = s
      devices = d
      error = null
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    }
  }

  refresh()
  // Steps land seconds to minutes apart (a scheduled push is 20 minutes
  // by default), so this polls rather than streaming -- cheap, and it
  // stops when the view is left.
  const timer = setInterval(refresh, 5000)
  onDestroy(() => clearInterval(timer))

  async function mintToken() {
    tokenError = null
    if (!tokenDevice) {
      tokenError = 'Choose which router this token is for.'
      return
    }
    minting = true
    const result = await createToken(`setup-${tokenDevice}`, 'ingest', tokenDevice)
    minting = false
    if (typeof result === 'string') {
      tokenError = result
      return
    }
    token = result.value ?? ''
  }

  async function copy(text: string, label: string) {
    try {
      await navigator.clipboard.writeText(text)
      copied = label
      setTimeout(() => (copied = ''), 1500)
    } catch {
      // Clipboard access can fail (permissions, non-secure context).
      // The block stays selectable either way.
    }
  }

  const ca = $derived(status ? caStep(status, address) : null)
  const syslog = $derived(status ? syslogStep(status) : null)
  const rules = $derived(status ? rulesStep(status) : null)
  const push = $derived(status ? pushStep(status) : null)
  const undeclared = $derived(undeclaredDevices(devices))
  const kinds = $derived(status?.pushKinds ?? [])
</script>

<div class="setup">
  <header>
    <h2>Connect a router</h2>
    <p class="lede">
      Run each block on your router. MikroView watches its own side and tells you when a step lands —
      it never connects to your router.
    </p>
  </header>

  {#if error}
    <p class="error">Could not load setup status: {error}</p>
  {/if}

  {#if status}
    <!-- Step 1 -->
    <section class:blocked={ca?.state === 'blocked'}>
      <h3><span class="num">1</span> Trust MikroView's certificate</h3>
      {#if ca}
        <p class="state {ca.state}">{ca.detail}</p>
      {/if}
      {#if ca?.state !== 'blocked'}
        <pre>{caTrustCommands(address)}</pre>
        <button class="copy" onclick={() => copy(caTrustCommands(address), 'ca')}>
          {copied === 'ca' ? 'Copied' : 'Copy'}
        </button>
        <p class="note">
          <code>check-certificate=no</code> belongs on this one line only — it is fetching the thing
          everything else checks against.
        </p>
      {/if}
    </section>

    <!-- Step 2 -->
    <section class:blocked={syslog?.state === 'blocked'}>
      <h3><span class="num">2</span> Send logs to MikroView</h3>
      {#if syslog}
        <p class="state {syslog.state}">{syslog.detail}</p>
      {/if}
      {#if syslog?.state !== 'blocked'}
        <pre>{syslogCommands(address, status.instance.syslogPort)}</pre>
        <button
          class="copy"
          onclick={() => status && copy(syslogCommands(address, status.instance.syslogPort), 'syslog')}
        >
          {copied === 'syslog' ? 'Copied' : 'Copy'}
        </button>
      {/if}
    </section>

    <!-- Step 3 -->
    <section>
      <h3><span class="num">3</span> Tag your firewall rules</h3>
      {#if rules}
        <p class="state {rules.state}">{rules.detail}</p>
      {/if}
      <pre>{ruleTaggingCommands()}</pre>
      <button class="copy" onclick={() => copy(ruleTaggingCommands(), 'rules')}>
        {copied === 'rules' ? 'Copied' : 'Copy'}
      </button>
      <p class="note">
        The letter is how MikroView knows what a rule did — <code>A</code>ccept, <code>D</code>rop,
        <code>R</code>eject, <code>L</code>og. The trailing <code>|</code> is required.
      </p>
    </section>

    <!-- Step 4 -->
    <section>
      <h3><span class="num">4</span> Push router state (optional)</h3>
      {#if push}
        <p class="state {push.state}">{push.detail}</p>
      {/if}
      <p class="note">
        This is what turns addresses into names, fills in the rule lookup buttons, and gives the
        Suggestions page something to suggest from.
      </p>

      {#if !token}
        <div class="mint">
          <select bind:value={tokenDevice} aria-label="Router this token is for">
            <option value="" disabled>Which router is this for?…</option>
            {#each devices as d (d.id)}
              <option value={d.id}>{d.name && d.name !== d.id ? `${d.name} (${d.id})` : d.id}</option>
            {/each}
          </select>
          <button class="primary" onclick={mintToken} disabled={minting}>
            {minting ? 'Creating…' : 'Create token & script'}
          </button>
        </div>
        {#if devices.length === 0}
          <p class="note">
            No routers known yet — finish step 2 first, and this list fills in on its own.
          </p>
        {/if}
        {#if tokenError}<p class="error">{tokenError}</p>{/if}
      {:else}
        <p class="note token-note">
          The token below is shown once and is already in the script. Anyone who can read the script
          on the router can read the token, so it is scoped to that one router.
        </p>
        <pre class="script">{pushScript(address, token, kinds)}</pre>
        <button class="copy" onclick={() => copy(pushScript(address, token, kinds), 'script')}>
          {copied === 'script' ? 'Copied' : 'Copy script'}
        </button>
        <p class="note">Then save it and run it once:</p>
        <pre>{scheduleCommands()}</pre>
        <button class="copy" onclick={() => copy(scheduleCommands(), 'sched')}>
          {copied === 'sched' ? 'Copied' : 'Copy'}
        </button>
      {/if}
    </section>

    <!-- Step 5: optional naming -->
    {#if undeclared.length > 0}
      <section>
        <h3><span class="num">5</span> Name your router (optional)</h3>
        <p class="note">
          {undeclared.length === 1 ? 'This router is' : 'These routers are'} sending logs but
          {undeclared.length === 1 ? 'is' : 'are'} identified by address. Paste this into
          <code>config.yaml</code> and restart to give
          {undeclared.length === 1 ? 'it' : 'them'} a name. MikroView does not edit that file itself:
          it decides which router an event stream is attributed to, so it stays under your control.
        </p>
        {#each undeclared as d (d.id)}
          <pre>{deviceStanza(d.sourceIp, d.name)}</pre>
        {/each}
      </section>
    {/if}
  {:else if !error}
    <p class="note">Loading…</p>
  {/if}
</div>

<style>
  .setup {
    max-width: 780px;
    margin: 0 auto;
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  h2 {
    margin: 0 0 4px;
    font-size: 18px;
    color: var(--fg);
  }

  .lede,
  .note {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
    color: var(--fg-muted);
  }

  section {
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 14px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    background: var(--bg-elevated);
  }

  section.blocked {
    border-color: var(--reject);
  }

  h3 {
    margin: 0;
    font-size: 14px;
    color: var(--fg);
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .num {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    background: var(--bg);
    border: 1px solid var(--border);
    font-size: 11px;
    color: var(--fg-muted);
  }

  .state {
    margin: 0;
    font-size: 13px;
    padding: 6px 10px;
    border-radius: 5px;
    border: 1px solid var(--border);
  }

  .state.done {
    border-color: var(--accept);
    color: var(--accept);
  }

  .state.waiting {
    color: var(--fg-muted);
  }

  .state.partial {
    border-color: var(--log);
    color: var(--log);
  }

  .state.blocked {
    border-color: var(--reject);
    color: var(--reject);
  }

  pre {
    margin: 0;
    padding: 10px;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 5px;
    font-size: 12px;
    line-height: 1.5;
    overflow-x: auto;
    white-space: pre;
    color: var(--fg);
    user-select: all;
  }

  pre.script {
    max-height: 320px;
    overflow-y: auto;
  }

  button {
    align-self: flex-start;
    border-radius: 5px;
    padding: 6px 12px;
    font-size: 12px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  button:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  button.primary {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--bg);
    font-weight: 600;
  }

  button.primary:disabled {
    opacity: 0.5;
  }

  .mint {
    display: flex;
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
  }

  .mint select {
    background: var(--bg);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 7px 10px;
    font-size: 13px;
  }

  .token-note {
    color: var(--fg);
  }

  .error {
    margin: 0;
    color: var(--reject);
    font-size: 13px;
  }

  code {
    font-size: 11px;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 3px;
    padding: 1px 4px;
  }
</style>
