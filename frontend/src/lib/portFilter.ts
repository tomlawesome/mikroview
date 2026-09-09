// SPDX-License-Identifier: AGPL-3.0-only
//
// The port filter's own arithmetic (#1018, round 53), kept out of
// Topography.svelte so it can be read and tested without a DOM: what a
// typed list means, what the pill reads, which half of which rib a door
// belongs on, and the wording of the line the map writes when nothing
// was seen.
//
// The one rule everything here serves: the map dims to the answer and
// removes nothing. A rib the port crossed keeps its verdict colour and
// width; every other rib is one thin grey line. A door is drawn from
// policy and is never counted as traffic.
import type { PortCandidate, PortDoor, PortRib } from './api'

/** The selection the pill carries. `proto` empty means either. */
export interface PortSelection {
  ports: number[]
  proto: '' | 'tcp' | 'udp'
}

/**
 * parsePortList reads the picker's text field: a comma-separated list of
 * ports and ranges, the same grammar RouterOS takes in a dst-port.
 *
 * Returns null for anything it cannot read whole. Refusing the list
 * rather than taking the parseable half is the point: a map filtered to
 * a different set from the one typed would be a wrong answer wearing
 * the operator's own words.
 */
export function parsePortList(text: string): number[] | null {
  const trimmed = text.trim()
  if (trimmed === '') return []
  const out: number[] = []
  const seen = new Set<number>()
  for (const raw of trimmed.split(',')) {
    const part = raw.trim()
    if (part === '') continue
    const dash = part.indexOf('-')
    if (dash > 0) {
      const lo = Number(part.slice(0, dash).trim())
      const hi = Number(part.slice(dash + 1).trim())
      if (!isPort(lo) || !isPort(hi) || hi < lo || hi - lo > MAX_PORTS) return null
      for (let p = lo; p <= hi; p++) {
        if (!seen.has(p)) {
          seen.add(p)
          out.push(p)
        }
      }
      continue
    }
    const n = Number(part)
    if (!isPort(n)) return null
    if (!seen.has(n)) {
      seen.add(n)
      out.push(n)
    }
  }
  if (out.length > MAX_PORTS) return null
  return out.sort((a, b) => a - b)
}

// Mirrors internal/api's maxSelectedPorts. The server refuses past it
// too; this one is here so the field can say no without a round trip.
export const MAX_PORTS = 64

function isPort(n: number): boolean {
  return Number.isInteger(n) && n >= 1 && n <= 65535
}

/**
 * portLabel is what the pill reads -- `445/tcp`. Several ports read as
 * the list, and a contiguous run reads as the range it was typed as, so
 * the pill says what was asked for rather than a count of it.
 *
 * Mirrors internal/api's portLabel exactly; the server sends its own
 * copy back with the answer, and this is what the pill shows before the
 * answer has arrived.
 */
export function portLabel(sel: PortSelection): string {
  const { ports, proto } = sel
  if (ports.length === 0) return ''
  let body: string
  if (ports.length === 1) body = String(ports[0])
  else if (ports[ports.length - 1] - ports[0] === ports.length - 1) body = `${ports[0]}-${ports[ports.length - 1]}`
  else body = ports.join(',')
  return proto ? `${body}/${proto}` : body
}

/**
 * pillSummary is the collapsed pill's own tail: `· 2 lines seen · 3
 * doors`, or `· nothing seen · 1 door` where the window carried none.
 *
 * "nothing seen" rather than "0 lines": zero is a number about traffic,
 * and the thing being said is that there was none.
 */
export function pillSummary(lines: number, doors: number): string {
  const seen = lines === 0 ? 'nothing seen' : `${lines} line${lines === 1 ? '' : 's'} seen`
  return `${seen} · ${doors} door${doors === 1 ? '' : 's'}`
}

/**
 * ribKey is the map's own direction key -- the same `from|to` grammar
 * reality.ts uses, so a lit direction and a drawn one are the same
 * string rather than two conventions that agree until they do not.
 */
export function ribKey(from: string, to: string): string {
  return `${from}|${to}`
}

/**
 * litRibs is every direction the map lights for a port answer.
 *
 * A direction the port was seen on lights. A pair the port *crossed*
 * lights both halves, because a crossing is one line through the
 * router and lighting the half it entered on while greying the half it
 * left on would draw a packet stopping where it did not: "both halves
 * of a crossed rib" (#1018). A wholly refused direction lights alone --
 * it died at the waist, and the far half carried nothing.
 */
export function litRibs(ribs: PortRib[]): Map<string, 'accept' | 'refused'> {
  const out = new Map<string, 'accept' | 'refused'>()
  const light = (key: string, verdict: 'accept' | 'refused') => {
    // Accept wins a tie: colour is the verdict, and a direction that
    // ever passed the port did pass it.
    if (verdict === 'accept' || !out.has(key)) out.set(key, verdict)
  }
  for (const r of ribs) {
    const verdict = r.accepts > 0 ? 'accept' : 'refused'
    if (r.in === '' && r.out === '') continue
    light(ribKey(r.in, r.out), verdict)
    if (r.accepts > 0 && r.in !== '' && r.out !== '') light(ribKey(r.out, r.in), verdict)
  }
  return out
}

