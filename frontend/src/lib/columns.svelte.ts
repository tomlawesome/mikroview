// SPDX-License-Identifier: AGPL-3.0-only

export interface ColumnDef {
  key: string
  label: string
}

// Order matches EventRow.svelte's cell order exactly -- both this array
// and EventRow render cells positionally into the same CSS Grid, so the
// two must stay in sync by index.
// The ratified #644 column set ("the stream, columns squared", round-29
// scene 4) kept nine columns: the same kind of fact always under the eye,
// Source and Destination each showing the resolved name (or the bare
// address) with a dim address column beside them. #644 dropped DEVICE,
// CHAIN, SRC PORT, NAT, INTERFACES and MAC to the row's detail sheet
// entirely. #717 (owner ruling, 2026-08-31: "I didn't quibble before,
// because... I knew we could add back the missing columns... It's now
// later") restores all six as columns here, threaded in beside the fact
// each belongs with rather than appended after Rule -- Device and Chain
// frame the row up front (their pre-#644 position, see git history at
// a03d486^); Src port and MAC ride with Source's own facts; Interfaces
// keeps its pre-#644 place right after Proto; NAT (a translated address,
// not fixed to either side -- see EventRow's natFilterKey) sits beside
// Port, its pre-#644 neighbour, just after Proto/Interfaces rather than
// before them, since Proto still has to stay ahead of Port here (that
// relative order belongs to the original nine and does not move). All
// six also stay in EventDetailSheet.svelte: the sheet is not just an
// overflow bin for facts with no column, it is still the row's one
// full-detail surface (raw line, MAC lookup, etc.).
// #729: the chooser's two pinned columns -- always shown, never offered as a
// toggle. Time is the row's one temporal anchor (and the sticky column the
// header mirrors); Rule is the one field #685 already ruled has no real
// ceiling and carries the row's own investigate/edit affordances. Every
// other column is a reader's preference, on by default (see DEFAULT_VISIBLE
// below) -- the owner's ruling on #729 keeps the shipped default at all
// fifteen; turning one off is something a reader chooses, not a repair for
// the table's width.
export const PINNED_COLUMNS: ReadonlySet<string> = new Set(['time', 'rule'])

