export interface ColumnDef {
  key: string
  label: string
}

// Order matches EventRow.svelte's cell order exactly -- both this array
// and EventRow render cells positionally into the same CSS Grid, so the
// two must stay in sync by index.
export const COLUMNS: ColumnDef[] = [
  { key: 'time', label: 'Time' },
  { key: 'device', label: 'Device' },
  { key: 'action', label: 'Action' },
  { key: 'chain', label: 'Chain' },
  { key: 'source', label: 'Source' },
  { key: 'destination', label: 'Destination' },
  { key: 'proto', label: 'Proto' },
  { key: 'iface', label: 'Interfaces' },
  { key: 'rule', label: 'Rule' },
]

// null = flexible (shares remaining width with other flexible columns,
// via `minmax(0, 1fr)`) -- the default for anything whose content length
// varies a lot (addresses, rule labels). A number is a fixed px width,
// used for naturally-bounded content (timestamps, badges, protocol) and
// for any column once the user has actually dragged it, at which point
// it stops flexing and holds the size they chose.
type Width = number | null

const DEFAULT_WIDTHS: Width[] = [104, 150, 92, 88, null, null, 74, 160, null]
const MIN_WIDTH = 56
const STORAGE_KEY = 'mikroview-column-widths-v2'

function loadInitial(): Width[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed) && parsed.length === DEFAULT_WIDTHS.length) {
        return parsed.map((w) => (w === null ? null : Math.max(MIN_WIDTH, Number(w) || 0)))
      }
    }
  } catch {
    // ignore malformed/unavailable storage, fall through to defaults
  }
  return [...DEFAULT_WIDTHS]
}

// Column widths for the live table, user-resizable via drag handles in
// LiveTable.svelte and persisted across sessions. Both the sticky header
// row and the event rows below it live in the same CSS Grid container, so
// a single template string here drives both.
class ColumnState {
  widths = $state<Width[]>(loadInitial())

  gridTemplate = $derived(
    this.widths.map((w) => (w === null ? 'minmax(0, 1fr)' : `${w}px`)).join(' '),
  )

  isDefault = $derived(this.widths.every((w, i) => w === DEFAULT_WIDTHS[i]))

  setWidth(index: number, px: number) {
    const next = [...this.widths]
    next[index] = Math.max(MIN_WIDTH, Math.round(px))
    this.widths = next
    this.persist()
  }

  reset() {
    this.widths = [...DEFAULT_WIDTHS]
    this.persist()
  }

  private persist() {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(this.widths))
    } catch {
      // storage unavailable -- widths just won't persist across reloads
    }
  }
}

export const columnState = new ColumnState()
