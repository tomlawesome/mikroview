# Interface visioning — round 13

Under #634. Three threads from the owner's batch:

1. **Aggregate style, two candidates** (`aggregate-styles.html`), after
   round 12 missed the sketch: **A — the bands**: full-width rows
   stacked at the foot of each card, part of the card, flags always
   the bottom band. **B — the divided section**: the round-12 chips,
   kept, but inside their own hairline-divided section at the card's
   right, the outer box wrapping everything, flags at the bottom of
   the stack.
2. **The metrics card corrected** (`the-deck.html`): it is now the
   page we already built (#488) — seismograph (default) · register ·
   table over one cursor, with the hourline ledger — restated in the
   deck's clothes, not an invented chart. Owner verbatim: "why aren't
   you using the metrics pages we already built?"
3. **The roll rail accepted** ("FWIW the side bar nav is great").

## Owner verdicts

- Roll rail (round 11): accepted.
- A vs B (2026-08-30): neither taken as-is — "I think we need a C,
  where the flags are their own boxes outside the main one, but
  stacked up, and I think watchers should be a purple or a cyan."
  → round 14 builds candidate C, with purple and cyan watcher
  variants to choose between.
- The corrected metrics card (chat, 2026-08-30): **accepted** — "Ok,
  lets move on with the seismograph as it is in 13." The owner compared
  it against the pre-rebuild dev seismograph live before ruling.

## Post-serve repairs (defects, not rework)

The owner could not operate the metrics card as served (chat,
2026-08-30: "The register/table links don't work") — the view
switchers were dead spans and only the seismograph was ever drawn.
Repaired in place so the review could continue: register and table
views built (restated from the shipped `MetricsRegister.svelte` /
`MetricsTable.svelte` — brink-at-top ribbons with flag columns; the
sortable minutes table, refused ink, amber cursor row, hour-total
footer), switchers wired, the scene description moved off the hourline
ledger. Shots: `shots/metrics-{seismograph,register,table}.png`.
