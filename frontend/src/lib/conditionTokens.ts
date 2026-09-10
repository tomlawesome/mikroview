// SPDX-License-Identifier: AGPL-3.0-only
//
// The grammar behind the conditions bar (#829, settings round 8).
//
// The bar is GitLab's filter bar with the decoration taken off: one quiet
// pill per condition, reading field, verb, value. This module is
// everything about that pill that is not drawing -- what the fields are
// called, which verbs each one offers, and how a typed value turns into
// an engine.Condition.
//
// It is deliberately separate from the component. The value-shape rule
// below is the part of the editor most likely to be wrong in a way
// nothing notices (an address list quietly stored as an equals against a
// comma-joined string matches nothing, forever), so it is a plain
// function over strings that a test can drive without a browser.
//
// Two vocabularies are closed here, and both are the engine's:
// internal/engine/conditions.go's Field and Operator sets, and its
// field x operator table. Neither is re-derived -- a field this file
// offered that the engine did not know would be a condition the server
// refuses on save, after the operator had finished writing it.

import type { DefinitionCondition } from './types'

// A verb as the bar shows it. Round 8: the verbs drop their `is` where
// the word stands alone -- `within`, `between`, `one of`, `classed as` --
// while `is` and `is not` keep it, because those two words are the verb.
export interface Verb {
  operator: string
  label: string
  // What this verb takes, in the dim words the list shows after it.
  hint: string
}

const IS: Verb = { operator: 'equals', label: 'is', hint: 'one value' }
const IS_NOT: Verb = { operator: 'notEquals', label: 'is not', hint: 'anything but one value' }
const ONE_OF: Verb = { operator: 'inSet', label: 'one of', hint: 'a comma-separated list' }
const NOT_ONE_OF: Verb = { operator: 'notInSet', label: 'not one of', hint: 'none of a list' }
const WITHIN: Verb = { operator: 'inCIDR', label: 'within', hint: 'a range like 10.0.70.0/24' }
const BETWEEN: Verb = { operator: 'inRange', label: 'between', hint: 'a low and a high, 1-1024' }
const CLASSED_AS: Verb = {
  operator: 'matchesClassification',
  label: 'classed as',
  hint: 'internal, external or any',
}

// The verbs each family of field offers, straight off the engine's own
// fieldOperators table. A verb offered here that the engine does not
// accept for that field is a condition the server refuses on save, after
// the operator has finished writing it.
const ADDRESS_VERBS = [WITHIN, CLASSED_AS, IS, IS_NOT, ONE_OF, NOT_ONE_OF, BETWEEN]
const PORT_VERBS = [IS, BETWEEN, ONE_OF, IS_NOT, NOT_ONE_OF]
const SET_VERBS = [IS, IS_NOT, ONE_OF, NOT_ONE_OF]
const MEMBERSHIP_VERBS = [IS, IS_NOT]
const TIME_VERBS = [BETWEEN]

// The ink a value wears. Round 7: an address is the stream's own address
// ink, a port is the accent, `internal` is the accept green, a time of
// day is amber, interfaces and chains are muted. Every one is a variable
// the app already defines -- no colour is minted here, which is what
// keeps the dataviz validator with nothing new to check.
export type ValueInk = string

export interface FieldSpec {
  // The engine's own field name -- what goes on the wire.
  field: string
  // The columns' short noun, which is what the bar shows (round 8).
  label: string
  // The dim phrase after the label in the field list, saying what the
  // field takes.
  hint: string
  verbs: Verb[]
  // The ink this field's values wear, unless the value itself asks for
  // another one (a classification, an action).
  ink: ValueInk
  // What else an operator might type to find this field. Typing `dst`
  // has to reach both `destination` and `port`, which neither of those
  // words contains.
  aliases: string[]
  // Values worth offering before anything is typed -- the second column
  // in the list is the dim word for what each one is.
  suggestions?: { value: string; hint: string }[]
}

