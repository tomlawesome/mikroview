// SPDX-License-Identifier: AGPL-3.0-only

// Mirrors internal/store/event.go's JSON tags.
export type Action = 'accept' | 'drop' | 'reject' | 'log' | 'unknown'

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
  role?: 'admin' | 'user'
  // False once the account signs in only through the identity provider.
  // Gates whether "Connect SSO" is offered -- there is nothing left to
  // convert otherwise.
  hasLocalPassword?: boolean
  ssoAvailable: boolean
}

// Mirrors internal/api's userSummary. Deliberately not the server's
// full auth.User -- that carries a password hash, and this type exists
// so there is nothing on the client side to accidentally render.
export interface UserSummary {
  id: string
  username: string
  role: 'admin' | 'user'
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

// Mirrors internal/detect.DetectorName's 12 string values. No longer a
// FlagType alias (see new_device/stale_rule above) -- kept as its own
// literal union now that FlagType has entries with no matching detector.
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

// Mirrors internal/detect.Settings' JSON tags, plus the detector's own
// name (as returned by GET /api/detectors).
export interface DetectorSettings {
  name: DetectorName
  enabled: boolean
  scope: DetectorScope
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

export interface Filters {
  device: string
  action: Action | ''
  protocol: string
  chain: string
  interface: string
  ip: string
  port: string
  srcScope: Scope
  dstScope: Scope
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
    ip: '',
    port: '',
    srcScope: '',
    dstScope: '',
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
    ip: params.get('ip') ?? '',
    port: params.get('port') ?? '',
    srcScope: parseScope(params.get('srcScope')),
    dstScope: parseScope(params.get('dstScope')),
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
}
