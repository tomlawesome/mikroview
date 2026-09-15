# Round 60 — the Pages landing page in the app's own identity (#1130)

2026-09-15. `site/index.html` still wears the pre-Atlas palette (`#0b0e14`,
`#4d9fff`), a light theme the app dropped in #708, feature cards, and a
"the fall — MikroView's landing view" browser-chrome frame. The app has
moved on: void ground, blue-washed inks, hairline borders, mono captions
in lowercase sentences, the fall's now-line, the tour's cyan ring. Three
directions, each a complete page: same copy (the router-credential wording
from 177417e1 kept word for word), same four screenshots from
`docs/screenshots/`, same data story (21:57:23, ⚑ 58, ◉ 5, the :445 port
scan on ether1 at 21:50, vlan-iot dark). Static, self-contained, no
external assets, `prefers-reduced-motion` honoured, every control
keyboard-reachable. Screenshots are referenced as `../../../screenshots/`
so the round needs no copies; the Pages build already copies them to
`img/`.

Shared decisions, whichever direction wins:

- **Dark only.** The theme toggle goes: the app has one theme, and a light
  landing page would show a product that does not exist.
- **The wordmark is the app's** — `MIKRO` in ink, `VIEW` in the mark blue,
  letter-spaced — not the lockup SVG's mixed-case `MikroView`.
- **Sections are named as the app names its views**: the fall, topography,
  the stream. No "What you get" grid of six cards; the three views carry
  the features, and push/secure/one-container are three quiet columns.
- **Captions speak in the app's voice** — mono, lowercase, the sentences
  the app itself says ("blank because nothing is logged — not because
  nothing is sent").

## The directions

### The fall, opened — `fall-opened.html`

The hero *is* a fall, drawn in CSS: six boundary columns with the app's
headers (name, rule comment, WATCHED / ✱ PORT SCAN / DARK — NO LOG RULE),
the amber now-line, marks pouring down beneath it, the flagged column
red-washed, the dark column hatched with its "nothing logged" sentence.
The headline sits below the now-line over the quiet columns; the dark,
flagged and busy columns stay visible beside it. Motion stops under
reduced-motion (marks freeze in place). The app's right-hand view rail is
the page nav on wide screens. Below, one section per view, screenshot
beside the words.

### The docket — `docket.html`

One column, an editorial record. A time gutter on the left, like the
fall's axis: `21:57:23 now`, `21:57 the fall`, `21:57:34 the stream`,
`always how it works`, `00:00:00 quickstart`. Screenshots are evidence,
full-width, each with a one-line mono caption that reads like the app's
callouts (`✱ port scan · 21:50 …`, `dropped …`, `accept · drop …`). No
cards, no frames, almost no motion (one breathing live dot). The quietest
of the three; the words do the work.

### The tour — `tour.html`

The app's journey tour, taken outside. A short headline, then each
screenshot carries the tour's cyan rings; hover, focus or press one and
its sentence appears in the bar beneath, exactly as the app's tour bar
does. The same sentences sit under the picture as a numbered list, so
keyboards and phones (where the rings hide) lose nothing. The page
teaches the picture rather than describing the product.

## Scenes (`shots/`)

`<name>-top.png` (1440 wide, first screen), `<name>-full.png` (full
page), `<name>-phone.png` (400 wide, first screen), for each of
`fall-opened`, `docket`, `tour`. Captured by `capture.mjs`, all looked at;
fixed before this commit: fall lanes misaligned under wrapping headers,
the now-label colliding with column headers on the phone, the tour's
last ring sitting on the sentence bar, the docket's captions splitting
into columns, the tour losing its side padding on phones.

Served for review from a disposable nginx container with `docs/` mounted.

## Verdicts

Pending.
