// SPDX-License-Identifier: AGPL-3.0-only
//
// The metrics page's one preference and its one cursor (#488,
// docs/design/screens/metrics/DESIGN.md).
//
// The record: "The choice is a per-user preference, persisted and
// applied before first paint, never changed by the app on its own --
// the same grammar as the rail's density states (#486)." That is
// literally lib/rail.svelte.ts's shape, and this deliberately copies
// it: read synchronously at module load so the page never paints one
// view and jumps to another, written only when the operator picks, and
// never written by anything else.
//
// The cursor's minute rides alongside but is *not* persisted -- "the
// cursor's selected minute survives every view switch" is a claim about
// this session, and an hour-old minute restored from storage on a
// reload would point at a minute that has since aged off the axis.

export type MetricsView = 'seismograph' | 'register' | 'table'

const STORAGE_KEY = 'mikroview-metrics-view'

// Seismograph is the default, per the owner's ratified verdict ("I
// think seismography wins overall, and should be the default").
const DEFAULT_VIEW: MetricsView = 'seismograph'

export const METRICS_VIEWS: { value: MetricsView; label: string; title: string }[] = [
  {
    value: 'seismograph',
    label: 'Seismograph',
    title: 'Horizon strips on one shared time axis, the brink at the right',
  },
  { value: 'register', label: 'Register', title: 'Vertical ribbons on shared minute-rows, the brink at the top' },
  { value: 'table', label: 'Table', title: 'The same hour as sortable, copyable figures' },
]

function isView(v: unknown): v is MetricsView {
  return v === 'seismograph' || v === 'register' || v === 'table'
}

function loadInitial(): MetricsView {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return isView(raw) ? raw : DEFAULT_VIEW
  } catch {
    // storage unavailable (private browsing, etc.) -- metrics still
    // works, it just forgets the choice between sessions
    return DEFAULT_VIEW
  }
}

export function viewLabel(v: MetricsView): string {
  return METRICS_VIEWS.find((o) => o.value === v)?.label ?? v
}

const initial = loadInitial()

class MetricsPref {
  view = $state<MetricsView>(initial)

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

  /** The header's view switch. The only thing that writes the preference. */
  setView(next: MetricsView) {
    if (this.view === next) return
    this.view = next
    try {
      localStorage.setItem(STORAGE_KEY, next)
    } catch {
      // as above -- an unwritable store costs the memory of the choice,
      // nothing else
    }
    this.announcement = `Metrics — ${viewLabel(next)}`
  }

  /** Move (or clear) the cursor. Deliberately does not persist. */
  select(iso: string | null) {
    this.minute = iso
  }
}

export const metricsPref = new MetricsPref()
