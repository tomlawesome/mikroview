# Interface visioning — round 21

Under #634. The owner's batch on round 20, verbatim in the trail:
the reach "shouldn't be a separate deck page. The point is that it
zooms in right in the same topography deck card"; the fall's port
spectra are "supposed to be lines/waves not triangles"; "the register
needs to take up far more of the available screen space and the table
feels like it could add a lot more useful information"; "we should be
able to left click to pan around the topography view too, as well as
mouse wheel clicked down".

**`the-whole.html`** (the round-20 walkthrough, corrected):

- **The reach dives in place**: the separate reach scene is gone; the
  descended view is a second layer inside `#s3`'s stage. Clicking the
  IoT card (or a docket where-name) scales the whole map away and
  fades the descent up, in the same card; ⌃ surfaces again. The
  altitude slider and lens row step aside while descended.
- **The fall's spectra are waves**: every panadapter needle is a
  smooth mound with soft skirts now. This binds the live build too —
  Fall.svelte still draws triangle needles (recorded on #616).
- **The register spreads**: ribbons nearly twice the width, columns
  spaced across the frame, the gutter and flag columns at the edges.
- **The table carries more**: natted (teal), top port and top talker
  columns join minute · accepted · refused · flag episodes; hour-total
  row extended to match. 13:52 keeps the amber cursor row.
- **Left-drag pans topography** as well as middle-drag (and both axes
  now); a 4 px threshold keeps plain clicks working as clicks.

## Owner verdicts

- Pending: the in-card dive, the wave spectra, the wider register,
  the richer table, left-drag panning.
