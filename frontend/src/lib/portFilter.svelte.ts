// SPDX-License-Identifier: AGPL-3.0-only
//
// The port filter's own state (#1018, round 53): what is selected, what
// the server answered, and the pill's three shapes -- idle (`⌕ port`),
// open as the picker bar, and collapsed onto the answer.
//
// Same split as baseline.svelte.ts next door: portFilter.ts is the pure
// half where the arithmetic and the wording live and where the unit
// tests point, and this half holds the fetched answer and knows how to
// re-read it.
import { fetchPorts, type PortDoor, type PortHost, type PortRib, type PortsResponse } from './api'
import { pickerPorts, pillSummary, placeableDoors, portLabel, type PickerPort, type PortSelection } from './portFilter'

const EMPTY: PortsResponse = {
  generatedAt: 0,
  windowSeconds: 0,
  candidates: [],
  events: 0,
  accepts: 0,
  drops: 0,
  lines: 0,
  ribs: [],
  hosts: [],
  doors: [],
}

class PortFilterState {
  /** Whether the pill is open as the picker bar. */
  open = $state(false)

  /** The ports selected, ascending, and the protocol chip beside them. */
  ports = $state<number[]>([])
  proto = $state<'' | 'tcp' | 'udp'>('tcp')

  /** What the text field currently holds, and whether it can be read. */
  typed = $state('')
  typedBad = $state(false)

  /**
   * The server's answer. Empty until a fetch lands, and empty again if
   * one fails -- the same choice baselineState.refresh makes, and for
   * the same reason: on a transient error the map falls back to drawing
   * nothing filtered rather than filtering to a stale answer nobody
   * asked for.
   */
  answer = $state<PortsResponse>(EMPTY)

  /**
   * The selection the answer in hand is actually about. The map draws
   * from `settled` rather than from `ports` alone, so a click on a chip
   * never redraws the map against the previous answer under a new
   * label. Public because the drawing tests drive this store directly,
   * the same way they drive zonesState and policyState.
   */
  answeredKey = $state<string>('')

  /** True while ports are selected: the map is filtered. */
  active = $derived(this.ports.length > 0)

  /** True once the answer in hand matches what is selected. */
  settled = $derived(this.answeredKey === this.key)

  get key(): string {
    return `${this.ports.join(',')}/${this.proto}`
  }

  get selection(): PortSelection {
    return { ports: this.ports, proto: this.proto }
  }

  /** What the pill reads: `445/tcp`. */
  label = $derived(portLabel({ ports: this.ports, proto: this.proto }))

  /** One chip per port -- see pickerPorts for why not one per pair. */
  candidates = $derived<PickerPort[]>(pickerPorts(this.answer.candidates))
  ribs = $derived<PortRib[]>(this.settled ? this.answer.ribs : [])
  hosts = $derived<PortHost[]>(this.settled ? this.answer.hosts : [])
  /** Every rule that names the port -- what the map's words are built
   * from, including one the drawing cannot place. */
  doors = $derived<PortDoor[]>(this.settled ? this.answer.doors : [])

  /** The subset the map draws. */
  placedDoors = $derived<PortDoor[]>(placeableDoors(this.doors))

  /** The collapsed pill's tail: `2 lines seen · 3 doors`. Counted over
   * the doors the map can actually place, so the number and the drawing
   * are one claim -- see placeableDoors. */
  summary = $derived(pillSummary(this.settled ? this.answer.lines : 0, this.settled ? this.placedDoors.length : 0))

  /** The addresses lit on the map, for the dots and the lane tallies. */
  hostIps = $derived(new Set(this.hosts.map((h) => h.ip)))

  /** True once the answer says the window carried nothing on the port. */
  nothingSeen = $derived(this.active && this.settled && this.answer.events === 0)

  /** Opens the picker, and reads the candidate list if it is not held. */
  async openPicker() {
    this.open = true
    if (this.answer.candidates.length === 0) await this.refresh()
  }

  closePicker() {
    this.open = false
  }

  /** Clears the filter entirely -- the ✕ and Esc both land here. */
  clear() {
    this.open = false
    this.ports = []
    this.typed = ''
    this.typedBad = false
    this.answeredKey = ''
    this.answer = EMPTY
  }

  /**
   * Once there is a selection the pill is its answer, not the picker
   * that made it (#1178; round 53: "Selected, the pill collapses onto
   * the answer"). Leaving the bar open with the chosen chip merely lit
   * left a 34-chip row across the bottom of the map saying nothing
   * about what it had found. Several ports at once are still reachable
   * -- the collapsed pill reopens the bar with its chips as they were
   * -- so the shape says which of the two states the operator is in
   * rather than showing both at once.
   *
   * Emptying the selection leaves the bar open: there is no answer to
   * collapse onto, and the next pick is what the operator came for.
   */
  private settle() {
    if (this.ports.length > 0) this.open = false
  }

  /** Adds or removes one port; the map re-filters as the set changes. */
  async togglePort(port: number) {
    this.ports = this.ports.includes(port)
      ? this.ports.filter((p) => p !== port)
      : [...this.ports, port].sort((a, b) => a - b)
    this.settle()
    await this.refresh()
  }

  /** Selects exactly the ports typed into the field. */
  async setPorts(ports: number[]) {
    this.ports = [...ports].sort((a, b) => a - b)
    this.settle()
    await this.refresh()
  }

  async setProto(proto: '' | 'tcp' | 'udp') {
    this.proto = proto
    await this.refresh()
  }

  async refresh() {
    const asked = this.key
    try {
      const res = await fetchPorts(this.ports, this.proto)
      // A slower earlier request must not land on top of a later
      // answer: the picker fires one of these per click, and the map
      // would otherwise settle on whichever finished last.
      if (asked !== this.key) return
      this.answer = res
      this.answeredKey = asked
    } catch {
      if (asked !== this.key) return
      // Deliberately *not* settled: an unread answer is not an answer of
      // zero. Marking it settled would put "no logged traffic on 445/tcp
      // in the window" and "0 of 12 hosts" on the map when all that
      // happened was a dropped connection -- a positive claim about the
      // network made out of a failed request.
      this.answer = EMPTY
      this.answeredKey = ''
    }
  }
}

export const portFilterState = new PortFilterState()
