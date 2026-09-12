# The stream — rulings on the built page

No rounds sit behind this file. The stream was drawn in the concept
rounds (29/30/36/37) and built from them; this is where owner rulings on
what shipped are recorded, so the next person to touch the page reads the
decision rather than re-deriving it. Same shape as
`../log-every-rule/DESIGN.md`.

## The filter strip (#1191, ruled 2026-09-12)

Found by a usability review at 1920, 1366 and 1100 wide: SOURCE and
DESTINATION each carried three unlabelled controls, the middle one a bare
underline with no placeholder, and the whole strip used the left 1050px
of a 1920px bar with `fold ▸` marooned alone at the far right.

The one-row strip stays. Three changes, and nothing moves:

1. Each of the three controls under SOURCE and DESTINATION is captioned
   in the strip's small-caps label style — **scope · name, IP or CIDR ·
   country**. The aria-labels already said this; the ruling makes it
   visible.
2. The address query field shows its "name, IP or CIDR" hint on desktop
   as well as on phones. This is the one ratified exception to round 29's
   "no placeholder prose inside the fields": the field was read as a bare
   underline with nothing to say what it takes.
3. The strip stretches to the bar's full width, so `fold ▸` sits beside
   `columns ▸` rather than alone at the edge. The slack is distributed
   between the groups — gap and flex — and never by widening the inputs,
   which keep the widths the thin bar gives them.

#983 (a search bar with filter tokens) stays a separate concept: this
ruling is about the strip as built, not about replacing it.
