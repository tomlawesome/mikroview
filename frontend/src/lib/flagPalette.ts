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

export function familyOf(type: string): FlagFamily {
  return FLAG_FAMILIES[type as FlagType] ?? custom
}

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
