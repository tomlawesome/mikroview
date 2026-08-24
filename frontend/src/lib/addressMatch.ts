// SPDX-License-Identifier: AGPL-3.0-only

// The Source/Destination boxes' "type what you see" matcher (#438): a
// full IP is an exact match, a CIDR is containment, and anything else is
// a case-insensitive substring against whatever the row displays (the
// resolved label) and the raw address text. Order matters and there is
// no mode switch -- the input's own shape decides which rule applies.
//
// Both IPv4 and IPv6 are supported (RouterOS logs either), using BigInt
// for the 128-bit IPv6 arithmetic. This is intentionally a real parser
// rather than a regex-only approximation like format.ts's isPublicIp
// (which documents why it can afford to be IPv4-only, display-decision
// only): getting this one wrong means the box silently matches the wrong
// rows, not just hiding an "investigate" button.

function parseIPv4(s: string): bigint | null {
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(s)
  if (!m) return null
  let value = 0n
  for (let i = 1; i <= 4; i++) {
    const octet = Number(m[i])
    if (octet > 255) return null
    value = (value << 8n) | BigInt(octet)
  }
  return value
}

// parseIPv6 expands the "::" zero-run compression (at most one, per
// RFC 4291) and accepts a trailing embedded IPv4 tail
// (e.g. "::ffff:192.0.2.1"), which replaces the last two 16-bit groups.
function parseIPv6(s: string): bigint | null {
  if (!s.includes(':')) return null
  const parts = s.split('::')
  if (parts.length > 2) return null

  const splitGroups = (group: string): string[] => (group === '' ? [] : group.split(':'))

  const expandEmbeddedV4 = (groups: string[]): string[] | null => {
    const last = groups[groups.length - 1]
    if (!last || !last.includes('.')) return groups
    const v4 = parseIPv4(last)
    if (v4 === null) return null
    const hi = (v4 >> 16n) & 0xffffn
    const lo = v4 & 0xffffn
    return [...groups.slice(0, -1), hi.toString(16), lo.toString(16)]
  }

  let head = expandEmbeddedV4(splitGroups(parts[0]))
  let tail = parts.length === 2 ? expandEmbeddedV4(splitGroups(parts[1])) : []
  if (head === null || tail === null) return null

  let groups: string[]
  if (parts.length === 1) {
    if (head.length !== 8) return null
    groups = head
  } else {
    const missing = 8 - head.length - tail.length
    if (missing < 0) return null
    groups = [...head, ...Array(missing).fill('0'), ...tail]
  }
  if (groups.length !== 8) return null

  let value = 0n
  for (const g of groups) {
    if (!/^[0-9a-fA-F]{1,4}$/.test(g)) return null
    value = (value << 16n) | BigInt(parseInt(g, 16))
  }
  return value
}

export type AddressFamily = 4 | 6

export interface ParsedAddress {
  family: AddressFamily
  value: bigint
}

/** parseAddress accepts a whole-string IPv4 or IPv6 literal, nothing else. */
export function parseAddress(s: string): ParsedAddress | null {
  const v4 = parseIPv4(s)
  if (v4 !== null) return { family: 4, value: v4 }
  const v6 = parseIPv6(s)
  if (v6 !== null) return { family: 6, value: v6 }
  return null
}

export interface ParsedCidr {
  family: AddressFamily
  base: bigint
  prefix: number
}

/** parseCidr accepts a whole-string "address/prefix", nothing else. */
export function parseCidr(s: string): ParsedCidr | null {
  const i = s.lastIndexOf('/')
  if (i < 0) return null
  const prefixText = s.slice(i + 1)
  if (!/^\d{1,3}$/.test(prefixText)) return null
  const addr = parseAddress(s.slice(0, i))
  if (!addr) return null
  const prefix = Number(prefixText)
  const maxPrefix = addr.family === 4 ? 32 : 128
  if (prefix > maxPrefix) return null
  return { family: addr.family, base: addr.value, prefix }
}

function addressEquals(candidate: string, ip: ParsedAddress): boolean {
  const parsed = parseAddress(candidate)
  return !!parsed && parsed.family === ip.family && parsed.value === ip.value
}

function addressInCidr(candidate: string, cidr: ParsedCidr): boolean {
  const parsed = parseAddress(candidate)
  if (!parsed || parsed.family !== cidr.family) return false
  const width = cidr.family === 4 ? 32n : 128n
  const full = (1n << width) - 1n
  const shift = width - BigInt(cidr.prefix)
  const mask = (full << shift) & full
  return (parsed.value & mask) === (cidr.base & mask)
}

export interface AddressCandidate {
  ip?: string
  hostName?: string
}

// matchesAddressQuery is the Source/Destination boxes' whole matching
// contract. Multiple candidates let one side carry more than one address
// worth checking -- the row's own address plus, for a srcnat/dstnat row,
// the NAT-translated counterpart on that side (see state.svelte.ts's
// srcCandidates/dstCandidates and the NAT-parity section of #438).
export function matchesAddressQuery(query: string, candidates: AddressCandidate[]): boolean {
  const q = query.trim()
  if (!q) return true

  const ip = parseAddress(q)
  if (ip) return candidates.some((c) => !!c.ip && addressEquals(c.ip, ip))

  const cidr = parseCidr(q)
  if (cidr) return candidates.some((c) => !!c.ip && addressInCidr(c.ip, cidr))

  const needle = q.toLowerCase()
  return candidates.some(
    (c) =>
      (c.hostName?.toLowerCase().includes(needle) ?? false) ||
      (c.ip?.toLowerCase().includes(needle) ?? false),
  )
}
