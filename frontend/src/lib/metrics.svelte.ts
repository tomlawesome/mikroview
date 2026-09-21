// SPDX-License-Identifier: AGPL-3.0-only
//
// The metrics page's one preference and its one cursor (#488,
// docs/design/screens/metrics/DESIGN.md).
//
// The record: "The choice is a per-user preference, persisted and
// applied before first paint, never changed by the app on its own --
// the same grammar as the rail's density states (#486)." That is
// literally lib/rail.svelte.ts's shape, and this deliberately copied
// it: read synchronously at module load so the page never paints one
// view and jumps to another, written only when the operator picks, and
// never written by anything else.
//
// #1283 moves the write off localStorage onto the shared per-user
// record, fetched from the server after sign-in -- which means the
// "before first paint" half of that promise no longer holds exactly:
// the view starts at DEFAULT_VIEW and is corrected to the operator's
// saved choice once ensureLoaded() resolves, same as every other module
// in this batch. A signed-out visitor, or one whose fetch hasn't landed
// yet, still gets a sensible default rather than nothing.
//
// The cursor's minute rides alongside but is *not* persisted -- "the
// cursor's selected minute survives every view switch" is a claim about
// this session, and an hour-old minute restored from storage on a
// reload would point at a minute that has since aged off the axis.

import { preferencesState } from './preferences.svelte'

export type MetricsView = 'seismograph' | 'register' | 'table'

// #1283: was its own localStorage key ('mikroview-metrics-view').
const PREFS_KEY = 'metrics'

// Seismograph is the default, per the owner's ratified verdict ("I
// think seismography wins overall, and should be the default").
const DEFAULT_VIEW: MetricsView = 'seismograph'

// Lowercase, matching round 30's own switcher (`#mviews`) and the
// lowercase-sentence style the rest of its scene bar and hourline use
// throughout (docs/design/concepts/round-30/the-whole.html #s4).
export const METRICS_VIEWS: { value: MetricsView; label: string; title: string }[] = [
  {
    value: 'seismograph',
    label: 'seismograph',
    title: 'Horizon strips on one shared time axis, the brink at the right',
  },
  { value: 'register', label: 'register', title: 'Vertical ribbons on shared minute-rows, the brink at the top' },
  { value: 'table', label: 'table', title: 'The same hour as sortable, copyable figures' },
]

function isView(v: unknown): v is MetricsView {
  return v === 'seismograph' || v === 'register' || v === 'table'
}

export function viewLabel(v: MetricsView): string {
  return METRICS_VIEWS.find((o) => o.value === v)?.label ?? v
}

class MetricsPref {
  view = $state<MetricsView>(DEFAULT_VIEW)

  // The selected minute, as its own ISO time rather than an axis index:
  // the axis slides every minute as new data arrives, so an index would
  // quietly drift onto a different minute while the operator was
  // reading it. Null means no minute is selected.
  minute = $state<string | null>(null)

  // Read by a live region on the page: the view switch is a change to
  // the whole surface, so it is spoken. The cursor's own moves are not
  // announced here -- they ride the slider's aria-valuetext (see
  // Metrics.svelte), which is the mechanism for a value that changes,
  // and doubling the two would talk over the page.
  announcement = $state('')

  constructor() {
    preferencesState.register(PREFS_KEY, (value) => {
      this.view = isView(value) ? value : DEFAULT_VIEW
    })
  }

  /** The header's view switch. The only thing that writes the preference. */
  setView(next: MetricsView) {
    if (this.view === next) return
    this.view = next
    preferencesState.set(PREFS_KEY, next)
    this.announcement = `Metrics — ${viewLabel(next)}`
  }

  /** Move (or clear) the cursor. Deliberately does not persist. */
  select(iso: string | null) {
    this.minute = iso
  }
}

export const metricsPref = new MetricsPref()
