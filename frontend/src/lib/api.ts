import type {
  ApiToken,
  AuthSession,
  DetectorScope,
  DetectorSettings,
  Device,
  Entity,
  EventsResult,
  Exclusion,
  Filters,
  Flag,
  FlagTimeBucket,
  ReputationResult,
  RuleUsage,
  Stats,
} from './types'

// Thrown instead of a plain Error by every fetch* function below --
// carries the HTTP status so a caller (App.svelte's polling effect) can
// tell a session-expiry 401 apart from any other failure and bounce to
// the login view, without api.ts itself needing to import lib/auth.svelte.ts
// (which imports api.ts -- keeping this one-directional avoids a
// circular import).
export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

// Every mutating request goes through this -- sets the CSRF mitigation
// header the backend requires once auth is active (internal/api's
// csrfHeaderName; a no-op while auth is inactive, but always sent since
// the frontend can't know which state it's in ahead of the response).
// Same-origin `fetch()` already includes cookies by default, so no
// explicit `credentials` option is needed.
async function postJSON(url: string, body: unknown = {}): Promise<Response> {
  return fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    body: JSON.stringify(body),
  })
}

async function putJSON(url: string, body: unknown = {}): Promise<Response> {
  return fetch(url, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    body: JSON.stringify(body),
  })
}

