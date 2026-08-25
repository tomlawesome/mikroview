// SPDX-License-Identifier: AGPL-3.0-only
//
// Hand-written detector copy -- extracted from the former Detectors.svelte
// page (#548) when #490 folded it into the engine room's watchers station
// (EngineRoomWatchers.svelte). Kept as its own module rather than inlined
// there because it is pure data, independent of how the bench renders it.
import type { DetectorScope, ListMode } from './types'

export interface DetectorInfo {
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

// Hand-written copy, keyed by definition id. Deliberately a partial
// record rather than an exhaustive one: GET /api/definitions lists
// every detection definition this binary evaluates, and a definition
// with no entry here still renders -- from the server's own name and
// description -- rather than being silently omitted. A detector that
// is evaluating but invisible on the station that exists to say what is
// being watched is the worst failure the bench can have.
export const DETECTORS: Partial<Record<string, DetectorInfo>> = {
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
  unexpected_mail_sender: {
    label: 'Unexpected mail sender',
    explanation:
      'Flags a LAN host connecting outbound to an SMTP port (25, 465, 587) when nothing has marked it as a legitimate mail sender. On a home or small-office network almost nothing should be talking SMTP directly to the internet -- a device that starts is either misconfigured or sending mail on somebody else\'s behalf. Tag the host as a trusted mail sender in Entities to exempt it.',
    scopeNote: 'Hosts restricts which LAN source IPs are watched. Nothing else applies -- the destination ports are the definition\'s own tunable list, not a scope field.',
    example: 'Stop flagging your own mail relay without tagging it: Hosts = 192.168.1.25, mode = deny.',
  },
  known_bad_ip: {
    label: 'Known bad IP',
    explanation:
      'Raises the confidence floor of flags already raised for a source address that appears in one of the locally-fetched blocklists (Spamhaus DROP and friends -- fetched at runtime on your own device, never shipped with mikroview). It raises nothing by itself: its whole job is to make an existing judgement about an address more confident when an independent source already considers that address hostile.',
    scopeNote: 'Hosts restricts which source IPs are looked up at all.',
    example: 'Skip lookups for a range you know is yours: Hosts = 203.0.113.0/24, mode = deny.',
  },
  netclass: {
    label: 'Network class reinforcement',
    explanation:
      'Like known bad IP, but for what kind of network an address belongs to: a Tor exit node or a commercial VPN endpoint reaching inbound raises the confidence floor of flags already raised for it. Only the two high-precision categories count, only inbound traffic is considered, and it never raises a flag on its own -- being behind a VPN is not itself suspicious.',
    scopeNote: 'Hosts restricts which source IPs are classified at all.',
  },
  reputation: {
    label: 'Reputation enrichment',
    explanation:
      'Not a detector: this definition carries the lookup policy every other definition\'s reputation enrichment uses -- how many lookups may be in flight at once, how long one may take, and how large a sample is taken when a flag names a group of addresses rather than one. Turning it off stops that enrichment happening; nothing stops being detected.',
  },
  stale_rule: {
    label: 'Stale firewall rule',
    explanation:
      'Flags a firewall rule label that has not fired in a long time (30 days by default), checked on a timer rather than per event. A rule nothing has matched in a month is usually one that no longer does what its author intended -- a leftover from a service that moved, or one made unreachable by a rule above it.',
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
// bench only ever renders what's meaningful.
// A definition with no entry here shows no scope editor at all, which
// is the honest default: showing a control that does nothing for a
// given definition would be actively misleading, and guessing which
// axes an unrecognized definition consults is exactly a guess. Its
// on/off checkbox still works, and its scope remains settable through
// the API.
export const SCOPE_FIELDS: Partial<Record<string, Array<'hosts' | 'ports' | 'classification' | 'rules'>>> = {
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
  // The five definitions that were always-on passes before the engine
  // port gave them an envelope. Each consults only the source-host
  // axis (scopeMatchesSource) or none at all -- reputation raises
  // nothing per event, and stale_rule runs on a timer, so no scope
  // field reaches either.
  unexpected_mail_sender: ['hosts'],
  known_bad_ip: ['hosts'],
  netclass: ['hosts'],
  reputation: [],
  stale_rule: [],
}

// A one-line "what's currently restricted" sentence -- the worded-state
// grammar (#490) that lets a viewer read a detector's scope as a fact,
// and lets a collapsed bench row say something concrete without opening
// the editor.
export function scopeSummary(sc: DetectorScope): string {
  const parts: string[] = []
  if (sc.hosts?.length) parts.push(`hosts ${sc.hostsMode === 'deny' ? 'deny' : 'allow'}-listed (${sc.hosts.length})`)
  if (sc.classification) parts.push(`${sc.classification} sources only`)
  if (sc.ports?.length) parts.push(`ports ${sc.portsMode === 'deny' ? 'deny' : 'allow'}-listed (${sc.ports.length})`)
  if (sc.rules?.length) parts.push(`rules ${sc.rulesMode === 'deny' ? 'deny' : 'allow'}-listed (${sc.rules.length})`)
  return parts.length > 0 ? parts.join(', ') : 'watching everything in range'
}

export function parseList(v: string): string[] {
  return v
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s.length > 0)
}

export function parsePorts(v: string): number[] {
  return parseList(v)
    .map((s) => Number(s))
    .filter((n) => Number.isInteger(n) && n > 0)
}

export interface DetectorDraft {
  hosts: string
  ports: string
  rules: string
  hostsMode: ListMode
  portsMode: ListMode
  rulesMode: ListMode
  classification: DetectorScope['classification']
}

export function draftFrom(scope: DetectorScope | undefined): DetectorDraft {
  const sc = scope ?? {}
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
