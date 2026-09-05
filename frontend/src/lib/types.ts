// SPDX-License-Identifier: AGPL-3.0-only

// Mirrors internal/store/event.go's JSON tags.
//
// accept/drop/reject are filter-table verdicts; log is a rule that logs
// without deciding the packet's fate; marked is a mangle mark rule
// (mark-connection / mark-routing / mark-packet) and natted is address
// translation (masquerade / src-nat / dst-nat / redirect). unknown means
// the parser genuinely could not tell -- see internal/store/event.go for
// what it will and will not infer. Adding a member here is a
// deliberately loud change: every Record<Action, ...> in the UI stops
// type-checking until it has a label and a color. See #437.
export type Action = 'accept' | 'drop' | 'reject' | 'log' | 'marked' | 'natted' | 'unknown'

export interface FirewallEvent {
  id: number
  time: string
  deviceId: string
  sourceIp: string
  action: Action
  ruleLabel: string
  // ruleName is a user-configured friendly display name for ruleLabel
  // (see docs/configuration.md's "Friendly names") -- undefined if none
  // is configured. ruleLabel stays the raw value for filtering/grouping.
  ruleName?: string
  chain: string
  inInterface?: string
  outInterface?: string
  connState?: string
  protocol?: string
  srcMac?: string
  srcIp?: string
  srcPort?: number
  dstIp?: string
  dstPort?: number
  // srcHostName/dstHostName are the same friendly-name relationship as
  // ruleName, but for srcIp/dstIp.
  srcHostName?: string
  dstHostName?: string
  // srcPortName/dstPortName (issue #109): same friendly-name
  // relationship, but for srcPort/dstPort -- undefined whenever the port
  // is 0/absent or has no matching entity. Unlike ruleName/hostName,
  // there is no config.yaml fallback source for these (see
  // internal/naming.Resolver.Port's doc comment): an entity is the only
  // way a port ever gets a name.
  srcPortName?: string
  dstPortName?: string
  natIp?: string
  natPort?: number
  natRaw?: string
  srcCountry?: string
  dstCountry?: string
  length?: number
  flags?: string
  raw: string
  // rawTruncated marks `raw` as having been cut to the server's cap
  // (store.MaxRawBytes). Set only for lines far longer than any real
  // RouterOS line, so the row can say so rather than presenting a
  // shortened line as though it were what the router sent.
  rawTruncated?: boolean
}

// A FirewallEvent as held in the client-side buffer, stamped with the
// browser's own receipt time. Used for age-based display expiry (see
// lib/retention.svelte.ts) instead of the event's own `time` field, for
// the same reason the backend windows on its own receipt clock rather
// than the RouterOS device's self-reported one: a device's clock isn't
// guaranteed accurate, but "when this browser got it" always is.
export interface ClientEvent extends FirewallEvent {
  receivedAt: number
}

// Mirrors internal/device/registry.go's Info, plus internal/api/rest.go's
// handleDevices-computed `status` (see that file's deviceView/deviceStatus).
export interface Device {
  id: string
  name: string
  sourceIp: string
  configured: boolean
  firstSeen: string
  lastSeen: string
  eventCount: number
  // status (issue #98): "live" (an event within the configured staleness
  // threshold), "stale" (LastSeen is older than that threshold), or
  // "never_seen" (configured, but zero events ever received). Computed
  // server-side, read-time, on every GET /api/devices -- always fresh,
  // never a value this client itself has to derive or keep in sync.
  status: 'live' | 'stale' | 'never_seen'
  // routerosVersion (issue #675's router cards) is what this device last
  // reported on a routerstate push -- empty until its first push
  // arrives, same absence-is-not-evidence convention as everything else
  // routerstate-derived.
  routerosVersion?: string
  // routerosStanding (#436) is how that reported version compares to the
  // dialect table's covered range -- 'below-minimum' or 'ahead-of-review'
  // are the only two cases the wizard's warning ever speaks to. Omitted
  // (not 'unknown') until a version has been reported, the same
  // absence-is-not-evidence convention routerosVersion already uses.
  routerosStanding?: 'below-minimum' | 'reviewed' | 'ahead-of-review'
  // multihomedCandidates (#442) is present only on a configured device
  // that has received nothing while undeclared devices stream: the
  // source addresses those undeclared devices arrive from, in id order,
  // from the server's Registry.MultihomedCandidates. Candidates, never
  // a diagnosis -- the server cannot know which arriving address (if
  // any) is the same router on another of its interfaces, and neither
  // can this client. The wizard's step 2 and the fleet cards read it.
  multihomedCandidates?: string[]
}

// Mirrors internal/device.MACEntry's JSON shape (GET /api/devices/macs,
// issue #675) -- one persisted MAC address' first/last-seen history and
// the IP it was last paired with. lastIp is what the Entities page's
// named-things table joins against a host entity's own IP key; absent
// when this MAC has never been paired with one.
export interface MACRegistryEntry {
  mac: string
  firstSeen: string
  lastSeen: string
  lastIp?: string
}

// Mirrors internal/store/query.go's Result.
export interface EventsResult {
  events: FirewallEvent[]
  hasMore: boolean
  windowStart: string
  serverTime: string
}

// Mirrors internal/store/ring.go's RuleCount.
export interface RuleCount {
  rule: string
  count: number
}

// Mirrors internal/store/ring.go's TimeBucket.
export interface TimeBucket {
  time: string
  byAction: Partial<Record<Action, number>>
}

// Mirrors internal/store/ring.go's HourTop (#644 round 21's top
// port/top talker table columns, served by GET /api/stats/tops). talker
// and port are absent exactly when complete is false, or when the
// minute genuinely held nothing to count -- either way, the table shows
// an em dash rather than treating an absent field as zero.
export interface HourTopBucket {
  time: string
  talker?: string
  port?: string
  complete: boolean
}

// Mirrors internal/api/rest.go's handleHealthz response. version is the
// build-time-stamped short commit SHA ("dev" for a plain local build) --
// the same value `mikroview -version` prints, and the only place a
// running deployment's build is checkable without host/container
// access.
export interface Healthz {
  status: string
  time: string
  uptime: string
  uptimeSeconds: number
  version: string
}

