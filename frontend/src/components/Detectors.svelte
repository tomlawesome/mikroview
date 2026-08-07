<script lang="ts">
  // Admin-only: per-detector on/off + scope restrictions (see
  // internal/detect.Scope's doc comment and docs/configuration.md's
  // "Per-detector toggles" section for exactly what each field does per
  // detector). A real view (see appState.view), gated the same way the
  // old modal was -- only reachable via Toolbar's admin-or-open-gated
  // button -- rather than a route of its own, since it has no meaning
  // for a non-admin caller.
  import { detectorSettingsState } from '../lib/detectorSettings.svelte'
  import type { DetectorName, DetectorScope, ListMode } from '../lib/types'

  interface DetectorInfo {
    label: string
    // What triggers this detector -- adapted from internal/detect's own
    // Config/package doc comments (the source of truth for thresholds
    // and windows), not reinvented here.
    explanation: string
    // What this detector's own scope fields specifically restrict --
    // mirrors internal/detect.Scope's doc comment's per-detector bullet
    // list, since a generic "restricts scope" line would be meaningless
    // (each detector's fields mean something different).
    scopeNote?: string
    // One concrete, detector-specific worked example -- not a single
    // generic example reused everywhere, since what's worth restricting
    // (and why) differs per detector.
    example?: string
  }

  const DETECTORS: Record<DetectorName, DetectorInfo> = {
    port_scan: {
      label: 'Port scan',
      explanation:
        'Flags a source that touches at least 15 distinct destination ports within 60 seconds. A burst of new-connection attempts spread across many ports in a short window is the classic signature of active scanning, not ordinary use of a handful of services.',
      scopeNote:
        'Hosts/Classification restrict which source IPs are tracked at all. Ports restricts which distinct destination ports count toward the 15-port threshold, not which events are tracked in the first place.',
      example: 'Ignore a vulnerability scanner you run yourself: Hosts = 192.168.1.20, mode = deny.',
    },
    activity_spike: {
      label: 'Activity spike',
      explanation:
        "Compares each source's own event rate against an adaptive baseline built from that source's own history (an exponential moving average), flagging when a host's current rate is at least 3x its own baseline and at least 200 events in 60 seconds. Judging each host against its own normal, rather than one fixed number applied to everyone, is what lets an always-busy host avoid false positives.",
      scopeNote:
        "Hosts/Classification restrict which source IPs this detector watches. Ports and rule labels don't apply -- it isn't keyed by destination.",
      example: 'Exclude a legitimately bursty internal backup or sync host: Hosts = 192.168.1.30, mode = deny.',
    },
    critical_port: {
      label: 'Critical-port attempts',
      explanation:
        'Flags an external source making at least 5 attempts within 5 minutes against one of the curated critical ports -- SSH, Telnet, FTP, SMB, RDP, VNC, and RouterOS’s own Winbox/API ports by default. These are worth watching precisely because they’re the services most commonly targeted by internet-wide scanning, and (for the RouterOS-specific ones) a common target once a scanner has fingerprinted a device as RouterOS.',
      scopeNote:
        "Hosts/Classification restrict which source IPs count. Ports narrows the effective subset of the server's configured critical-port list this instance reacts to -- layered on top of, not instead of, that list.",
      example: 'Only watch for RDP and SSH probes, ignoring the rest of the critical-port list: Ports = 22, 3389, mode = allow only.',
    },
    global_spike: {
      label: 'Network-wide volume spike',
      explanation:
        "Compares the whole network's current events-per-second against a slow-moving baseline of itself, firing when current traffic is at least 4x that baseline and at least 5 events/sec -- the floor exists so a near-idle network doesn't “spike” off essentially nothing.",
    },
    distributed_brute_force: {
      label: 'Distributed brute-force',
      explanation:
        'The inverse of critical-port attempts: flags at least 10 distinct external source IPs hitting the same critical port within 5 minutes -- many different attackers against one service, rather than one attacker hitting it repeatedly. The signature of a coordinated or botnet campaign against that service.',
      scopeNote:
        "Hosts/Classification restrict which source IPs count toward a port's distinct-source total. Ports narrows the effective critical-port subset watched, same as critical-port attempts.",
      example: 'Focus botnet-style detection on SSH only: Ports = 22, mode = allow only.',
    },
    outbound_anomaly: {
      label: 'Outbound anomaly',
      explanation:
        'Flags a LAN source contacting at least 25 distinct external destinations within 5 minutes -- one of the strongest signals of a compromised or malware-infected device (C2 beaconing, botnet participation), since almost nothing on an ordinary home or small-office network legitimately starts talking to that many new external hosts at once.',
      scopeNote:
        "Hosts restricts which LAN source IPs are watched. Classification, ports, and rules don't apply -- the source is always internal by design.",
      example: 'Exclude a host that legitimately talks to many external IPs, e.g. a DNS resolver or NTP relay: Hosts = 192.168.1.1, mode = deny.',
    },
    internal_recon: {
      label: 'Internal reconnaissance',
      explanation:
        "Flags a LAN source contacting at least 10 distinct internal destinations within 60 seconds -- a network sweep, the classic lateral-movement signature of an attacker (or malware) that already has a foothold on the LAN and is probing what else is reachable.",
      scopeNote:
        "Hosts restricts which LAN source IPs are watched. Classification, ports, and rules don't apply.",
      example: 'Limit recon watching to the subnet where a sweep would matter most, e.g. a guest/IoT VLAN: Hosts = 192.168.20.0/24, mode = allow only.',
    },
    rule_spike: {
      label: 'Rule hit-rate spike',
      explanation:
        "Uses the same adaptive-baseline technique as the network-wide spike detector, but per firewall rule: flags a rule whose hit rate is at least 5x its own historical baseline and at least 0.2 events/sec (~12/min). A normally-quiet rule suddenly lighting up is visible this way even when it's nowhere near large enough to move the network-wide total -- often the first sign of either a new attack pattern or a misconfiguration.",
      scopeNote:
        "Rules restricts which rule labels this detector reacts to. Hosts, ports, and classification don't apply -- it isn't keyed by any host.",
      example: 'Restrict rule-hit-rate monitoring to one rule you especially care about: Rules = r13, mode = allow only.',
    },
    repeated_drops: {
      label: 'Repeated drops on a port',
      explanation:
        "Flags the same (source, destination port) pair getting dropped or rejected at least 10 times within 15 minutes against one of your locally-hosted services. Unlike critical-port attempts, this isn't restricted to a curated port list or to external sources -- for a self-hoster this is very often a misconfigured port-forward (the real client just keeps retrying a port that isn't open the way they think), not necessarily an attack.",
      scopeNote:
        'Hosts restricts source IP, Ports restricts destination port -- both meaningful and combined with AND. Classification and rules don’t apply.',
      example: 'Stop flagging a known misconfigured client retrying a port you haven’t fixed yet: Hosts = 203.0.113.9, Ports = 8080, mode = deny.',
    },
    low_slow_scan: {
      label: 'Low-and-slow port scan',
      explanation:
        'The paced counterpart to port scan: catches a scan deliberately spread out to stay under that detector’s short 60-second window. Judged over a 3-hour window instead, and gated by several independent signals rather than one count -- at least 8 distinct ports AND 5 distinct hosts, at least 80% of tracked attempts drop/reject, observed for at least 45 minutes, and destination breadth well above this source’s own historical baseline. A single "distinct ports per hour" threshold was deliberately rejected as too prone to false positives from things like container orchestration, health checks, and browsers slowly accumulating distinct destinations.',
      scopeNote:
        'Hosts/Classification restrict which source IPs are tracked. Ports restricts which distinct destination ports count toward its breadth threshold, same as port scan.',
      example: 'Exclude a monitoring/health-check host that legitimately probes many ports slowly: Hosts = 192.168.1.100, mode = deny.',
    },
    off_hours_activity: {
      label: 'Off-hours activity',
      explanation:
        'Flags a source active during a fixed clock window (23:00-06:00 by default) it has no established history of being active in. Judged per hour-of-day against that specific host’s own adaptive baseline for that specific hour (same EMA technique as activity spike, tracked 24 times over -- once per hour), and gated by two independent floors before anything can fire: that hour must have at least 14 distinct prior days of history behind it (a single busy night isn’t a baseline), and the current count must clear an absolute floor of 5 events, not just look large against a near-zero baseline. A naive "any activity in this window" version was deliberately rejected -- a phone syncing or a scheduled job at 3am shouldn’t be indistinguishable from a real deviation.',
      scopeNote:
        "Hosts/Classification restrict which source IPs this detector watches, same as activity spike. Ports and rule labels don't apply -- it isn't keyed by destination.",
      example: 'Exclude a host with a legitimate overnight job (backup, sync): Hosts = 192.168.1.40, mode = deny.',
    },
    device_silence: {
      label: 'Device gone quiet',
      explanation:
        "Checks every configured router's last-seen time on a fixed interval, flagging one that hasn't sent any syslog in at least the configured staleness threshold (15 minutes by default). Unlike every other detector here, this isn't a pattern in the traffic -- it's the absence of it, so it's the one way mikroview notices a router that's stopped talking entirely (crashed, rebooted, lost network, or had its syslog config wiped) rather than a router that's merely quiet right now. A device that's never sent anything at all doesn't count -- see the Fleet view for that state instead.",
    },
  }

  // Which scope fields apply to each detector -- kept in sync with
  // internal/detect.Scope's doc comment. Showing a control that does
  // nothing for a given detector would be actively misleading, so the
  // form only ever renders what's meaningful.
  const SCOPE_FIELDS: Record<DetectorName, Array<'hosts' | 'ports' | 'classification' | 'rules'>> = {
    port_scan: ['hosts', 'classification', 'ports'],
    activity_spike: ['hosts', 'classification'],
    critical_port: ['hosts', 'classification', 'ports'],
    distributed_brute_force: ['hosts', 'classification', 'ports'],
    outbound_anomaly: ['hosts'],
    internal_recon: ['hosts'],
    rule_spike: ['rules'],
    repeated_drops: ['hosts', 'ports'],
    global_spike: [],
    low_slow_scan: ['hosts', 'classification', 'ports'],
    off_hours_activity: ['hosts', 'classification'],
    device_silence: [],
  }

  let expanded = $state<DetectorName | null>(null)
  let errors = $state<Partial<Record<DetectorName, string>>>({})
  let saving = $state<Partial<Record<DetectorName, boolean>>>({})

  // Local editable copies, keyed by detector name -- edits don't touch
  // detectorSettingsState.list until Save, so closing without saving
  // discards them.
  let drafts = $state<Record<string, { hosts: string; ports: string; rules: string; hostsMode: ListMode; portsMode: ListMode; rulesMode: ListMode; classification: DetectorScope['classification'] }>>({})

  function draftFor(name: DetectorName) {
    const existing = detectorSettingsState.list.find((d) => d.name === name)
    const sc = existing?.scope ?? {}
    return {
      hosts: (sc.hosts ?? []).join(', '),
      ports: (sc.ports ?? []).join(', '),
      rules: (sc.rules ?? []).join(', '),
      hostsMode: sc.hostsMode ?? '',
      portsMode: sc.portsMode ?? '',
      rulesMode: sc.rulesMode ?? '',
      classification: sc.classification ?? '',
    }
  }

  function toggleExpanded(name: DetectorName) {
    if (expanded === name) {
      expanded = null
      return
    }
    drafts[name] = draftFor(name)
    expanded = name
  }

  function parseList(v: string): string[] {
    return v
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0)
  }

  function parsePorts(v: string): number[] {
    return parseList(v)
      .map((s) => Number(s))
      .filter((n) => Number.isInteger(n) && n > 0)
  }

  async function toggleEnabled(name: DetectorName, enabled: boolean, scope: DetectorScope) {
    saving[name] = true
    const err = await detectorSettingsState.update(name, enabled, scope)
    saving[name] = false
    errors[name] = err ?? undefined
  }

  async function saveScope(name: DetectorName) {
    const d = drafts[name]
    const existing = detectorSettingsState.list.find((x) => x.name === name)
    const scope: DetectorScope = {
      hosts: parseList(d.hosts),
      hostsMode: d.hostsMode,
      ports: parsePorts(d.ports),
      portsMode: d.portsMode,
      rules: parseList(d.rules),
      rulesMode: d.rulesMode,
      classification: d.classification,
    }
    saving[name] = true
    const err = await detectorSettingsState.update(name, existing?.enabled ?? true, scope)
    saving[name] = false
    errors[name] = err ?? undefined
    if (!err) expanded = null
  }

  // A one-line "what's currently restricted" summary for the collapsed
  // state -- so the scope column has something concrete to show instead
  // of sitting blank until "Edit scope" is clicked.
  function scopeSummary(sc: DetectorScope): string {
    const parts: string[] = []
    if (sc.hosts?.length) parts.push(`hosts ${sc.hostsMode === 'deny' ? 'deny' : 'allow'}-listed (${sc.hosts.length})`)
    if (sc.classification) parts.push(`${sc.classification} sources only`)
    if (sc.ports?.length) parts.push(`ports ${sc.portsMode === 'deny' ? 'deny' : 'allow'}-listed (${sc.ports.length})`)
    if (sc.rules?.length) parts.push(`rules ${sc.rulesMode === 'deny' ? 'deny' : 'allow'}-listed (${sc.rules.length})`)
    return parts.length > 0 ? parts.join(', ') : 'No restrictions -- watching everything in range.'
  }
