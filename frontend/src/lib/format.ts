export function formatTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleTimeString(undefined, { hour12: false })
}

export function formatAddr(ip?: string, port?: number): string {
  if (!ip) return '—'
  return port ? `${ip}:${port}` : ip
}

// IPv4-only mirror of internal/reputation's isPublic (the same RFC1918 /
// loopback / link-local ranges the backend already rejects with
// ErrNotPublic) -- used client-side just to decide whether the
// investigate affordance is worth showing at all, not as a security
// boundary (the backend re-checks regardless).
export function isPublicIp(ip?: string): boolean {
  if (!ip) return false
  const m = ip.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/)
  if (!m) return false
  const [a, b] = [Number(m[1]), Number(m[2])]
  if (a === 10) return false
  if (a === 172 && b >= 16 && b <= 31) return false
  if (a === 192 && b === 168) return false
  if (a === 127) return false
  if (a === 169 && b === 254) return false
  if (a === 0) return false
  return true
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

export function formatHM(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false })
}

export function formatEps(eps: number): string {
  if (eps < 1) return eps.toFixed(1)
  return Math.round(eps).toString()
}
