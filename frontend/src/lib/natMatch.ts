// SPDX-License-Identifier: AGPL-3.0-only

import type { RouterNatRule } from './api'
import type { FirewallEvent } from './types'
import { addressInCidr, parseAddress, parseCidr } from './addressMatch'

// The subtraction half of #445's NAT popup.
//
// The one rule the whole feature turns on: **only a logged prefix names
// a rule; everything else is subtraction.** When the operator tagged the
// NAT rule with a log-prefix, the event resolves to that rule by name
// and none of this runs. When they did not, RouterOS's log line reports
// a translation without saying which rule performed it, and no amount of
// inference fixes that -- so instead of guessing, this partitions the
// pushed table into the rules the event *could* have come from and the
// rules it positively rules out, and hands back the reason for every
// exclusion so the operator can audit the partition rather than trust
// it.
//
// Three properties are load-bearing, and each of them is a way this
// could have quietly become a guess:
//
//   - **Exclusion only by positive contradiction.** A rule leaves the
//     could-have set only when something the event states is
//     incompatible with something the rule states. Nothing is scored,
//     ranked or preferred.
//   - **Unknown never excludes.** A condition this cannot evaluate -- an
//     address-list name where an address was expected, a rule condition
//     the log line has no field for -- keeps the rule in the could-have
//     set, with the unevaluated condition reported. A silent
//     can't-tell-so-drop-it would produce a shorter, more confident, and
//     wrong answer.
//   - **RouterOS order is preserved.** First match wins on the router,
//     so the table's own order is evidence. Nothing here reorders.

// NatEventFacts is everything about one event the partition may read --
// deliberately a narrow structural type rather than FirewallEvent, so it
// is obvious at a glance that nothing else about the event (its action,
// its raw line, its names) can leak into the decision.
export interface NatEventFacts {
  chain?: string
  protocol?: string
  srcIp?: string
  dstIp?: string
  dstPort?: number
  natPort?: number
  inInterface?: string
  outInterface?: string
}

// natFactsFromEvent is the only bridge from a rendered row to the
// partition. It exists so the narrowing happens once, in one place: a
// component reaching into the event itself would make it easy for a
// field with no bearing on which rule matched to start influencing the
// answer without anyone noticing.
export function natFactsFromEvent(e: FirewallEvent): NatEventFacts {
  return {
    chain: e.chain,
    protocol: e.protocol,
    srcIp: e.srcIp,
    dstIp: e.dstIp,
    dstPort: e.dstPort,
    natPort: e.natPort,
    inInterface: e.inInterface,
    outInterface: e.outInterface,
  }
}

// A single condition's outcome. 'unknown' is a first-class answer, not
// an error: it is what keeps a rule in the could-have set.
type Outcome = 'contradicts' | 'consistent' | 'unknown'

export interface NatRuleVerdict {
  rule: RouterNatRule
  // ruledOut is the one clause naming the contradiction ("protocol udp ≠
  // tcp"), or null when nothing the event states contradicts this rule.
  // The popover prefixes it with "ruled out:".
  ruledOut: string | null
  // notEvaluable lists this rule's own conditions the partition could
  // not decide, verbatim enough to look up on the router
  // ("src-address=wan-hosts"). Only populated for rules still in the
  // could-have set -- once a rule is out, what else could not be
  // decided about it no longer changes anything.
  notEvaluable: string[]
}

export interface NatPartition {
  // Both halves stay in the order the table arrived in, which is
  // RouterOS's own ordinal order.
  couldHave: NatRuleVerdict[]
  ruledOut: NatRuleVerdict[]
  total: number
  // discriminable is false when no rule in the table carries a single
  // field the partition could subtract on -- the signature of a push
  // script predating the schema that added them. Nothing can be ruled
  // out, so the popup shows the whole table and says why, rather than
  // presenting "14 of 14 could have performed it" as though it had done
  // some work.
  discriminable: boolean
}

// RouterOS writes "(unknown 0)" into a log line's in:/out: slot when the
// packet had no interface on that side. It is a placeholder, not the
// name of an interface, and reading it as one would rule out every rule
// that names a real interface -- turning a missing fact into a
// confident, wrong subtraction.
function knownInterface(v: string | undefined): string {
  const s = (v ?? '').trim()
  if (!s || s.startsWith('(')) return ''
  return s
}

// splitNegation peels RouterOS's leading "!" off a match value. Negation
// is a plain inversion of the same comparison, so it is handled rather
// than abandoned to 'unknown' -- but only once the inner value itself
// could be evaluated, so "!wan-hosts" stays as unknowable as
// "wan-hosts".
function splitNegation(spec: string): { negated: boolean; value: string } {
  const s = spec.trim()
  if (s.startsWith('!')) return { negated: true, value: s.slice(1).trim() }
  return { negated: false, value: s }
}

