# The reshaped interface adopts nothing: Svelte 5 stays, primitives are house-built, styling is scoped CSS on a token floor

Date: 2026-08-23. Phase 2 of #385 — the framework, design-system and
styling decision the epic deferred to #482. The direction it serves was
ratified the same day in #483 after four visioning rounds; the mockups
and their verdict trail live under `docs/design/concepts/`.

## The question

"Which UI library" was never one question. Three separable ones have
been travelling together, and an answer that settles only one leaves
the others to be re-litigated screen by screen:

1. **Framework** — stay on Svelte 5 with runes, or move.
2. **Design system** — adopt a component library, build a house set of
   primitives, or keep composing ad-hoc per screen.
3. **Styling** — keep hand-rolled scoped CSS as-is, or formalise a
   layer underneath it.

## The evidence this is decided against

The four visioning rounds are the test bench. Every surface the
ratified direction needs was built, iterated and owner-reviewed as
self-contained HTML + SVG + scoped CSS, with no build step and zero
dependencies: the fall (a continuously animating field of hundreds of
SVG marks — the landing page), Atlas II's topography, the reach
drill-in, the aligned live table, modals, filter bars, and two full
theme skins over one structure. Two facts fall out of that:

- **Nothing the direction needs is something a library provides.** The
  hard surfaces — the fall, the map — are bespoke SVG that no component
  library ships. The ordinary surfaces — fields, popovers, modals,
  tabs, tables — are already specified to the letter by owner-ratified
  interaction specs (#438's paired filters and swap, #413's anchored
  editor with focus trap and hold-while-open, #445's two-mode popup,
  #487's wizard-as-modal, #489's tabs, round 4's column grid), and
  most already exist in `frontend/src/components/`.
- **The rendering shape is Svelte's shape.** Fine-grained reactive
  state driving DOM and SVG attributes, no virtual-DOM churn across a
  field of thousands of marks, scoped styles per component. The
  mockups' CSS animations and SVG structure translate to Svelte 5
  components mechanically.

## Decision 1 — framework: Svelte 5 stays

Not settled by inertia; settled because the alternatives buy nothing
here. A migration (React, Solid, or anything else) would spend the
whole phase reaching parity with a codebase that already renders this
product well, to gain ecosystem access this decision then declines to
use (no component library is being adopted). Svelte 5 with runes is
the framework the reshape is built with. This closes the question
explicitly rather than leaving it implied.

## Decision 2 — design system: house primitives, no component library

The reshape formalises a **house primitive set** instead of adopting
anything. The set, scoped here so phase 3 does not invent it screen by
screen: **button · field/select · chip/badge · anchored popover ·
modal · tabs · table grid · toast · nav rail**. The behavioural
contracts for the non-trivial ones are already written in the ratified
specs named above; the primitives implement those contracts once, and
screens compose them. Most exist today as components; the work is
consolidation as screens are rebuilt, not a big-bang rewrite.

The costs are owned, not hidden: house primitives mean owning focus
management, ARIA and keyboard behaviour ourselves. That ownership is
already committed — the ratified specs specify focus trapping,
screen-reader narration and never-colour-alone in detail, so a library
would not relieve the obligation; it would only implement it in terms
the specs would then have to be bent around.

## Decision 3 — styling: scoped CSS, formalised on a token floor

Hand-rolled scoped CSS stays. What phase 3 adds is the **token floor**:
the design tokens the mockups already converged on (surface/panel/grid
steps, ink hierarchy, the validated lane and status palettes, the NOW
amber, spacing and type scale) become CSS custom properties defined
once, and components consume tokens rather than raw values. The
palette-validation discipline from the visioning rounds (the six
computable checks, per surface) travels with the tokens.

Round 4 also fixed this decision's ceiling, in the owner's words:
**tokens are plumbing, not identity.** A token swap restyles; it does
not make a theme. Real theme identities — the water and space concepts
on the record — are design work above the floor: voice, texture,
motion, mark rendering, per theme. That work is #492's (v0.4.0+), and
it presupposes exactly this floor; nothing about it is prejudged here.

## The bundle budget moves with this decision

The 92,000-byte gzip gate was set on #462 as ~15% headroom over the
pre-reshape bundle. The owner's ruling (2026-08-23): it is a tripwire
against drift, not a design ceiling. This change resets
`frontend/scripts/check-bundle-budget.mjs` to **200,000 bytes gzipped**
so the v0.4.0 reshape builds against room rather than a number derived
from the interface it replaces. After the reshaped UI ships, the budget
is re-derived the original way: measured + ~15%.

## Rejected alternatives

- **Visual component libraries** (Skeleton, Flowbite-Svelte,
  shadcn-svelte ports, and kin). Screened before taste: copyleft or
  share-alike licensing conflicts with the commercial licence for
  anything shipped; the survivors arrive with a runtime, an icon set
  and a theming engine — a large swing against a stated product value
  (dependency-light) — and their components would have to be bent to
  match interaction specs the owner has already ratified.
- **Headless behaviour libraries** (Bits UI, Melt UI — MIT, small,
  accessibility-focused; the serious candidate). Rejected on three
  counts: the behaviours mikroview needs are few and already specified
  to the letter, so the library's generality is dead weight; part of
  the behaviour is already implemented and shipped; and every
  inner-loop dependency is supply-chain surface on a product whose
  security posture is the selling point. If a future primitive proves
  genuinely hard to get right by hand, revisiting this line item is a
  one-paragraph follow-up, not a violation.
- **A utility-CSS framework** (Tailwind and kin). A new build
  dependency and a rewrite touching every component, to reach a
  consistency the scoped-CSS-plus-tokens floor already provides.
- **A charting library for the fall or the map.** Re-affirming the
  epic's own bar — *"a charting library is not the answer to a design
  problem."* Four rounds of mockups drew every chart this product
  needs in plain SVG; the shipped versions do the same.
- **Framework migration.** Covered under decision 1: all cost, no
  capability this direction uses.

## What this settles elsewhere

On merge, #482 closes, and #385's "Explicitly not settled" list drops
two lines: the framework/design-system choice (this document) and the
landing page (the fall, ratified in #483's round-4 record).
