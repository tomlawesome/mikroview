// SPDX-License-Identifier: AGPL-3.0-only

import type { Action } from './types'

/**
 * ACTIONS is every action value, in the order the UI presents them:
 * the filter verdicts first, then the non-filter rule kinds, then
 * unknown last.
 *
 * It is also the order EventsChart draws its series in, which is the
 * adjacency app.css's palette note was checked against -- so reordering
 * this reorders the legend and changes which colors sit next to each
 * other.
 */
export const ACTIONS: readonly Action[] = [
  'accept',
  'drop',
  'reject',
  'log',
  'marked',
  'natted',
  'unknown',
] as const

/**
 * ACTION_LABELS is the short display name, for legends and bar lists
 * where the column is narrow and the value is already in context.
 *
 * Typed as Record<Action, string>, so adding an action to the union
 * fails the type check here rather than rendering `undefined` in a
 * legend.
 */
export const ACTION_LABELS: Record<Action, string> = {
  accept: 'Accept',
  drop: 'Drop',
  reject: 'Reject',
  log: 'Log',
  marked: 'Marked',
  natted: 'Natted',
  unknown: 'Unknown',
}

/**
 * ACTION_FILTER_OPTIONS is the action pick-list, with an "any" entry
 * first. Longer labels than ACTION_LABELS for the two non-filter classes:
 * a dropdown is where someone is looking for a thing rather than reading
 * back a thing they already chose, and "marked"/"natted" are mikroview's
 * words while "mangle"/"NAT" are RouterOS's.
 *
 * The value stays the raw action, never the decorated label -- same rule
 * ruleLabel/ruleName follow: display text decorates, it never becomes
 * the key something is filtered or grouped by.
 *
 * Shared rather than declared per component: FilterBar and
 * AddTopTalkerWidget each had their own copy of this list, which is
 * exactly the shape of thing that gets a new entry in one copy and not
 * the other -- and the widget's filters are saved and replayed, so a
 * missing option there is a filter an operator cannot rebuild.
 */
export const ACTION_FILTER_OPTIONS: { value: Action | ''; label: string }[] = [
  { value: '', label: 'Any action' },
  ...ACTIONS.map((value) => ({
    value: value as Action | '',
    label:
      value === 'marked'
        ? 'Marked (mangle)'
        : value === 'natted'
          ? 'Natted (NAT)'
          : ACTION_LABELS[value],
  })),
]