// nameMatches compares a rule's single-name match value (protocol, an
// interface) against the event's, case-insensitively: RouterOS logs
// "proto TCP" upper-case while a rule's protocol is configured
// lower-case, and a partition that treated those as a contradiction
// would rule out every rule on every event.
export function nameMatches(spec: string, actual: string): boolean | null {
  const { negated, value } = splitNegation(spec)
  if (!value || !actual) return null
  const hit = value.toLowerCase() === actual.trim().toLowerCase()
  return negated ? !hit : hit
}

// portSpecMatches evaluates RouterOS's port syntax: a single port, a
// comma-separated list, a range, any of those negated. Anything it does
// not recognise is null -- an unrecognised spec must not silently
// evaluate as "no match" and rule the rule out.
export function portSpecMatches(spec: string, port: number | undefined): boolean | null {
  if (port === undefined || !Number.isFinite(port)) return null
  const { negated, value } = splitNegation(spec)
  if (!value) return null

  let hit = false
  let sawPart = false
  for (const raw of value.split(',')) {
    const part = raw.trim()
    if (!part) continue
    // Search from index 1 so a leading "-" is a malformed spec rather
    // than an empty low bound quietly reading as 0.
    const dash = part.indexOf('-', 1)
    if (dash > 0) {
      const lo = part.slice(0, dash).trim()
      const hi = part.slice(dash + 1).trim()
      if (!/^\d+$/.test(lo) || !/^\d+$/.test(hi)) return null
      sawPart = true
      if (port >= Number(lo) && port <= Number(hi)) hit = true
    } else {
      if (!/^\d+$/.test(part)) return null
      sawPart = true
      if (Number(part) === port) hit = true
    }
  }
  if (!sawPart) return null
  return negated ? !hit : hit
}

// addressSpecMatches evaluates a rule's src-address/dst-address against
// the event's address: a literal, a CIDR, or a RouterOS "from-to" range.
// An address-list name, or anything else that is not one of those three,
// is null -- mikroview holds address lists as their own pushed table and
// deliberately does not join them in here, so the honest answer is that
// this cannot be decided.
export function addressSpecMatches(spec: string, actual: string | undefined): boolean | null {
  const { negated, value } = splitNegation(spec)
  if (!value || !actual) return null
  const target = parseAddress(actual)
  if (!target) return null

  let hit: boolean | null = null
  const cidr = parseCidr(value)
  if (cidr) {
    hit = addressInCidr(actual, cidr)
  } else {
    const literal = parseAddress(value)
    if (literal) {
      hit = literal.family === target.family && literal.value === target.value
    } else {
      const dash = value.indexOf('-', 1)
      if (dash > 0) {
        const lo = parseAddress(value.slice(0, dash).trim())
        const hi = parseAddress(value.slice(dash + 1).trim())
        if (lo && hi && lo.family === hi.family && lo.family === target.family) {
          hit = target.value >= lo.value && target.value <= hi.value
        }
      }
    }
  }
  if (hit === null) return null
  return negated ? !hit : hit
}

// isNatChain mirrors internal/routeros/parser.go's isNATChain and
// EventRow.svelte's natFilterKey: only the two dedicated NAT chains
// state which side of the connection a translation rewrote.
function isNatChain(chain: string | undefined): boolean {
  const c = (chain ?? '').toLowerCase()
  return c === 'srcnat' || c === 'dstnat'
}

interface Check {
  // condition is how the rule's own setting reads, used when the check
  // cannot be evaluated ("src-address=wan-hosts").
  condition: string
  outcome: Outcome
  // reason is the single clause naming the contradiction, used only when
  // outcome is 'contradicts'.
  reason: string
}

function check(condition: string, outcome: Outcome, reason: string): Check {
  return { condition, outcome, reason }
}

// fromMatch turns a three-valued comparison into a Check.
function fromMatch(condition: string, matched: boolean | null, reason: string): Check {
  if (matched === null) return check(condition, 'unknown', reason)
  return check(condition, matched ? 'consistent' : 'contradicts', reason)
}