// Mirrors internal/api/rest.go's handleStats response.
export interface Stats {
  total: number
  byAction: Partial<Record<Action, number>>
  topRules: RuleCount[]
  timeSeries: TimeBucket[]
  eventsPerSecond: number
  capacity: number
  count: number
  windowSeconds: number
  // How far back the buffer actually reaches, as opposed to how far back
  // it was configured to: null when it holds nothing. Not the same as a
  // query's windowStart, which is the configured retention -- capacity
  // eviction moves this one and leaves that one alone (#703).
  oldestHeld: string | null
  // When this process started observing, and when the snapshot its
  // counters were restored from was taken (#795). Mirrors
  // internal/store.Stats: `restoredTo` is absent on a cold start rather
  // than null, so its presence *is* the answer to "was this a warm
  // restart"; `liveSince` is optional here only so an older server that
  // does not send it leaves the statement off rather than rendering an
  // invalid date. See lib/provenance.ts for the one place they are read.
  liveSince?: string
  restoredTo?: string
  connectedClients: number
  // The event buffer's budget, the range it may be moved within, and
  // what the process is actually costing (#796) -- mirrors
  // internal/api.StoreSettings. Optional so a test fixture or an older
  // server that does not send it leaves the memory control absent
  // rather than drawing a slider over undefined bounds.
  memory?: StoreMemory
  // Syslog listener saturation -- mirrors internal/syslog.ListenerStats.
  // Optional so an older server (or a test fixture) that does not send
  // it simply shows nothing rather than rendering NaN.
  syslog?: SyslogListenerStats
}

// Mirrors internal/api.StoreSettings (#796). Every figure is in bytes.
export interface StoreMemory {
  // store.maxMemory in effect right now.
  maxMemory: number
  // The ends of the slider. See internal/config.MaxMemoryCeiling for the
  // headroom rule that produced max.
  min: number
  max: number
  // What max is a share of -- the cgroup limit if the server is in one,
  // otherwise the machine's RAM. Zero when the server could read
  // neither, in which case the track's right-hand end says the ceiling
  // is a conservative default rather than naming a total nobody knows.
  hostTotal: number
  // internal/config.AssumedBytesPerEvent, so a proposed budget turns
  // into an event count without a second copy of that constant here.
  bytesPerEvent: number
  // What the server process currently holds from the operating system.
  resident: number
  // Whether the figure came from the settings store rather than
  // config.yaml.
  stored: boolean
}

// GET/PUT /api/settings/history (#910, round 42's disk group). Every
// byte figure is in bytes; days are whole days.
export interface HistorySettings {
  // Whether a key file is mounted. Without one nothing is kept on disk
  // whatever `enabled` says, and the group draws no control at all.
  keyed: boolean
  enabled: boolean
  // The days allowed, 1-365.
  days: number
  // The byte cap applied alongside days -- whichever is reached first
  // lets the oldest day go.
  maxBytes: number
  // What is actually on disk right now, or null when nothing is.
  held: HistoryHeld | null
  // True when the cap, not the days, is what decides the window.
  capped: boolean
  // Today's rate on disk, bytes per day once compressed. 0 when there is
  // no rate to reckon from yet, in which case every "at today's rate"
  // phrase is left off rather than invented.
  bytesPerDay: number
}

export interface HistoryHeld {
  // How many day files are on disk.
  days: number
  // The oldest and newest day files' dates, YYYY-MM-DD.
  oldest: string
  newest: string
  bytes: number
}

// Mirrors internal/syslog.ListenerStats. The connection pool is finite,
// and filling it means a router MikroView is meant to be watching gets
// turned away with its log lines never arriving -- a silent blackout,
// which used to be visible only as a repeated line in the container log.
export interface SyslogListenerStats {
  inUse: number
  capacity: number
  // How many slots only routers listed under `devices:` in config.yaml
  // may use. 0 when no devices are declared, since holding capacity back
  // for nobody would only shrink the pool.
  reservedForConfigured: number
  rejected: number
  // Above zero means a *declared* router was turned away, which is the
  // condition worth showing rather than saturation on its own.
  rejectedConfigured: number
}

// Mirrors internal/api/auth.go's sessionResponse.
export interface AuthSession {
  setupRequired: boolean
  authenticated: boolean
  username?: string
  role?: 'admin' | 'user' | 'viewer'
  // False once the account signs in only through the identity provider.
  // Gates whether "Connect SSO" is offered -- there is nothing left to
  // convert otherwise.
  hasLocalPassword?: boolean
  ssoAvailable: boolean
  // This session's own start (#677's sessions row: "signed in 4 d") --
  // when this login happened, not when the account was created. Absent
  // while unauthenticated, and on an older server that predates it.
  signedInSince?: string
}

// Mirrors internal/api's userSummary. Deliberately not the server's
// full auth.User -- that carries a password hash, and this type exists
// so there is nothing on the client side to accidentally render.
export interface UserSummary {
  id: string
  username: string
  role: 'admin' | 'user' | 'viewer'
  createdAt: string
  lastLogin?: string
  hasLocalPassword: boolean
  sso: boolean
}

// Mirrors internal/api/tokens.go's tokenResponse. value is present only
// in the response to creating a token (see api.ts's createToken) --
// never on a listed token, and never recoverable afterward.
export interface ApiToken {
  id: string
  name: string
  // Mirrors internal/auth.TokenKind: "api" is a read-only token,
  // "ingest" a RouterOS push token scoped to one device (#186/#326).
  kind: 'api' | 'ingest'
  // Set only on ingest tokens -- the config.yaml device id the token
  // speaks for.
  device?: string
  createdAt: string
  lastUsedAt?: string
  value?: string
}

// Mirrors internal/reputation/reputation.go's Result.
export interface ReputationResult {
  ip: string
  ports?: number[]
  hostnames?: string[]
  vulns?: string[]
  tags?: string[]
  abuseScore?: number
  totalReports?: number
  countryCode?: string
  isp?: string
  // usageType/isTor (issue #58): AbuseIPDB-only, same as abuseScore --
  // absent when that source isn't configured or has nothing to report.
  usageType?: string
  isTor?: boolean
  // netClass (issue #114): mikroview's own local attribution of the IP to
  // a Tor exit / VPN / datacenter / privacy relay, from the network-class
  // feeds. Present only on a live lookup (not on a flag's captured
  // reputation snapshot), and only when the IP matched a listed range.
  // Display context, never a score.
  netClass?: NetClass
}

export interface NetClass {
  category: string
  source: string
  label: string
  detail?: string
  // Pre-rendered "Label (Detail)", so the UI does not reassemble it.
  display: string
}