// The fifteen fields, one per engine.Field, named as the columns name
// them (round 8's build list). The order is the order the list offers
// them: what an operator reaches for first, not alphabetical.
export const FIELDS: FieldSpec[] = [
  {
    field: 'sourceAddress',
    label: 'source',
    hint: 'an address, a range, or internal / external',
    verbs: ADDRESS_VERBS,
    ink: 'var(--fg)',
    aliases: ['src', 'source address', 'from', 'ip'],
    suggestions: [{ value: 'internal', hint: 'this network' }, { value: 'external', hint: 'off this network' }],
  },
  {
    field: 'destinationAddress',
    label: 'destination',
    hint: 'an address, a range, or internal / external',
    verbs: ADDRESS_VERBS,
    ink: 'var(--fg)',
    aliases: ['dst', 'dest', 'destination address', 'to', 'ip'],
    suggestions: [{ value: 'internal', hint: 'this network' }, { value: 'external', hint: 'off this network' }],
  },
  {
    field: 'destinationPort',
    label: 'port',
    hint: 'one port, a list, or a range',
    verbs: PORT_VERBS,
    ink: 'var(--accent)',
    aliases: ['dst port', 'dstport', 'destination port', 'dport'],
    suggestions: [
      { value: '1-1024', hint: 'the well-known ports' },
      { value: '1-65535', hint: 'every port' },
      { value: '22', hint: 'ssh' },
      { value: '3389', hint: 'rdp' },
    ],
  },
  {
    field: 'sourcePort',
    label: 'src port',
    hint: 'one port, a list, or a range',
    verbs: PORT_VERBS,
    ink: 'var(--accent)',
    aliases: ['source port', 'sport'],
  },
  {
    field: 'action',
    label: 'action',
    hint: 'what the router did with it',
    verbs: SET_VERBS,
    ink: 'var(--fg)',
    aliases: ['verdict', 'drop', 'accept', 'reject'],
    suggestions: [
      { value: 'drop', hint: 'dropped' },
      { value: 'accept', hint: 'let through' },
      { value: 'reject', hint: 'refused' },
      { value: 'log', hint: 'logged only' },
    ],
  },
  {
    field: 'chain',
    label: 'chain',
    hint: 'the firewall chain it hit',
    verbs: SET_VERBS,
    ink: 'var(--fg-muted)',
    aliases: ['forward', 'input', 'output'],
    suggestions: [
      { value: 'input', hint: 'to the router' },
      { value: 'forward', hint: 'through it' },
      { value: 'output', hint: 'from it' },
    ],
  },
  {
    field: 'protocol',
    label: 'proto',
    hint: 'tcp, udp, icmp',
    verbs: SET_VERBS,
    ink: 'var(--fg)',
    aliases: ['protocol', 'tcp', 'udp'],
    suggestions: [
      { value: 'tcp', hint: 'tcp' },
      { value: 'udp', hint: 'udp' },
      { value: 'icmp', hint: 'icmp' },
    ],
  },
  {
    field: 'connectionState',
    label: 'state',
    hint: 'new, established, related',
    verbs: SET_VERBS,
    ink: 'var(--fg)',
    aliases: ['connection state', 'conn'],
    suggestions: [
      { value: 'new', hint: 'a fresh connection' },
      { value: 'established', hint: 'one already up' },
      { value: 'related', hint: 'part of another' },
      { value: 'invalid', hint: 'no known connection' },
    ],
  },
  {
    field: 'inInterface',
    label: 'in iface',
    hint: 'the interface it arrived on',
    verbs: SET_VERBS,
    ink: 'var(--fg-muted)',
    aliases: ['in interface', 'ingress', 'iface'],
  },
  {
    field: 'outInterface',
    label: 'out iface',
    hint: 'the interface it left by',
    verbs: SET_VERBS,
    ink: 'var(--fg-muted)',
    aliases: ['out interface', 'egress', 'iface'],
  },
  {
    field: 'timeOfDay',
    label: 'time',
    hint: 'a span of the clock, 22:00-06:00',
    verbs: TIME_VERBS,
    ink: 'var(--now)',
    aliases: ['time of day', 'hour', 'clock'],
    suggestions: [
      { value: '22:00-06:00', hint: 'overnight' },
      { value: '09:00-17:00', hint: 'working hours' },
    ],
  },
  {
    field: 'dayOfWeek',
    label: 'day',
    hint: 'a day, or a list of them',
    verbs: SET_VERBS,
    ink: 'var(--fg)',
    aliases: ['day of week', 'weekend', 'weekday'],
    suggestions: [
      { value: 'saturday, sunday', hint: 'the weekend' },
      { value: 'monday, tuesday, wednesday, thursday, friday', hint: 'the working week' },
    ],
  },
  {
    field: 'ruleLabel',
    label: 'rule',
    hint: 'the label on the firewall rule',
    verbs: SET_VERBS,
    ink: 'var(--fg-muted)',
    aliases: ['rule label', 'comment'],
  },
  {
    field: 'addressListMembership',
    label: 'list',
    hint: 'a router and one of its address lists',
    verbs: MEMBERSHIP_VERBS,
    ink: 'var(--fg)',
    aliases: ['address list', 'membership'],
  },
  {
    field: 'sourceIdentity',
    label: 'identity',
    hint: 'one device, by MAC and address',
    verbs: MEMBERSHIP_VERBS,
    ink: 'var(--fg)',
    aliases: ['source identity', 'device', 'mac'],
  },
]

