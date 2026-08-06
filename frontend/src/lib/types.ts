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
  natIp?: string
  natPort?: number
  natRaw?: string
  srcCountry?: string
  dstCountry?: string
  length?: number
  flags?: string
  raw: string
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

// Mirrors internal/device/registry.go's Info.
export interface Device {
  id: string
  name: string
  sourceIp: string
  configured: boolean
  firstSeen: string
  lastSeen: string
  eventCount: number
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
}

// Mirrors internal/api/auth.go's sessionResponse.
export interface AuthSession {
  authDisabled: boolean
  setupRequired: boolean
  authenticated: boolean
  username?: string
  role?: 'admin' | 'user'
  ssoAvailable: boolean
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

// Mirrors internal/detect.DetectorName -- same string values as
// FlagType, kept as a distinct alias since they're the same thing only
// by coincidence today (see that type's doc comment).
export type DetectorName = FlagType

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
