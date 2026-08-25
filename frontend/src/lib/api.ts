// SPDX-License-Identifier: AGPL-3.0-only

import { parseAddress, parseCidr } from './addressMatch'
import type {
  ApiToken,
  AuditResult,
  AuthSession,
  CoverageEvidence,
  Definition,
  DefinitionSuppression,
  DetectorScope,
  Device,
  Entity,
  EventsResult,
  Exclusion,
  Filters,
  Flag,
  FlagTimeBucket,
  Healthz,
  ReputationResult,
  RuleUsage,
  Stats,
  SetupStatus,
  Suggestion,
  SuggestionStatus,
  UserSummary,
  WatchlistEntry,
  WatchlistCoverage,
  WatchlistIdentity,
  WatchlistMatch,
  WatchlistPermittedDest,
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

// asAddressOrCidr returns v trimmed, if that trimmed form is something
// store.Query.IP (internal/store/query.go) already understands on its
// own -- a bare IP or a CIDR block, matched server-side against src OR
// dst -- or null otherwise (a name, a label fragment, which has no
// server-side equivalent at all).
//
// Returns the normalised (trimmed) string rather than a plain boolean so
// the value that was validated is necessarily the value that gets sent.
// A boolean-returning check, with the caller then forwarding the
// original untrimmed field, was this function's first version -- and it
// shipped a real bug: a pasted address with trailing whitespace (e.g.
// " 203.0.113.5 ") passed the check but was sent padded, which fails both
// parseQuery's net.ParseIP and net.ParseCIDR, dropping matchesFilters to
// its exact-string-equal fallback -- which no event's address can equal,
// so the "server-filtered baseline" comes back empty while the
// client-side matcher (which does trim) leaves the existing rows looking
// fine. Silently wrong, not visibly broken. Returning the validated
// string itself removes the class of bug rather than just this instance.
function asAddressOrCidr(v: string): string | null {
  const trimmed = v.trim()
  return parseAddress(trimmed) || parseCidr(trimmed) ? trimmed : null
}

// Exported so lib/state.svelte.ts can build the same query-param shape for
// the URL bar (see App.svelte's filter-sync effect) without duplicating
// the "which filter fields are non-empty" logic.
//
// This is also what refetchWithFilters() sends to GET /api/events, and
// that request is not a nice-to-have: state.svelte.ts's own doc comment
// calls it the "actually complete" layer, because internal/store/query.go
// scans the *whole* retained buffer newest-to-oldest and fills its
// 500-event limit with events that already match the query (see that
// file's Query loop, matchesFilters called before the `len(matched) >=
// limit` check) -- not just the 500 most recent overall. Sending nothing
// for an address filter would silently swap that for "the 500 most recent
// events, address unfiltered", which can starve out a selective address
// that only appears further back than 500 events of unrelated traffic.
//
// So: whichever of srcQuery/dstQuery parses as a plain IP or CIDR is sent
// as `ip` too (internal/api/rest.go's parseQuery, unchanged) -- store.Query.IP
// matches either side, so it is always a *superset* of what the
// client-side matcher (lib/addressMatch.ts) then narrows to for its own
// side. Never a false negative, only ever "the server did slightly less
// narrowing than the one field alone needed". If both boxes hold an
// address, only one can be forwarded (the store's `ip` param carries a
// single value) -- srcQuery wins arbitrarily, matching the row's own
// left-to-right column order; either choice is still a valid superset,
// since the client re-applies both sides' matchers regardless of what the
// server already excluded.
//
// Everything else this issue added -- srcQuery/dstQuery holding a name or
// label fragment, srcCountry/dstCountry, and text in the Port box -- has
// no server-side match at all: parseQuery has no concept of a resolved
// label, a GeoIP country code, or a port's display name. Those still
// round-trip through the URL for bookmarking, but the request they
// produce is genuinely broader (the 500 most recent events, that one
// field unfiltered) until a future change teaches store.Query/parseQuery
// about them -- this issue's contract was the bar, not the query engine.
// refetchWithFilters() re-applies the full client-side filter to
// whatever comes back regardless, so nothing unfiltered ever reaches the
// screen; the cost is confined to how deep into the retained buffer a
// label/country/text-port search can actually reach.
export function buildQuery(filters: Partial<Filters> & { limit?: number; sinceId?: number }): string {
  const params = new URLSearchParams()
  if (filters.device) params.set('device', filters.device)
  if (filters.action) params.set('action', filters.action)
  if (filters.protocol) params.set('protocol', filters.protocol)
  if (filters.chain) params.set('chain', filters.chain)
  if (filters.interface) params.set('interface', filters.interface)
  if (filters.srcQuery) params.set('srcQuery', filters.srcQuery)
  if (filters.dstQuery) params.set('dstQuery', filters.dstQuery)
  const srcAddr = filters.srcQuery ? asAddressOrCidr(filters.srcQuery) : null
  const dstAddr = filters.dstQuery ? asAddressOrCidr(filters.dstQuery) : null
  if (srcAddr) {
    params.set('ip', srcAddr)
  } else if (dstAddr) {
    params.set('ip', dstAddr)
  }
  // Only forwarded when it parses as the plain integer parseQuery expects
  // (see internal/api/rest.go) -- #438 lets this field hold text (a
  // service name, an operator label), and the server 400s on anything it
  // can't strconv.Atoi, which would turn a text port search into a failed
  // refetch (appState.fetchFailed) instead of the client-side-only match
  // it should be.
  if (filters.port && /^\d+$/.test(filters.port)) params.set('port', filters.port)
  if (filters.srcScope) params.set('srcScope', filters.srcScope)
  if (filters.dstScope) params.set('dstScope', filters.dstScope)
  if (filters.srcCountry) params.set('srcCountry', filters.srcCountry)
  if (filters.dstCountry) params.set('dstCountry', filters.dstCountry)
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

export async function fetchHealthz(): Promise<Healthz> {
  const res = await fetch('/api/healthz')
  if (!res.ok) throw new ApiError(`fetchHealthz: ${res.status}`, res.status)
  return res.json()
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

// The pushed firewall rule / NAT tables (issue #186 step 4) -- read from
// mikroview's own local store, never the router. `available: false`
// means that device has never pushed this table, which the UI shows as
// "no data pushed yet" rather than an empty table pretending to be real.
export interface RouterFilterRule {
  ordinal: number
  comment: string
  chain: string
  action: string
  srcAddressList: string
  logPrefix: string
  // #408's schema fields. Optional here because a router whose push
  // script predates them sends nothing, and because nothing in the UI
  // reads them yet -- typed so the data is not lost on the way in, not
  // because a component depends on it.
  connectionState?: string[]
  inInterface?: string
  outInterface?: string
}

// The NAT record's full rule anatomy (#408, as #445's prerequisite):
// what a rule matches and what it translates to. #445 is what will read
// it -- partitioning the table into rules consistent with an event
// rather than showing all of them equally -- so these stay optional and
// unrendered until that lands.
export interface RouterNatRule {
  ordinal: number
  comment: string
  chain: string
  action: string
  toAddresses?: string
  toPorts?: string
  dstPort?: string
  protocol?: string
  inInterface?: string
  outInterface?: string
  srcAddress?: string
  dstAddress?: string
  disabled?: boolean
  dynamic?: boolean
}

export interface RouterTable<T> {
  available: boolean
  updatedAt?: string
  rules: T[]
}

export async function fetchRouterRules(device: string): Promise<RouterTable<RouterFilterRule>> {
  const res = await fetch(`/api/routeros/${encodeURIComponent(device)}/rules`)
  if (!res.ok) throw new ApiError(`fetchRouterRules: ${res.status}`, res.status)
  return res.json()
}

export async function fetchRouterNat(device: string): Promise<RouterTable<RouterNatRule>> {
  const res = await fetch(`/api/routeros/${encodeURIComponent(device)}/nat`)
  if (!res.ok) throw new ApiError(`fetchRouterNat: ${res.status}`, res.status)
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

// clearAllFlags is clearFlag applied to every currently-active flag in
// one request (issue #198's "Clear all") -- regular clears only, same as
// clearFlag; there is no bulk permanent variant (see
// internal/flags.Store.ClearAll's doc comment for why). Returns how many
// were actually cleared, so the caller can refresh() rather than guess.
export async function clearAllFlags(): Promise<number> {
  const res = await postJSON('/api/flags/clear-all')
  if (!res.ok) throw new ApiError(`clearAllFlags: ${res.status}`, res.status)
  const body = await res.json()
  return body.cleared ?? 0
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


// Change the signed-in account's own password (#294 item 4). Returns
// error text on failure, like every other mutating wrapper here.
//
// The server takes no username: it acts on the session's own account,
// deliberately, so a body cannot point this at somebody else.
export async function changePassword(currentPassword: string, newPassword: string): Promise<string | null> {
  const res = await postJSON('/api/auth/password', { currentPassword, newPassword })
  if (res.ok) return null
  return (await res.text()) || `changePassword: ${res.status}`
}

// Returns error text on failure, like every other mutating wrapper
// here. It used to return void and ignore the status entirely -- the one
// exception among roughly 25 -- so a failed logout was indistinguishable
// from a successful one, and authState.logout() went on to clear the
// session locally while the server still had it.
export async function logout(): Promise<string | null> {
  const res = await postJSON('/api/auth/logout')
  if (res.ok) return null
  return (await res.text()) || `logout: ${res.status}`
}

// No role argument: mikroview has one admin, and the server refuses a
// request for a second (see auth.ErrSingleAdmin). Moving the role is
// CLI-only and recovery-key gated (`mikroview -transfer-admin`), so
// there is nothing for this call to choose between.
export async function createUser(username: string, password: string): Promise<string | null> {
  const res = await postJSON('/api/auth/users', { username, password, role: 'user' })
  if (res.ok) return null
  return (await res.text()) || `createUser: ${res.status}`
}

export async function fetchUsers(): Promise<UserSummary[]> {
  const res = await fetch('/api/auth/users')
  if (!res.ok) throw new ApiError(`fetchUsers: ${res.status}`, res.status)
  return (await res.json()) ?? []
}

export async function deleteUser(id: string): Promise<string | null> {
  const res = await deleteJSON(`/api/auth/users/${encodeURIComponent(id)}`)
  if (res.ok) return null
  return (await res.text()) || `deleteUser: ${res.status}`
}

// The one definitions surface (issue #407), replacing /api/detectors and
// /api/watchlist/entries wholesale -- both are gone server-side, with no
// alias and no friendlier-error stub, so nothing below may fall back to
// them. A shipped detector and a watchlist expectation are the same
// thing to the engine, and they are one list here for the same reason:
// two endpoints over one thing is what let a detector's enabled flag and
// an entry's scope drift into two answers to the same question.
export async function fetchDefinitions(): Promise<{
  definitions: Definition[]
  coverageEvidence: CoverageEvidence
}> {
  const res = await fetch('/api/definitions')
  if (!res.ok) throw new ApiError(`fetchDefinitions: ${res.status}`, res.status)
  const body = await res.json()
  return {
    definitions: body.definitions ?? [],
    coverageEvidence: body.coverageEvidence ?? { complete: false },
  }
}

// updateDefinition sends only the fields it is given. Every field is
// optional server-side and an absent one means "leave this alone", so a
// caller toggling `enabled` cannot silently clear a definition's scope
// by not sending one.
export interface DefinitionUpdate {
  name?: string
  enabled?: boolean
  scope?: DetectorScope
  params?: Record<string, unknown>
  suppressions?: DefinitionSuppression[]
  expectation?: WatchlistEntryRequest
}

export async function updateDefinition(id: string, req: DefinitionUpdate): Promise<Definition | string> {
  const res = await putJSON(`/api/definitions/${encodeURIComponent(id)}`, req)
  if (res.ok) return await res.json()
  return (await res.text()) || `updateDefinition: ${res.status}`
}

// resetDefinition discards every param override in one call, putting a
// shipped definition back to exactly what it shipped with. Clearing
// every override and "reset to default" are the same state server-side,
// not two operations that could fall out of sync.
export async function resetDefinition(id: string): Promise<Definition | string> {
  const res = await postJSON(`/api/definitions/${encodeURIComponent(id)}/reset`)
  if (res.ok) return await res.json()
  return (await res.text()) || `resetDefinition: ${res.status}`
}

// cloneDefinition copies an expectation definition into a new one with
// its own identity. Refused server-side for a shipped detection
// definition, whose logic is compiled into the binary and keyed by its
// own id -- a copy of one would evaluate nothing.
export async function cloneDefinition(id: string, name?: string): Promise<Definition | string> {
  const res = await postJSON(`/api/definitions/${encodeURIComponent(id)}/clone`, { name: name ?? '' })
  if (res.ok) return await res.json()
  return (await res.text()) || `cloneDefinition: ${res.status}`
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

// Admin-only CRUD over internal/watchlist's persisted entry set (#243) --
// what Control Ports grew into. Unlike Entities' single Upsert primitive,
// creating and updating are two separate endpoints server-side (an
// entry's id is server-generated, not an operator-chosen key), so this
// mirrors that split rather than forcing one shared function.

// WatchlistEntryRequest is the expectation block internal/api's
// expectationRequest accepts -- deliberately narrower than
// WatchlistEntry itself: observing/permitted/observed are never settable
// here, only through their own dedicated endpoints below, so a plain
// edit cannot silently wipe an entry's accumulated observations. name
// rides beside it on create, and is not part of the block itself.
export interface WatchlistEntryRequest {
  name?: string
  source?: WatchlistIdentity
  destIp?: string
  ports?: number[]
  invert?: boolean
  includeStructuralNoise?: boolean
}

// expectationBlock splits a request into the two halves the definitions
// API takes: the entry's own matching data, and the name that lives on
// the definition envelope around it.
function expectationBlock(req: WatchlistEntryRequest) {
  return {
    source: req.source ?? {},
    destIp: req.destIp ?? '',
    ports: req.ports ?? [],
    invert: req.invert ?? false,
    includeStructuralNoise: req.includeStructuralNoise ?? false,
  }
}

// Returns the entries and, alongside them, what can be said about
// whether anything is able to feed each one (#274). Keyed by entry id
// rather than folded into the entry: coverage is derived from what
// routers have pushed right now, not a property of the entry itself.
export async function fetchWatchlistEntries(): Promise<{
  entries: WatchlistEntry[]
  coverage: Record<string, WatchlistCoverage>
}> {
  const { definitions } = await fetchDefinitions()
  const entries: WatchlistEntry[] = []
  const coverage: Record<string, WatchlistCoverage> = {}
  for (const d of definitions) {
    if (d.intent !== 'expectation' || !d.expectation) continue
    // The definition's own name is the envelope's, not the entry's --
    // the entry carries a copy for rendering, but the server is the one
    // that decides it (an entry created with no name gets a generated
    // one), so it is read back from the definition rather than assumed.
    entries.push({ ...d.expectation, name: d.name, enabled: d.enabled })
    if (d.coverage) coverage[d.id] = d.coverage
  }
  return { entries, coverage }
}

export async function createWatchlistEntry(req: WatchlistEntryRequest): Promise<WatchlistEntry | string> {
  const res = await postJSON('/api/definitions', {
    name: req.name ?? '',
    intent: 'expectation',
    kind: 'declarative',
    expectation: expectationBlock(req),
  })
  if (res.ok) return definitionEntry(await res.json())
  return (await res.text()) || `createWatchlistEntry: ${res.status}`
}

export async function updateWatchlistEntry(id: string, req: WatchlistEntryRequest): Promise<WatchlistEntry | string> {
  const result = await updateDefinition(id, { name: req.name ?? '', expectation: expectationBlock(req) })
  if (typeof result === 'string') return result
  return definitionEntry(result)
}

export async function deleteWatchlistEntry(id: string): Promise<string | null> {
  const res = await deleteJSON(`/api/definitions/${encodeURIComponent(id)}`)
  if (res.ok) return null
  return (await res.text()) || `deleteWatchlistEntry: ${res.status}`
}

// definitionEntry pulls the operator-facing entry out of a definition
// response. The entry is always present for an expectation definition;
// the fallback exists so a shape change surfaces as an entry with no
// fields rather than as a thrown TypeError inside a Svelte render.
function definitionEntry(d: Definition): WatchlistEntry {
  return { ...(d.expectation ?? { id: d.id, createdAt: '' }), name: d.name, enabled: d.enabled }
}

// promoteWatchlistDestinations moves the given destination/port pairs
// from an inverted expectation's Observed candidate list into its
// Permitted allow-list -- a pair not previously observed is still
// accepted, the same "deliberate choice, not an
// error" contract the backend documents.
export async function promoteWatchlistDestinations(
  id: string,
  destinations: WatchlistPermittedDest[],
): Promise<WatchlistEntry | string> {
  const res = await postJSON(`/api/definitions/${encodeURIComponent(id)}/promote`, { destinations })
  if (res.ok) return definitionEntry(await res.json())
  return (await res.text()) || `promoteWatchlistDestinations: ${res.status}`
}

// setWatchlistObserving flips whether an inverted expectation is in
// observe mode -- the raw mechanism only; see the handler's own doc
// comment for why nothing here judges when to call it.
export async function setWatchlistObserving(id: string, observing: boolean): Promise<WatchlistEntry | string> {
  const res = await postJSON(`/api/definitions/${encodeURIComponent(id)}/observing`, { observing })
  if (res.ok) return definitionEntry(await res.json())
  return (await res.text()) || `setWatchlistObserving: ${res.status}`
}

// fetchWatchlistMatches answers a windowed query over the persisted
// match log for one source device (internal/matchlog's own query
// contract) -- mac and/or ip identify the source; at least one is
// required (mirroring matchlog.Identity's MAC-preferred rule), since/
// until/limit are all optional. Session-gated like every other read
// here (accessUser, not admin-only -- see internal/api's authzMatrix),
// since this is also the read-only-API-token-reachable correlation
// surface #243 exists for.
export async function fetchWatchlistMatches(params: {
  mac?: string
  ip?: string
  since?: string
  until?: string
  limit?: number
}): Promise<WatchlistMatch[]> {
  const q = new URLSearchParams()
  if (params.mac) q.set('mac', params.mac)
  if (params.ip) q.set('ip', params.ip)
  if (params.since) q.set('since', params.since)
  if (params.until) q.set('until', params.until)
  if (params.limit) q.set('limit', String(params.limit))
  const res = await fetch(`/api/matches?${q.toString()}`)
  if (!res.ok) throw new ApiError(`fetchWatchlistMatches: ${res.status}`, res.status)
  const body = await res.json()
  return body.matches ?? []
}

// fetchRecentMatches asks the other question the match log answers: not
// "what has this device done" but "what has broken recently" -- the most
// recent matches across every entry, newest (by lastSeen) first (#586,
// for the Matches tab of #584).
//
// Same route, a different mode: entries=all, which internal/api's
// handleMatchesQuery refuses to combine with mac or ip (see its doc
// comment -- "no identity" must never quietly become "every device").
// So this is a separate function rather than another optional field on
// fetchWatchlistMatches: the two parameter sets are mutually exclusive
// server-side, and a single signature that can express an illegal
// request is one a caller can send by accident.
//
// until is the paging cursor and filters on *firstSeen*, exclusively
// (matchlog's file and Postgres backends both do: `first_seen < until`).
// See matches.svelte.ts for what that means for "load older".
export async function fetchRecentMatches(params: {
  since?: string
  until?: string
  limit?: number
}): Promise<WatchlistMatch[]> {
  const q = new URLSearchParams({ entries: 'all' })
  if (params.since) q.set('since', params.since)
  if (params.until) q.set('until', params.until)
  if (params.limit) q.set('limit', String(params.limit))
  const res = await fetch(`/api/matches?${q.toString()}`)
  if (!res.ok) throw new ApiError(`fetchRecentMatches: ${res.status}`, res.status)
  const body = await res.json()
  return body.matches ?? []
}

// Admin-only review surface over internal/suggest's candidate pool
// (#243 slice 5) -- watchlist entries suggested from data RouterOS has
// already pushed. Every candidate id routinely contains a raw NUL byte
// (see Suggestion's own doc comment in types.ts), so every path below
// goes through encodeURIComponent, the same convention
// deleteWatchlistEntry etc. already use for their own ids.

export async function fetchSuggestions(status?: SuggestionStatus): Promise<Suggestion[]> {
  const q = status ? `?status=${encodeURIComponent(status)}` : ''
  const res = await fetch(`/api/suggestions${q}`)
  if (!res.ok) throw new ApiError(`fetchSuggestions: ${res.status}`, res.status)
  const body = await res.json()
  return body.candidates ?? []
}

// acceptSuggestion turns an Off candidate into a real watchlist entry --
// see internal/api's handleSuggestionsAccept for exactly what entry
// shape results per candidate kind. Returns both the updated candidate
// and the entry it became.
export async function acceptSuggestion(
  id: string,
): Promise<{ candidate: Suggestion; entry: WatchlistEntry } | string> {
  const res = await postJSON(`/api/suggestions/${encodeURIComponent(id)}/accept`)
  if (res.ok) return await res.json()
  return (await res.text()) || `acceptSuggestion: ${res.status}`
}

export async function hideSuggestion(id: string): Promise<Suggestion | string> {
  const res = await postJSON(`/api/suggestions/${encodeURIComponent(id)}/hide`)
  if (res.ok) return await res.json()
  return (await res.text()) || `hideSuggestion: ${res.status}`
}

export async function unhideSuggestion(id: string): Promise<Suggestion | string> {
  const res = await postJSON(`/api/suggestions/${encodeURIComponent(id)}/unhide`)
  if (res.ok) return await res.json()
  return (await res.text()) || `unhideSuggestion: ${res.status}`
}

// resetSuggestions is #243 slice 5's "nuke" action: permanently deletes
// every watchlist entry and starts over from a fresh look at the router.
// confirm must be sent true (see internal/api's handleSuggestionsReset)
// -- there is no accidental-call path here, by design.
export async function resetSuggestions(): Promise<Suggestion[] | string> {
  const res = await postJSON('/api/suggestions/reset', { confirm: true })
  if (res.ok) {
    const body = await res.json()
    return body.candidates ?? []
  }
  return (await res.text()) || `resetSuggestions: ${res.status}`
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

export async function createToken(
  name: string,
  kind: 'api' | 'ingest',
  device?: string,
): Promise<ApiToken | string> {
  // Omitting kind means read-only server-side; sending it explicitly
  // keeps what the operator chose in the dialog and what goes on the
  // wire identical. device is required for ingest and rejected
  // otherwise -- the server enforces it, the dialog mirrors it.
  const res = await postJSON('/api/tokens', device ? { name, kind, device } : { name, kind })
  if (res.ok) return res.json()
  return (await res.text()) || `createToken: ${res.status}`
}

export async function revokeToken(id: string): Promise<string | null> {
  const res = await deleteJSON(`/api/tokens/${encodeURIComponent(id)}`)
  if (res.ok) return null
  return (await res.text()) || `revokeToken: ${res.status}`
}

// fetchAuditLog serves a windowed slice of the admin-action audit log
// (issue #112) -- admin-only (see internal/api/audit.go's callerIsAdmin
// gate, the same strict check fetchEntities/fetchTokens use). No
// filter/pagination UI yet (limit defaults server-side to the most
// recent 200 entries, see internal/audit.defaultLimit) -- this is a
// simple, read-only accountability list, not a searchable log viewer.
export async function fetchAuditLog(): Promise<AuditResult> {
  const res = await fetch('/api/audit')
  if (!res.ok) throw new ApiError(`fetchAuditLog: ${res.status}`, res.status)
  return res.json()
}

// startSSOLink begins converting the signed-in account to SSO-only.
// POST, not a navigation, so the CSRF header applies -- linking
// destroys the account's local password, and a GET-initiated flow could
// be triggered cross-site (see internal/api/oidc.go's
// handleOIDCLinkStart). Returns the provider URL for the caller to
// navigate to, or an error message.
export async function startSSOLink(): Promise<{ url: string } | string> {
  const res = await postJSON('/api/auth/oidc/link')
  if (!res.ok) return (await res.text()) || `startSSOLink: ${res.status}`
  return res.json()
}

// The guided setup wizard's view of what has landed (#320). Admin-only
// server-side; the menu entry is gated the same way.
export async function fetchSetupStatus(): Promise<SetupStatus> {
  const res = await fetch('/api/setup/status')
  if (!res.ok) throw new ApiError(`fetchSetupStatus: ${res.status}`, res.status)
  return res.json()
}