/**
 * doorHalf is where a door is drawn: which direction's half of which
 * pair carries the rule's two posts.
 *
 * A rule that refuses stops the packet on the way *in*, so its door
 * sits on the half between the in-interface and the router. A rule that
 * accepts opens the way *out*, so its door sits on the half between the
 * router and the out-interface. Round 53 draws exactly this -- `#12 lan
 * → srv accept` on the Servers half, `#23 wan → any drop` on the WAN
 * half, `#31 guest → srv drop` on the Guest half -- and it is also the
 * honest reading: a door is where the rule acts, not where the pair
 * happens to be labelled.
 *
 * Returns null for a rule naming neither interface. Such a rule guards
 * every boundary, and a door on all of them at once says nothing.
 */
export function doorHalf(door: Pick<PortDoor, 'action' | 'in' | 'out'>): { from: string; to: string } | null {
  const inIface = door.in ?? ''
  const outIface = door.out ?? ''
  if (inIface === '' && outIface === '') return null
  const refuses = door.action !== 'accept'
  // The preferred side, falling back to the other where the rule names
  // only one interface -- which is the common shape of a WAN drop.
  const near = refuses ? inIface || outIface : outIface || inIface
  const far = near === inIface ? outIface : inIface
  return { from: near, to: far }
}

/** doorInk: a leaf that opens is the accept ink, a bar the alarm. */
export function doorAccepts(door: Pick<PortDoor, 'action'>): boolean {
  return door.action === 'accept'
}

/**
 * zoneTally is a lane card's own line under a port filter: `2 of 12
 * hosts on 445/tcp`. Counted over the hosts the card actually draws, so
 * the number and the lit dots are the same claim.
 */
export function zoneTally(onPort: number, total: number, label: string): string {
  return `${onPort} of ${total} host${total === 1 ? '' : 's'} on ${label}`
}

/**
 * emptyNote is the one line the map writes under itself when the window
 * carried nothing on the port -- not an empty state, a sentence.
 *
 * The wording follows the number of rules, because "one rule names it"
 * and "no rule names it" are different findings and the second is the
 * more interesting one: a port nothing carries and nothing names is a
 * port this network has no opinion about.
 */
export function emptyNote(label: string, doors: PortDoor[]): string {
  const head = `no logged traffic on ${label} in the window`
  if (doors.length === 0) return `${head} · no pushed rule names it either`
  if (doors.length === 1) {
    const d = doors[0]
    return `${head} · one rule names it — ${d.label} ${d.who}, ${whereOf(d)}`
  }
  return `${head} · ${doors.length} rules name it — ${doors.map((d) => d.label).join(', ')}, their doors drawn`
}

// whereOf is where a door's own sentence points: the interface the rule
// names, never a zone name we would have to guess at.
//
// A rule naming neither interface guards every boundary at once, so
// there is no rib to put a door on and the map draws none -- the
// sentence says that rather than choosing a side. Round 53 draws doors
// on ribs only and gives no vocabulary for a rule that sits everywhere;
// inventing one mid-build is the thing this repo's build rule forbids,
// so the words carry it and the drawing stays as designed.
function whereOf(d: PortDoor): string {
  const iface = d.in || d.out
  return iface ? `the door on the ${iface} side` : 'on every boundary, so no one door is drawn'
}

/**
 * pickerPorts is one chip per port, busiest first.
 *
 * The answer counts a port per protocol, because that is what the window
 * carried; the picker selects a port and the protocol chip beside it
 * decides which. Two identical `53` chips that light and toggle together
 * would be one control drawn twice, and the one whose title said
 * "5 lines on 53/udp" would filter to 53/tcp when tcp was selected.
 */
export function pickerPorts(candidates: PortCandidate[]): PickerPort[] {
  const byPort = new Map<number, PickerPort>()
  for (const c of candidates) {
    const held = byPort.get(c.port)
    if (held) {
      held.count += c.count
      held.named = held.named || c.named
      if (c.count > 0) held.protos.push(c.proto)
      continue
    }
    byPort.set(c.port, { port: c.port, count: c.count, named: c.named, protos: c.count > 0 ? [c.proto] : [] })
  }
  return [...byPort.values()].sort((a, b) => b.count - a.count || a.port - b.port)
}

export interface PickerPort {
  port: number
  /** Lines on this port in the window, over both protocols. */
  count: number
  named: boolean
  /** The protocols it was actually carried on, for the chip's title. */
  protos: string[]
}

/**
 * placeableDoors are the doors the map can actually put somewhere: a
 * door is two posts across a rib, and a rule naming neither interface
 * has no rib to stand on.
 *
 * The pill counts these rather than every rule in the answer, so the
 * number it gives and the doors on the map are the same claim.
 */
export function placeableDoors(doors: PortDoor[]): PortDoor[] {
  return doors.filter((d) => doorHalf(d) !== null)
}