const BY_FIELD = new Map(FIELDS.map((f) => [f.field, f]))

export function fieldSpec(field: string): FieldSpec | undefined {
  return BY_FIELD.get(field)
}

// The engine's own vocabulary for a classified address (store.Scope's
// three words). Named here because the value-shape rule below needs to
// recognise them as words rather than as one-item lists.
const CLASSIFICATIONS = new Set(['internal', 'external', 'any'])

// The two fields whose value is a pair rather than a list: an address
// list is [router, list] and an identity is [mac, address]. Both are
// comma-separated on screen and neither is a set, so the shape rule has
// to stop treating a comma as "one of".
//
// The two pairs disagree on empty sides, though (#1076). An address list
// with no router or no list names nothing -- both stay required. An
// identity's engine side (matchlog.Identity.Empty, via
// compileSourceIdentityCondition) only refuses a pair with *neither* a mac
// nor an address; a mac with no address, or an address with no mac, is
// still a device worth naming. sourceIdentity is handled on its own below,
// so this set is left with the pair that really does need both sides.
const PAIR_FIELDS = new Set(['addressListMembership'])

// A range's separator on screen is an en dash (round 8 draws `1 – 1024`)
// but nobody types one, so both it and the hyphen are accepted. A time of
// day uses the same grammar -- `22:00-06:00` -- which is why this is one
// rule and not two.
const RANGE = /^\s*(\S.*?)\s*[-–]\s*(\S.*?)\s*$/

export interface ParsedValue {
  operator: string
  values: string[]
}

