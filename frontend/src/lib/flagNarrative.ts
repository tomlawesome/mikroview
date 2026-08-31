// SPDX-License-Identifier: AGPL-3.0-only
//
// The drawer's headline and story (#678, round 29's first two ratified
// items): "One source, twenty doors.", "A camera asking for a mail
// server.", "Nine identical attempts, one every couple of minutes --
// machine-regular, not a person retrying." Generated per flag type from
// the evidence the flag already carries (target, count, evidence,
// firstSeen/lastSeen) -- this is writing, not data plumbing, so every
// sentence below states only what that evidence actually shows. Plain,
// specific, never scolding: no "malicious", no exclamation, no
// security-vendor register.
import { formatHM } from './format'
import type { Flag, FlagType } from './types'

const SMALL_NUMBERS = [
  'zero',
  'one',
  'two',
  'three',
  'four',
  'five',
  'six',
  'seven',
  'eight',
  'nine',
  'ten',
  'eleven',
  'twelve',
  'thirteen',
  'fourteen',
  'fifteen',
  'sixteen',
  'seventeen',
  'eighteen',
  'nineteen',
  'twenty',
]

// Twenty reads as "twenty doors" in running prose, matching the
// ratified example; a count past that is exact enough as a numeral.
function spellSmall(n: number): string {
  return Number.isInteger(n) && n >= 0 && n < SMALL_NUMBERS.length ? SMALL_NUMBERS[n] : String(n)
}

function humanizeDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return ''
  if (ms < 60_000) {
    const s = Math.max(1, Math.round(ms / 1000))
    return `${s} second${s === 1 ? '' : 's'}`
  }
  if (ms < 3_600_000) {
    const m = Math.max(1, Math.round(ms / 60_000))
    return `${m} minute${m === 1 ? '' : 's'}`
  }
  if (ms < 86_400_000) {
    const h = Math.max(1, Math.round(ms / 3_600_000))
    return `${h} hour${h === 1 ? '' : 's'}`
  }
  const d = Math.max(1, Math.round(ms / 86_400_000))
  return `${d} day${d === 1 ? '' : 's'}`
}

// " in six minutes" -- a trailing clause, empty when there's nothing
// worth naming (firstSeen === lastSeen, a single-event flag).
function durationClause(ms: number): string {
  const text = humanizeDuration(ms)
  return text ? ` in ${text}` : ''
}

function span(f: Pick<Flag, 'firstSeen' | 'lastSeen'>): number {
  return new Date(f.lastSeen).getTime() - new Date(f.firstSeen).getTime()
}

function listPortsPhrase(ports: number[]): string {
  const unique = [...new Set(ports)].sort((a, b) => a - b)
  if (unique.length === 0) return 'a critical port'
  if (unique.length === 1) return `port ${unique[0]}`
  if (unique.length === 2) return `ports ${unique[0]} and ${unique[1]}`
  return `ports ${unique.slice(0, -1).join(', ')} and ${unique[unique.length - 1]}`
}

