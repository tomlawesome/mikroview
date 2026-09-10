// SPDX-License-Identifier: AGPL-3.0-only
//
// The words on the counting line and the says line (#829, settings round
// 8). Everything here is a rendering of engine.DetectionSpec's own closed
// vocabularies -- key mode, counting mode, distinct field, and the
// placeholder set each key mode supplies -- into the sentence the drawer
// reads as.
//
// Kept beside the parse rules rather than inside the component for the
// same reason: the placeholder set is validated server-side
// (engine.ValidateDetectionDetailTemplate), so a set offered here that
// the server does not accept is a sentence refused on save after it has
// been written, and that is worth a test of its own.

// What the counting line's second phrase says. Total counts events;
// distinct counts distinct values of one field, which is the thing the
// noun has to name -- "8 distinct destination addresses", not "8 events".
export function countingPhrase(counting: string, distinctField: string): string {
  if (counting !== 'distinct') return 'matching events'
  return `distinct ${pluralNoun(distinctField)}`
}

// The distinct-countable fields, in the plural, as the columns name them.
// Only the fields the engine will actually count distinctly are here --
// offering one it refuses would be a choice that fails on save.
const DISTINCT_NOUNS: Record<string, string> = {
  sourceAddress: 'source addresses',
  destinationAddress: 'destination addresses',
  sourcePort: 'source ports',
  destinationPort: 'destination ports',
  protocol: 'protocols',
  chain: 'chains',
  ruleLabel: 'rules',
  inInterface: 'in interfaces',
  outInterface: 'out interfaces',
}

export const DISTINCT_FIELDS = Object.keys(DISTINCT_NOUNS)

function pluralNoun(field: string): string {
  return DISTINCT_NOUNS[field] ?? field
}

// What the window is counted per: the engine's key modes in the words an
// operator would use for them. "everything together" rather than
// "global", because the question the phrase answers is "per what?" and
// the honest answer for a global window is that there is no per.
const KEY_PHRASES: Record<string, string> = {
  perSource: 'source address',
  perSourcePort: 'source address and port',
  perTarget: 'destination address',
  perDestinationPort: 'destination port',
  global: 'everything together',
}

export const KEY_MODES = Object.keys(KEY_PHRASES)

export function keyPhrase(key: string): string {
  return KEY_PHRASES[key] ?? key
}

// The placeholders one key mode supplies, mirroring the engine's own
// detectionTemplateTokens: {Count} always, plus whichever key components
// that mode resolves. Offered on the says line and inserted on click, so
// nobody has to know the vocabulary to use it.
const KEY_TOKENS: Record<string, string[]> = {
  perSource: ['SourceAddress'],
  perSourcePort: ['SourceAddress', 'DestinationPort'],
  perTarget: ['DestinationAddress'],
  perDestinationPort: ['DestinationPort'],
  global: [],
}

export function placeholdersFor(key: string): string[] {
  return ['Count', ...(KEY_TOKENS[key] ?? [])].map((t) => `{${t}}`)
}

// One piece of a rendered says line: either literal text, or a
// placeholder the engine will substitute. The stamps are drawn from this
// rather than from a regex in the template, so a token the current key
// mode cannot resolve is still drawn -- as a stamp that is visibly not
// one of the offered ones, rather than as text that looks like prose.
export interface SaysPart {
  text: string
  placeholder: boolean
}

const TOKEN = /\{([A-Za-z][A-Za-z0-9]*)\}/g

export function saysParts(template: string): SaysPart[] {
  const out: SaysPart[] = []
  let last = 0
  for (const m of template.matchAll(TOKEN)) {
    const at = m.index ?? 0
    if (at > last) out.push({ text: template.slice(last, at), placeholder: false })
    out.push({ text: m[0], placeholder: true })
    last = at + m[0].length
  }
  if (last < template.length) out.push({ text: template.slice(last), placeholder: false })
  return out
}

// Go duration strings are what the definitions surface carries windows as
// ("300s", "5m0s"). The counting line shows seconds with an `s` after the
// number, the way the drawing does, so this is the one place the two
// representations meet.
export function windowSeconds(goDuration: string | undefined, fallback: number): number {
  if (!goDuration) return fallback
  const m = /^(?:(\d+)h)?(?:(\d+)m)?(?:([\d.]+)s)?$/.exec(goDuration.trim())
  if (!m) return fallback
  const h = Number(m[1] ?? 0)
  const min = Number(m[2] ?? 0)
  const sec = Number(m[3] ?? 0)
  const total = h * 3600 + min * 60 + sec
  return total > 0 ? Math.round(total) : fallback
}

export function goDuration(seconds: number): string {
  return `${Math.max(1, Math.round(seconds))}s`
}
