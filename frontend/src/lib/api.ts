import type { AuthSession, Device, EventsResult, Filters, Flag, ReputationResult, Stats } from './types'

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

export async function fetchFlags(): Promise<Flag[]> {
  const res = await fetch('/api/flags')
  if (!res.ok) throw new ApiError(`fetchFlags: ${res.status}`, res.status)
  const body = await res.json()
  return body.flags ?? []
}

export async function clearFlag(id: string): Promise<void> {
  const res = await postJSON(`/api/flags/${encodeURIComponent(id)}/clear`)
  if (!res.ok) throw new ApiError(`clearFlag: ${res.status}`, res.status)
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

export async function logout(): Promise<void> {
  await postJSON('/api/auth/logout')
}

export async function createUser(username: string, password: string, role: 'admin' | 'user'): Promise<string | null> {
  const res = await postJSON('/api/auth/users', { username, password, role })
  if (res.ok) return null
  return (await res.text()) || `createUser: ${res.status}`
}
