# Interface visioning — round 33: suggestions and matches

Under #691 (backend capability with no front-end home), phase 2, item 3.
Round 31's docket carries forward **verbatim** — same tabs, same table,
same drawers, same draft, mend, permit and remove — and gains the two
things the watchlist tab has behind `WATCHLIST_SUBTABS_ENABLED`
(`Watchlist.svelte:644`) and round 30 drew nowhere: **suggestions**
(what mikroview would watch, from what the routers pushed) and
**matches** (what each watch has seen). Both in
`suggestions-matches.html`. No sub-tabs: round 30 has none, and neither
thing needs one.

The rule the placements follow: **a suggestion is a watch that has not
been said yes to, and a match is a line in the watch's own drawer.**
Nothing opens a panel, a modal, a sub-tab or a second page.

## What is placed

1. **Matches** live where round 30 put the watch's single last-match
   line, at the foot of the drawer's story column. That one line grows
   into a short list — *what it matched · last 3 of 214* — of
   `when · source → destination:port · n× · rule`, and ends in a quiet
   `older ▸`. This is the one accepted thing that changes shape, and it
   changes by growing, not redrawing. `GET /api/matches?mac=…&limit=3`
   → `lastSeen`, `tuple`, `count`, `event`; `older ▸` is the same call
   with `until`. The backend queries matches by device (`mac`/`ip`),
   not by watch: the build asks for the watch's source and keeps the
   records whose `entryId` is this watch, or gets a per-entry query —
   that gap is filed (see #691's round-33 comment). A learning watch's
   list is the same data its *where it has reached* list groups by place
   (`firstSeen` → *since Sun*), so the build reads one from the other.
2. **Suggestions** are a second body of the same table, under the
   watches, under a quiet heading: *mikroview suggests · from what
   rb5009 and hap-ax2 pushed · 3*. Each is a row in the watch row
   grammar — name · boundary · window · state · last event — with a
   dashed chip that says where it came from: `◇ suggested — a new lease
   on iot`, `— from drop rule iot-no-smb`, `· stale — the list is gone`.
   `GET /api/suggestions?status=off` → `kind` (`device`, `port`,
   `addressList`), `name`, `justification`, `routerDevice`, `stale`.
   A suggested row sorts and filters with nothing: it is not a watch.
3. **The drawer** opens as a watch's does: the story says why (a lease
   nobody covers; a rule the router already enforces; a list that has
   gone), the side column shows *the lease / the rule / the list, as
   pushed*, and the verbs are `watch it — it learns first` (a device,
   `POST …/accept` → an inverted entry, observing, so it arrives among
   the watches as `◌ learning — nothing seen yet`) or `watch it — every
   attempt is a match` (a port → a non-inverted entry, `◉ watching`),
   and `not this` (`POST …/hide`).
4. **A stale suggestion** leads with the honest verb: `let it go` first,
   `watch it anyway` quiet. The chip and the side column say what went.
5. **Set aside** is the heading's right-hand pill — *2 set aside · show
   them* — which reveals the hidden rows in dimmer ink (`status=hide`),
   each with one verb, `bring it back` (`POST …/unhide`). Nothing is
   ever thrown away from here.
6. **`start over — wipe every watch`** is the heading's far-right quiet
   pill, and uses round 28's arm-then-confirm: one click reads `confirm — every watch
   goes, and it suggests afresh` in alarm ink; a second click is `POST
   /api/suggestions/reset {confirm: true}`; any other click disarms.
   Afterwards the watch body says *Started over* where the rows were,
   and the eye in the chrome reads 0.

## Deliberately not here

- No cross-watch matches view (`GET /api/matches?entries=all`, the
  *all entries* mode of the app's `MatchesTab`). Its home is not the
  docket: it is the stream with a *matched a watch* lens, which belongs
  to a stream round. Recorded on #691 as a follow-up.
- No `provisional` mark on a match: the field is always false today
  (`matchlog.go:152`; #406 wires it), so drawing it would be a #750-kind
  of gap.
- No source for a suggestion beyond the three kinds the backend
  generates (DHCP lease, drop rule with ports, address list).
- No suggestion for something already watched: the backend does not
  offer one (`status=on` rows carry `entryId` and are simply watches).

## Screenshots

`shots/` — captured by `capture.mjs`, viewed and clean: `watchlist`,
`matches-held`, `matches-learning`, `suggestion-device`,
`suggestion-port`, `suggestion-stale`, `accepted`, `set-aside-shown`,
`set-aside-drawer`, `brought-back`, `start-over-armed`, `started-over`.

No new data palette: the dashed `ink-3` chip is round 31's draft chip,
the accept arrives in round 31's learning and watching inks, and the
armed link is the alarm ink round 28 uses — the dataviz validator has
nothing new to check.

## Verdicts

2026-09-01, owner: "But yes, I like it over all. It's approved."

Noted while viewing: the owner could not find `watch it`, `not this`,
`show them` or `start over` at first. The first two sit at the foot of a
suggested row's drawer; the last two are the quiet links on the
suggestions heading. Owner, on lifting those two links: "Yes,
though you didn't explain start over" — so both now wear the drawer
verbs' pill, and the reset says what it does before it is clicked:
`start over — wipe every watch`.
