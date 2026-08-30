# Interface visioning — round 14

Under #634. Two deliveries — the owner directed (chat, 2026-08-30) that
features batch into the round in flight rather than opening a new round
per remark, so the correction below is round 14, not a round 15.

**First delivery** (`aggregate-style-c.html`): candidate C from the
round-13 verdict — every aggregate its own box, free of the card,
stacked at its lower right shoulder; two scenes, watchers purple `#Cp`
vs cyan `#Cc`. Palette check against the void `#06080e`: purple ≈ 7.2:1,
cyan ≈ 11.4:1, both pass AA for the 9.5 px chip text.

**Correction** (`aggregates-under.html#C`), owner verbatim:

> "Getting frustrated. I said underneath. Full width of the box they
> belong to. Both equally sized."

So: the boxes sit underneath the card they belong to, the card's full
width, equally sized, stacked, flags always the foot. Client spokes
drop by each lane's stack height; the ribs that met the Internet and
WireGuard cards re-anchor below the new boxes. Also rolled in:

- **Watchers purple — ratified** ("Go with purple").
- **Altitude slider**: "You should also be able to click anywhere on
  the slider to get the different views and there should be a tiny
  indicator for each of them, no text, just a symbol on the line."
  Built: a tiny dot per stop on the line itself, the survey stop an
  atlas diamond; clicking the line jumps to the nearest stop.

Repairs inherited from round 13, fixed in both files: the
altitude/survey CSS was scoped to `#topo` while the stages were
`#topo-A/B`, so the survey layer drew on top of the cards, and the
slider targeted the same wrong id.

## Owner verdicts

- Aggregate boxes: outside-at-the-shoulder killed; **underneath, full
  width, equally sized** is the shape (verbatim above).
- Watcher colour: **purple, ratified**.
- Slider stop marks + click-to-jump: owner-specified, built.
- C as corrected: **APPROVED** ("Yes, finally C is approved.",
  chat, 2026-08-30).
- Still pending: round 13's corrected metrics card.
