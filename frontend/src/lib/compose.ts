// SPDX-License-Identifier: AGPL-3.0-only
//
// The reach's printed command (#626/#633, round 2 scene 4): a denial
// becomes a rule in two clicks -- drafted here, pasted by the operator,
// per the observes-never-connects invariant. mikroview prints, it never
// runs: composing text is the whole job of this module.
import type { PolicyEdge } from './policy.svelte'

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
