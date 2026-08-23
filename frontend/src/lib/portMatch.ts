// SPDX-License-Identifier: AGPL-3.0-only

import { lookupPort } from './commonPorts'

export interface PortCandidate {
  port?: number
  portName?: string
}

// Every label a port might be shown under, for the same "type what you
// see" contract the Source/Destination boxes use (lib/addressMatch.ts):
// the raw number, the operator's own name (#413), and any well-known
// service name(s) lib/commonPorts.ts knows for it.
function portLabels(c: PortCandidate): string[] {
  if (!c.port) return []
  const labels = [String(c.port)]
  if (c.portName) labels.push(c.portName)
  for (const info of lookupPort(c.port) ?? []) labels.push(info.name)
  return labels
}

// matchesPortQuery is the Port box's dual contract (#438): a bare
// integer is an exact port-number match on either side, unchanged from
// before this issue; anything else is a case-insensitive substring
// match against the displayed label on either side.
export function matchesPortQuery(query: string, candidates: PortCandidate[]): boolean {
  const q = query.trim()
  if (!q) return true

  if (/^\d+$/.test(q)) {
    const n = Number(q)
    return candidates.some((c) => c.port === n)
  }

  const needle = q.toLowerCase()
  return candidates.some((c) => portLabels(c).some((label) => label.toLowerCase().includes(needle)))
}