// Mirrors internal/flags.Flag's JSON tags.
export type FlagType =
  | 'port_scan'
  | 'activity_spike'
  | 'critical_port'
  | 'global_spike'
  | 'distributed_brute_force'
  | 'outbound_anomaly'
  | 'internal_recon'
  | 'rule_spike'
  | 'repeated_drops'
  | 'low_slow_scan'
  | 'off_hours_activity'
  | 'device_silence'
  // new_device (issue #103 phase 1): raised directly from the ingest
  // path (main.go), not through internal/detect like every other flag
  // type above -- see internal/flags.TypeNewDevice. This is exactly the
  // divergence DetectorName's doc comment below used to warn about: it
  // has no corresponding detector-settings entry (no on/off toggle, no
  // scope), so it must NOT be added to DetectorName.
  | 'new_device'
  // stale_rule (issue #102): raised by a standalone periodic sweep (see
  // internal/detect.StaleRuleDetector), not the generic per-event
  // detector-enable/scope pipeline every DetectorName-backed type goes
  // through -- same "no matching detector-settings entry" exception as
  // new_device above, not an oversight.
  | 'stale_rule'
  // unexpected_mail_sender (issue #108): a LAN source, untagged
  // "trusted-mail-sender" in the entities store, originating an
  // outbound connection to an external destination on an SMTP port (25,
  // 465, 587) -- see internal/flags.TypeUnexpectedMailSender. Always on
  // and deterministic (no threshold/window to tune), so like
  // new_device/stale_rule above it has no matching DetectorName entry.
  | 'unexpected_mail_sender'
  // known_bad_ip (issue #113 Part B): a source IP matching a locally-
  // cached CIDR range from a vetted threat-intel feed (Spamhaus DROP/
  // EDROP by default -- see internal/blocklist's doc comment). Raised
  // directly from internal/detect.Observe on a deterministic list-
  // membership check, not gated by DetectorName/Scope -- same "no
  // matching detector-settings entry" exception as new_device/stale_rule
  // above.
  | 'known_bad_ip'

// The definition ids the engine room's watchers station carries
// hand-written copy for (lib/detectorCopy.ts). No longer a closed set of
// everything the server evaluates: GET /api/definitions lists every
// shipped definition, including the five that were always-on passes
// before the engine port gave them an envelope (unexpected_mail_sender,
// stale_rule, known_bad_ip, netclass, reputation). A definition whose id
// is not in this union still renders -- from the server's own name and
// description -- so the station can never silently omit something that
// is actually evaluating. See EngineRoomWatchers.svelte.
export type DetectorName =
  | 'port_scan'
  | 'activity_spike'
  | 'critical_port'
  | 'global_spike'
  | 'distributed_brute_force'
  | 'outbound_anomaly'
  | 'internal_recon'
  | 'rule_spike'
  | 'repeated_drops'
  | 'low_slow_scan'
  | 'off_hours_activity'
  | 'device_silence'

// Mirrors internal/detect.ListMode.
export type ListMode = '' | 'allow' | 'deny'

// Mirrors internal/detect.Scope's JSON tags. See that type's doc
// comment (and docs/configuration.md's "Per-detector toggles" section)
// for exactly which fields each detector consults.
export interface DetectorScope {
  hosts?: string[]
  hostsMode?: ListMode
  ports?: number[]
  portsMode?: ListMode
  classification?: Scope
  rules?: string[]
  rulesMode?: ListMode
}

// One detection definition as the detector-settings page needs it,
// projected from GET /api/definitions' own envelope (see Definition
// below). name is the definition's id -- the same string the old
// /api/detectors endpoint keyed on, so a deployment's saved scope and
// the page's hand-written copy both still line up -- while label and
// description carry the server's own operator-facing text, which is the
// only copy that exists for a definition this page has no entry for.
export interface DetectorSettings {
  name: string
  label: string
  description?: string
  enabled: boolean
  scope: DetectorScope
  // Carried through from Definition.learning (#639) -- see that field's
  // doc comment.
  learning?: LearningState
  // Carried through from Definition.params/paramSchema (#677's
  // port-scan window row, the detector's own numeric tuning -- distinct
  // from scope above, which restricts what the detector *watches*
  // rather than the threshold it fires at).
  params?: Record<string, unknown>
  paramSchema?: DefinitionParamSchema[]
  // Carried through from Definition.provenance.origin (#787). The
  // editing panel needs it because two of its actions are only offered
  // where the server can perform them: a shipped definition's name
  // belongs to the binary that ships the logic and cannot be renamed,
  // and reset only means something for one that has stock params to go
  // back to.
  origin?: DefinitionOrigin
  // Whether any param currently differs from what the definition shipped
  // with -- Definition.distance flattened to the one bit the bench shows
  // (#787), so a row can say it has been tuned without the panel open.
  overridden?: boolean
}

// Mirrors internal/api's definitionView (issue #407) -- one definition
// as the definitions API serves it: the whole engine envelope, plus the
// four things a caller cannot derive from it (whether this binary can
// evaluate it, how far its params are from the shipped defaults, whether
// it can answer a replay question, and -- for an expectation -- whether
// anything can actually feed it).
export type DefinitionIntent = 'detection' | 'expectation'
export type DefinitionKind = 'declarative' | 'programmatic'
export type DefinitionOrigin = 'shipped' | 'custom'

// One param's declared type/bounds/description, so a tuning control is
// rendered from the server's declaration rather than from a second copy
// of every definition's knobs written in TypeScript -- the duplication
// the evaluation-engine ADR exists to remove, which re-listing them here
// would simply move up a layer.
export interface DefinitionParamSchema {
  name: string
  type: 'int' | 'duration' | 'float' | 'portList' | 'hostList' | 'stringList' | 'enum' | 'bool'
  description: string
  unit?: string
  required?: boolean
  min?: number
  max?: number
  enumValues?: string[]
}

export interface DefinitionProvenance {
  origin: DefinitionOrigin
  shippedParams?: Record<string, unknown>
}

// A definition either produces a replay receipt or declares why it never
// can; known is false only when the server could not build it at all.
// Kept as three fields rather than collapsed into one, because "cannot
// build" and "can never answer" are different facts and showing the
// second for the first would state a property nobody established.
export interface DefinitionReplayability {
  known: boolean
  capable: boolean
  reason?: string
}

// What one replay covered (internal/api's windowView, engine.Window).
// Mandatory on a receipt and never omitted: a count without the window it
// was counted over is the overclaim #403's contract exists to rule out.
// duration is a Go duration string ("4h12m0s") -- read it with
// parseGoDurationSeconds, the same way a duration param is read.
export interface ReplayWindow {
  start: string
  end: string
  duration: string
  eventCount: number
}

// One emission a replay would have produced (internal/api's sampleView),
// bounded server-side -- see ReplayReceipt.sampleTruncated.
export interface ReplaySample {
  at: string
  target: string
  detail: string
  ports?: number[]
  hosts?: string[]
  labels?: string[]
  provisional: boolean
}

// The answer when the corpus was long enough to answer honestly
// (internal/api's receiptView, engine.Receipt).
//
// The two truncation flags are separate facts and not interchangeable:
// corpusTruncated means the corpus read was cut short, so emissionCount
// is a floor rather than a total; sampleTruncated means only that the
// listed sample is bounded, with emissionCount still exact.
export interface ReplayReceipt {
  window: ReplayWindow
  emissionCount: number
  sample: ReplaySample[]
  sampleTruncated: boolean
  corpusTruncated: boolean
  anyProvisional: boolean
}