// parseValue turns what an operator typed into an engine condition's
// operator and values, by the shape of the text alone.
//
// The shape picks the operator, rather than the operator being chosen
// first and the text validated against it (#829). Someone writing a
// condition is thinking about the thing they want to match, not about
// which of seven comparisons the engine calls it: typing 10.0.70.0/24
// means "in that range" whatever it is called, and a bar that made them
// say so twice would be the fiddly interaction round 4-6 were rejected
// for.
//
// The order matters and is not arbitrary. A CIDR contains no comma and no
// dash, so it is tested first and cannot be mistaken for a list. A range
// is tested before a list because `1-1024` contains neither a comma nor a
// classification word. A classification word is tested before a bare
// value so `internal` reaches matchesClassification rather than being
// stored as an address literally spelled "internal", which would match
// nothing and look right.
//
// A `not` prefix asks for the negative form, and only two comparisons
// have one -- the engine has no notInCIDR and no notInRange. Rather than
// silently dropping the `not`, which would store the opposite of what was
// asked for, it is refused with the reason.
export function parseValue(field: string, typed: string): ParsedValue | { error: string } {
  const spec = BY_FIELD.get(field)
  if (!spec) return { error: `there is no ${field} to match on` }
  const allowed = new Set(spec.verbs.map((v) => v.operator))

  let text = typed.trim()
  let negated = false
  const notPrefix = /^not\s+/i
  if (notPrefix.test(text)) {
    negated = true
    text = text.replace(notPrefix, '').trim()
  }
  if (text === '') return { error: 'nothing to match on yet' }

  const wanted = (op: string): ParsedValue | { error: string } => {
    if (!negated) {
      return allowed.has(op) ? { operator: op, values: [] } : { error: refuse(spec, op) }
    }
    const negative = NEGATIVE_OF[op]
    if (!negative) return { error: `there is no "not" for a ${verbLabel(op)} line -- take it out, or write it as a list` }
    return allowed.has(negative) ? { operator: negative, values: [] } : { error: refuse(spec, negative) }
  }

  // sourceIdentity is a pair like the others, but splitList's usual
  // empty-dropping would turn `mac,` into a single part and reject it --
  // the one shape the engine explicitly allows. Split on the comma without
  // dropping empties instead, so a blank side survives as "", and only
  // refuse where the engine itself would: not exactly two parts, or both
  // of them blank.
  if (field === 'sourceIdentity') {
    const raw = text.split(',')
    const bothEmpty = raw.length === 2 && raw.every((p) => p.trim() === '')
    if (raw.length !== 2 || bothEmpty) {
      return { error: 'identity takes mac,ip -- either side may be left empty, not both' }
    }
    const parts = raw.map((p) => p.trim())
    const op = wanted('equals')
    return 'error' in op ? op : { operator: op.operator, values: parts }
  }

  // A pair is two operands, not a set, so it never becomes inSet however
  // many commas it has.
  if (PAIR_FIELDS.has(field)) {
    const parts = splitList(text)
    if (parts.length !== 2) {
      return { error: `${spec.label} takes two parts separated by a comma -- ${spec.hint}` }
    }
    const op = wanted('equals')
    return 'error' in op ? op : { operator: op.operator, values: parts }
  }

  if (text.includes('/')) {
    const op = wanted('inCIDR')
    return 'error' in op ? op : { operator: op.operator, values: splitList(text) }
  }

  const range = RANGE.exec(text)
  if (range && !text.includes(',')) {
    const op = wanted('inRange')
    return 'error' in op ? op : { operator: op.operator, values: [range[1], range[2]] }
  }

  if (text.includes(',')) {
    const op = wanted('inSet')
    const parts = splitList(text)
    if (parts.length === 0) return { error: 'nothing to match on yet' }
    return 'error' in op ? op : { operator: op.operator, values: parts }
  }

  if (CLASSIFICATIONS.has(text.toLowerCase())) {
    const op = wanted('matchesClassification')
    if (!('error' in op)) return { operator: op.operator, values: [text.toLowerCase()] }
    // A field that cannot be classified takes the word as an ordinary
    // value rather than refusing it -- "any" is a legitimate interface
    // name somewhere.
  }

  const op = wanted('equals')
  return 'error' in op ? op : { operator: op.operator, values: [text] }
}

const NEGATIVE_OF: Record<string, string> = {
  equals: 'notEquals',
  inSet: 'notInSet',
}

function refuse(spec: FieldSpec, operator: string): string {
  return `a ${spec.label} line cannot be ${verbLabel(operator)} -- it takes ${spec.hint}`
}

function splitList(text: string): string[] {
  return text
    .split(',')
    .map((p) => p.trim())
    .filter((p) => p.length > 0)
}

const VERB_LABELS: Record<string, string> = {
  equals: 'is',
  notEquals: 'is not',
  inSet: 'one of',
  notInSet: 'not one of',
  inCIDR: 'within',
  inRange: 'between',
  matchesClassification: 'classed as',
}

export function verbLabel(operator: string): string {
  return VERB_LABELS[operator] ?? operator
}

// valueText is how a stored condition's operands read in the pill. A
// range is drawn with the en dash the design uses; everything else is a
// comma list, which is also what an operator would have typed to make it,
// so a saved token can be read straight back into the bar.
export function valueText(c: DefinitionCondition): string {
  if (c.operator === 'inRange') return `${c.values[0] ?? ''} – ${c.values[1] ?? ''}`
  // An identity with an empty side has to read back as `mac,` -- the space
  // the other pairs and lists use would still parse, but only because
  // parseValue trims it away again, and `mac,` is what #1076's ruling
  // shows on the pill.
  if (c.field === 'sourceIdentity') return c.values.join(',')
  return c.values.join(', ')
}

// editText is valueText's other direction: what goes back into the input
// when a token is picked up to be changed. The en dash goes back to a
// hyphen, because that is what the shape rule and the keyboard both
// expect.
export function editText(c: DefinitionCondition): string {
  if (c.operator === 'inRange') return `${c.values[0] ?? ''}-${c.values[1] ?? ''}`
  // Same reasoning as valueText: an identity's empty side has to come back
  // as `mac,`, not `mac, `, so it parses to the same pair it started as.
  if (c.field === 'sourceIdentity') return c.values.join(',')
  return c.values.join(', ')
}

