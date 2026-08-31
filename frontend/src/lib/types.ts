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
  connectedClients: number
  // Syslog listener saturation -- mirrors internal/syslog.ListenerStats.
  // Optional so an older server (or a test fixture) that does not send
  // it simply shows nothing rather than rendering NaN.
  syslog?: SyslogListenerStats
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

export interface DefinitionSuppression {
  id: string
  target: string
  reason?: string
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
  suppressions?: DefinitionSuppression[]
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

// Mirrors internal/flags.Evidence's JSON tags -- structured supporting
// detail beyond a flag's free-text `detail` string. Which fields a given
// flag actually has depends on its type; see internal/detect.Scope's
// doc comment (or docs/configuration.md) for exactly which.
export interface Evidence {
  ports?: number[]
  hosts?: string[]
  nat?: NATInfo
}

// Mirrors internal/flags.Exclusion's JSON tags -- one permanently-
// excluded (Type, Target) pair (see flags.svelte.ts's clearPermanent and
// exclusions.svelte.ts). id is the same flagID(Type, Target) key a
// Flag's own id already uses.
export interface Exclusion {
  id: string
  type: FlagType
  target: string
}

// An operator's triage judgement on a flag (issue #638) -- set once via
// POST /api/flags/{id}/verdict and never re-asked afterward. 'expected'
// (legitimate traffic) and 'noise' (real traffic, wrong threshold) both
// clear the flag as a side effect of judging it; 'real' (genuine
// concern) does not, and is the invariant that later auto-tune must
// never contradict by suggesting a threshold that would have dropped it.
export type Verdict = 'expected' | 'noise' | 'real'

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
  // Verdict/verdictBy/verdictAt (#638): all three present together or
  // all absent -- absent means never judged, not "judged with no
  // opinion." verdictBy is the account that judged it; verdictAt is
  // RFC3339.
  verdict?: Verdict
  verdictBy?: string
  verdictAt?: string
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