// The answer when it could not be answered honestly (internal/api's
// declineView, engine.Decline): the corpus held less traffic than the
// definition's window needs. corpusSpan and definitionWindow are Go
// duration strings, like ReplayWindow.duration.
export interface ReplayDecline {
  reason: string
  corpusSpan: string
  definitionWindow: string
}

// Exactly one of receipt or decline is set, mirroring engine.Result's own
// structural either/or -- a caller has to handle the decline rather than
// reading a short corpus as a receipt with a suspiciously small count.
export interface ReplayResult {
  receipt?: ReplayReceipt
  decline?: ReplayDecline
  // The same replay run again with the definition's live params -- the
  // number the candidate above is being compared against, "currently: 41"
  // beside "would have fired 3 times" (#786). Receipt-or-decline exactly
  // like the outer result, because it is the same kind of answer; it
  // never carries a `current` of its own.
  //
  // Present only where the request carried a candidate. With an empty
  // candidate the receipt above already *is* the current number, so the
  // server omits this rather than repeating it.
  current?: ReplayResult
}

export interface Definition {
  id: string
  name: string
  description?: string
  intent: DefinitionIntent
  kind: DefinitionKind
  enabled: boolean
  scope?: DetectorScope
  params?: Record<string, unknown>
  paramSchema?: DefinitionParamSchema[]
  provenance: DefinitionProvenance
  available: boolean
  // Present only where a param differs from what the definition shipped
  // with -- an empty object and an absent key both mean "stock".
  distance?: Record<string, { shipped?: unknown; current?: unknown }>
  replay: DefinitionReplayability
  // Set only for an expectation definition; see WatchlistCoverage.
  coverage?: WatchlistCoverage
  // The operator-facing entry an expectation converts back to. Absent
  // for a detection definition.
  expectation?: WatchlistEntry
  // The structure an operator-authored detector carries: its match
  // conditions and the aggregation around them (#502). Absent for a
  // shipped definition, whose structure is Go in the binary, and for an
  // expectation, whose structure is fixed. Threshold and window are not
  // here -- they are ordinary params, tuned through the same editor as
  // every other definition's.
  detection?: DefinitionDetection
  // What this definition costs the ingest path. Set only where an
  // operator chose the conditions that decide it.
  dispatch?: DefinitionDispatch
  // Baseline warm-up state (#639) -- absent entirely for a definition
  // with no warm-up concept (LearningReporter not implemented). See
  // LearningState's own doc comment for what each field means and
  // EngineRoomWatchers.svelte for the five states this renders as.
  learning?: LearningState
}

// Mirrors internal/api's baselineFloorView JSON shape (#639) -- the
// minimum history a baseline-backed definition needs before a key can be
// trusted. Each field carries `omitempty` server-side: a dimension the
// floor does not bind (BaselineFloor's own Go doc comment) is omitted
// entirely rather than sent as a meaningless 0, so absent and 0 both
// mean "no floor on this dimension" and must be treated identically.
// MinDuration renders as days, MinSamples as samples, both together
// where both bind (off_hours).
export interface LearningFloor {
  minDurationSeconds?: number
  minSamples?: number
}

// Mirrors internal/engine.LearningProgress's JSON shape -- how far the
// single furthest-along not-yet-ready key has gotten.
export interface LearningProgress {
  observedForSeconds: number
  samples: number
}

// Mirrors internal/engine.LearningState's JSON shape (#639): one
// definition's baseline warm-up status, aggregated live across every
// key the running engine currently holds -- not a persisted, possibly
// stale, view (see the issue's "ask the live engine" architecture
// decision). floor is always present, including when keys is 0.
// nearest is omitted both when every observed key is ready and when no
// key has been observed at all, so a caller must check keys/ready
// before treating an absent nearest as "everything is ready."
export interface LearningState {
  floor: LearningFloor
  keys: number
  ready: number
  nearest?: LearningProgress
}

// The condition language, unchanged from the one expectations and
// shipped detectors already use -- there is no second one.
export interface DefinitionCondition {
  field: string
  operator: string
  values: string[]
}

export interface DefinitionDetection {
  conditions: DefinitionCondition[]
  key: string
  counting: string
  distinctField?: string
  // The sentence a raised flag shows. Its placeholders are a closed set
  // the server validates when the definition is created.
  detailTemplate: string
}

// alwaysConsulted is true when the definition's conditions give the
// engine's dispatch index nothing to narrow on, so it is evaluated
// against every event rather than only the ones that could match. Such a
// detector is accepted rather than refused -- reason says what it costs.
export interface DefinitionDispatch {
  alwaysConsulted: boolean
  reason?: string
}

// What GET /api/definitions returns alongside the definitions: whether
// the pushed filter tables every coverage answer was derived from can
// honestly be treated as all of them (#367). When complete is false,
// every definite negative has already been downgraded to 'unknown'
// server-side, and this is the only place a caller can see why.
export interface CoverageEvidence {
  complete: boolean
  missingDevices?: string[]
}

// Mirrors internal/entities.Entity's JSON tags (issue #107). type is
// deliberately a plain string, not a closed union, mirroring the
// backend's own "extensible, not a fixed enum" choice (internal/
// entities.Store never validates Type); 'host'/'rule'/'port' below are
// just the values this UI knows how to label/discover today, not a
// validation allowlist -- an arbitrary string still round-trips fine.
export type EntityType = 'host' | 'rule' | 'port' | (string & {})

export interface Entity {
  type: EntityType
  key: string
  label?: string
  tags?: string[]
}

// Mirrors internal/api's nameProvenanceResponse (issue #413): where the
// name currently shown for one row token comes from, and whether saving
// a label for it here would change anything.
//
// `editable` is the field the inline editor is built around. The owner
// ruled on 2026-08-22 that RouterOS keeps winning (#186 step 4c), so a
// label saved for a host the router already names is stored and then
// never displayed. POST /api/entities cannot tell the caller that -- the
// write really did succeed -- so the editor asks this first and renders
// an explanation instead of an input when the answer is false. `source`
// says which pushed table holds the winning name and `router` says on
// which device, because "change it on the router" is not actionable
// without both.
export type NameSource =
  | 'none'
  | 'entity'
  | 'config'
  | 'router-dns-static'
  | 'router-dhcp-lease'
  | 'router-wireguard-peer'
  | 'router'

export interface NameProvenance {
  type: EntityType
  key: string
  device?: string
  // name is what is displayed today, '' when nothing names the key.
  name: string
  source: NameSource
  // label is the operator's own saved label for this key, reported even
  // when a router-pushed name out-ranks it -- so the editor can say
  // "your label is not what is shown" rather than presenting an empty
  // field over the top of one that exists.
  label: string
  editable: boolean
  router?: string
}

