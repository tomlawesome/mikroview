# Navigation (#486) — design round 2

Round 1 closed 2026-08-23. Verdict, verbatim:

> "I like Q, the rail, but we need it so that there's a fully docked
> option (only a little symbol to pop it back out shows) and a way to
> have it show either icons only or icons and text."

This round is Q II — one direction, one file
(`direction-q2-rail.html`), designing exactly what the verdict asks.
Four scenes: **s1** the three states · **s2** the drawer flow from
docked · **s3** the controls close up (with the keyboard story) ·
**s4** a working page in icons mode.

## The model

- **Three persistent states — full (216px) · icons (54px) · docked
  (0px + handle)** — a per-user preference, applied before first
  paint, never changed by the app on its own. Round 1's deterministic
  auto-collapse on Live places is **superseded**: with dock
  available, immersion is the operator's explicit choice.
- **Two controls in one foot slot**: ⇔ toggles density (its
  aria-label names the destination), ⇤ docks (its label teaches the
  way back). In the drawer, ⇤'s slot becomes ⇥ "keep open" — one
  slot, two directions, no third control.
- **The handle** (docked): 26×44px glass tab, top-left, first in tab
  order, and it **wears the open-flag badge** — docking navigation
  never docks the alarm. Connection loss stays the banner's job in
  every state.
- **From docked, » opens a drawer, not a state change**: the rail
  overlays in the last-chosen density, focus lands on the current
  page, and it dismisses on navigate / Esc / click-away. Rationale:
  docked exists for watching; a state flip would tax every trip with
  a re-dock click. ⇥ pins deliberately.
- **Icons mode never abandons labels**: tooltip on hover *and* focus,
  full label + count in the aria-label, visible focus ring.
- **Defaults**: full at ≥1280px, icons below; docked never a default.
- **Unchanged from ratified Q**: groups and order, page homes, badge
  semantics, reserved-slot rule, #490 grammar, chrome states, mobile
  bottom bar (dock/density are pointer-width affordances).

## Validation record

- Palette and surface unchanged from round 1 (`round-1/README.md`
  carries the validator record for the lane set on `#06080e`); no new
  data colour is introduced this round.
- `prefers-reduced-motion`: the drawer's slide becomes instant.
- Screenshots in `shots/`, regenerated with
  `cd frontend && node ../docs/design/screens/navigation/round-2/capture.mjs`.

## Open with the owner (round-2 batch)

1. The drawer-not-state-flip call for » (with ⇥ as the deliberate
   pin) — right, or should » simply restore the persistent state?
2. Carried from round 1: does Expect earn a quiet dot when a watch is
   currently broken, or does Detect's badge carry the whole story?
3. Carried from round 1: wordmark in the rail head — keep, or reserve
   it for login? (Docked already hides it.)

## Owner verdicts

_Pending — recorded verbatim here when the batch returns; the running
log lives on #518._
