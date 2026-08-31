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
// via `minmax(FLEX_MIN_WIDTH, 1fr)`) -- the default for anything whose
// content length varies enough that no fixed number reads right. A
// number is a fixed px width, used for naturally-bounded content
// (timestamps, badges, protocol, ports, addresses) and for any column
// once the user has actually dragged it, at which point it stops
// flexing and holds the size they chose.
type Width = number | null

// #685: the ratified round-29 table (the-whole.html, #s5) sets no
// explicit column widths at all -- it is a plain `<table>` with
// `border-collapse: collapse` and no `table-layout: fixed`, so the
// browser's own auto layout sizes every column to its content across
// the whole scene. There is nothing to lift a percentage from; the
// measure below is that same content-driven sizing, worked out by hand
// against the scene's own rows (docs/design/concepts/round-29's data,
// mirrored in /tmp/r29/stream.txt) since this is a CSS Grid, not a
// table, and grid has no auto-layout algorithm to defer to:
//   time      12 chars ("14:02:11.482")
//   action    a small flat badge (<=6 letters: ACCEPT/REJECT/MARKED/NATTED)
//   source    a name (<=11 chars typically) or a bare geo IP + country
//             (<=18 chars) plus its copy/edit buttons
//   address   a bare IP (<=10 chars) or an em dash, no buttons
//   destination/address  mirror source/address
//   proto     3 chars ("tcp"/"udp")
//   port      up to 4 digits
//   rule      the one genuinely unbounded field (up to ~15 chars seen,
//             no real ceiling) plus its copy/edit/investigate buttons --
//             the sole flexible column, so extra width goes where it is
//             actually needed instead of split three ways.
// Before this, source/destination/rule were three *equal* flexible
// columns: on a wide viewport each 1fr got the same large share of
// leftover space regardless of what it held, which is exactly the
// reported bug (source "given roughly a third of the table for a short
// IP") -- and the address columns' 132px was more than an em dash or a
// ten-character IP ever needs, reading as empty rather than measured.
const DEFAULT_WIDTHS: Width[] = [124, 80, 160, 104, 160, 104, 60, 60, null]
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
//
// #685: rule is now the only flexible column and carries three buttons
// (copy, edit, the pushed-table lookup trigger) beside its text -- the
// generic 96px floor left it pinched at the edge of usability, so it
// gets a taller floor of its own.
const FLEX_MIN_WIDTH = 140
// v5 (#685): the column measure changed shape, not just its numbers --
// source/destination/address went from flexible to fixed and rule is
// now the sole flexible column -- so a v4 array (three equal flex
// columns) would apply that stale shape to columns that no longer work
// that way. Bumped so it falls back to the new defaults instead.
const STORAGE_KEY = 'mikroview-column-widths-v5'

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
