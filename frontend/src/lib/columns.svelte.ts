// SPDX-License-Identifier: AGPL-3.0-only

export interface ColumnDef {
  key: string
  label: string
}

// Order matches EventRow.svelte's cell order exactly -- both this array
// and EventRow render cells positionally into the same CSS Grid, so the
// two must stay in sync by index.
// The ratified #644 column set ("the stream, columns squared", round-29
// scene 4): the same kind of fact always under the eye. Source and
// Destination each show the resolved name (or the bare address), with a
// dim address column beside them; everything the old DEVICE / CHAIN /
// SRC PORT / NAT / INTERFACES columns carried now lives in the row's
// detail sheet instead of on the row.
export const COLUMNS: ColumnDef[] = [
  { key: 'time', label: 'Time' },
  { key: 'action', label: 'Action' },
  { key: 'source', label: 'Source' },
  { key: 'srcAddr', label: 'Address' },
  { key: 'destination', label: 'Destination' },
  { key: 'dstAddr', label: 'Address' },
  { key: 'proto', label: 'Proto' },
  { key: 'port', label: 'Port' },
  { key: 'rule', label: 'Rule' },
]

// null = flexible (shares remaining width with other flexible columns,
// via `minmax(0, 1fr)`) -- the default for anything whose content length
// varies a lot (addresses, rule labels). A number is a fixed px width,
// used for naturally-bounded content (timestamps, badges, protocol) and
// for any column once the user has actually dragged it, at which point
// it stops flexing and holds the size they chose.
type Width = number | null

const DEFAULT_WIDTHS: Width[] = [124, 92, null, 132, null, 132, 74, 76, null]
const MIN_WIDTH = 56
// Flexible columns used to be `minmax(0, 1fr)`, which lets them shrink to
// nothing. An address cell holds its label plus a copy button and an
// investigate button, both `flex: none` at 17px and 15px with 4px gaps --
// so once the column falls under ~60px the buttons take everything and
// the label collapses to width 0. It is still in the DOM, still
// focusable, and invisible.
//
// This is not hypothetical: the left rail (#544) took 216px off the
// content column, the address columns dropped to ~53px, and two live
// scenarios failed on an element that existed but could not be seen or
// clicked. A floor here costs a horizontal scrollbar in the narrowest
// cases, which is the better failure.
const FLEX_MIN_WIDTH = 96
// v4: #644's squared columns replaced the twelve-column set with nine --
// bumped so anyone with a v3 width array saved just falls back to the
// new defaults instead of applying stale widths to a different column
// set.
const STORAGE_KEY = 'mikroview-column-widths-v4'

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
    this.widths.map((w) => (w === null ? `minmax(${FLEX_MIN_WIDTH}px, 1fr)` : `${w}px`)).join(' '),
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