// Mirrors internal/audit.Entry's JSON tags (issue #112) -- one recorded
// admin-privileged mutation (user created, entity upserted/deleted, API
// token created/revoked, detector setting changed, flag exclusion
// removed). action is deliberately a plain string, not a closed union,
// same "extensible, not gatekept client-side" reasoning EntityType above
// already follows for the backend's own internal/audit.Entry.Action.
export interface AuditEntry {
  id: number
  timestamp: string
  actor: string
  action: string
  target: string
  detail?: string
}

// Mirrors internal/audit.Result's JSON tags -- the response to
// GET /api/audit, same HasMore-signals-truncation shape as EventsResult.
export interface AuditResult {
  entries: AuditEntry[]
  hasMore: boolean
}

// Mirrors internal/rules.Usage's JSON tags (issue #103) -- one rule
// label's lifetime firing record, served by GET /api/rules. Used by the
// Entities panel (issue #109) as the "discovered but unnamed rules"
// source: every rule label mikroview has ever seen fire, independent of
// whether it currently has an entity/label.
export interface RuleUsage {
  rule: string
  firstSeen: string
  lastSeen: string
  count: number
}

// Mirrors internal/flags.NATInfo's JSON tags.
export interface NATInfo {
  ip?: string
  port?: number
  raw?: string
}

// Mirrors internal/flags.HostPort's JSON tags -- one (destination host,
// destination port) combination actually observed together on a single
// event (issue #654). Never build this by crossing Evidence.hosts against
// Evidence.ports: those are independent sets, and pairing every host with
// every port implies combinations that were never seen (see #654 -- the
// motivating case is a future watchlist draft that would otherwise offer
// to permit connections the device never made).
export interface HostPort {
  host: string
  port: number
}

// Mirrors internal/flags.Evidence's JSON tags -- structured supporting
// detail beyond a flag's free-text `detail` string. Which fields a given
// flag actually has depends on its type; see internal/detect.Scope's
// doc comment (or docs/configuration.md) for exactly which.
export interface Evidence {
  ports?: number[]
  hosts?: string[]
  nat?: NATInfo
  // pairs/pairsTotal/pairsTotalIsFloor (#654): critical_port, and since
  // #641 outbound_anomaly and internal_recon, whose pairs are what an
  // expected verdict permits and what a "watch for this" draft is built
  // from. pairs is capped the same way ports/hosts are (see
  // internal/engine's maxEvidencePairs); pairsTotal is the distinct-pair
  // count before that display cap, present only when the cap actually
  // truncated the list -- absent (undefined) means pairs is already the
  // complete set. A consumer must check pairsTotal, not just
  // pairs.length, before treating the list as complete (issue #654's
  // "never silently truncate" requirement).
  //
  // pairsTotal is itself bounded (internal/engine's
  // maxEvidencePairsTracked, a resource-safety cap independent of the
  // display cap -- see that constant's own doc comment for why an
  // exact count isn't worth an unbounded map): past that second
  // ceiling, pairsTotal stops growing and pairsTotalIsFloor is true,
  // meaning pairsTotal is a lower bound ("at least this many"), not the
  // real count. A consumer must render that case as "50 of 200+", never
  // a flat "50 of 200" -- see pairsTruncated/pairsTruncationLabel in
  // lib/evidencePairs.ts.
  pairs?: HostPort[]
  pairsTotal?: number
  pairsTotalIsFloor?: boolean
  // srcMac (#654): currently only port_scan and repeated_drops, and only
  // when the triggering event's source was a local device -- absent for
  // every other detector and for an external source. Lets a consumer
  // identify the device by MAC (stable across a DHCP lease change)
  // instead of by IP.
  srcMac?: string
}

// Mirrors internal/flags.Exclusion's JSON tags -- one recorded
// expectation for a (Type, Target) pair (#640; read by
// ExpectationsLedger.svelte). id is the same flagID(Type, Target) key a
// Flag's own id already uses.
export interface Exclusion {
  id: string
  type: FlagType
  target: string
  // #640 turned an exclusion into a sized expectation, and these three
  // are what the ledger (ExpectationsLedger.svelte) reads. All optional
  // because the Go side omits them when empty and because an entry
  // recorded before #640 genuinely has none.
  //
  // size is the measure recorded when the expectation was made -- the
  // firing the operator judged normal. Absent (not zero) means the
  // detector declares no size, which is the older, blunter "ignore this
  // host on this detector": the row reads "any size" rather than "up
  // to 0", which is the opposite meaning.
  size?: number
  // How many firings this expectation has suppressed -- the ledger's
  // evidence that it is earning its place.
  absorbed?: number
  // When the expectation was first recorded, RFC 3339.
  since?: string
}

// An operator's judgement of a flag (#640), set via
// POST /api/flags/{id}/verdict. Every flag ends as one of these four --
// there is no way to dismiss one without a judgement:
//
//   - 'expected': normal for this host, at this size. Clears, and
//     records an expectation that absorbs further firings within 1.5x
//     the size this one had.
//   - 'checked': looked at, fine this time. Clears, suppresses nothing,
//     and is remembered so a re-fire can say when it was checked.
//   - 'investigate': of concern, being looked at. The one verdict that
//     leaves the flag open; the row then offers expected or resolved.
//   - 'resolved': dealt with, normally by a firewall change. Clears, and
//     deliberately does not suppress -- if it comes back, the fix was
//     not what was intended.
export type Verdict = 'expected' | 'checked' | 'investigate' | 'resolved'

export interface Flag {
  id: string
  type: FlagType
  target: string
  detail: string
  count: number
  firstSeen: string
  lastSeen: string
  cleared: boolean
  clearedAt?: string
  // 0-100, present only for detectors that make a statistical judgment
  // call rather than a deterministic threshold crossing (currently just
  // activity_spike's per-host baseline) -- absent means "not scored," not
  // "zero confidence."
  confidence?: number
  // A reputation snapshot captured *at raise time* (not fetched live) --
  // only present for single-IP detectors with a reputation key
  // configured. Reuses ReputationResult rather than a separate type.
  reputation?: ReputationResult
  // The target's ISO 3166-1 alpha-2 country code, from the same GeoIP
  // lookup already applied to the underlying event -- absent for an
  // internal target or when GeoIP isn't configured.
  country?: string
  // Structured supporting evidence -- see Evidence's own doc comment.
  // Absent/empty for detectors with nothing beyond `detail` to show.
  evidence?: Evidence
  // Mirrors internal/flags.Flag.Provisional (#642): true for a flag
  // raised while its judgement's baseline had not yet cleared its
  // history floor -- a z-score existed but was not yet trusted. Absent
  // (omitempty on the wire) is the common case and means settled, same
  // "absence is the default" convention verdict/confidence above
  // already follow. Fixed at episode start, same as firstSeen -- it does
  // not flip to false in place if the same episode's baseline later
  // clears its floor (see internal/flags.Store.add's own doc comment).
  provisional?: boolean
  // Verdict/verdictBy/verdictAt (#638, #640): all three present together
  // or all absent -- absent means never judged, not "judged with no
  // opinion." verdictBy is the account that judged it; verdictAt is
  // RFC3339.
  verdict?: Verdict
  verdictBy?: string
  verdictAt?: string
  // priorVerdict/priorVerdictAt (#640): the checked or resolved
  // judgement this pair carried the last time it was cleared, kept
  // across the re-fire that resets `verdict`. Present only on a flag
  // that has come back after one of those two verdicts -- which is
  // exactly when the card says "you checked this on 2 Sept and found it
  // fine" or "resolved on 2 Sept -- it's back".
  priorVerdict?: Verdict
  priorVerdictAt?: string
  // size/expectedSize (#640): this firing's own size (the measure the
  // detector compares against its threshold -- distinct ports for
  // port_scan, and so on), and the size an expectation for this pair had
  // recorded when this firing broke past it. expectedSize is present
  // only on a firing an expectation refused to absorb, so its presence
  // is exactly the "expected up to 30, saw 120" case. Both absent for a
  // detector that declares no size.
  size?: number
  expectedSize?: number
}

