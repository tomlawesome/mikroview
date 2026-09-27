// SPDX-License-Identifier: AGPL-3.0-only
//
// Wording shared by the surfaces that show #1352's country sources and
// network owners: the Engine Room card, the IP popover, the dossier and
// the stream's flag tooltip. Kept here so the four say it the same way.
import type { GeoLookup, GeoSourceName } from './types'

/** Each source's name as the card and its top line write it. */
export const GEO_SOURCE_NAMES: Record<GeoSourceName, string> = {
  dbip: 'DB-IP Lite',
  ipinfo: 'IPinfo Lite',
  maxmind: 'MaxMind GeoLite2',
}

export const DBIP_URL = 'https://db-ip.com'
export const IPINFO_URL = 'https://ipinfo.io'
export const MAXMIND_URL = 'https://www.maxmind.com'

/** The card's top line: which source the flags come from right now. */
export function inUseLine(source: GeoSourceName | null): string {
  return source ? `Flags from ${GEO_SOURCE_NAMES[source]}` : 'No flag source loaded yet'
}

/**
 * The network owner as one phrase -- "AS13335 Cloudflare, Inc." -- or
 * null when the source does not know it, so a caller shows nothing
 * rather than an empty label.
 */
export function ownerLabel(lookup: GeoLookup | null | undefined): string | null {
  if (!lookup) return null
  const asn = lookup.asn ? `AS${lookup.asn}` : ''
  const name = lookup.asName?.trim() ?? ''
  const label = [asn, name].filter(Boolean).join(' ')
  return label || null
}

/**
 * A country code as its name in the reader's own language ("GB" ->
 * "United Kingdom"), falling back to the code itself where the browser
 * has no name for it.
 */
export function countryName(code: string | null | undefined): string {
  if (!code) return ''
  try {
    const names = new Intl.DisplayNames(undefined, { type: 'region' })
    return names.of(code.toUpperCase()) ?? code
  } catch {
    return code
  }
}

/**
 * How long until `iso`, for the card's "next refresh" -- "in 3d" -- the
 * forward twin of formatRelative. A time already passed reads "due".
 */
export function formatUntil(iso: string, nowMs: number): string {
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return iso
  const s = Math.floor((t - nowMs) / 1000)
  if (s <= 0) return 'due'
  if (s < 60) return `in ${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `in ${m}m`
  const h = Math.floor(m / 60)
  if (h < 24) return `in ${h}h`
  return `in ${Math.floor(h / 24)}d`
}
