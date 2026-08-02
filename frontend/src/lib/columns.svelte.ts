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

const DEFAULT_WIDTHS = [104, 150, 92, 88, 190, 190, 74, 160, 220]
const MIN_WIDTH = 56
const STORAGE_KEY = 'mikroview-column-widths'

function loadInitial(): number[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed) && parsed.length === DEFAULT_WIDTHS.length) {
        return parsed.map((w) => Math.max(MIN_WIDTH, Number(w) || 0))
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
  widths = $state<number[]>(loadInitial())

  gridTemplate = $derived(this.widths.map((w) => `${w}px`).join(' '))

  // Cumulative right-edge offset of each column, in px -- used to place
  // resize handles in a single overlay layer rather than nested inside
  // each header cell (nesting them hits a CSS stacking-context trap: a
  // sticky header cell's own z-index scopes its children's z-index, so a
  // handle overlapping the *next* cell can't paint above that cell's
  // content no matter how high its z-index is set).
  offsets = $derived.by(() => {
    const out: number[] = []
    let sum = 0
    for (const w of this.widths) {
      sum += w
      out.push(sum)
    }
    return out
  })

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