// checksFor builds every condition this rule states, in the order they
// are reported. Order matters only for which single clause is shown when
// a rule is contradicted more than once: the first is the most
// categorical (a disabled rule cannot have done anything at all,
// whatever else it says).
function checksFor(rule: RouterNatRule, ev: NatEventFacts): Check[] {
  const out: Check[] = []

  if (rule.disabled) {
    out.push(check('disabled=yes', 'contradicts', 'disabled'))
  }

  // Chain is only evidence when the event is itself on a NAT chain. A
  // NAT annotation also rides along on ordinary forward-chain lines,
  // reporting a translation some earlier NAT rule performed -- there the
  // event's chain says nothing about which NAT chain that rule sat in,
  // so it must not exclude. Same reasoning as EventRow.svelte's
  // natFilterKey.
  if (rule.chain) {
    if (isNatChain(ev.chain)) {
      const same = rule.chain.toLowerCase() === (ev.chain ?? '').toLowerCase()
      out.push(
        check(
          `chain=${rule.chain}`,
          same ? 'consistent' : 'contradicts',
          `chain ${rule.chain}, event is ${ev.chain}`,
        ),
      )
    } else {
      out.push(
        check(
          `chain=${rule.chain}`,
          'unknown',
          `chain ${rule.chain}, event is ${ev.chain ?? 'unknown'}`,
        ),
      )
    }
  }

  if (rule.protocol) {
    out.push(
      fromMatch(
        `protocol=${rule.protocol}`,
        nameMatches(rule.protocol, ev.protocol ?? ''),
        `protocol ${rule.protocol} ≠ ${(ev.protocol || 'unknown').toLowerCase()}`,
      ),
    )
  }

  if (rule.dstPort) {
    out.push(
      fromMatch(
        `dst-port=${rule.dstPort}`,
        portSpecMatches(rule.dstPort, ev.dstPort),
        `dst-port ${rule.dstPort}, event port ${ev.dstPort}`,
      ),
    )
  }

  // to-ports is the translated port, so it is checked against the port
  // in the log line's NAT annotation rather than against the original.
  if (rule.toPorts) {
    out.push(
      fromMatch(
        `to-ports=${rule.toPorts}`,
        portSpecMatches(rule.toPorts, ev.natPort),
        `to-ports ${rule.toPorts}, translated port ${ev.natPort}`,
      ),
    )
  }

  const evIn = knownInterface(ev.inInterface)
  if (rule.inInterface) {
    out.push(
      fromMatch(
        `in-interface=${rule.inInterface}`,
        nameMatches(rule.inInterface, evIn),
        `in-interface ${rule.inInterface}, event in ${evIn || 'unknown'}`,
      ),
    )
  }

  const evOut = knownInterface(ev.outInterface)
  if (rule.outInterface) {
    out.push(
      fromMatch(
        `out-interface=${rule.outInterface}`,
        nameMatches(rule.outInterface, evOut),
        `out-interface ${rule.outInterface}, event out ${evOut || 'unknown'}`,
      ),
    )
  }

  if (rule.srcAddress) {
    out.push(
      fromMatch(
        `src-address=${rule.srcAddress}`,
        addressSpecMatches(rule.srcAddress, ev.srcIp),
        `src-address ${rule.srcAddress}, event source ${ev.srcIp ?? 'unknown'}`,
      ),
    )
  }

  if (rule.dstAddress) {
    out.push(
      fromMatch(
        `dst-address=${rule.dstAddress}`,
        addressSpecMatches(rule.dstAddress, ev.dstIp),
        `dst-address ${rule.dstAddress}, event destination ${ev.dstIp ?? 'unknown'}`,
      ),
    )
  }

  return out
}

// Every rule field the partition can subtract on. A table where not one
// rule carries any of them is the pre-schema push script layer 3
// detects: chain alone is left, and a popup that ruled rules out on
// chain while announcing it could not narrow would be contradicting
// itself, so the floor is all-or-nothing on this list.
function carriesDiscriminatingFields(rule: RouterNatRule): boolean {
  return !!(
    rule.protocol ||
    rule.dstPort ||
    rule.toPorts ||
    rule.toAddresses ||
    rule.inInterface ||
    rule.outInterface ||
    rule.srcAddress ||
    rule.dstAddress ||
    rule.disabled
  )
}

// partitionNatTable is the whole subtraction, applied to the pushed
// table in the order it arrived (RouterOS's own).
export function partitionNatTable(rules: RouterNatRule[], ev: NatEventFacts): NatPartition {
  const discriminable = rules.some(carriesDiscriminatingFields)
  const couldHave: NatRuleVerdict[] = []
  const ruledOut: NatRuleVerdict[] = []

  for (const rule of rules) {
    const checks = discriminable ? checksFor(rule, ev) : []
    const contradiction = checks.find((c) => c.outcome === 'contradicts')
    if (contradiction) {
      ruledOut.push({ rule, ruledOut: contradiction.reason, notEvaluable: [] })
      continue
    }
    couldHave.push({
      rule,
      ruledOut: null,
      notEvaluable: checks.filter((c) => c.outcome === 'unknown').map((c) => c.condition),
    })
  }

  return { couldHave, ruledOut, total: rules.length, discriminable }
}
