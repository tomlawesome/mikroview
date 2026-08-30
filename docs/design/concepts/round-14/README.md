# Interface visioning — round 14

Under #634. One thread, from the owner's round-13 verdict (verbatim):

> "I think we need a C, where the flags are their own boxes outside the
> main one, but stacked up, and I think watchers should be a purple or
> a cyan."

**Candidate C** (`aggregate-style-c.html`): every aggregate is its own
box — the round-12 chip rendering kept, but standing entirely free of
the card, stacked at the card's lower right shoulder, flags always the
foot of the stack. Two scenes, identical except the watcher's colour:

- `#Cp` — watchers **purple** `#a78bfa`
- `#Cc` — watchers **cyan** `#45d7e6`

Interpretation note: the verdict names the flags as the boxes that step
outside; here the watcher aggregate steps out with them (one stack, the
ratified watchers-above/flags-bottom order). If watchers should stay
inside the card, that is a cheap correction — say so.

Palette check (contrast against the void `#06080e`, 9.5 px mono chip
text): purple ≈ 7.2:1, cyan ≈ 11.4:1 — both pass AA for small text.
Adjacency to think about before ratifying: purple is the fall's violet
("other" raindrops); cyan sits between the fall's accept blue and the
NAT badge teal `#2dd4bf`.

Repairs inherited from round 13, fixed in this copy: the altitude/survey
CSS was scoped to `#topo` while the stages were `#topo-A/B`, so the
survey dot layer drew on top of the cards; the altitude slider targeted
the same wrong id. Both scoped correctly here.

## Owner verdicts

- Pending: C's shape (boxes outside, stacked, flags the foot); purple
  vs cyan; and round 13's corrected metrics card.