// Same CSRF mitigation header as postJSON/putJSON -- DELETE isn't a
// "safe" method either (see internal/api's isSafeMethod), so it's
// required here too. body is optional: most DELETE endpoints identify
// their target via the URL path (a plain ID), but /api/entities
// identifies which record to remove via a JSON body instead (see
// internal/api's handleEntitiesDelete) -- an arbitrary entity Key never
// has to round-trip through a URL at all this way.
async function deleteJSON(url: string, body?: unknown): Promise<Response> {
  return fetch(url, {
    method: 'DELETE',
    headers:
      body === undefined
        ? { 'X-Requested-With': 'mikroview' }
        : { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

// Exported so lib/state.svelte.ts can build the same query-param shape for
// the URL bar (see App.svelte's filter-sync effect) without duplicating
// the "which filter fields are non-empty" logic.
export function buildQuery(filters: Partial<Filters> & { limit?: number; sinceId?: number }): string {
  const params = new URLSearchParams()
  if (filters.device) params.set('device', filters.device)
  if (filters.action) params.set('action', filters.action)
  if (filters.protocol) params.set('protocol', filters.protocol)
  if (filters.chain) params.set('chain', filters.chain)
  if (filters.interface) params.set('interface', filters.interface)
  if (filters.ip) params.set('ip', filters.ip)
  if (filters.port) params.set('port', filters.port)
  if (filters.srcScope) params.set('srcScope', filters.srcScope)
  if (filters.dstScope) params.set('dstScope', filters.dstScope)
  if (filters.rule) params.set('rule', filters.rule)
  if (filters.rule && filters.ruleRegex) params.set('ruleRegex', 'true')
  if (filters.limit) params.set('limit', String(filters.limit))
  if (filters.sinceId) params.set('sinceId', String(filters.sinceId))
  const qs = params.toString()
  return qs ? `?${qs}` : ''
}

export async function fetchEvents(
  filters: Partial<Filters> & { limit?: number; sinceId?: number } = {},
): Promise<EventsResult> {
  const res = await fetch(`/api/events${buildQuery(filters)}`)
  if (!res.ok) throw new ApiError(`fetchEvents: ${res.status}`, res.status)
  return res.json()
}

export async function fetchDevices(): Promise<Device[]> {
  const res = await fetch('/api/devices')
  if (!res.ok) throw new ApiError(`fetchDevices: ${res.status}`, res.status)
  const body = await res.json()
  return body.devices ?? []
}

// fetchRules serves every rule label mikroview has ever seen fire (issue
// #103's internal/rules.Store, via GET /api/rules) -- issue #109's
// "discovered but unnamed rules" source for the Entities panel, the same
// role fetchDevices already plays for auto-discovered hosts.
export async function fetchRules(): Promise<RuleUsage[]> {
  const res = await fetch('/api/rules')
  if (!res.ok) throw new ApiError(`fetchRules: ${res.status}`, res.status)
  const body = await res.json()
  return body.rules ?? []
}

export async function fetchCriticalPorts(): Promise<number[]> {
  const res = await fetch('/api/critical-ports')
  if (!res.ok) throw new ApiError(`fetchCriticalPorts: ${res.status}`, res.status)
  const body = await res.json()
  return body.ports ?? []
}

export async function fetchStats(): Promise<Stats> {
  const res = await fetch('/api/stats')
  if (!res.ok) throw new ApiError(`fetchStats: ${res.status}`, res.status)
  return res.json()
}

export async function lookupIp(ip: string): Promise<ReputationResult> {
  const res = await fetch(`/api/lookup/ip/${encodeURIComponent(ip)}`)
  if (!res.ok) throw new ApiError(`lookupIp: ${res.status}`, res.status)
  return res.json()
}

// Mirrors internal/api/flags.go's handleFlagsList response: the flag
// list plus the last hour of newly-raised-episode counts by type (see
// FlagTimeBucket) for FlagsChart -- one endpoint, same convention
// GET /api/stats already uses for its own timeSeries field.
export interface FlagsResponse {
  flags: Flag[]
  timeSeries: FlagTimeBucket[]
}

export async function fetchFlags(): Promise<FlagsResponse> {
  const res = await fetch('/api/flags')
  if (!res.ok) throw new ApiError(`fetchFlags: ${res.status}`, res.status)
  const body = await res.json()
  return { flags: body.flags ?? [], timeSeries: body.timeSeries ?? [] }
}

export async function clearFlag(id: string): Promise<void> {
  const res = await postJSON(`/api/flags/${encodeURIComponent(id)}/clear`)
  if (!res.ok) throw new ApiError(`clearFlag: ${res.status}`, res.status)
}

// clearFlagPermanent is clearFlag plus a permanent exclusion of that
// flag's (Type, Target) in the same step -- "Clear and never flag this
// again" (see internal/flags.Store.ClearAndExclude).
export async function clearFlagPermanent(id: string): Promise<void> {
  const res = await postJSON(`/api/flags/${encodeURIComponent(id)}/clear-permanent`)
  if (!res.ok) throw new ApiError(`clearFlagPermanent: ${res.status}`, res.status)
}

// fetchExclusions/removeExclusion: admin-only (see internal/api's
// callerIsAdminOrOpen gate on both endpoints) "undo a mistake" surface
// for permanent exclusions.
export async function fetchExclusions(): Promise<Exclusion[]> {
  const res = await fetch('/api/flags/exclusions')
  if (!res.ok) throw new ApiError(`fetchExclusions: ${res.status}`, res.status)
  const body = await res.json()
  return body.exclusions ?? []
}

export async function removeExclusion(id: string): Promise<void> {
  const res = await deleteJSON(`/api/flags/exclusions/${encodeURIComponent(id)}`)
  if (!res.ok) throw new ApiError(`removeExclusion: ${res.status}`, res.status)
}

export async function fetchAuthSession(): Promise<AuthSession> {
  const res = await fetch('/api/auth/session')
  if (!res.ok) throw new ApiError(`fetchAuthSession: ${res.status}`, res.status)
  return res.json()
}

// register/login return the error message text on failure (shown
// directly in the form) rather than throwing -- a wrong password is an
// expected, common outcome for these two calls, not an exceptional one.
export async function register(username: string, password: string): Promise<string | null> {
  const res = await postJSON('/api/auth/register', { username, password })
  if (res.ok) return null
  return (await res.text()) || `register: ${res.status}`
}

export async function login(username: string, password: string): Promise<string | null> {
  const res = await postJSON('/api/auth/login', { username, password })
  if (res.ok) return null
  return (await res.text()) || `login: ${res.status}`
}

// skipAuthSetup permanently disables authentication for this deployment
// (see internal/auth.Store.Disable). Only reachable during the one-time
// first-run choice -- reversing it afterward is CLI-only by design
// (mikroview -enable-auth-setup), not something this function or any
// other API call can do.
export async function skipAuthSetup(): Promise<string | null> {
  const res = await postJSON('/api/auth/skip')
  if (res.ok) return null
  return (await res.text()) || `skipAuthSetup: ${res.status}`
}

export async function logout(): Promise<void> {
  await postJSON('/api/auth/logout')
}

export async function createUser(username: string, password: string, role: 'admin' | 'user'): Promise<string | null> {
  const res = await postJSON('/api/auth/users', { username, password, role })
  if (res.ok) return null
  return (await res.text()) || `createUser: ${res.status}`
}

export async function fetchDetectorSettings(): Promise<DetectorSettings[]> {
  const res = await fetch('/api/detectors')
  if (!res.ok) throw new ApiError(`fetchDetectorSettings: ${res.status}`, res.status)
  const body = await res.json()
  return body.detectors ?? []
}

export async function updateDetectorSettings(
  name: string,
  enabled: boolean,
  scope: DetectorScope,
): Promise<string | null> {
  const res = await putJSON(`/api/detectors/${encodeURIComponent(name)}`, { enabled, scope })
  if (res.ok) return null
  return (await res.text()) || `updateDetectorSettings: ${res.status}`
}

// fetchEntities/upsertEntity/deleteEntity: admin-only CRUD over
// internal/entities' persisted (type, key) -> label/tags store (issue
// #107) -- the shared foundation a future mail-sender allowlist and
// UI-managed IP/port/rule aliasing both build on.
export async function fetchEntities(): Promise<Entity[]> {
  const res = await fetch('/api/entities')
  if (!res.ok) throw new ApiError(`fetchEntities: ${res.status}`, res.status)
  const body = await res.json()
  return body.entities ?? []
}

// upsertEntity creates a new entity, or replaces an existing one in
// place, identified by (type, key) -- used for both "add" and "edit" in
// the admin panel, mirroring the backend's own single Upsert primitive.
export async function upsertEntity(entity: Entity): Promise<string | null> {
  const res = await postJSON('/api/entities', entity)
  if (res.ok) return null
  return (await res.text()) || `upsertEntity: ${res.status}`
}

export async function deleteEntity(type: string, key: string): Promise<string | null> {
  const res = await deleteJSON('/api/entities', { type, key })
  if (res.ok) return null
  return (await res.text()) || `deleteEntity: ${res.status}`
}

// Admin-only read-only API token management (issue #101) -- see
// internal/api/tokens.go. createToken's response is the only place the
// raw bearer value is ever returned; fetchTokens never includes it.
export async function fetchTokens(): Promise<ApiToken[]> {
  const res = await fetch('/api/tokens')
  if (!res.ok) throw new ApiError(`fetchTokens: ${res.status}`, res.status)
  const body = await res.json()
  return body.tokens ?? []
}

export async function createToken(name: string): Promise<ApiToken | string> {
  const res = await postJSON('/api/tokens', { name })
  if (res.ok) return res.json()
  return (await res.text()) || `createToken: ${res.status}`
}

export async function revokeToken(id: string): Promise<string | null> {
  const res = await deleteJSON(`/api/tokens/${encodeURIComponent(id)}`)
  if (res.ok) return null
  return (await res.text()) || `revokeToken: ${res.status}`
}