// Mirrors internal/flags.FlagTimeBucket's JSON tags -- same shape
// convention as TimeBucket above, but counting newly-raised flag
// episodes by Type instead of raw events by Action. byType omits types
// with a zero count for a given minute.
export interface FlagTimeBucket {
  time: string
  byType: Partial<Record<FlagType, number>>
}

// Mirrors internal/matchlog.Identity's JSON tags (#243) -- a device's
// resolved identity, MAC-preferred with an IP fallback. At least one
// field is populated wherever this appears as evidence (a matched
// event's own identity); either or both may be empty when it appears as
// an entry's *scope* instead (unscoped means "any source").
export interface WatchlistIdentity {
  mac?: string
  ip?: string
}

// Mirrors internal/watchlist.PermittedDest's JSON tags.
export interface WatchlistPermittedDest {
  destIp: string
  port: number
}

// Mirrors internal/watchlist.ObservedDest's JSON tags -- one candidate
// destination/port pair seen while an inverted entry was Observing, not
// yet promoted or dismissed.
export interface WatchlistObservedDest {
  destIp: string
  port: number
  firstSeen: string
  lastSeen: string
  count: number
}

// Mirrors internal/watchlist.Window's JSON tags (#680): when an entry is
// expected to see traffic.
//
// start/end are "HH:MM" clock times, and both carry `omitzero`
// server-side -- 00:00 is the zero value, so a midnight-to-six window
// arrives as `{end: "06:00"}` with no start at all. A missing one means
// 00:00; never read absence as "no window".
//
// end <= start means the window runs into the following date, which is
// the normal case rather than the edge: 22:00-06:00 is one night across
// two dates. days is empty for "every day", 0 = Sunday, and filters on
// the date the window *opened*. zone is an IANA name, empty meaning UTC,
// and is the only local-time concept anywhere in mikroview -- every other
// timestamp in this file is UTC.
export interface WatchWindow {
  start?: string
  end?: string
  days?: number[]
  zone?: string
}

// Mirrors internal/watchlist.NightState. Three states, and the third is
// the point: "not observed" is a night mikroview was down for, or one
// where no rule was logging the pathway. It must never be rendered as
// "empty" -- that would present an absence of ours as a fact about the
// network.
export type WatchNightState = 'kept' | 'empty' | 'not observed'

// Mirrors internal/watchlist.Night's JSON tags -- one occurrence of the
// window and what happened in it. first/count carry `omitzero`/`omitempty`
// server-side and are only meaningful on a kept night.
export interface WatchNight {
  opened: string
  state: WatchNightState
  first?: string
  count?: number
}

// Mirrors internal/watchlist.Ring's JSON tags -- the recorded break in a
// run of kept nights, written at the moment it broke. Absent entirely
// when the ring is intact. `since` is the close of the first empty window
// in the current run. The coverage-derived break (no rule logs this
// pathway) is a different kind of broken and is not this: it comes from
// live router state and arrives on the definition's own `coverage`.
export interface WatchRing {
  broken?: boolean
  since?: string
  reason?: string
}

// Mirrors internal/watchlist.Entry's JSON tags (#243) -- see that type's
// own doc comment for the full non-inverted/inverted matching rules.
// ports/invert/includeStructuralNoise/observing/permitted/observed all
// carry `omitempty` server-side, so any of them may be entirely absent
// from a response rather than present with a zero value (empty
// array/false) -- code reading these must treat absence and "present
// but empty/false" identically, never assume a key exists.
export interface WatchlistEntry {
  id: string
  name?: string
  // Read back from the owning definition's own `enabled`, same as name
  // above -- it is an envelope property, not part of the entry itself,
  // but the only one the broken-ring predicate (#546) needs, so it rides
  // along here rather than in a second id-keyed map. Only enabled
  // expectations count toward "broken": an operator who switched a watch
  // off is not promising mikroview can see it.
  enabled: boolean
  source?: WatchlistIdentity
  destIp?: string
  ports?: number[]
  invert?: boolean
  // Only meaningful when invert is true. omitempty server-side, so a
  // false value is absent entirely, not present-and-false -- treat
  // absence and false identically (see the doc comment above).
  observing?: boolean
  includeStructuralNoise?: boolean
  permitted?: WatchlistPermittedDest[]
  observed?: WatchlistObservedDest[]
  // The watch window and its nightly memory (#680). All three carry
  // `omitzero`/`omitempty` server-side: an entry with no window has none
  // of them, which is what a row renders as "always".
  window?: WatchWindow
  nights?: WatchNight[]
  ring?: WatchRing
  createdAt: string
}

// Mirrors internal/matchlog.Tuple's JSON tags -- what a match was
// actually recorded under: the matching event's own resolved identity
// (never the entry's, possibly-unscoped, Source), the destination it
// reached, and the port.
export interface WatchlistMatchTuple {
  source: WatchlistIdentity
  destIp: string
  port: number
}

// Mirrors internal/matchlog.Record's JSON tags -- one watchlist match,
// evidence-first. event is the full matched FirewallEvent, exactly what
// the live view would have shown, not a summary. count > 1 means every
// occurrence after the first collapsed into this same record rather than
// being stored individually (see internal/matchlog's own doc comment on
// why -- the rate cap that stops a noisy entry from recreating the
// haystack this feature exists to avoid).
export interface WatchlistMatch {
  id: string
  entryId: string
  tuple: WatchlistMatchTuple
  event: FirewallEvent
  firstSeen: string
  lastSeen: string
  count: number
}

