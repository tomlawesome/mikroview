// SPDX-License-Identifier: AGPL-3.0-only

// The RouterOS commands the setup wizard generates (#320), and the
// rules for deciding whether each step has landed.
//
// Kept out of the component so both are testable without a browser:
// getting a command subtly wrong is the failure this whole feature
// exists to prevent, and "the wizard said step 3 was done when it
// wasn't" would be worse than no wizard at all.

import type { Device, SetupMark, SetupStatus } from './types'

// 'quiet' is #487's fifth reading, and the only one that is not a claim
// about a router: a step with nothing to wait for (step 5's naming is
// config-file work) is neither done nor waiting, and calling it either
// would be a small lie in a feature whose whole point is not telling
// them.
export type StepState = 'done' | 'waiting' | 'blocked' | 'partial' | 'quiet'

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
//
// tls.enabled only turns HTTPS off on the API port -- it does not stop
// a certificate from being loaded. main.go loads/generates one whenever
// cfg.TLS.Enabled || cfg.Listen.SyslogTLS != "", and hands that same
// certificate to the syslog TLS listener regardless of tls.enabled
// (#374). So the check only skips entirely when neither HTTP TLS nor
// syslog TLS is on -- matching main.go's own condition for when a
// certificate is even in play.
export function certificateCovers(status: SetupStatus, address: string): boolean {
  if (!status.instance.tlsEnabled && !status.instance.syslogEnabled) return true
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
//
// Filter rules only, deliberately. The prefix convention also covers
// mangle (M) and NAT (N) rules -- see docs/routeros-setup.md -- but
// bulk-enabling log=yes across every mangle rule can turn a router's
// whole packet throughput into log lines, since mark-packet matches per
// packet rather than per connection. That is the established/related
// trap below, one order of magnitude worse, and it is not something to
// do to someone from a "run this" box. The doc walks it per rule.
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
// verified against a real RouterOS 7.23.3 router -- recorded in code as
// routeros.ReviewedVersion (internal/routeros/versions.go), which is
// what the scheduled freshness check compares against so a release
// nobody has read cannot pass unnoticed (#436). Changing the commands
// here without moving that marker leaves the two disagreeing. The field renaming is
// the one place a typo silently breaks a feature without RouterOS
// complaining, so it lives in exactly one place.
const blockSpecs: Record<string, BlockSpec> = {
  'filter-rule': {
    varName: 'rule',
    source: '/ip/firewall/filter',
    record:
      '{"ordinal"=$i; "comment"=($v->"comment"); "chain"=($v->"chain"); "action"=($v->"action"); ' +
      '"srcAddressList"=($v->"src-address-list"); "logPrefix"=($v->"log-prefix"); "dstPort"=($v->"dst-port"); ' +
      '"protocol"=($v->"protocol"); "log"=($v->"log"); "dstAddress"=($v->"dst-address"); "srcAddress"=($v->"src-address"); ' +
      '"connectionState"=($v->"connection-state"); "inInterface"=($v->"in-interface"); "outInterface"=($v->"out-interface")}',
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
  'ip-address': {
    varName: 'addr',
    source: '/ip/address',
    record:
      '{"address"=($v->"address"); "network"=($v->"network"); "interface"=($v->"interface"); "comment"=($v->"comment")}',
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
    // routerosVersion rides the payload rather than a record: it
    // describes the router, not a row of any table (#408 carrying
    // #436's derived version source). Optional server-side, and the
    // same line in every block.
    `:local ${payload} [:serialize to=json value={"kind"="${kind}"; "page"=1; "pages"=1; "routerosVersion"=[/system/resource get version]; "records"=$${recs}}]`,
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

export function syslogStep(status: SetupStatus, devices: Device[] = []): StepStatus {
  if (!status.instance.syslogEnabled) {
    return {
      state: 'blocked',
      detail:
        'Syslog is switched off (listen.syslogTls is empty in config.yaml), so no router-side ' +
        'configuration can work until it is set.',
    }
  }
  // The source-address split (#442) reads as partial, in the voice step
  // 3 uses when events arrive without an action: evidence has arrived,
  // but composed wrongly. Not blocked -- everything on mikroview's side
  // works, which is the whole problem.
  const splits = sourceSplits(devices)
  if (splits.length > 0) {
    return { state: 'partial', detail: sourceSplitObservation(splits) }
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
        'Events are arriving, but none carry an action from a log-prefix. The rules log ' +
        'without one, so rows show "unknown". Add the prefixes below.',
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

// --- The source-address split (#442) -----------------------------------
//
// A router holds an address on every network it routes, and its logs
// arrive stamped with whichever one faces this instance -- frequently
// not the one declared as sourceIp. The declared device then sits
// silent while the real stream auto-discovers under another address,
// and a token minted for the declared identity enriches nothing.
//
// The server pairs the two (Registry.MultihomedCandidates, #499) and
// this module only words it. The wording states both facts and hands
// the operator the one fact only they hold -- whether the two addresses
// are one box. Nothing here claims they are.

export interface SourceSplit {
  // The declared identity, as config.yaml names it: sourceIp, or the id
  // when a declaration carries no address.
  declared: string
  // Every undeclared address logs arrive from, in id order. All of them,
  // never a pick: the server returns candidates, not a diagnosis.
  arriving: string[]
}

// sourceSplits is one entry per declared device the server has paired
// with arriving undeclared addresses.
export function sourceSplits(devices: Device[]): SourceSplit[] {
  return devices
    .filter((d) => d.configured && (d.multihomedCandidates?.length ?? 0) > 0)
    .map((d) => ({ declared: d.sourceIp || d.id, arriving: d.multihomedCandidates ?? [] }))
}

// srcAddressCommand is the recommended remedy: the router keeps the
// address it was declared under, so the token step 4 mints and the
// tables it pushes need no reissuing. Assumes the logging action is
// named mikroview -- step 2's own `add` created it under that name, the
// same assumption every wizard command already makes.
export function srcAddressCommand(declared: string): string {
  return `/system logging action set mikroview src-address=${declared}`
}

// arrivingAddresses is every undeclared address across the splits, in
// first-seen order and without repeats. The server pairs each silent
// declared device with the same discovered set, so with two declared
// devices silent this is the set once, not twice.
export function arrivingAddresses(splits: SourceSplit[]): string[] {
  const seen = new Set<string>()
  for (const s of splits) for (const a of s.arriving) seen.add(a)
  return [...seen]
}

// prose joins addresses the way a sentence does: "a", "a and b",
// "a, b and c". Exported for the wizard body, which words the same
// addresses in the same voice.
export function prose(items: string[], joiner: 'and' | 'or' = 'and'): string {
  if (items.length <= 1) return items.join('')
  return `${items.slice(0, -1).join(', ')} ${joiner} ${items[items.length - 1]}`
}

// sourceSplitObservation is step 2's observation line when the split is
// on: what you told mikroview, what the router shows, no diagnosis.
export function sourceSplitObservation(splits: SourceSplit[]): string {
  const arriving = arrivingAddresses(splits)
  const declared = splits.map((s) => s.declared)
  const from = `${prose(arriving)}, ${arriving.length === 1 ? 'an address' : 'addresses'} you haven't declared`
  const silent = `${prose(declared)}, which you declared in config.yaml, ${declared.length === 1 ? 'has' : 'have'} sent nothing`
  return `Connected — but from ${from}, while ${silent}.`
}

// sourceSplitReceipt is the step list's sub-line for the same reading.
export function sourceSplitReceipt(splits: SourceSplit[]): string {
  return `syslog from ${arrivingAddresses(splits).join(', ')} · declared ${splits.map((s) => s.declared).join(', ')} silent`
}

// --- The claim ledger ---------------------------------------------------
//
// #487 turns the wizard page into a modal, and the modal into a claim
// ledger: every check above is an observation -- mikroview never
// connects to the router -- so each step is a claim about what has
// arrived, and ends in exactly one of done, skipped, or forced past.
// See docs/design/screens/wizard/DESIGN.md, the ratified record this
// implements; where it and a mockup disagree, the record wins.
//
// Everything here is pure: the component renders it and the modal's
// state module drives it, but neither owns the wording or the rules.
// Getting a claim wrong is the failure this whole feature exists to
// prevent, so the claims stay testable without a browser.

// Flavour is how an observation line reads. The record names four and
// calls them the complete set -- waiting, arrived, counting, quiet.
//
// 'attention' is not a fifth flavour of observation: it is the
// mikroview-side check logic this design inherits from #371/#374, where
// nothing is being waited for because nothing router-side can work yet
// (the certificate cannot cover the address; the syslog listener is
// off). Kept distinct precisely so it never borrows the patient,
// nothing-is-wrong voice the waiting flavour is required to use.
export type Flavour = 'waiting' | 'arrived' | 'counting' | 'quiet' | 'attention'

// Outcome is where a step stands in the ledger. 'open' is a step that
// has neither evidence nor a decision yet -- it is not a fourth
// outcome, it is the absence of one.
export type Outcome = 'done' | 'skipped' | 'forced' | 'open'

export interface LedgerStep {
  n: number
  title: string
  // The step body's lead sentence -- one anatomy for every step:
  // lead sentence, the router-side command (with Copy), the observation
  // line.
  lead: string
  status: StepStatus
  flavour: Flavour
  outcome: Outcome
  // The step list's sub-line, carried for the wizard's life: the receipt
  // when evidence arrived, the decision when one was recorded, and the
  // honest gap when neither.
  receipt: string
  // Whether Next runs a check here. Step 3 counts and can only count
  // upward, and step 5 has nothing to wait for, so on both Next is
  // always free -- there is no waiting check to force past.
  hasCheck: boolean
}

// STEP_TITLES is the ratified five, in order. Exported because the step
// list, the header and the spoken announcement all name the same step
// and must not drift.
export const STEP_TITLES = [
  'Trust the certificate',
  'Send logs',
  'Tag firewall rules',
  'Push router state',
  'Name your router',
] as const

export const STEP_COUNT = STEP_TITLES.length

// arrived reports whether a step's evidence has landed. 'partial' counts:
// every step's check is "waiting → arrived", and a partial reading means
// the first push (or the first tagged rule) has already arrived -- it is
// a growing receipt, not a half-failure.
function arrived(state: StepState): boolean {
  return state === 'done' || state === 'partial'
}

// when renders a receipt's timestamp. Local time, no date for something
// that happened today: a receipt is read next to the thing it describes,
// and "14:02:11" says more at a glance than a full ISO string.
function when(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const today = new Date()
  const sameDay =
    d.getFullYear() === today.getFullYear() &&
    d.getMonth() === today.getMonth() &&
    d.getDate() === today.getDate()
  const time = d.toLocaleTimeString(undefined, { hour12: false })
  return sameDay ? time : `${d.toLocaleDateString()} ${time}`
}

// caReceipt: what arrived, when, from where. The source address is the
// "from where" -- on a first run it is also the first proof the router
// can reach mikroview at all.
export function caReceipt(status: SetupStatus): string {
  const fetched = status.sources.filter((s) => s.caFetchedAt)
  if (fetched.length === 0) return ''
  const first = fetched[0]
  const more = fetched.length > 1 ? ` (+${fetched.length - 1} more)` : ''
  return `ca.crt fetched by ${first.source} · ${when(first.caFetchedAt ?? '')}${more}`
}

export function syslogReceipt(status: SetupStatus, devices: Device[] = []): string {
  const splits = sourceSplits(devices)
  if (splits.length > 0) return sourceSplitReceipt(splits)
  const seen = status.sources.filter((s) => s.syslogFirstSeenAt)
  if (seen.length === 0) return ''
  const first = seen[0]
  const more = seen.length > 1 ? ` (+${seen.length - 1} more)` : ''
  return `syslog connected from ${first.source} · ${when(first.syslogFirstSeenAt ?? '')}${more}`
}

export function rulesReceipt(status: SetupStatus): string {
  const withEvents = status.devices.filter((d) => d.events > 0)
  if (withEvents.length === 0) return ''
  const total = withEvents.reduce((n, d) => n + d.events, 0)
  const decoded = withEvents.reduce((n, d) => n + d.decodedActions, 0)
  return `${decoded} of ${total} events carry an action`
}

export function pushReceipt(status: SetupStatus): string {
  let newest = ''
  const kinds = new Set<string>()
  for (const d of status.devices) {
    for (const [kind, at] of Object.entries(d.pushedKinds ?? {})) {
      kinds.add(kind)
      if (!newest || at > newest) newest = at
    }
  }
  if (kinds.size === 0) return ''
  return `${[...kinds].sort().join(', ')} · ${when(newest)}`
}

// nameStep is step 5. It is conditional and informational rather than a
// check: naming a router is config-file work mikroview deliberately does
// not do for the operator (the sourceIp -> id mapping decides who an
// event stream is attributed to, so it stays under file control), which
// means there is nothing here to wait for and nothing to force past.
//
// The row exists either way, so the ledger's count of five is stable --
// it is simply marked "nothing to name" until a push surfaces a device
// config.yaml does not name.
export function nameStep(devices: Device[]): StepStatus {
  const undeclared = undeclaredDevices(devices)
  if (undeclared.length === 0) {
    return { state: 'quiet', detail: 'Nothing to name — every router sending is already declared.' }
  }
  const which = undeclared.map((d) => d.sourceIp || d.id).join(', ')
  return {
    state: 'quiet',
    detail:
      `${undeclared.length === 1 ? 'One router is' : `${undeclared.length} routers are`} ` +
      `identified by address (${which}). Naming ${undeclared.length === 1 ? 'it' : 'them'} is a ` +
      `config.yaml edit — there is nothing to wait for here.`,
  }
}

// LEADS are the step bodies' lead sentences. Wording is design, so it
// lives with the step it belongs to rather than being assembled in the
// component.
const LEADS = [
  "The router has to trust mikroview's certificate authority before it will open a TLS connection. Run this on the router; it fetches the certificate and imports it.",
  'Point the router at this instance. The handshake itself is the evidence — a failed one never counts as arrived.',
  'The letter in the log-prefix is how mikroview knows what a rule did. This tags every existing filter rule by its action, in one pass.',
  'A push turns addresses into names, fills the rule lookups, and gives suggestions something to suggest from. The token below is minted for one router and is already in the script.',
  'Mikroview does not edit config.yaml itself: the sourceIp mapping decides who an event stream is attributed to, so it stays under your control.',
] as const

// stepMarks indexes marks by step, so building the ledger stays one pass.
function markFor(marks: SetupMark[], step: number): SetupMark | undefined {
  return marks.find((m) => m.step === step)
}

// decisionReceipt words a recorded decision for the step list. Skip is
// quiet and force is loud, and the two must stay tellable apart at a
// glance, so they are worded as differently as they are coloured.
function decisionReceipt(mark: SetupMark): string {
  if (mark.outcome === 'skipped') return `skipped by ${mark.actor} · ${when(mark.at)}`
  return `forced past by ${mark.actor} · ${when(mark.at)}`
}

// flavourFor maps a check's state onto how its observation line reads.
// Step 3 is the counting one -- it can only count upward, so any
// arrival there reads as counting rather than as a single arrival.
function flavourFor(step: number, state: StepState): Flavour {
  if (state === 'blocked') return 'attention'
  if (state === 'quiet') return 'quiet'
  if (!arrived(state)) return 'waiting'
  return step === 3 ? 'counting' : 'arrived'
}

// buildLedger is the whole ledger in one pure function: the five steps,
// what each one has observed, and where each one stands.
//
// Evidence outranks a mark, always. That is the record's "forced is not
// failed": a step forced past that later receives its evidence turns
// green and stops explaining anybody's silence, while the audit entry
// stays as history rather than as a scar the interface keeps pointing
// at.
export function buildLedger(status: SetupStatus, devices: Device[], address: string): LedgerStep[] {
  const checks: StepStatus[] = [
    caStep(status, address),
    syslogStep(status, devices),
    rulesStep(status),
    pushStep(status),
    nameStep(devices),
  ]
  const receipts = [
    caReceipt(status),
    syslogReceipt(status, devices),
    rulesReceipt(status),
    pushReceipt(status),
    '',
  ]
  // Steps 3 and 5 have no waiting check to force past: step 3 counts
  // upward and step 5 has nothing to wait for, so Next is always free
  // on both.
  const checked = [true, true, false, true, false]

  return checks.map((check, i) => {
    const n = i + 1
    const mark = markFor(status.marks, n)
    const hasEvidence = arrived(check.state)
    let outcome: Outcome = 'open'
    if (hasEvidence) outcome = 'done'
    else if (mark) outcome = mark.outcome
    return {
      n,
      title: STEP_TITLES[i],
      lead: LEADS[i],
      status: check,
      flavour: flavourFor(n, check.state),
      outcome,
      receipt: hasEvidence ? receipts[i] : mark ? decisionReceipt(mark) : '',
      hasCheck: checked[i],
    }
  })
}

// firstOpenStep is where Run setup… reopens the ledger: the first step
// still waiting. A step already decided is not still waiting, so
// reopening does not drop the operator back onto a question they have
// answered -- and if nothing is left open, the ledger opens on its
// first step rather than on the finish, since reopening deliberately
// shows the ledger as it stands.
export function firstOpenStep(ledger: LedgerStep[]): number {
  const open = ledger.find((s) => s.outcome === 'open' && s.status.state !== 'quiet')
  return open?.n ?? 1
}

// forcedPastRecord is the exact line the amber button writes, quoted on
// the button itself before it is pressed. The operator sees the record
// they are about to create, which is the point: the record is the
// feature, so it is never a surprise produced after the fact.
//
// `actor` is what the button quotes; the server resolves the real one
// from the session when it writes, so a client that lied here would be
// caught by its own audit entry disagreeing.
export function forcedPastRecord(step: LedgerStep, actor: string, now: Date): string {
  return `setup · step ${step.n} forced past · ${notObserved(step)} · ${actor || 'you'} · ${when(now.toISOString())}`
}

// notObserved is the "what was not observed" clause, in mikroview's own
// words rather than a generic "check failed". A check that could not be
// run is a different sentence from one that ran and saw nothing.
export function notObserved(step: LedgerStep): string {
  if (step.status.state === 'blocked') return 'the check could not run on mikroview’s side'
  switch (step.n) {
    case 1:
      return 'no router has fetched /ca.crt'
    case 2:
      return 'no router has opened a syslog connection'
    case 3:
      return 'no events carrying a decoded action have arrived'
    case 4:
      return 'no pushed table has arrived'
    default:
      return 'nothing has arrived'
  }
}

// finishHeadline reads the ledger back in one sentence, as the record
// asks: what is true now, then how the five steps stand.
export function finishHeadline(ledger: LedgerStep[]): string {
  const opening = ledger[2] && arrived(ledger[2].status.state)
    ? 'Logs are flowing.'
    : ledger[1] && arrived(ledger[1].status.state)
      ? 'The router is connected.'
      : 'Nothing has arrived from a router yet.'

  const evidence = ledger.filter((s) => s.outcome === 'done').length
  const skipped = ledger.filter((s) => s.outcome === 'skipped').length
  const forced = ledger.filter((s) => s.outcome === 'forced').length

  const clauses: string[] = []
  clauses.push(
    evidence === 0
      ? 'No step stands on evidence yet'
      : `${count(evidence)} ${evidence === 1 ? 'step stands' : 'steps stand'} on evidence`,
  )
  if (skipped > 0) clauses.push(`${count(skipped)} ${skipped === 1 ? 'was' : 'were'} skipped`)
  if (forced > 0) clauses.push(`${count(forced)} ${forced === 1 ? 'was' : 'were'} forced past`)
  return `${opening} ${clauses.join('; ')}.`
}

// count words small numbers, because "Four steps stand on evidence"
// reads as a sentence and "4 steps" reads as a readout.
function count(n: number): string {
  return ['zero', 'one', 'two', 'three', 'four', 'five'][n] ?? String(n)
}

// silenceExplanation is what a surface with nothing to show says about
// why. It is the reach of "the record is the feature": the Stream's
// empty state does not merely say it is empty, it names the step that
// accounts for the silence and who decided it.
//
// Returns null when the ledger explains nothing -- an empty surface with
// no decision behind it is simply empty, and inventing a cause for it
// would be the opposite of this feature.
export function silenceExplanation(marks: SetupMark[]): string | null {
  const forced = marks.filter((m) => m.outcome === 'forced')
  const skipped = marks.filter((m) => m.outcome === 'skipped')
  const first = forced[0] ?? skipped[0]
  if (!first) return null
  const verb = first.outcome === 'forced' ? 'forced past' : 'skipped'
  const note = first.note ? ` — ${first.note}` : ''
  return `Setup step ${first.step} (${STEP_TITLES[first.step - 1] ?? 'unknown'}) was ${verb} by ${first.actor} on ${when(first.at)}${note}.`
}

// SKIP_CONSEQUENCES is what a skipped step costs, stated plainly in the
// ledger's dashed row. The record is explicit that a skipped step is
// never a reproach: it states its consequence, so the operator can see
// what they chose rather than being told off for choosing it.
export const SKIP_CONSEQUENCES = [
  'the router will not trust this certificate, so its TLS connection will fail',
  'no logs arrive, so the stream stays empty',
  'events arrive without an action, so rows read "unknown"',
  'the stream stays address-only — no names, no rule lookups, nothing to suggest from',
  'routers stay identified by their address rather than a name',
] as const

// announceStep is what a screen reader is told when the step changes:
// which step, its title, and where it stands. The record asks for
// exactly this sentence -- "Step 4 of 5 — Push router state — waiting
// for the first push" -- rather than the step title alone, which would
// announce a move without announcing what was moved to.
export function announceStep(step: LedgerStep): string {
  return `Step ${step.n} of ${STEP_COUNT} — ${step.title} — ${step.status.detail}`
}
