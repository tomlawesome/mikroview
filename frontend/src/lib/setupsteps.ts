// SPDX-License-Identifier: AGPL-3.0-only

// The RouterOS commands the setup wizard generates (#320), and the
// rules for deciding whether each step has landed.
//
// Kept out of the component so both are testable without a browser:
// getting a command subtly wrong is the failure this whole feature
// exists to prevent, and "the wizard said step 3 was done when it
// wasn't" would be worse than no wizard at all.

import type { Device, SetupStatus } from './types'

export type StepState = 'done' | 'waiting' | 'blocked' | 'partial'

export interface StepStatus {
  state: StepState
  // What to show the operator. For 'waiting' this says what is being
  // waited for; for 'blocked' it says what to fix, on MikroView's side.
  detail: string
}

// instanceAddress is the address a router should be pointed at: the one
// the operator's own browser is currently using. Taken from the live
// location rather than configuration, because that is the address
// known to work from at least one place on the network.
export function instanceAddress(loc: { host: string }): string {
  return loc.host
}

// hostname strips a port. Certificate names never carry one, so this is
// what tls.hosts is compared against.
export function hostname(hostPort: string): string {
  // IPv6 literals arrive as [::1]:8080.
  if (hostPort.startsWith('[')) {
    const end = hostPort.indexOf(']')
    return end === -1 ? hostPort : hostPort.slice(1, end)
  }
  const colon = hostPort.lastIndexOf(':')
  return colon === -1 ? hostPort : hostPort.slice(0, colon)
}

// certificateCovers reports whether the running certificate claims the
// address the operator is using. This is step 0, and it exists because
// getting it wrong produces a failure three steps later
// ("name verification failed") whose cause is on MikroView's side, not
// the router's.
export function certificateCovers(status: SetupStatus, address: string): boolean {
  if (!status.instance.tlsEnabled) return true
  const host = hostname(address)
  const hosts = status.instance.hosts
  // An empty list means the generated certificate covers
  // localhost/127.0.0.1 only -- see internal/servertls.defaultHosts.
  const effective = hosts.length > 0 ? hosts : ['localhost', '127.0.0.1']
  return effective.includes(host)
}

// --- The commands -------------------------------------------------------
//
// Every one is emitted with the operator's real values already in it.
// The wizard never renders a placeholder: a saved script still
// containing <mikroview-host> was one of the failures that prompted
// this feature, and it fails much later, somewhere else.

export function caTrustCommands(address: string): string {
  return [
    `/tool fetch url="https://${address}/ca.crt" check-certificate=no dst-path=mikroview-ca.crt`,
    `/certificate import file-name=mikroview-ca.crt passphrase=""`,
  ].join('\n')
}

export function syslogCommands(address: string, syslogPort: string): string {
  const host = hostname(address)
  const port = portOf(syslogPort)
  return [
    `/system logging action add name=mikroview target=remote remote=${host} remote-port=${port} remote-protocol=tls check-certificate=yes`,
    `/system logging add topics=firewall,info action=mikroview`,
  ].join('\n')
}

// portOf takes the port out of a listen address like ":6514" or
// "0.0.0.0:6514" -- the router needs the port, not the bind address.
export function portOf(listenAddr: string): string {
  const colon = listenAddr.lastIndexOf(':')
  return colon === -1 ? listenAddr : listenAddr.slice(colon + 1)
}

// ruleTaggingCommands bulk-tags existing rules by action, which is the
// only way one command can set the right letter: MikroView decodes
// accept/drop/reject from the prefix, so a single generic prefix would
// label every row the same.
export function ruleTaggingCommands(): string {
  return [
    `/ip firewall filter set [find !dynamic action=drop] log=yes log-prefix="D|drop|"`,
    `/ip firewall filter set [find !dynamic action=reject] log=yes log-prefix="R|reject|"`,
    `/ip firewall filter set [find !dynamic action=accept] log=yes log-prefix="A|accept|"`,
    ``,
    `# The established/related accept rule logs every packet, not every`,
    `# connection -- that is your whole traffic volume. Turn it back off:`,
    `/ip firewall filter set [find connection-state=established,related] log=no log-prefix=""`,
  ].join('\n')
}

// pushScript builds the whole state-push script with the token and
// address already embedded. One block per table, each an independent
// fetch, so one failing does not stop the others.
export function pushScript(address: string, token: string, kinds: string[]): string {
  const blocks: string[] = []
  for (const kind of kinds) {
    const b = pushBlock(address, token, kind)
    if (b) blocks.push(b)
  }
  return blocks.join('\n\n')
}

interface BlockSpec {
  varName: string
  source: string
  record: string
}

// blockSpecs mirrors docs/routeros-setup.md's table, which is itself
// verified against a real RouterOS 7.23.3 router. The field renaming is
// the one place a typo silently breaks a feature without RouterOS
// complaining, so it lives in exactly one place.
const blockSpecs: Record<string, BlockSpec> = {
  'filter-rule': {
    varName: 'rule',
    source: '/ip/firewall/filter',
    record:
      '{"ordinal"=$i; "comment"=($v->"comment"); "chain"=($v->"chain"); "action"=($v->"action"); ' +
      '"srcAddressList"=($v->"src-address-list"); "logPrefix"=($v->"log-prefix"); "dstPort"=($v->"dst-port"); ' +
      '"protocol"=($v->"protocol"); "log"=($v->"log"); "dstAddress"=($v->"dst-address"); "srcAddress"=($v->"src-address")}',
  },
  'address-list': {
    varName: 'al',
    source: '/ip/firewall/address-list',
    record:
      '{"list"=($v->"list"); "address"=($v->"address"); "comment"=($v->"comment"); "dynamic"=($v->"dynamic")}',
  },
  'dhcp-lease': {
    varName: 'lease',
    source: '/ip/dhcp-server/lease',
    record: '{"hostname"=($v->"host-name"); "mac"=($v->"mac-address"); "address"=($v->"address")}',
  },
  arp: {
    varName: 'arp',
    source: '/ip/arp',
    record: '{"address"=($v->"address"); "mac"=($v->"mac-address")}',
  },
}

