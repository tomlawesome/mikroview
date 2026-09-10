// SPDX-License-Identifier: AGPL-3.0-only
//
// The ratified flag-type palette (#633, visioning rounds 18-19/29):
// each flag type wears one of six family inks as a left stripe and mark
// ink, one unbroken line running row-into-drawer. The six hexes are the
// design record's own data colours -- fixed values, not theme variables,
// because they are the identity of the flag types across themes (#492
// retunes chrome, not the type inks). The mark carries the record's
// severity grammar: ✱ an alarm, ▲ an advisory.
import type { FlagType } from './types'

export interface FlagFamily {
  ink: string
  mark: '✱' | '▲'
}

const hostile: FlagFamily = { ink: '#ff5470', mark: '✱' }
const scan: FlagFamily = { ink: '#ff9e64', mark: '✱' }
const outbound: FlagFamily = { ink: '#f072c8', mark: '✱' }
const repeat: FlagFamily = { ink: '#e0765a', mark: '✱' }
const surge: FlagFamily = { ink: '#e8b05a', mark: '▲' }
const presence: FlagFamily = { ink: '#b8c56a', mark: '▲' }

// Sixteen real detectors into the six ratified families, by what the
// flag is *about*: hostility toward something specific (red), a scan's
// breadth (orange), traffic leaving that shouldn't (pink), the same
// refusal repeating (rust), volume against a baseline (amber), and a
// device appearing or falling silent (olive).
// An operator-authored detection wears the app's own accent rather
// than one of the six family inks: the record's palette classifies the
// sixteen built-ins by what each flag is about, and a custom
// detection's subject is known only to its author. The advisory mark,
// not the alarm -- severity is the author's call, and ▲ is the
// palette's unopinionated grade. Looked up through familyOf below;
// indexing FLAG_FAMILIES directly crashes the render the moment a
// custom detector raises its first flag, and the deck mounts every
// card, so that one flag took down every scene at once.
const custom: FlagFamily = { ink: '#9db8e8', mark: '▲' }

// What their authors filed the operator-authored detectors under (#829),
// keyed by the same string a flag carries as its type -- a custom
// detector's definition id. Empty until something reads the definitions
// list; a flag whose detector is not in it falls through to the accent
// below, exactly as every custom flag did before the family field
// existed.
//
// A module-level register rather than a parameter on familyOf, because
// the ink has to reach a dozen call sites that have a flag and nothing
// else: the docket, the fall, the map, the totals bar and the station's
// own chips all ask familyOf(f.type) and know nothing about definitions.
// Passing the family down each of those paths would be the same fact
// threaded through five components; registering it once lets them pick
// it up unchanged.
const customFamilies = new Map<string, FlagFamily>()

// rememberCustomFamilies replaces the register wholesale from the
// definitions list. Wholesale, not merged, so a detector that was
// deleted or re-filed to nothing stops wearing an ink nothing chose --
// a merge would leave the old answer in place for exactly the case the
// operator changed it.
export function rememberCustomFamilies(byType: Record<string, string | undefined>): void {
  customFamilies.clear()
  for (const [type, name] of Object.entries(byType)) {
    if (!name) continue
    const fam = NAMED_FAMILIES[name]
    if (fam) customFamilies.set(type, fam)
  }
}

// The seven the operator may choose between, in the order the picker
// draws them (round 7/8 of #490): the six the record assigns to the
// built-ins, worst first, then the operator-authored accent.
export const NAMED_FAMILIES: Record<string, FlagFamily> = {
  hostile,
  scan,
  outbound,
  repeat,
  surge,
  presence,
  custom,
}

// The picker's own order, and the names the API stores -- listed once
// here so the editor and the register cannot come to disagree about
// which seven there are.
export const FAMILY_NAMES: readonly string[] = [
  'hostile',
  'scan',
  'outbound',
  'repeat',
  'surge',
  'presence',
  'custom',
]

export function familyOf(type: string): FlagFamily {
  return FLAG_FAMILIES[type as FlagType] ?? customFamilies.get(type) ?? custom
}

// The families worst-first, as the record lists them: alarms before
// advisories, and within each the palette's own order. A campaign row
// (#988, round 47) wears the worst ink among its flags, so a port scan
// beside a critical-port hit reads as the hit.
const SEVERITY: readonly FlagFamily[] = [hostile, scan, outbound, repeat, surge, presence, custom]

export function worstFamilyOf(types: readonly string[]): FlagFamily {
  let worst = custom
  let rank = SEVERITY.length
  for (const t of types) {
    const r = SEVERITY.indexOf(familyOf(t))
    if (r < rank) {
      rank = r
      worst = SEVERITY[r]
    }
  }
  return worst
}

// The health dial's generic advisory-severity ink (#648): the ring
// splits by mark (✱ alarm / ▲ advisory), not by family, and the record
// says reuse the ratified inks rather than mint a new one -- this is
// surge's own hex, the family that already owns the ▲ mark most often.
export const ADVISORY_INK = surge.ink

export const FLAG_FAMILIES: Record<FlagType, FlagFamily> = {
  critical_port: hostile,
  known_bad_ip: hostile,
  distributed_brute_force: hostile,
  port_scan: scan,
  low_slow_scan: scan,
  internal_recon: scan,
  outbound_anomaly: outbound,
  unexpected_mail_sender: outbound,
  repeated_drops: repeat,
  activity_spike: surge,
  global_spike: surge,
  rule_spike: surge,
  off_hours_activity: surge,
  stale_rule: surge,
  new_device: presence,
  device_silence: presence,
}
