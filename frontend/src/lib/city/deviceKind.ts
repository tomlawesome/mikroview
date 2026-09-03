// SPDX-License-Identifier: AGPL-3.0-only
//
// Which shape a building in the city is drawn as (#864).
//
// The ratified record (docs/design/screens/city/DESIGN.md, "The device
// library") sets the rule: "Type comes from what mikroview knows -- the
// entities register's device kind where it has one, otherwise a guess
// from name and traffic shape, shown as the generic puck until better
// is known. A wrong shape is a labelling defect, never a data claim."
//
// Read that last sentence as a constraint on this file, not a
// disclaimer. Nothing here may make a shape carry a fact the data did
// not carry. A camera shape says "something about this host's name or
// the ports it answered on looked like a camera" -- it never says
// mikroview identified a camera, and no other surface may read it that
// way. Mikroview never probes anything (AGENTS.md), so every input
// below arrived on its own: a label an operator typed, a name the
// router pushed, and ports and peers seen in the log stream.
//
// The order, most trustworthy first:
//
//   1. the entities register -- an operator's own `kind:<type>` tag
//   2. this is an interface or tunnel end   -> the gateway post
//   3. this is one of mikroview's own configured routers
//   4. the name, by the table below
//   5. the traffic shape, by the table below
//   6. nothing matched                      -> the puck
//
// Names beat traffic because a name is usually something a person chose
// for the thing itself, while traffic shape is inference over a window
// that may be short. Both are guesses.

import { DEVICE_KINDS, type DeviceKind } from './devices'

/** What a host was seen doing, as far as the event buffer reaches. */
export interface TrafficShape {
  /** How many distinct hosts opened a connection to it in the window. */
  talkedToBy?: number
  /** The ports it was reached ON (destination ports), not the ones it used. */
  servedPorts?: number[]
}

export interface DeviceKindInput {
  /** Its displayed name: entity label, router-pushed name, or DNS. */
  name?: string
  /** Tags on its entities-register record, if it has one. */
  tags?: string[]
  /** True when it is a router mikroview itself has configured or discovered. */
  isRouter?: boolean
  /**
   * True when it is the router the estate hangs off. A second router is
   * drawn with antennas, per the record: "router with antennas (the
   * downstream/secondary router)".
   */
  isPrimaryRouter?: boolean
  /** True when it is an interface or a tunnel end rather than a host. */
  isGateway?: boolean
  /** What it was seen doing. */
  traffic?: TrafficShape
}

/** Where a shape came from, so a caller can say so rather than imply more. */
export type DeviceKindSource = 'register' | 'gateway' | 'router' | 'name' | 'traffic' | 'fallback'

export interface DeviceKindVerdict {
  kind: DeviceKind
  source: DeviceKindSource
  /** Plain-English reason, safe to show: never phrased as a finding. */
  why: string
}

const KIND_SET = new Set<string>(DEVICE_KINDS)

/**
 * Names are compared in two ways. `words` must appear as a whole
 * segment of a hyphen/underscore/dot-separated name -- so `tv` matches
 * `tv-lounge` and `lounge.tv` but not `tvheadend` or `atv`. `parts`
 * match anywhere, and are only used where the string is long enough to
 * be unambiguous on its own (`macbook`, `chromecast`).
 */
interface NameRule {
  kind: DeviceKind
  words?: string[]
  parts?: string[]
  /** For the shapes a plain word list cannot express (model numbers). */
  re?: RegExp
  why: string
}

// First match wins, so the order of this table is the rule.
const NAME_RULES: NameRule[] = [
  {
    // Interface and tunnel names, which RouterOS spells consistently.
    kind: 'post',
    re: /^(ether|sfp|wlan|bridge|vlan|wg|l2tp|ovpn|sstp|pptp|gre|eoip|ipsec)[-\d]*$/,
    why: 'its name is a RouterOS interface or tunnel name',
  },
  {
    kind: 'camera',
    words: ['cam', 'cams', 'camera', 'cameras', 'ipcam', 'cctv'],
    parts: ['camera', 'ipcam', 'cctv'],
    why: 'its name contains a camera word',
  },
  {
    kind: 'server',
    words: ['nas', 'server', 'srv', 'pihole', 'dns', 'nfs', 'smb', 'vm', 'docker', 'unifi', 'plex'],
    parts: ['server', 'pihole', 'synology', 'proxmox', 'truenas', 'freenas', 'jellyfin'],
    why: 'its name contains a server or service word',
  },
  {
    kind: 'phone',
    words: ['phone', 'mobile', 'cell'],
    parts: ['iphone', 'phone', 'pixel', 'galaxy', 'oneplus', 'android', 'xiaomi'],
    why: 'its name contains a phone word',
  },
  {
    kind: 'tv',
    words: ['tv', 'roku', 'chromecast', 'firestick', 'appletv', 'shield'],
    parts: ['chromecast', 'firetv', 'appletv', 'smarttv', 'bravia'],
    why: 'its name contains a TV word',
  },
  {
    kind: 'laptop',
    words: ['laptop', 'macbook', 'notebook', 'thinkpad', 'xps', 'chromebook'],
    parts: ['laptop', 'macbook', 'thinkpad', 'chromebook', 'notebook'],
    why: 'its name contains a laptop word',
  },
  {
    kind: 'workstation',
    words: ['pc', 'desktop', 'workstation', 'imac', 'tower', 'ws'],
    parts: ['desktop', 'workstation', 'imac'],
    why: 'its name contains a desktop word',
  },
  {
    kind: 'switch',
    words: ['switch', 'sw', 'crs', 'css'],
    parts: ['switch'],
    // MikroTik's Cloud Router Switch models, which are switches despite
    // the "router" in the expansion.
    re: /^(crs|css)\d/,
    why: 'its name contains a switch word',
  },
  {
    kind: 'router-ant',
    words: ['ap', 'wap', 'wifi', 'hap', 'cap'],
    re: /^c?hap[-a-z\d]*$/,
    why: 'its name looks like an access point or a second router',
  },
  {
    kind: 'router',
    words: ['router', 'gw', 'gateway', 'edgerouter', 'mikrotik', 'opnsense', 'pfsense'],
    parts: ['router', 'mikrotik', 'edgerouter', 'opnsense', 'pfsense'],
    re: /^(rb|ccr)\d/,
    why: 'its name looks like a router',
  },
]