export function pushBlock(address: string, token: string, kind: string): string {
  const spec = blockSpecs[kind]
  if (!spec) return ''
  const recs = `${spec.varName}Recs`
  const payload = `${spec.varName}Payload`
  return [
    `:local ${recs} [:toarray ""]`,
    `:foreach i,v in=[${spec.source} print as-value] do={`,
    `  :local rec ${spec.record}`,
    `  :set ${recs} ($${recs}, {$rec})`,
    `}`,
    `:local ${payload} [:serialize to=json value={"kind"="${kind}"; "page"=1; "pages"=1; "records"=$${recs}}]`,
    `/tool fetch url="https://${address}/api/ingest/routeros" http-method=post http-data=$${payload} ` +
      `http-header-field=("Content-Type: application/json,Authorization: Bearer ${token}") ` +
      `check-certificate=yes output=none`,
  ].join('\n')
}

export function scheduleCommands(): string {
  return [
    `/system script add name=mv-push policy=read,test source="<paste the script above>"`,
    `/system scheduler add name=mv-push interval=20m policy=read,test on-event="/system script run mv-push"`,
    `/system script run mv-push`,
  ].join('\n')
}

// deviceStanza is what an operator pastes into config.yaml to give a
// router a name of their choosing. Emitted rather than written by
// MikroView: the sourceIp -> id mapping decides who an event stream is
// attributed to, and that stays under file control (owner decision on
// #320, 2026-08-13).
export function deviceStanza(sourceIp: string, name: string): string {
  return [`devices:`, `  - sourceIp: "${sourceIp}"`, `    name: "${name || 'my-router'}"`].join('\n')
}

// --- Step status --------------------------------------------------------

export function caStep(status: SetupStatus, address: string): StepStatus {
  if (!certificateCovers(status, address)) {
    const shown = hostname(address)
    return {
      state: 'blocked',
      detail:
        `MikroView's certificate does not cover ${shown}, so the router will refuse it ` +
        `("name verification failed"). Add ${shown} to tls.hosts in config.yaml and restart, ` +
        `then come back — the router needs no change, the same CA signs the new certificate.`,
    }
  }
  if (status.sources.some((s) => s.caFetchedAt)) {
    return { state: 'done', detail: 'A router downloaded the certificate authority.' }
  }
  return { state: 'waiting', detail: 'Waiting for a router to download /ca.crt.' }
}

export function syslogStep(status: SetupStatus): StepStatus {
  if (!status.instance.syslogEnabled) {
    return {
      state: 'blocked',
      detail:
        'Syslog is switched off (listen.syslogTls is empty in config.yaml), so no router-side ' +
        'configuration can work until it is set.',
    }
  }
  if (status.sources.some((s) => s.syslogFirstSeenAt)) {
    return { state: 'done', detail: 'A router has an open syslog connection.' }
  }
  return { state: 'waiting', detail: 'Waiting for a router to connect.' }
}

export function rulesStep(status: SetupStatus): StepStatus {
  const withEvents = status.devices.filter((d) => d.events > 0)
  if (withEvents.length === 0) {
    return {
      state: 'waiting',
      detail:
        'Connected, but no events yet — that means no firewall rule has log=yes, ' +
        'or no traffic has matched one.',
    }
  }
  const undecoded = withEvents.filter((d) => d.decodedActions === 0)
  if (undecoded.length === withEvents.length) {
    return {
      state: 'partial',
      detail:
        'Events are arriving, but none carry an action. The rules log without a log-prefix, ' +
        'so every row shows "unknown". Add the prefixes below.',
    }
  }
  const total = withEvents.reduce((n, d) => n + d.events, 0)
  const decoded = withEvents.reduce((n, d) => n + d.decodedActions, 0)
  if (decoded < total) {
    return {
      state: 'partial',
      detail: `${decoded} of ${total} events carry an action — some rules are still untagged.`,
    }
  }
  return { state: 'done', detail: `${total} events, all with a decoded action.` }
}

export function pushStep(status: SetupStatus): StepStatus {
  const pushed = new Set<string>()
  for (const d of status.devices) {
    for (const kind of Object.keys(d.pushedKinds ?? {})) pushed.add(kind)
  }
  if (pushed.size === 0) {
    return { state: 'waiting', detail: 'Waiting for the first push. Run the script by hand to test it.' }
  }
  const missing = status.pushKinds.filter((k) => !pushed.has(k))
  if (missing.length > 0) {
    return {
      state: 'partial',
      detail: `Arrived: ${[...pushed].sort().join(', ')}. Still missing: ${missing.join(', ')}.`,
    }
  }
  return { state: 'done', detail: 'Every table has been pushed.' }
}

// undeclaredDevices are routers sending syslog that config.yaml does not
// name. They work as they are; declaring one only swaps its address for
// a name of the operator's choosing.
export function undeclaredDevices(devices: Device[]): Device[] {
  return devices.filter((d) => !d.configured)
}
