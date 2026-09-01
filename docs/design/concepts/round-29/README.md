# Interface visioning — round 29

Under #634. The verdict batch that opened this round (verbatim,
2026-08-30):

- Journey beats 1–3: **"55,56,57 yes"**; beat 5: **"59, yes"**.
- Beat 4, the tour: **"58, yes but it should highlight key
  handles/inputs/outputs"** … **"and label/explain concisely"** —
  the tour spec already ratified on #634.
- The bubble: **"60. outlined not filled otherwise apporved"**.

Built here, in `the-whole.html`:

- **The bubble goes outlined.** Same behaviour exactly (orange
  "clear all" → one click arms red "confirm" → second click clears;
  click-away disarms) — but drawn as an outline: transparent fill,
  amber border and text; armed swaps both to alarm red. A faint
  matching wash on hover only.
- **The tour beat shows its ratified shape.** Beat 4 now
  demonstrates the highlight-with-labels model on the fall: three
  key handles ringed in accent hairline — the brink (now arrives
  here), a band (click reaches in), the held hour (scroll looks
  back) — each labelled in a few plain words, with "THE FALL ·
  1 OF 6 · NEXT ▸" as the walk. A ring and a sentence, never over
  the top.

Beats 1–3 and 5, and everything else, carry forward verbatim from
round 28. No new data colours.

## Owner verdicts

- The outlined bubble (verbatim, 2026-08-30): **"61. perfect"**.
- The labelled tour beat (verbatim, 2026-08-30): **"62. approve"**.

Every round-29 surface is accepted. This closes the #634 visioning
thread: rounds 13–29 hand over to implementation under #620/#633.

## Not everything in a scene is a requirement

The audit-log tab's rows are demo filler, not spec (owner, 2026-08-31,
item 99). `opened flag`, `changed theme to dark`, `signed in from` and
`router pushed` are there to fill the tab in a walkthrough; four of the
six would mean logging reads, client-side preferences and ingest. The
audit log stays as built — admin-privileged mutations only — and #679
records the three shapes considered and why this one won.

---

**Superseded as the current picture by [round 30](../round-30/README.md)**,
which carries every round-29 surface verbatim and adds the stream's
filter (#697), an ordered top for the stream, and the chrome the owner
struck on 2026-08-31. Round 29 remains the record of its own verdicts.