// Mirrors internal/suggest.Kind (#243 slice 5) -- what a candidate
// becomes if accepted. addressList is defined server-side but never
// actually generated yet (see that Kind's own Go doc comment), so it
// should never appear here in practice.
export type SuggestionKind = 'device' | 'port' | 'addressList'

// Mirrors internal/suggest.Status -- see internal/suggest's package doc
// comment for what each value means and how it's reached. 'off' is the
// default for every newly generated candidate and the default review
// view.
export type SuggestionStatus = 'off' | 'on' | 'hide'

// Mirrors internal/suggest.Candidate's JSON tags. id routinely contains
// a raw NUL byte (the generator's internal join separator) -- always
// build API paths with encodeURIComponent(id), never string-concatenate
// it directly, or the request never reaches the server at all.
export interface Suggestion {
  id: string
  kind: SuggestionKind
  status: SuggestionStatus
  // Set once an accepted (status: 'on') candidate's generating
  // justification stops appearing in a later background sync -- the
  // rule or device it was suggested from changed or was removed. Never
  // auto-cleared except by that justification holding again; needs a
  // clear, hard-to-miss visual treatment, not a subtle one (#243 slice
  // 5 design: "a bright, hard-to-miss highlight").
  stale?: boolean
  name: string
  justification: string
  routerDevice: string
  source?: WatchlistIdentity
  ports?: number[]
  addressList?: string
  // Set once status is 'on': the real watchlist entry this became.
  entryId?: string
  firstSeen: string
  updatedAt: string
}

// Mirrors internal/store's Scope.
export type Scope = '' | 'internal' | 'external'

// #438 replaced the single "ip" box (matched src OR dst, either raw) with
// side-scoped srcQuery/dstQuery -- see lib/addressMatch.ts for what each
// accepts (label, IP or CIDR) and lib/state.svelte.ts's swapSourceDestination
// for how the two sides (plus their scope and country) swap together.
// srcCountry/dstCountry are the owner-ratified country section of that
// issue -- see lib/countryMatch.ts. Per the project's dev-stage norm
// (AGENTS.md "Removals are wholesale"), the retired `ip` field is not
// migrated: a saved preset or bookmarked URL that still carries it simply
// stops applying that half of its filter.
export interface Filters {
  device: string
  action: Action | ''
  protocol: string
  chain: string
  interface: string
  srcQuery: string
  dstQuery: string
  port: string
  srcScope: Scope
  dstScope: Scope
  srcCountry: string
  dstCountry: string
  rule: string
  ruleRegex: boolean
}

export function emptyFilters(): Filters {
  return {
    device: '',
    action: '',
    protocol: '',
    chain: '',
    interface: '',
    srcQuery: '',
    dstQuery: '',
    port: '',
    srcScope: '',
    dstScope: '',
    srcCountry: '',
    dstCountry: '',
    rule: '',
    ruleRegex: false,
  }
}

// Accepts only the two recognized scope values -- anything else
// (including missing/malformed) falls back to '' (any), mirroring
// internal/api's parseScope.
function parseScope(v: string | null): Scope {
  return v === 'internal' || v === 'external' ? v : ''
}

// Reconstructs a Filters object from a URL's query string, using the same
// param names lib/api.ts's buildQuery writes -- what makes a filtered view
// shareable/bookmarkable as a plain link. Unknown params are ignored.
export function filtersFromSearchParams(params: URLSearchParams): Filters {
  return {
    device: params.get('device') ?? '',
    action: (params.get('action') as Action | null) ?? '',
    protocol: params.get('protocol') ?? '',
    chain: params.get('chain') ?? '',
    interface: params.get('interface') ?? '',
    srcQuery: params.get('srcQuery') ?? '',
    dstQuery: params.get('dstQuery') ?? '',
    port: params.get('port') ?? '',
    srcScope: parseScope(params.get('srcScope')),
    dstScope: parseScope(params.get('dstScope')),
    srcCountry: params.get('srcCountry') ?? '',
    dstCountry: params.get('dstCountry') ?? '',
    rule: params.get('rule') ?? '',
    ruleRegex: params.get('ruleRegex') === 'true',
  }
}

// Mirrors internal/watchlist.CoverageState (#274 item 1): whether
// anything a router has pushed could actually feed a watchlist entry.
//
// 'unknown' is the default and by far the most common -- the router push
// is optional, so most deployments have nothing to answer from. The UI
// says nothing at all in that state; only the two definite negatives are
// worth an operator's attention, and a false one of those is worse than
// silence.
export type WatchlistCoverage = 'unknown' | 'covered' | 'no-logging' | 'out-of-scope'

// Mirrors internal/api's PersistenceInfo (#677's settings persistence
// row) -- which backend this deployment's persisted stores (flags,
// definitions, watchlist entries, entities, tokens/accounts) actually
// use right now.
export interface PersistenceInfo {
  backend: 'file' | 'postgres'
  // The directory the JSON documents live under -- absent for postgres,
  // which has no filesystem path to report.
  dir?: string
}

// GET /api/router-backups (#394, round 44's "router backups" group).
// Mirrors internal/api's routerBackupsResponse.
export interface RouterBackupsResponse {
  // False when no retention key is configured at all -- #394's "no key,
  // no backups": the drop box refuses every login and routers is always
  // empty.
  enabled: boolean
  routers: RouterBackupRouter[]
  totalGenerations: number
  totalRouters: number
  totalBytes: number
  // The SFTP drop box's own listening port ("arrive by"), absent when
  // backup.enabled is false.
  port?: string
}

// One router's block (round 44's per-router strip). IntervalSeconds/
// LastArrival/Missed together carry the owner's 2026-09-05 decision:
// the interval is learned from arrivals, not the scheduler line the
// wizard printed, and a router with one push has neither.
export interface RouterBackupRouter {
  device: string
  generations: RouterBackupGeneration[] // oldest first
  intervalKnown: boolean
  intervalSeconds?: number
  lastArrival?: string
  missed: number
}

// One kept generation -- the shape round 44's strip and newest-pair
// line are drawn from. The size/arrival fields are absent for whichever
// half of the pair has not arrived yet, so "not here" reads differently
// from "zero bytes".
export interface RouterBackupGeneration {
  id: string
  backupArrivedAt?: string
  rscArrivedAt?: string
  backupBytes?: number
  rscBytes?: number
  // The .backup's header label ("plain" or "encrypted"), absent until
  // that half of this generation has arrived.
  header?: string
}