</script>

<div class="page scrollbar">
  <p class="intro">
    Every detector below only ever raises a flag (see the Flags tab) for a human to review and clear -- nothing
    here blocks, drops, or otherwise acts on traffic. Turn a detector off, or narrow it to specific hosts, ports,
    rules, or a source classification, without touching its underlying thresholds.
  </p>

  <div class="scope-primer">
    <h2>About scope restrictions</h2>
    <p>
      Scope restricts which events a detector reacts to, on top of its own threshold logic. Each field below is an
      independent restriction; when more than one is set on the same detector, they combine with AND. Within one
      field, the mode only matters once you've entered values in it -- an empty list applies no restriction
      regardless of mode. Once populated: <strong>allow only</strong> admits just the listed entries and excludes
      everything else; <strong>deny</strong> excludes the listed entries and admits everything else.
    </p>
  </div>

  <ul class="list">
    {#each detectorSettingsState.list as d (d.name)}
      {@const info = DETECTORS[d.name]}
      <li class="card">
        <div class="card-main">
          <label class="switch">
            <input
              type="checkbox"
              checked={d.enabled}
              disabled={saving[d.name]}
              onchange={(e) => toggleEnabled(d.name, e.currentTarget.checked, d.scope)}
            />
            <span class="name">{info.label}</span>
          </label>
          <p class="explanation">{info.explanation}</p>
          {#if errors[d.name]}
            <p class="error">{errors[d.name]}</p>
          {/if}
        </div>

        <div class="card-scope">
          {#if SCOPE_FIELDS[d.name].length === 0}
            <span class="scope-label">Scope</span>
            <p class="no-scope">No scope restrictions apply to this detector -- it isn't keyed by any host, port, or rule. Only the on/off toggle applies.</p>
          {:else}
            <div class="scope-status">
              <span class="scope-label">Scope</span>
              <button class="scope-toggle" onclick={() => toggleExpanded(d.name)}>
                {expanded === d.name ? 'Hide scope' : 'Edit scope'}
              </button>
            </div>
            {#if expanded !== d.name}
              <p class="scope-summary">{scopeSummary(d.scope)}</p>
            {/if}
          {/if}

          {#if expanded === d.name}
            <div class="scope-section">
              {#if info.scopeNote}
                <p class="scope-note"><strong>What this restricts:</strong> {info.scopeNote}</p>
              {/if}
              {#if info.example}
                <p class="scope-example"><strong>Example:</strong> {info.example}</p>
              {/if}

            <div class="scope-form">
              {#if SCOPE_FIELDS[d.name].includes('hosts')}
                <label class="field">
                  <span>Hosts (comma-separated IPs or CIDRs)</span>
                  <div class="field-row">
                    <select bind:value={drafts[d.name].hostsMode}>
                      <option value="">no restriction</option>
                      <option value="allow">allow only</option>
                      <option value="deny">deny</option>
                    </select>
                    <input type="text" placeholder="192.168.1.50, 203.0.113.0/24" bind:value={drafts[d.name].hosts} />
                  </div>
                </label>
              {/if}
              {#if SCOPE_FIELDS[d.name].includes('classification')}
                <label class="field">
                  <span>Source classification</span>
                  <select bind:value={drafts[d.name].classification}>
                    <option value="">any</option>
                    <option value="internal">internal only</option>
                    <option value="external">external only</option>
                  </select>
                </label>
              {/if}
              {#if SCOPE_FIELDS[d.name].includes('ports')}
                <label class="field">
                  <span>Ports (comma-separated)</span>
                  <div class="field-row">
                    <select bind:value={drafts[d.name].portsMode}>
                      <option value="">no restriction</option>
                      <option value="allow">allow only</option>
                      <option value="deny">deny</option>
                    </select>
                    <input type="text" placeholder="22, 3389" bind:value={drafts[d.name].ports} />
                  </div>
                </label>
              {/if}
              {#if SCOPE_FIELDS[d.name].includes('rules')}
                <label class="field">
                  <span>Rule labels (comma-separated)</span>
                  <div class="field-row">
                    <select bind:value={drafts[d.name].rulesMode}>
                      <option value="">no restriction</option>
                      <option value="allow">allow only</option>
                      <option value="deny">deny</option>
                    </select>
                    <input type="text" placeholder="r13" bind:value={drafts[d.name].rules} />
                  </div>
                </label>
              {/if}
              <div class="scope-actions">
                <button class="cancel" onclick={() => (expanded = null)}>Cancel</button>
                <button class="save" disabled={saving[d.name]} onclick={() => saveScope(d.name)}>
                  {saving[d.name] ? 'Saving…' : 'Save scope'}
                </button>
              </div>
            </div>
          </div>
        {/if}
        </div>
      </li>
    {/each}
  </ul>
</div>

<style>
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .intro {
    margin: 0;
    max-width: 80ch;
    font-size: 13px;
    color: var(--fg-muted);
  }

  .scope-primer {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 12px 16px;
    max-width: 80ch;
  }

  .scope-primer h2 {
    margin: 0 0 6px;
    font-size: 12px;
    font-weight: 600;
    color: var(--fg-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .scope-primer p {
    margin: 0;
    font-size: 13px;
    color: var(--fg-muted);
    line-height: 1.5;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .card {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 16px 18px;
    display: flex;
    gap: 28px;
    flex-wrap: wrap;
  }

  /* Two columns at full width -- explanation on the left, scope status/
     form on the right -- so a wide viewport is actually used instead of
     leaving most of it blank the way the old modal-width column would
     have. Wraps to a single stacked column once there's no room for
     both to breathe. */
  .card-main {
    flex: 1 1 420px;
    min-width: 280px;
  }

  .card-scope {
    flex: 1 1 360px;
    min-width: 280px;
    padding-left: 28px;
    border-left: 1px solid var(--border);
  }

  @media (max-width: 760px) {
    .card-scope {
      padding-left: 0;
      border-left: none;
      padding-top: 14px;
      border-top: 1px solid var(--border);
    }
  }

  .switch {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
  }

  .name {
    font-size: 14px;
    font-weight: 600;
    color: var(--fg);
  }

  .explanation {
    margin: 8px 0 0;
    font-size: 13px;
    color: var(--fg-muted);
    line-height: 1.5;
  }

  .error {
    margin: 8px 0 0;
    color: var(--reject);
    font-size: 12px;
  }

  .scope-label {
    font-size: 12px;
    font-weight: 600;
    color: var(--fg-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .scope-status {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }

  .scope-toggle {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 4px 10px;
    font-size: 12px;
    flex: none;
  }

  .scope-toggle:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .scope-summary {
    margin: 8px 0 0;
    font-size: 12px;
    color: var(--fg-dim);
  }

  .no-scope {
    margin: 8px 0 0;
    font-size: 12px;
    color: var(--fg-dim);
    font-style: italic;
  }

  .scope-section {
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .scope-note,
  .scope-example {
    margin: 0;
    font-size: 12px;
    color: var(--fg-muted);
    line-height: 1.5;
  }

  .scope-example {
    color: var(--fg);
  }

  .scope-form {
    margin-top: 4px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 5px;
    font-size: 12px;
    color: var(--fg-muted);
  }

  .field-row {
    display: flex;
    gap: 8px;
  }

  input,
  select {
    background: var(--bg);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 6px 8px;
    font-size: 13px;
  }

  input {
    flex: 1;
    min-width: 0;
  }

  input:focus,
  select:focus {
    outline: none;
    border-color: var(--accent);
  }

  .scope-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  .cancel,
  .save {
    border-radius: 5px;
    padding: 6px 12px;
    font-size: 12px;
  }

  .cancel {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .cancel:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .save {
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    font-weight: 600;
  }

  .save:hover {
    opacity: 0.9;
  }

  .save:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
