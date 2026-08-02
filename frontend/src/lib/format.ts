export function formatTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleTimeString(undefined, { hour12: false })
}

export function formatAddr(ip?: string, port?: number): string {
  if (!ip) return '—'
  return port ? `${ip}:${port}` : ip
}

// Converts an ISO 3166-1 alpha-2 country code (e.g. "US") to its flag
// emoji by combining Unicode regional indicator symbols -- no image
// assets or lookup table needed. Returns '' for anything that isn't
// exactly two letters (missing/unresolved GeoIP data).
export function countryFlag(code?: string): string {
  if (!code || code.length !== 2) return ''
  const upper = code.toUpperCase()
  if (!/^[A-Z]{2}$/.test(upper)) return ''
  const REGIONAL_INDICATOR_A = 0x1f1e6
  const points = [...upper].map((c) => REGIONAL_INDICATOR_A + (c.charCodeAt(0) - 65))
  return String.fromCodePoint(...points)
}

export function formatEps(eps: number): string {
  if (eps < 1) return eps.toFixed(1)
  return Math.round(eps).toString()
}
