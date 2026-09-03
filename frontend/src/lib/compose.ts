// SPDX-License-Identifier: AGPL-3.0-only
//
// The reach's printed command (#626/#633, round 2 scene 4): a denial
// becomes a rule in two clicks -- drafted here, pasted by the operator,
// per the observes-never-connects invariant. mikroview prints, it never
// runs: composing text is the whole job of this module.
import type { PolicyEdge } from './policy.svelte'
import type { ReachStrand } from './reach'

export interface ComposeInput {
  /** The centred host's address -- one end of the rule. */
  hostIp: string
  /** 'out': the host speaks (src). 'in': it is spoken to (dst). */
  direction: 'out' | 'in'
  /** The far side: a single address, or a CIDR when scoped to the
   * whole subnet. */
  target: string
  port: number
  proto: string
  /** allow: an accepting rule. block: the explicit, logged, *named*
   * drop -- for keeping the denial while retiring its anonymity. */
  mode: 'allow' | 'block'
  /** Display names for the comment, host first. */
  hostName: string
  targetName: string
  /** The refusing rule's comment on this pair, when the pushed table
   * carries one -- an allow is placed before it so it actually fires. */
  placeBefore?: string
}

// A RouterOS comment/prefix takes no quotes of its own; keep to a safe
// alphabet rather than escaping (these are drafts for a human to read).
function slug(s: string): string {
  return (
    s
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 24) || 'strand'
  )
}

export function composeCommand(c: ComposeInput): string {
  const src = c.direction === 'out' ? c.hostIp : c.target
  const dst = c.direction === 'out' ? c.target : c.hostIp
  const action = c.mode === 'allow' ? 'accept' : 'drop'
  const name = `${slug(c.hostName)}-${slug(c.targetName)}-${c.port}`
  const comment =
    c.mode === 'allow'
      ? `${c.hostName} → ${c.targetName} :${c.port}`
      : `named block: ${c.hostName} → ${c.targetName} :${c.port}`
  const lines = [
    `/ip firewall filter add chain=forward src-address=${src} dst-address=${dst} \\`,
    `    protocol=${c.proto} dst-port=${c.port} action=${action} log=yes log-prefix="${name}" \\`,
    `    comment="${comment}"${c.mode === 'allow' && c.placeBefore ? ` place-before=[find comment="${c.placeBefore}"]` : ''}`,
  ]
  return lines.join('\n')
}

/** The refusing rule's comment for a pair, read from the pushed table
 * -- gives the allow its place-before so it fires before the drop. */
export function refusingCommentFor(edges: PolicyEdge[], from: string, to: string): string | undefined {
  const e = edges.find((p) => p.key === `${from}|${to}`)
  return e?.refused && e.comment ? e.comment : undefined
}

/** What a strand needs turned into a ComposeInput: the centred host's
 * own identity, its boundary interface, and the pushed policy table --
 * everything reachComposeInput reads besides the strand itself. */
export interface ReachComposeContext {
  hostIp: string
  hostName: string
  /** The centred host's own zone/district -- 2D's `reach.zoneId`, the
   * city's own standing building's district id. */
  zoneId: string
  wanInterface: string | null
  zones: { id: string; cidr?: string | null; name: string }[]
  edges: PolicyEdge[]
}

export interface ReachComposeOverrides {
  mode?: 'allow' | 'block'
  /** A chosen port hit; null/absent falls back to the strand's own
   * busiest port. */
  port?: number | null
  /** A freely typed port, as text the same way the 2D panel's input
   * does -- wins over `port` when it parses to a valid port number. */
  free?: string
  scope?: 'host' | 'subnet'
}

/**
 * reachComposeInput turns a reach strand into the ComposeInput
 * composeCommand prints -- the one place either view decides which
 * port, target and place-before a drafted rule gets, so the 2D map's
 * rich picker (allow/block, host/subnet, a free-typed port) and the
 * city's plain default draft (#868, DESIGN.md "The reach": "it's been
 * asking · tcp/445 · 14×" and the printed line, no picker of its own)
 * are two callers of one function rather than two guesses that happen
 * to agree today. Returns null exactly when there is nothing to draft
 * from -- no port and no free-typed one, or no far-side address at all.
 */
export function reachComposeInput(strand: ReachStrand, ctx: ReachComposeContext, over: ReachComposeOverrides = {}): ComposeInput | null {
  const counterpartIface = strand.counterpart === 'internet' ? (ctx.wanInterface ?? '') : strand.counterpart
  const counterpartZone = strand.counterpart !== 'internet' ? ctx.zones.find((z) => z.id === strand.counterpart) : undefined
  const mode = over.mode ?? 'allow'
  const scope = over.scope ?? 'host'
  const peerAddr = strand.peerAddrs[0] ?? ''
  const peerName = strand.peers[0] ?? (strand.counterpart === 'internet' ? 'the internet' : strand.counterpart)

  const free = Number.parseInt(over.free ?? '', 10)
  const chosenPort =
    !Number.isNaN(free) && free > 0 && free < 65536
      ? { port: free, proto: 'tcp' }
      : (() => {
          const port = over.port ?? strand.portHits[0]?.port ?? null
          if (port === null) return null
          const hit = strand.portHits.find((h) => h.port === port)
          return hit ? { port: hit.port, proto: hit.proto } : { port, proto: 'tcp' }
        })()
  if (!chosenPort) return null

  const target = scope === 'subnet' && counterpartZone?.cidr ? counterpartZone.cidr : peerAddr
  if (!target) return null
  const targetName = scope === 'subnet' ? (counterpartZone?.name ?? peerName) : peerName

  const pairFrom = strand.direction === 'out' ? ctx.zoneId : counterpartIface
  const pairTo = strand.direction === 'out' ? counterpartIface : ctx.zoneId

  return {
    hostIp: ctx.hostIp,
    direction: strand.direction,
    target,
    port: chosenPort.port,
    proto: chosenPort.proto,
    mode,
    hostName: ctx.hostName,
    targetName,
    placeBefore: refusingCommentFor(ctx.edges, pairFrom, pairTo),
  }
}
