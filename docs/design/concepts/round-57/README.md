# Round 57 — the token bar (#983)

Fable's shape, settled 2026-09-13, drawn on the shipped
`frontend/src/components/FilterBar.svelte` and `lib/filterChips.ts`.
The stream's filter box (`.fbox`) gains a third face: face one is the
named-field strip (`.bar.thin`, unchanged), face two is the read-only
chip summary the box already renders from `appState.filters` via
`buildFilterChips()`. Face three, drawn here, is a GitLab-style
typed/clicked path to the same chips — a field menu, then a value
menu, and committing a value writes a chip identical to the one face
two already draws: same `.chip`/`.chip-x`/`<em>` markup, same
`Remove the {field} filter` aria-label, same `appState.filters`
underneath. **The token bar is a third face of one state, not a
second filter store.**

## The rule

**Token fields.** Eight bounded filters, one per `Filters` key group:
`device`, `action`, `chain`, `proto`, `interface`, `port`, `source`,
`destination`. Free text in the same box stays the rule search
(`filters.rule`), untokenised, exactly as the `.fbtype` input works
today.

**Value pickers.**
- `device` — the list of known devices by name, the same set
  `filterChips` resolves against.
- `action`, `chain`, `proto`, `interface` — a fixed list offering
  exactly what the strip's own controls offer today. `action` reuses
  `ACTION_FILTER_OPTIONS` verbatim (accept · drop · reject · log ·
  marked (mangle) · natted (NAT) · unknown); `chain` reuses
  `appState.chainOptions` (the five built-in chains plus anything
  observed). `proto` and `interface` have no fixed list in the strip
  today — both are plain text inputs — so their menus list only what
  has been seen in the current window (this round: `tcp`/`udp` for
  proto, `ether1`/`bridge-lan`/`bridge-iot` for interface). Never a
  value that has not arrived.
- `port` — typed number, Enter commits.
- `source`/`destination` — one second-level menu with that side's
  three sub-fields (scope from the `Scope` union — internal/external
  — country from observed countries, or typed IP/text), committing to
  ONE composite token rendered the way the ratified chip already
  reads: `source:185.220.101.34 · external · DE` (scene 05). Fewer
  than three parts render with fewer joins — `sideValue()` already
  skips blanks, so a scope-only or address-only pick still renders
  correctly.

**Saved filters.** Applying one expands it into its tokens, each
individually removable — scene 05. Saving snapshots the current
tokens + free text under the existing `saved ▾`. **Rejected:** a
saved filter as a single opaque named token. It hides what it
filters — the whole point of a token bar is that every active term is
visible and removable on its own.

**Mobile.** The drawer stays (#683 round 29), unchanged — scene 06.
The token bar is the wide-screen face only; the field/value menu is
not built for the drawer.

**Keyboard.** Menu opens on focus; ArrowDown enters it; Enter picks;
Esc closes; Backspace in an empty input deletes the last token; plain
typing always lands in free text and is never swallowed by the menu.

## The scenes

`node ../docs/design/concepts/round-57/capture.mjs` from `frontend/`.

- `empty` — no filter, the box as shipped today.
- `field-menu` — the box focused, the field menu open: all eight
  fields, each with a one-line hint of what it takes (device's known
  list, action's/chain's real option lists, proto's/interface's
  observed-only rule, port's Enter-to-commit, source's/destination's
  three sub-fields).
- `value-menu` — `device` chosen as the field (a pending token,
  `device:` with a caret, not yet a chip); the value menu lists the
  known devices, `cam-porch` keyboard-focused first (ArrowDown enters
  the list on the first item).
- `tokens-and-text` — three committed tokens (`device:cam-porch`,
  `action:drop`, `chain:forward`) plus `iot-to-lan` being typed in the
  same box — free text, not a fourth token, narrowing the rule label
  alongside the three tokens.
- `saved-applied` — the `WAN scans` saved filter just clicked in
  `saved ▾`, expanded into three separately removable tokens
  (`action:drop`, `chain:input`, the composite `source` token).
- `drawer-beside` — the same applied filter, wide token bar on the
  left, the untouched narrow drawer on the right, so the two faces
  read as one state side by side.

The field/value popups borrow `FilterPresetsMenu`'s own floating-menu
dress (`.fpmenu`'s elevated background, hairline border, radius,
shadow) rather than inventing new chrome, anchored to the box's left
edge instead of the trigger's right edge.

## Data story

Round 30's stream (`the-whole.html #s5`), carried forward verbatim —
same devices, same rules, same rows in every scene:

- **cam-porch → nas :445**, `forward` chain, `iot-to-lan-drop` rule,
  DROP, repeating every ~64 s since 13:52:07, 14 times so far,
  flagged. Drives `tokens-and-text`.
- **rb5009's four logged WAN scans**, `input` chain, `wan-in-drop`
  rule, DROP: 185.220.101.34 (DE) on :8291, 198.51.100.90 (CN) on
  :22, 203.0.113.199 (RU) on :443, 45.155.205.11 (NL) on :8291. The DE
  one (6 attempts today) drives `saved-applied` and `drawer-beside`.
- Known devices (the `device` value menu): cam-porch, hue-bridge,
  laptop-anna, nas, phone-tom, rb5009, tom-desktop, unifi.
- Two saved filters exist in this story: `WAN scans` (applied in
  scene 05) and `IoT lockdown` (listed, not applied).

## For the build

Deferred — this is a design round, not an implementation plan. The
build is a separate issue, filed only after the owner ratifies this
shape.

## Noticed, unverified

Capping the drawer's height for `drawer-beside` (so it scrolls like a
real phone viewport instead of running the mockup off the page)
surfaced a CSS combination worth a real check: `FilterBar.svelte`'s
`.bar` sets `flex-wrap: wrap`, and `.bar.drawer` sets
`flex-direction: column; max-height: 80vh; overflow-y: auto` without
overriding `flex-wrap`. A flex column with wrap left on does not
scroll when content exceeds its max-height — it opens a second column
beside the first instead, which is what this file did until it added
`flex-wrap: nowrap` to its own copy of the rule. Whether the real
drawer's content ever exceeds 80vh on a real phone (and so ever hits
this) is not confirmed — worth a quick check, not filed as an issue
from here since it is unreproduced against the running app.

## Open questions

Numbered for the owner's reply.

1. `proto`/`interface` have no fixed list anywhere in the app today —
   this round's menus for both are seen-this-window lists, invented
   for the token bar rather than reused from an existing control (see
   the rule above). Fine as the shape, or should the strip itself grow
   real option lists for these two first, so the token bar has
   something to reuse rather than being the first place either field
   gets a picker?
2. The field menu's hint text (`accept · drop · reject · log · marked
   (mangle) · natted (NAT) · unknown`) is long for `action`. Keep the
   full list inline, or shorten to a count (`7 values`) and let the
   value menu itself carry the full list?
3. Backspace-deletes-last-token is specified for an empty input. Is
   "empty" the free-text value only, or does a half-typed value menu
   query (e.g. mid-typing an IP in the source sub-menu) count as
   non-empty and block it too?
