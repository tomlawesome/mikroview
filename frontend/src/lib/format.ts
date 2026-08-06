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

// formatRelative renders how long ago `iso` was, as a short "Xs/Xm/Xh/Xd
// ago" string -- used where "how long ago" reads faster at a glance than
// an exact clock time (see formatHM/formatTime for that instead), e.g.
// the Fleet view's last-seen column. `nowMs` is a parameter rather than
// read from Date.now() internally so a caller driven by a reactive
// ticking clock (e.g. appState.now) re-renders this on the same cadence
// as everything else, instead of each call site free-running its own
// timer. Returns the original string unchanged if it doesn't parse, and
// never a negative duration (a lastSeen a few ms ahead of `nowMs` due to
// clock skew between client and server reads as "just now", not
// "-1s ago").
export function formatRelative(iso: string, nowMs: number): string {
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return iso
  const deltaMs = Math.max(0, nowMs - t)
  const s = Math.floor(deltaMs / 1000)
  if (s < 5) return 'just now'
  if (s < 60) return `${s}s ago`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  const d = Math.floor(h / 24)
  return `${d}d ago`
}
