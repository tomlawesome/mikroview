# Interface visioning — round 12

Under #634. Round 11's aggregate tabs were wrong twice, owner verbatim:
"the watcher/flag boxes are supposed to be a part of the entity they
belong to, not separate boxes! They also stay where they are when the
map zooms in and out or moves, which is wrong! Maybe put them _inside_
the entity box instead, where possible" — and they must match the
house style, "not square edges and extra thick lines."

The zoom bug was real: the tabs were drawn outside the map's camera
group, so they ignored zoom and pan.

`the-deck.html` — the correction: the counts live **inside** their
entity's card, rounded and hairline like every other chip, watchers
amber above, flags red beneath (always the bottom); at survey altitude
they ride beneath the zone label; and they sit inside the camera group,
so zoom and pan carry them with everything else (`shots/s3-zoomed.png`
is the proof).

## Owner verdicts

Pending.