// #1149: each half of a repeated pair says which side it is. Both address
// columns read "Address" and the port pair read "Src port"/"Port", so the
// only thing telling a reader which was which was where it sat --
// EventDetailSheet.svelte has said "Src port"/"Dst port" all along.
export const COLUMNS: ColumnDef[] = [
  { key: 'time', label: 'Time' },
  { key: 'device', label: 'Device' },
  { key: 'action', label: 'Action' },
  { key: 'chain', label: 'Chain' },
  { key: 'source', label: 'Source' },
  { key: 'srcAddr', label: 'Src address' },
  { key: 'srcPort', label: 'Src port' },
  { key: 'mac', label: 'MAC' },
  { key: 'destination', label: 'Destination' },
  { key: 'dstAddr', label: 'Dst address' },
  { key: 'proto', label: 'Proto' },
  { key: 'iface', label: 'Interfaces' },
  { key: 'port', label: 'Dst port' },
  { key: 'nat', label: 'NAT' },
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
//
// #717 restores six columns (see COLUMNS' own comment above). None of
// them get `null`: each is fixed and sized to its content by the same
// by-hand method as the nine above, because a seventh/eighth flexible
// column would go straight back to the reported bug this measure was
// built to fix, and because Rule is the one field the owner and #685
// have already agreed has no real ceiling -- these six do:
//   device    a configured friendly name, or (undeclared) the
//             device's own source IP -- <=15 chars either way, a copy
//             button and (since #600 gave a device name somewhere
//             everyone reads) the same pencil the address columns
//             carry, which is why this sits close to their 160px
//   chain     a RouterOS chain word, <=11 chars ("postrouting"),
//             plain click-to-filter text, no buttons
//   src port  mirrors Port exactly (up to 4 digits, bare number, no
//             buttons) -- the same fact, just the other side of the
//             connection
//   mac       always exactly 17 characters ("AA:BB:CC:DD:EE:FF") --
//             the one column with a harder bound than a timestamp,
//             plain text, no buttons (no Filters field takes a MAC,
//             see EventDetailSheet.svelte's own comment on its MAC row)
//   interfaces  two interface names joined by "→" -- RouterOS defaults
//             (ether1, bridge1, wlan2) are short, but custom VLAN/
//             bridge names run longer with no fixed ceiling the way
//             Rule doesn't either; fixed and ellipsis-truncated rather
//             than flexible, since Rule stays the only column that
//             gets to claim leftover width
//   nat       a translated address, `formatAddr`'s "ip:port" shape --
//             bounded like an IPv4 address plus ":" plus up to 5
//             digits, click-to-filter when the chain says which side
//             it is (mirrors EventRow's natFilterKey), otherwise plain
// #1149: MAC 150 -> 168 and the two address columns 104 -> 132. Both were
// measured under their content: a 17-character MAC needs ~163px at the
// row's 14px mono, and a 13-character bare IPv4 ~129px, so every one of
// them was cut short. (#685 sized the address columns for the
// ten-character IP in the round-29 data; the addresses on a real LAN run
// to fifteen.) The numbers only move the defaults -- a reader who
// has dragged a column keeps the width they chose (STORAGE_KEY is not
// bumped: the stored array's shape has not changed).
const DEFAULT_WIDTHS: Width[] = [124, 150, 80, 90, 160, 132, 60, 168, 160, 132, 60, 170, 60, 150, null]
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
// v6 (#717): six columns restored (nine -> fifteen). The length guard
// in loadInitial below would already reject a saved 9-entry v5 array,
// but the key is still bumped, matching the convention every prior
// shape change here has followed -- a stored width array and the
// column set it was measured against should always be nameably the
// same version, not just accidentally the same length.
const STORAGE_KEY = 'mikroview-column-widths-v6'

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

// Keyed by column key rather than shadowing widths' index-positional shape:
// EventRow.svelte (#729) calls isColumnVisible(key) once per optional cell,
// per row -- up to 13 times across 810 rows on the live view's own
// MAX_RENDERED_ROWS ceiling. #728 already put that render path within
// reach of vitest's timeout at nine cells fewer; an index lookup would mean
// COLUMNS.findIndex (a linear scan) on every one of those calls, so this is
// a plain object for O(1) property access instead.
type Visibility = Record<string, boolean>

// #729: every column defaults to visible -- the owner's ruling keeps the
// shipped default at all fifteen, so this is what a reader who has never
// touched the chooser sees.
const DEFAULT_VISIBLE: Visibility = Object.fromEntries(COLUMNS.map((c) => [c.key, true]))

// #1150: at 1366 the fifteen columns measure 1762px into a 1308px box,
// so the right-hand end of the table -- Rule included -- fell off the
// edge. Below the breakpoint MAC and Interfaces start hidden instead,
// which is the ruling's set; it names "the two MAC columns", but this
// table has only ever had one (COLUMNS above), and Src MAC is a detail-
// sheet row rather than a column.
//
// Nothing here is silent: the `columns ▸` picker reads isColumnVisible
// for every checkbox, so these draw unticked, and ticking one back on
// goes through toggleColumn/persistVisibility like any other choice the
// picker makes -- one mechanism, not a second one for narrow screens.
// Above the breakpoint nothing changes at all.
const NARROW_BREAKPOINT = 1500
const NARROW_DEFAULT_HIDDEN: readonly string[] = ['mac', 'iface']

// Read once, at module load, and deliberately not tracked: this is where
// a reader *starts*, not a live layout rule. A column disappearing
// mid-session because the window was dragged narrower is exactly the
// silent change the ruling rules out -- and it would fight the reader's
// own saved choice every time they resized. matchMedia is absent under
// jsdom, where "wide" is the right answer: the all-fifteen default is
// what #729 shipped.
function startsNarrow(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia(`(max-width: ${NARROW_BREAKPOINT}px)`).matches
    : false
}

// What a reader who has never touched the picker starts with.
function startingVisibility(): Visibility {
  const next: Visibility = { ...DEFAULT_VISIBLE }
  if (startsNarrow()) for (const key of NARROW_DEFAULT_HIDDEN) next[key] = false
  return next
}

// Same reader-preference mechanism the widths above already use (a plain
// localStorage entry, versioned key bumped whenever the shape it was
// measured against changes) -- not a second mechanism invented for this
// issue. A separate key from STORAGE_KEY: widths and visibility are
// independent choices, and giving them one combined value would force a
// version bump (and a reset to defaults) on every reader's saved widths the
// day visibility shipped, for no reason tied to widths at all.
const VISIBILITY_STORAGE_KEY = 'mikroview-column-visibility-v1'

function loadInitialVisibility(): Visibility {
  const starting = startingVisibility()
  try {
    const raw = localStorage.getItem(VISIBILITY_STORAGE_KEY)
    if (raw) {
      const parsed = JSON.parse(raw)
      if (parsed && typeof parsed === 'object') {
        const next: Visibility = {}
        for (const col of COLUMNS) {
          // A pinned column reads as visible regardless of what was saved --
          // guards against a stored value from a build that let it be
          // hidden, or hand-edited storage, either of which would otherwise
          // drop Time or Rule off the table with no way back short of
          // clearing storage. A key missing from an older saved value (one
          // written before a new column existed) falls back to the
          // starting default for this width, so a column added later
          // starts where a fresh reader's would.
          next[col.key] = PINNED_COLUMNS.has(col.key) ? true : Boolean(parsed[col.key] ?? starting[col.key])
        }
        return next
      }
    }
  } catch {
    // ignore malformed/unavailable storage, fall through to defaults
  }
  return starting
}

// Column widths and visibility for the live table. Widths are
// user-resizable via drag handles in LiveTable.svelte; visibility is
// user-chosen via the chooser in FilterBar.svelte (#729). Both persist
// across sessions the same way, and both feed the one `gridTemplate`
// string that drives the sticky header row and the event rows below it,
// since both live in the same CSS Grid container.
class ColumnState {
  widths = $state<Width[]>(loadInitial())
  visible = $state<Visibility>(loadInitialVisibility())

  visibleColumns = $derived(COLUMNS.filter((c) => this.visible[c.key]))

  // Computed once per columnState.visible change (COLUMNS has 15 entries),
  // not once per row -- EventRow.svelte reads this to take a fast,
  // unconditional path when nothing is hidden (the shipped default), so
  // the ordinary case costs the same as before the chooser existed rather
  // than paying for thirteen per-cell {#if} checks it doesn't need. See
  // that component's own comment on why this matters for #728.
  allVisible = $derived(COLUMNS.every((c) => this.visible[c.key]))

  gridTemplate = $derived(
    this.widths
      .filter((_, i) => this.visible[COLUMNS[i].key])
      .map((w) => (w === null ? `minmax(${FLEX_MIN_WIDTH}px, 1fr)` : `${w}px`))
      .join(' '),
  )

  isDefault = $derived(this.widths.every((w, i) => w === DEFAULT_WIDTHS[i]))

  setWidth(index: number, px: number) {
    const next = [...this.widths]
    next[index] = Math.max(MIN_WIDTH, Math.round(px))
    this.widths = next
    this.persist()
  }

  // The same write, addressed by column key rather than by position.
  //
  // `widths` is positional over the full COLUMNS list while everything
  // on screen -- the grid template, the header cells, the resize
  // handles -- runs over `visibleColumns`, so the two indices agree
  // only while every column is on. A caller holding a screen position
  // and passing it to setWidth therefore resizes a different column as
  // soon as the reader hides one (#729's chooser), which is a silent
  // wrong answer rather than an error. Callers that have a key should
  // use this; the positional form stays for callers that genuinely
  // mean a COLUMNS index.
  //
  // An unknown key is a no-op: there is no sensible column to widen
  // instead, and guessing one is exactly the failure above.
  setWidthForKey(key: string, px: number) {
    const index = COLUMNS.findIndex((c) => c.key === key)
    if (index === -1) return
    this.setWidth(index, px)
  }

  reset() {
    this.widths = [...DEFAULT_WIDTHS]
    this.persist()
  }

  isColumnVisible(key: string): boolean {
    return this.visible[key] ?? true
  }

  // No-op on a pinned column (Time, Rule) -- the chooser in FilterBar
  // never renders a checkbox for one, but this guards the state itself
  // rather than trusting every caller to check PINNED_COLUMNS first.
  toggleColumn(key: string) {
    if (PINNED_COLUMNS.has(key)) return
    this.visible = { ...this.visible, [key]: !this.visible[key] }
    this.persistVisibility()
  }

  private persist() {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(this.widths))
    } catch {
      // storage unavailable -- widths just won't persist across reloads
    }
  }

  private persistVisibility() {
    try {
      localStorage.setItem(VISIBILITY_STORAGE_KEY, JSON.stringify(this.visible))
    } catch {
      // storage unavailable -- the choice just won't persist across reloads
    }
  }
}

export const columnState = new ColumnState()
