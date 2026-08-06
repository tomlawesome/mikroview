import type { ClientEvent, Entity, RuleUsage } from './types'

// "Discovered but unnamed" derivation for the Entities admin panel
// (issue #109) -- mirrors internal/device.Registry's own pattern
// (auto-discovered, shown even before configured/labeled) so a user can
// name a host/rule/port without already knowing its raw IP/label/number.
//
// Rules have a real backend registry to read from (internal/rules.Store,
// exposed via GET /api/rules) -- every rule label ever seen firing,
// independent of the client's event buffer. Hosts and ports don't have
// an equivalent long-lived server-side registry (unlike a RouterOS
// device, an arbitrary traffic host/port has no "device" to register),
// so those two are derived client-side from the event buffer the app
// already holds (appState.events) -- necessarily only as complete as
// that buffer, which is an acceptable trade-off for a discovery aid, not
// a source of truth (the entity itself, once named, is authoritative
// and persisted regardless of buffer contents).

export interface DiscoveredItem {
  key: string
  lastSeen: string
}

function namedKeySet(entities: Entity[]): Set<string> {
  return new Set(entities.map((e) => `${e.type}:${e.key}`))
}

function newestFirst(items: DiscoveredItem[]): DiscoveredItem[] {
  return [...items].sort((a, b) => b.lastSeen.localeCompare(a.lastSeen))
}

// discoverHosts collects every distinct srcIp/dstIp seen in events that
// doesn't already have a "host" entity -- keyed on the IP itself, with
// lastSeen the most recent event.time it appeared in (either side).
export function discoverHosts(
  events: Pick<ClientEvent, 'srcIp' | 'dstIp' | 'time'>[],
  entities: Entity[],
): DiscoveredItem[] {
  const named = namedKeySet(entities)
  const seen = new Map<string, string>()
  for (const e of events) {
    for (const ip of [e.srcIp, e.dstIp]) {
      if (!ip || named.has(`host:${ip}`)) continue
      const prev = seen.get(ip)
      if (!prev || e.time > prev) seen.set(ip, e.time)
    }
  }
  return newestFirst([...seen.entries()].map(([key, lastSeen]) => ({ key, lastSeen })))
}

// discoverPorts is the same idea as discoverHosts, over srcPort/dstPort.
// Port 0 (absent -- e.g. non-TCP/UDP protocols) is never a candidate,
// matching internal/naming.Resolver.Port's own "0 means no port"
// contract on the backend.
export function discoverPorts(
  events: Pick<ClientEvent, 'srcPort' | 'dstPort' | 'time'>[],
  entities: Entity[],
): DiscoveredItem[] {
  const named = namedKeySet(entities)
  const seen = new Map<string, string>()
  for (const e of events) {
    for (const port of [e.srcPort, e.dstPort]) {
      if (!port || port <= 0) continue
      const key = String(port)
      if (named.has(`port:${key}`)) continue
      const prev = seen.get(key)
      if (!prev || e.time > prev) seen.set(key, e.time)
    }
  }
  return newestFirst([...seen.entries()].map(([key, lastSeen]) => ({ key, lastSeen })))
}

// discoverRules filters GET /api/rules' full history down to labels with
// no "rule" entity yet -- unlike hosts/ports this reads from the
// authoritative persisted registry (internal/rules.Store), not the
// client's own capped event buffer, so it stays complete regardless of
// how much traffic the browser has actually seen since load.
export function discoverRules(usage: RuleUsage[], entities: Entity[]): DiscoveredItem[] {
  const named = namedKeySet(entities)
  const items = usage
    .filter((u) => u.rule && !named.has(`rule:${u.rule}`))
    .map((u) => ({ key: u.rule, lastSeen: u.lastSeen }))
  return newestFirst(items)
}
