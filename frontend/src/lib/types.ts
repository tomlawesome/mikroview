// Mirrors internal/store/event.go's JSON tags.
export type Action = 'accept' | 'drop' | 'reject' | 'log' | 'unknown'

export interface FirewallEvent {
  id: number
  time: string
  deviceId: string
  sourceIp: string
  action: Action
  ruleLabel: string
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
  natIp?: string
  natPort?: number
  natRaw?: string
  length?: number
  flags?: string
  raw: string
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

// Mirrors internal/api/rest.go's handleStats response.
export interface Stats {
  total: number
  byAction: Partial<Record<Action, number>>
  eventsPerSecond: number
  capacity: number
  count: number
  windowSeconds: number
  connectedClients: number
}

export interface Filters {
  device: string
  action: Action | ''
  protocol: string
  chain: string
  interface: string
  ip: string
  port: string
  rule: string
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
    rule: '',
  }
}