/**
 * Ports a host answering ON them was probably offering that service.
 * A destination port is the strongest passive hint mikroview holds, and
 * still only a hint: a host answering on 554 is drawn as a camera
 * because that is what streams RTSP, not because anything identified a
 * camera.
 */
const PORT_RULES: { kind: DeviceKind; ports: number[]; why: string }[] = [
  { kind: 'camera', ports: [554, 8554], why: 'it was reached on an RTSP video port' },
  { kind: 'server', ports: [53, 853, 5353], why: 'it was reached on a DNS port' },
  {
    kind: 'server',
    ports: [139, 445, 548, 2049, 3306, 5001, 5432, 8006],
    why: 'it was reached on a file or database port',
  },
]

/** How many distinct hosts asking makes something read as a service. */
const SERVICE_PEERS = 4

/** Lowercase, and reduce every separator to a single hyphen. */
function normalise(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

function matchesName(rule: NameRule, flat: string): boolean {
  if (rule.re?.test(flat)) return true
  if (rule.words?.some((w) => `-${flat}-`.includes(`-${w}-`))) return true
  return rule.parts?.some((p) => flat.includes(p)) ?? false
}

/**
 * The kind an operator recorded themselves, from the entities register's
 * open-ended tags: `kind:camera`, or the bare kind name as a tag. The
 * register has no dedicated device-kind field (its shape is `type`,
 * `key`, `label`, `tags` -- see docs/configuration.md), so a tag is
 * where an operator's own answer can live.
 */
function kindFromTags(tags: readonly string[] | undefined): DeviceKind | null {
  for (const raw of tags ?? []) {
    const tag = raw.trim().toLowerCase()
    const value = tag.startsWith('kind:') || tag.startsWith('device:') ? tag.split(':', 2)[1] : tag
    if (KIND_SET.has(value)) return value as DeviceKind
  }
  return null
}

/**
 * The shape a building is drawn as, and where that shape came from.
 * Falls back to the puck, which is the record's "until better is known"
 * shape and carries no claim at all.
 */
export function deviceKindVerdict(input: DeviceKindInput): DeviceKindVerdict {
  const tagged = kindFromTags(input.tags)
  if (tagged) {
    return { kind: tagged, source: 'register', why: 'an operator tagged it with this kind' }
  }

  if (input.isGateway) {
    return { kind: 'post', source: 'gateway', why: 'it is an interface or a tunnel end' }
  }

  if (input.isRouter) {
    return input.isPrimaryRouter
      ? { kind: 'router', source: 'router', why: 'it is the router this estate hangs off' }
      : { kind: 'router-ant', source: 'router', why: 'it is a second, downstream router' }
  }

  const flat = normalise(input.name ?? '')
  if (flat) {
    for (const rule of NAME_RULES) {
      if (matchesName(rule, flat)) return { kind: rule.kind, source: 'name', why: rule.why }
    }
  }

  const traffic = input.traffic
  if (traffic) {
    const served = new Set(traffic.servedPorts ?? [])
    for (const rule of PORT_RULES) {
      if (rule.ports.some((p) => served.has(p))) {
        return { kind: rule.kind, source: 'traffic', why: rule.why }
      }
    }
    if ((traffic.talkedToBy ?? 0) >= SERVICE_PEERS) {
      return {
        kind: 'server',
        source: 'traffic',
        why: `${traffic.talkedToBy} distinct hosts asked it for something`,
      }
    }
  }

  return { kind: 'puck', source: 'fallback', why: 'nothing mikroview saw suggests a type' }
}

/** The shape alone. See deviceKindVerdict for where it came from. */
export function deviceKindFor(input: DeviceKindInput): DeviceKind {
  return deviceKindVerdict(input).kind
}
