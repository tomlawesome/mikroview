import type { Device, EventsResult, Filters, Stats } from './types'

function buildQuery(filters: Partial<Filters> & { limit?: number; sinceId?: number }): string {
  const params = new URLSearchParams()
  if (filters.device) params.set('device', filters.device)
  if (filters.action) params.set('action', filters.action)
  if (filters.protocol) params.set('protocol', filters.protocol)
  if (filters.chain) params.set('chain', filters.chain)
  if (filters.interface) params.set('interface', filters.interface)
  if (filters.ip) params.set('ip', filters.ip)
  if (filters.port) params.set('port', filters.port)
  if (filters.rule) params.set('rule', filters.rule)
  if (filters.limit) params.set('limit', String(filters.limit))
  if (filters.sinceId) params.set('sinceId', String(filters.sinceId))
  const qs = params.toString()
  return qs ? `?${qs}` : ''
}

export async function fetchEvents(
  filters: Partial<Filters> & { limit?: number; sinceId?: number } = {},
): Promise<EventsResult> {
  const res = await fetch(`/api/events${buildQuery(filters)}`)
  if (!res.ok) throw new Error(`fetchEvents: ${res.status}`)
  return res.json()
}

export async function fetchDevices(): Promise<Device[]> {
  const res = await fetch('/api/devices')
  if (!res.ok) throw new Error(`fetchDevices: ${res.status}`)
  const body = await res.json()
  return body.devices ?? []
}

export async function fetchStats(): Promise<Stats> {
  const res = await fetch('/api/stats')
  if (!res.ok) throw new Error(`fetchStats: ${res.status}`)
  return res.json()
}
