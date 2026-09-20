# Log every rule — the ratified design (#1134, from #435)

Ruled by the owner on #1134, 2026-09-12, after walking v0.5.1. There are
no rounds behind this file: the page was built under #435 and this is the
owner's ruling on what was wrong with it, recorded where the next person
to touch the page will find it.

The page was called **Tune logging**, which named what it did to the
config rather than what the operator gets back, and the owner spent a
long time working out that it wanted a pasted export at all. It is
**Log every rule** now — nav label, page title and deck card. Three
things changed with the name. It rendered outside the deck on the
reading that it was a workflow stepped into and left, which left it the
only page in the app with no navigation on it; it is an ordinary deck
card now, taking the deck's own shell exactly as Entities and Settings
do, and no second shell was invented for it. Its input was a raw
`<input type="file">` beside a bare textarea; it is one drop zone now,
which is also the paste target and the click-to-browse target — drop,
click or paste, one control — showing the file name and the rule count
once something is in it, with the browser's native file control kept but
never shown. And it leads with what it is for, verbatim: *"Drop in your
router's export (`/export hide-sensitive`). You get it back with logging
switched on for every firewall rule that is not logging yet, ready to
paste into the router. Nothing you paste is stored."* The never-stored
sentence stays as a footnote under the drop zone rather than the
headline. Layout, top to bottom: lead sentence, router picker only when
there is more than one router, the drop zone, Analyse, results as
before.

Unchanged by the ruling: the two `/api/tune-logging` endpoints, their
paths and their `edit` tier gate. The rename is the page's, not the
API's, which is why the view key, the response types and the Go
handlers all keep the endpoints' spelling.

Superseded: the names "Logging gaps" and "Coverage fix" were offered
alongside and the owner chose "Log every rule"; #435's "deliberately
outside the deck" reading is closed — a workflow with no way out is a
trap.