// Mirrors internal/api's setupStatus (#320). Everything here is an
// observation mikroview made on its own side -- it never connects to a
// router, so "did that step work" is answered by what arrived, not by
// asking the router.
export interface SetupStatus {
  instance: {
    tlsEnabled: boolean
    // tls.hosts as configured. Empty means the generated certificate
    // covers localhost/127.0.0.1 only, which is the most common reason
    // a router's first fetch fails.
    hosts: string[]
    syslogPort: string
    syslogEnabled: boolean
  }
  sources: {
    source: string
    caFetchedAt?: string
    syslogFirstSeenAt?: string
    syslogLastSeenAt?: string
  }[]
  devices: {
    device: string
    configured: boolean
    sourceIp: string
    events: number
    // Events whose action was decoded from a log-prefix. Zero with
    // events above zero means the rules log without the convention.
    decodedActions: number
    pushedKinds?: Record<string, string>
  }[]
  pushKinds: string[]
  // The claim ledger's own marks (#487) -- see SetupMark. Always
  // present, empty when nothing has been skipped or forced past.
  marks: SetupMark[]
}

// Mirrors internal/setup.Mark (#487): the operator's own statement about
// a step that produced no evidence. The other half of the claim ledger,
// and served alongside the evidence because the surfaces that need it
// are not only the wizard -- an empty stream explains its own silence
// with the forced-past line that accounts for it.
export interface SetupMark {
  // 1-5, matching the wizard's five steps.
  step: number
  // 'skipped' is quiet and moves on; 'forced' went past the heavy
  // warning and is recorded loudly. There is no third outcome: a step
  // with evidence needs no mark at all.
  outcome: 'skipped' | 'forced'
  // Resolved server-side from the session, never sent by the client.
  actor: string
  at: string
  // What had not arrived when the decision was made, as the wizard's own
  // observation line worded it.
  note?: string
}

// --- RouterOS version-aware commands (#436) --------------------------
//
// Mirrors internal/routeros's response to POST /api/setup/commands. The
// commands themselves moved server-side with #436: the wizard used to
// generate RouterOS syntax itself (lib/setupsteps.ts before this issue),
// and now only renders what the server sends, selected by the row that
// covers the router's (derived or picked) version.

// RouterosStanding is how a version compares to the dialect table's
// covered range. 'unknown' is a version the table cannot speak to, and
// never appears for a row, picked or router entry the caller can render
// a name against without evidence.
export type RouterosStanding = 'unknown' | 'below-minimum' | 'reviewed' | 'ahead-of-review'

// RouterosRow is one entry of the dialect table -- a version range this
// dialect's commands are the same across, how that row was verified, and
// any per-version note (e.g. 7.24.0's find-lookup bug).
export interface RouterosRow {
  from: string
  to: string
  dialect: string
  verifiedBy: string
  note: string
}

export interface RouterosTable {
  minimum: string
  newest: string
  rows: RouterosRow[]
}

// PickedVersion is the operator's version pick echoed back with its
// standing, once the server has matched it against a row -- null when no
// version was sent (routeros.picked in the contract).
export interface PickedVersion {
  version: string
  standing: RouterosStanding
  dialect: string
}

// RouterosWarningRouter is one router the response carries a version for
// (only those the server knows a version for at all), what it reports,
// and how that compares to the table.
export interface RouterosWarningRouter {
  id: string
  name: string
  routerosVersion: string
  standing: RouterosStanding
  note: string
}

// CommandStep is one rendered block: the commands themselves, and any
// note that goes with this exact step (e.g. the 7.24.0 rule-tagging
// caveat) -- distinct from the router-standing warning, which is about
// the router's version generally rather than one step's content.
export interface CommandStep {
  commands: string
  note: string
}

export interface SetupCommandsResponse {
  routeros: RouterosTable
  picked: PickedVersion | null
  routers: RouterosWarningRouter[]
  steps: {
    caTrust: CommandStep
    syslog: CommandStep
    ruleTagging: CommandStep
    push: CommandStep
    schedule: CommandStep
    // backup/backupSchedule are step 6's two blocks (#394, round 45),
    // rendered only once a device, a token, the drop box's port and a
    // configured retention key are all present -- see
    // internal/api/setupcommands.go's handleSetupCommands. Blank
    // (commands: '') is how the wizard reads "cannot be printed yet",
    // the same convention every other step's blank block already uses.
    backup: CommandStep
    backupSchedule: CommandStep
  }
}

// SetupCommandsRequest is the POST /api/setup/commands body. Every field
// but address is optional -- kinds/token are omitted before step 4 has
// anything to embed, version is omitted until the operator has picked
// one or a router has reported, and device is omitted until step 4 or
// 6 has a router chosen (it names step 6's backup script is being
// rendered for; the push script needs no such field).
export interface SetupCommandsRequest {
  address: string
  syslogPort?: string
  token?: string
  kinds?: string[]
  version?: string
  device?: string
}

// --- Tune logging (#435) ----------------------------------------------
//
// Mirrors internal/routeros/export and the two /api/tune-logging
// handlers (the #435 fixed contract). The upload never leaves this
// request/response pair -- nothing here is persisted, mirrored by the
// component that renders it (TuneLogging.svelte) never writing the
// export text anywhere but its own component state.

// TuneLoggingObserving is how long mikroview has been watching this
// device -- present whether or not that is yet a full day (§2 of the
// contract: "advise, never lock, compute nothing early").
export interface TuneLoggingObserving {
  since: string
  hours: number
}

// TuneLoggingRouterInfo is the export header's own version, read the
// same way #436's dialect table reads a device's reported version --
// reused RouterosStanding rather than a second enum for the same
// answer.
export interface TuneLoggingRouterInfo {
  version: string
  standing: RouterosStanding
  dialect: string
}

// TuneLoggingRule is one /ip firewall filter `add` line from the
// uploaded export, with RouterOS's own packet/byte counters (#435
// decision 4) matched in from the latest push where the ordinal and
// chain+action agree.
export interface TuneLoggingRule {
  id: number
  chain: string
  action: string
  comment: string
  inInterface: string
  outInterface: string
  inInterfaceList: string
  outInterfaceList: string
  boundary: string
  crossesDark: boolean
  log: boolean
  logPrefix: string
  packets: number
  bytes: number
  countersKnown: boolean
  line: number
}

// TuneLoggingRejection is why the parser would not proceed -- key
// material detected, meaning the upload was not `export hide-sensitive`
// (§5 of the contract).
export interface TuneLoggingRejection {
  reason: string
}

export interface TuneLoggingAnalyseRequest {
  device: string
  export: string
  darkBoundaries: string[]
}

export interface TuneLoggingAnalyseResponse {
  ready: boolean
  observing: TuneLoggingObserving
  routeros: TuneLoggingRouterInfo
  rules: TuneLoggingRule[]
  rejected: TuneLoggingRejection | null
}

export interface TuneLoggingRenderRequest {
  device: string
  export: string
  selected: number[]
}

export interface TuneLoggingRenderResponse {
  annotated: string
  commands: string
  changed: number
  routeros: TuneLoggingRouterInfo
}
