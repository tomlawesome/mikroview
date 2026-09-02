// SPDX-License-Identifier: AGPL-3.0-only

export function formatTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleTimeString(undefined, { hour12: false })
}

// The stream's time column (#644's squared columns). Milliseconds, not
// just seconds: at real event rates several rows share a second, and the
// order the table shows is decided below one -- whole-second stamps make
// distinct arrivals read as simultaneous.
export function formatTimeMs(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return `${d.toLocaleTimeString(undefined, { hour12: false })}.${String(d.getMilliseconds()).padStart(3, '0')}`
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

// formatDayMonth renders a date as a bare day and short month -- "2
// Sept" in a UK locale, "Sep 2" in a US one. For the returning-flag
// cards (#640), which say when a pair was last judged ("you checked this
// on 2 Sept and found it fine"): the day is what the operator needs to
// place the event, and a clock time would imply a precision the sentence
// is not making a claim about.
//
// Locale-driven like every other helper here (formatTime, formatHM),
// rather than a hand-built month table: the browser already knows how
// this reader writes a date. Returns the original string unchanged if it
// does not parse, same as its neighbours.
export function formatDayMonth(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
}

export function formatEps(eps: number): string {
  if (eps < 1) return eps.toFixed(1)
  return Math.round(eps).toString()
}

// formatDurationShort renders a duration in seconds as a compact
// "primary unit + secondary unit" string -- "2m 52s", "5h 33m", "3d 4h".
// Two significant units is enough precision for an at-a-glance estimate,
// and dropping to one once the duration reaches days avoids a "3d 4h 12m
// 08s" string nobody needs.
export function formatDurationShort(totalSeconds: number): string {
  const s = Math.max(0, Math.round(totalSeconds))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${s % 60}s`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ${m % 60}m`
  const d = Math.floor(h / 24)
  return `${d}d ${h % 24}h`
}

// formatUptimeDaysHours renders a duration in seconds as days and hours
// only -- "12 d 4 h" -- for the account menu's foot, where uptime sits
// beside the version: "0.9 · AGPL-3.0 · up 12 d 4 h".
//
// Two units, and no smaller one, is the ratified design rather than a
// simplification (round 37, accepted by the owner 2026-09-02): "a
// ticking second is a clock, not a fact". The counter underneath still
// advances every second; reading only these two units off it means the
// rendered string changes once an hour, so a menu left open does not
// twitch. Both units always appear, so the string keeps one shape.
export function formatUptimeDaysHours(totalSeconds: number): string {
  const s = Math.max(0, Math.round(totalSeconds))
  const days = Math.floor(s / 86_400)
  const hours = Math.floor((s % 86_400) / 3600)
  return `${days} d ${hours} h`
}

// parseGoDurationSeconds reads a Go time.Duration.String() value (the
// wire format internal/engine's ValidateParams normalizes a "duration"
// param to -- see validateDurationParam's `d.String()` -- e.g. "1m0s",
// "500ms", "1h30m0s") into whole seconds. Used by the port-scan window
// row (#677) to show/edit a definition's window param as a plain
// second count ("60 s") rather than Go's compound notation.
export function parseGoDurationSeconds(s: string): number {
  const re = /(\d+(?:\.\d+)?)(h|ms|m|s)/g
  let total = 0
  let m: RegExpExecArray | null
  while ((m = re.exec(s))) {
    const v = parseFloat(m[1])
    switch (m[2]) {
      case 'h':
        total += v * 3600
        break
      case 'm':
        total += v * 60
        break
      case 's':
        total += v
        break
      case 'ms':
        total += v / 1000
        break
    }
  }
  return total
}

// formatDaysSince renders how long ago iso was as a whole-day count --
// "4 d", or "under a day" for anything still inside the first 24h.
// Mirrors EngineRoom.svelte's own quietFor day-count convention (same
// Math.floor-of-whole-days reasoning: "quiet 3 d" there, "signed in 4
// d" here for #677's sessions row) rather than introducing a second way
// to say the same kind of thing.
export function formatDaysSince(iso: string): string {
  const days = Math.floor((Date.now() - new Date(iso).getTime()) / 86_400_000)
  return days >= 1 ? `${days} d` : 'under a day'
}

// formatBufferDepth summarizes how full the server's in-memory event ring
// (store.maxEvents) is and, once full, roughly how far back it reaches at
// the current rate -- the two facts an operator needs to tell "the ring
// is comfortably covering my retention window" from "it wrapped
// minutes ago and I'd have no way to know" (issue #244).
//
// The ring (internal/store/ring.go) is fixed-capacity: once count reaches
// capacity, every new event overwrites the oldest. So "how far back" is
// only a meaningful question once it's full -- before that, count *is*
// the entire history held since boot (or since Clear), not a fraction of
// a longer one.
//
// eventsPerSecond is a 10s rolling average (see Store.Stats), so below
// roughly one event per ten seconds the estimate is dominated by that
// window's own noise rather than the real rate -- reporting "buffer
// full" without a duration in that case is honest where a wildly
// swinging number would not be.
export function formatBufferDepth(capacity: number, count: number, eventsPerSecond: number): string {
  if (capacity <= 0) return ''
  if (count < capacity) {
    const pct = Math.round((count / capacity) * 100)
    return `${pct}% of buffer used`
  }
  if (eventsPerSecond < 0.1) return 'buffer full'
  return `holding last ${formatDurationShort(capacity / eventsPerSecond)}`
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

// rawTooltip is the verbatim router log line as shown on hover, plus a
// note when the server cut it.
//
// The line is the one thing in a row that is meant to be exactly what
// the router sent, so a shortened one that says nothing would be a
// quiet lie. Truncation only happens above the server's cap
// (store.MaxRawBytes, 2 KiB), which is roughly five times the longest
// genuine RouterOS line -- so in practice this note appears only for
// deliberately oversized input. See #285.
export function rawTooltip(raw: string, truncated?: boolean): string {
  return truncated ? `${raw}\n\n[truncated — the line sent was longer than MikroView stores]` : raw
}