function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? '' : 's'}`
}

// Sentence-initial capitalisation for a spelled-out count -- "Nine
// identical attempts", "Three distinct sources" -- the two stories
// below that open a sentence with spellSmall() rather than the target.
function capitalize(s: string): string {
  return s.length === 0 ? s : s.charAt(0).toUpperCase() + s.slice(1)
}

// -- headlines: one plain sentence, standing alone, never repeating the
// row's own target string (the where column already shows it) --------

const HEADLINES: Partial<Record<FlagType, (f: Flag) => string>> = {
  port_scan: (f) => {
    const n = f.evidence?.ports?.length || f.count
    return `One source, ${spellSmall(n)} door${n === 1 ? '' : 's'}.`
  },
  critical_port: () => 'Knocking on doors that matter.',
  activity_spike: () => 'Far louder than its own usual.',
  global_spike: () => 'Busier than usual, across the board.',
  distributed_brute_force: () => 'Many sources, the same lock.',
  outbound_anomaly: () => "Reaching a lot of places it hasn't before.",
  internal_recon: () => 'Checking what else is on the network.',
  rule_spike: () => 'One rule doing a lot more work than usual.',
  repeated_drops: () => 'Same ask, same refusal, still asking.',
  low_slow_scan: () => 'A scan paced to stay under the radar.',
  off_hours_activity: () => 'Awake at a time it has no history of.',
  device_silence: () => 'Gone quiet.',
  new_device: () => 'A device mikroview has never seen before.',
  stale_rule: () => 'A rule that stopped mattering.',
  unexpected_mail_sender: () => "Something that shouldn't send mail, sending mail.",
  known_bad_ip: () => 'A source already known to be bad.',
}

// -- stories: the specifics, in sentences -- what asked, of what, how
// often, and (where the detector guarantees it) what refused it -------

const STORIES: Partial<Record<FlagType, (f: Flag) => string>> = {
  port_scan: (f) => {
    const ports = f.evidence?.ports ?? []
    const desc =
      ports.length >= 2
        ? `${plural(ports.length, 'port')} (${Math.min(...ports)}–${Math.max(...ports)})`
        : ports.length === 1
          ? `port ${ports[0]}`
          : plural(f.count, 'port')
    return `${f.target} touched ${desc}${durationClause(span(f))} -- one source, walking the doors in order.`
  },
  critical_port: (f) => {
    const portsPhrase = listPortsPhrase(f.evidence?.ports ?? [])
    return `${f.target} made ${plural(f.count, 'attempt')} against ${portsPhrase}${durationClause(span(f))} -- ports this deployment has flagged as worth knowing about the moment anything external asks.`
  },
  activity_spike: (f) => {
    const confidence = f.confidence != null ? ` mikroview scores this ${f.confidence}% likely to be a real departure from its own baseline, not noise.` : ''
    return `${f.target} fired ${plural(f.count, 'time')}${durationClause(span(f))}, well above what this host normally does in that stretch.${confidence}`
  },
  global_spike: (f) =>
    `The whole network fired ${plural(f.count, 'time')}${durationClause(span(f))}, well above its usual baseline for the hour. No single source stands out -- the rise is broad, not one actor.`,
  distributed_brute_force: (f) => {
    const hostCount = f.evidence?.hosts?.length || f.count
    return `${capitalize(spellSmall(hostCount))} distinct source${hostCount === 1 ? '' : 's'} tried ${f.target}${durationClause(span(f))}, ${plural(f.count, 'attempt')} between them -- no single one repeating enough to explain the total alone.`
  },
  outbound_anomaly: (f) => {
    const hostCount = f.evidence?.hosts?.length || f.count
    return `${f.target} reached ${spellSmall(hostCount)} distinct destination${hostCount === 1 ? '' : 's'}${durationClause(span(f))}, more than it usually touches -- the shape a device calling out to something new takes.`
  },
  internal_recon: (f) => {
    const hostCount = f.evidence?.hosts?.length || f.count
    return `${f.target} reached ${spellSmall(hostCount)} other host${hostCount === 1 ? '' : 's'} on the network${durationClause(span(f))}, more than a single device usually needs to talk to inside its own lane.`
  },
  rule_spike: (f) =>
    `Rule ${f.target} fired ${plural(f.count, 'time')}${durationClause(span(f))}, well above its own usual rate -- the rule itself hasn't changed, only how often something is hitting it.`,
  repeated_drops: (f) => {
    const cadence = f.count > 1 ? `, one every ${humanizeDuration(span(f) / (f.count - 1))}` : ''
    return `${capitalize(spellSmall(f.count))} identical attempt${f.count === 1 ? '' : 's'}${cadence} -- machine-regular, not a person retrying -- every one refused at the same boundary.`
  },
  low_slow_scan: (f) =>
    `${f.target} has been probing slowly enough to stay under a burst threshold -- ${plural(f.count, 'attempt')} spread over ${humanizeDuration(span(f)) || 'a long stretch'}, still adding up to a scan.`,
  off_hours_activity: (f) => {
    const stretch = humanizeDuration(span(f))
    return `${f.target} logged ${plural(f.count, 'event')}${stretch ? ` over ${stretch}` : ''} during a stretch of the clock it has no established history of using.`
  },
  device_silence: (f) => `${f.target} has gone quiet -- nothing heard from it since ${formatHM(f.lastSeen)}, longer than its usual check-in gap.`,
  new_device: (f) => `${f.target} has never been seen on this network before now -- mikroview's device history has no prior record of it.`,
  stale_rule: (f) => `Rule ${f.target} hasn't fired in a long time -- dead weight, or a hole nobody needs open anymore, either way worth a look.`,
  unexpected_mail_sender: (f) =>
    `${f.target} has no record of ever being tagged a mail sender, and just made ${f.count > 1 ? plural(f.count, 'outbound connection') : 'an outbound connection'} on an SMTP port${durationClause(span(f))}. Quiet devices don't send mail on their own; compromised ones do.`,
  known_bad_ip: (f) => {
    const detail = f.detail ? f.detail.charAt(0).toLowerCase() + f.detail.slice(1) : 'matches a known-bad range'
    return `${f.target} ${detail} -- a locally-cached range from a vetted threat-intel feed, not a judgment based on anything seen here.`
  },
}

// A custom detection's type is its author's own name for it (see
// Flags.svelte's own labelFor) -- there is no template for prose we
// cannot know, so the fallback states plainly what the flag itself
// already carries rather than inventing anything.
function defaultHeadline(f: Flag): string {
  return f.detail || `${f.type} raised for ${f.target}.`
}

function defaultStory(f: Flag): string {
  return `${f.target}: ${f.detail} -- ${plural(f.count, 'occurrence')} since ${formatHM(f.firstSeen)}.`
}

export function headlineFor(f: Flag): string {
  return (HEADLINES[f.type] ?? defaultHeadline)(f)
}

export function storyFor(f: Flag): string {
  return (STORIES[f.type] ?? defaultStory)(f)
}