// The action inks, ActionBadge.svelte's own (#829 uses the ink, not the
// badge -- round 8 took the boxes off). An action the app has no ink for
// falls through to the field's own.
const ACTION_INKS: Record<string, string> = {
  drop: 'var(--drop)',
  accept: 'var(--accept)',
  reject: 'var(--reject)',
  log: 'var(--log)',
}

// inkFor is the ink one token's value wears. The field decides it, except
// where the value itself is something the app already colours everywhere
// else: a DROP is the drop amber wherever it appears, and a classified
// address is the accept green.
export function inkFor(c: DefinitionCondition): ValueInk {
  const spec = BY_FIELD.get(c.field)
  if (c.operator === 'matchesClassification') return 'var(--accept)'
  if (c.field === 'action') {
    const ink = ACTION_INKS[(c.values[0] ?? '').toLowerCase()]
    if (ink) return ink
  }
  return spec?.ink ?? 'var(--fg)'
}

// narrowFields is the typing-narrows rule: fourteen fields become two.
// Matched against the label and against the words someone might reach for
// instead, because `dst` has to find both `destination` and `port` and
// neither of those words contains it.
export function narrowFields(typed: string): FieldSpec[] {
  const q = typed.trim().toLowerCase()
  if (q === '') return FIELDS
  return FIELDS.filter(
    (f) => f.label.toLowerCase().includes(q) || f.aliases.some((a) => a.toLowerCase().includes(q)),
  )
}

// narrowValues is the same rule one step along: what the list offers once
// a field is chosen. With nothing typed it offers the field's verbs, so
// the grammar is visible before anything is committed to; once anything is
// typed it offers values, because by then the shape of what is being
// typed has already picked the verb.
export interface ListRow {
  value: string
  hint: string
}

export function narrowValues(spec: FieldSpec, typed: string): ListRow[] {
  const q = typed.trim().toLowerCase()
  if (q === '') return spec.verbs.map((v) => ({ value: v.label, hint: v.hint }))
  const rows = spec.suggestions ?? []
  return rows.filter((r) => r.value.toLowerCase().includes(q) || r.hint.toLowerCase().includes(q))
}

// conditionFrom is the whole parse: a field and what was typed into one
// condition ready for the wire, or the reason it is not one yet. The
// reason is what the bar's amber line says, so it names the line to
// finish rather than describing a rule.
//
// pinned is a verb the operator picked off the list before typing. It
// only ever wins over a bare single value, which is the one shape that
// says nothing about which comparison was meant: someone who chose
// `one of` and then typed one port meant a list of one, not an equals.
// Every other shape is decisive and overrides the pin, because the text
// in front of them is what they last said.
export function conditionFrom(
  field: string,
  typed: string,
  pinned?: string,
): DefinitionCondition | { error: string } {
  const parsed = parseValue(field, typed)
  if ('error' in parsed) return parsed
  if (pinned && parsed.operator === 'equals' && pinned !== 'equals') {
    const spec = BY_FIELD.get(field)
    if (spec?.verbs.some((v) => v.operator === pinned)) {
      return { field, operator: pinned, values: parsed.values }
    }
  }
  return { field, operator: parsed.operator, values: parsed.values }
}

// verbFor is the verb the unfinished token shows while it is being
// written -- what conditionFrom would settle on, without needing the
// value to be finished for the operator to see which line they are on.
export function verbFor(field: string, typed: string, pinned?: string): string {
  const spec = BY_FIELD.get(field)
  if (!spec) return ''
  // A trailing separator is a shape in progress, and the token is meant
  // to say which line you are on before the line is finished: the drawing
  // shows `port between 1-` while the high end is still being typed. Read
  // off the separator rather than off a parse, because a half-written
  // range does not parse and never should.
  const half = typed.trimEnd()
  if (/[-–]$/.test(half) && spec.verbs.some((v) => v.operator === 'inRange')) return BETWEEN.label
  if (/,$/.test(half) && spec.verbs.some((v) => v.operator === 'inSet')) return ONE_OF.label
  const built = conditionFrom(field, typed, pinned)
  if (!('error' in built)) return verbLabel(built.operator)
  if (pinned) return verbLabel(pinned)
  return spec.verbs[0].label
}
