# Changelog

Notable changes to mikroview. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Removals before 1.0 are wholesale — no compatibility aliases or
stub commands are left behind (see `AGENTS.md`, "Removals are
wholesale"). This file is where they are communicated, so read it before
upgrading.

`0.1.0` was tagged on 2026-08-07 without its notes being cut, so
everything sat under Unreleased until `0.2.0`. Its section below is the
file exactly as it stood at the `v0.1.0` tag, split back out rather than
rewritten.

## [Unreleased]

### Added

- **A restart no longer throws away what mikroview has learned** (#795,
  Go half). Every five minutes, and once more at shutdown, mikroview
  writes a small snapshot of its derived state and reads the newest
  usable one back on the next boot: the hourline's per-minute counters
  and running totals, each detector's rolling 60-minute windows and
  per-source day bookkeeping, and every device's first seen, last seen
  and event count. Before this, a redeploy blanked the metrics register,
  reset every detector to its full warm-up, and quietly replaced months
  of "first seen" dates with today's.
  New settings, all on by default and all documented in
  `docs/configuration.md`: `snapshot.interval` (5m, 30s minimum),
  `snapshot.keep` (6 generations) and `snapshot.dir`
  (`snapshots/` beside the data directory), with `MIKROVIEW_SNAPSHOT_INTERVAL`,
  `MIKROVIEW_SNAPSHOT_KEEP` and `MIKROVIEW_SNAPSHOT_DIR` overrides. A
  cadence under the minimum or a retention under one generation is
  clamped with a named warning (CFG-0070, CFG-0071) rather than
  refused, and a snapshot directory that cannot be created or written
  costs one startup log line and no snapshots -- never a refusal to
  boot.
  **Events are still never written to disk.** A snapshot holds counts,
  minute stamps, rule and log-prefix labels, device ids/names with their
  first and last seen, and per-source window counts keyed by address. It
  holds no event lines, no payloads and none of the rule/NAT/DHCP tables
  routers push; SECURITY.md now says so beside the "no persistence for
  events" promise, including that the files are not encrypted at rest
  (#853). Snapshots stay files even on a Postgres deployment: they are
  derived, disposable counters, not custody data, and losing the whole
  directory costs one cold start.
  `GET /api/stats` gained `liveSince` (when this process started
  observing) and, after a warm restart only, `restoredTo` (when the
  snapshot it loaded was taken) -- absent rather than null on a cold
  start.
- **Tune logging** (#435): a new "Tune logging" page turns a dark
  connection -- one with no rule logging what crosses it -- into a
  watched one. Upload the router's `/export hide-sensitive`; once
  mikroview has observed the device for 24 hours, the page lists every
  filter rule that crosses an operator-chosen dark boundary, ticked by
  default, each shown with how often it has fired and how many bytes
  since observation began -- RouterOS's own per-rule packet/byte
  counters, now carried on the push -- as the cost of turning its
  logging on. Reached from the wizard's finish screen and from a dark
  pair on the topography's coverage lens. Rendering gets back the
  annotated export (download first, copy second) or the equivalent
  `/ip firewall filter set` commands, with a `beforeunload` guard until
  either is used. The change is mechanically checked before it is ever
  returned -- parse the config before and after, strip logging
  attributes, compare -- so it can only ever be logging, never anything
  else; a check failure answers 500 rather than shipping an edit nobody
  asked for. The uploaded export itself is never logged, persisted, or
  stored anywhere: it lives in memory for the one request and is gone
  once the page is left.
- **RouterOS commands are now version-aware, server-rendered, and keyed
  by dialect rather than by a single reviewed marker** (#436, Go half).
  `internal/routeros` gained a dialect table (`Rows`): a row is a version
  range, the dialect it renders, an honest `verifiedBy` ("exercised on
  CHR 7.23.3" vs. "release notes read 2026-08-29"), and any per-version
  note -- today one dialect ("a") across three rows, with 7.24.0 split
  out for its `find`-lookup bug. `ReviewedVersion` is now checked against
  the table's own newest row rather than carried only in prose.
  The six RouterOS command templates (CA trust, syslog, rule tagging,
  push script, schedule) moved out of the frontend into
  `internal/routeros/commands.go`, byte-identical to what they replace,
  behind a new `POST /api/setup/commands` (same access tier as
  `GET /api/setup/status`): it renders the table's bounds, what an
  operator-picked version resolves to, every router whose version is
  known and its standing, and the five command blocks themselves.
  `GET /api/devices` gains `routerosStanding` (`below-minimum` /
  `reviewed` / `ahead-of-review`, omitted when unknown) beside
  `routerosVersion`. The wizard's push script already appends
  `?ros=$[/system/resource get version]` to its `/ca.crt` fetch; mikroview
  now reads that (validated, capped, and used for nothing else) as a
  fallback version hint for the fetching address, so a router's version is
  known from the wizard's first step rather than only after its first
  push -- a real push always overrides the hint.
  `scripts/routeros-freshness.sh` now reports a stable release with no
  covering row, rather than only comparing against a marker.
- **The setup wizard names a multi-homed router's source-address
  split** (#442). A router holds an address on every network it routes,
  and its logs arrive stamped with whichever one faces mikroview --
  often not the one declared as `sourceIp`. The declared router then
  sits silent while the real stream auto-discovers under another
  address, and a token minted for the declared identity enriches
  nothing: pushes return 200 and the popups still say no data. Step 2
  (Send logs) now reads this as partial -- "Connected — but from
  10.0.20.1, an address you haven't declared, while 192.168.88.1, which
  you declared in config.yaml, has sent nothing" -- states that
  mikroview cannot tell whether the two are one router, and prints the
  fix with the operator's values: keep the declared address by setting
  `src-address` on the router's logging action (recommended, because the
  token and its pushed tables follow the declared identity), or change
  `sourceIp` and reissue the token. Two genuinely different routers is
  not an error; the notice clears itself when the declared one sends its
  first log. The router cards on Entities and the viewer's Fleet carry a
  one-line echo pointing at step 2. `GET /api/devices` gains
  `multihomedCandidates` on a configured device that has received nothing
  while undeclared devices stream -- every arriving address, never a
  pick. Detection itself shipped in #499.
- **The watchers station now lists what it has been told to expect**
  (#640, part C). An **Expected** verdict teaches mikroview that a
  certain amount of a certain thing, from a certain host, is normal
  here. Until now nothing in the interface said what it had learned, so
  a flag that stopped appearing was indistinguishable from a detector
  that had quietly stopped working. A new section under the detector
  bench (**Settings ▸ detection ▸ tune…**) lists every expectation:
  detector, host, the size recorded when it was made ("up to 30", or
  "any size" for a detector that declares no size), how many firings it
  has absorbed since, and when it was made. **Forget** on a row removes
  it and that pair raises again from its next firing. The absorbed count
  is the point of the list -- an expectation that has absorbed nothing
  for months is visibly not earning its place. Two new endpoints back
  it: `GET /api/flags/expectations` (any signed-in user, the same tier
  as `GET /api/flags` -- an expectation is the reason a flag you would
  otherwise see is absent) and `DELETE /api/flags/expectations/{id}`
  (user tier and audit-logged, matching the verdict that records one:
  the operator who can call a flag expected can take it back). A viewer
  reads the ledger and gets no Forget button. Nothing removed here;
  part B (above) retired the admin-only exclusions API this replaces.
- **A detector's candidate numbers can be tried before they are saved**
  (#786). Changing a threshold was a guess with no shown workings: the
  new value went live, and whether it was right showed up later as flags
  that did or did not arrive. A **Try** button now sits at the foot of an
  expanded detector row, beside Save. Pressing it replays the numbers as
  typed over the traffic mikroview still holds -- the detector's own
  logic, with the candidate window and threshold substituted for its
  stored ones -- and puts the answer in one slot under the fields. Either
  a receipt: *"Would have fired 3 times in the last 4h 12m"*, with the
  hosts that would have been flagged listed beneath it. Or a decline:
  *"Can't replay: needs a 6h window, only 4h 12m held"*, in the same
  slot, in grey rather than red, because a corpus shorter than the
  detector's own window is an honest limit of the traffic held rather
  than an error, and reporting "it would have fired zero times" instead
  would be a claim nothing established. Where the server says its read
  was cut short, the count reads "at least"; where the listed sample is
  bounded, the hosts read "at least these". Try writes nothing -- the
  detector the engine is evaluating is untouched, whether the receipt is
  encouraging or not -- and it never blocks Save. New `replayDefinition`
  wrapper over `POST /api/definitions/{id}/replay`, which existed
  server-side but had no caller anywhere in the client. One known limit,
  recorded rather than hidden: a candidate carries only the window and
  the threshold, which is the closed set the engine's replay accepts -- a
  detector's other tuning fields are saved as normal but are not part of
  what Try asks.
- **Try now says what the detector counts as it stands, beside the
  candidate's count** (#786). *"Would have fired 3 times in the last 4h
  12m — currently: 41"*: without the second number the first one settles
  nothing, since a count only means something against the count it would
  replace. It is measured, not looked up: `POST
  /api/definitions/{id}/replay` runs the same replay a second time with
  the detector's live params over the same traffic and returns it as
  `current`, because nothing already counted asks the same question (the
  flag time series counts newly-raised episodes over the last sixty
  minutes, and a flag's own count is re-fires within one episode). Where
  the live window is longer than the traffic held, the slot says so in
  the same grey the decline uses rather than showing a number nothing
  established. No new counter and no new storage; a Try with nothing
  changed carries no comparison, because its count is the current one
  already.
- **A live memory control for the event buffer, not just a config-file
  setting** (#796). Settings' memory group now carries a slider under
  the hours bar: dragging it only proposes a figure, and nothing changes
  until you press apply. Applying it -- from the UI, or directly via the
  new `PUT /api/settings/store` (admin-only, audit-logged as
  `settings.store_max_memory`) -- stores the figure and resizes the
  running ring immediately: growing it keeps everything already held,
  shrinking it drops the oldest events first. An out-of-range figure is
  refused with a 400 rather than clamped. Once set, the stored figure
  wins over `store.maxMemory` in `config.yaml` on every future restart
  too -- mikroview says so in the startup log, and deleting the new
  `store.settingsStorePath` document (`/var/lib/mikroview/settings.json`
  by default) is how you go back to the file's figure. The allowed range
  is worked out per host at startup: 32MiB at the low end, and at the
  high end whatever's left of the smaller of the cgroup memory limit or
  the machine's RAM once a quarter of it (or 256MiB, whichever's larger)
  is reserved as headroom and the same 1.47x ring-to-resident overhead
  CFG-0012 already quotes is priced in -- so the slider never offers a
  figure the host would be OOM-killed for taking, and never reports a
  deliberately large `config.yaml` budget as out of range. `GET
  /api/stats` gains a `memory` object (`maxMemory`, `min`, `max`,
  `hostTotal`, `bytesPerEvent`, `resident`, `stored`) so any reader sees
  the same numbers the UI does, and the setting is now included in
  `-backup`/`-restore` alongside every other store. A viewer sees the
  bar and the figure; only an admin is offered the drag.
- **A detector's thresholds and windows can be edited in the app**
  (#787). Until now the only thing the watchers station could change was
  a detector's *scope* -- which hosts, ports or rules it watches -- and
  even that was typed as comma-separated text. A detector whose
  threshold or window was wrong for a particular network could not be
  corrected anywhere in the interface. A row on the bench now expands
  downward in place (one open at a time; no side drawer) into its
  editing panel: typed tuning fields built from `GET
  /api/definitions/schema`, so every field's type, bounds, unit and
  description come from the server's own declaration rather than a
  second copy of every detector's knobs written in the frontend. A
  duration is edited as a plain second count and written back as the Go
  duration string the server validates. Each scope axis is now a set of
  removable chips with an add box that suggests what the app already
  knows -- hosts from Entities, rule labels from the router-pushed
  filter tables -- and the ports box takes a range (`8000-8010`) as well
  as a single port, refusing anything that is not a port with a reason
  instead of dropping it. The allow/deny/no-restriction select stays,
  and source classification stays a select because it holds one value,
  not a list. **Reset to stock** puts a detector's params back to
  exactly what it shipped with, leaving its scope alone (the server's
  reset is a params operation, and a button that also cleared an
  operator's host exclusions would be doing something nobody pressed it
  for). A viewer sees every row and every fact and no control at all --
  hidden, never disabled, the same grammar the run/pause tick already
  used. New `fetchDefinitionSchema` and `getDefinition` wrappers;
  `resetDefinition` and `cloneDefinition` have callers for the first
  time. **Clone** copies a detector you wrote -- its match conditions,
  its aggregation and its tuning -- into a second detector that appears
  paused, already expanded, with its name selected to be typed over, so
  authoring a variant is one press and a rename (#810). It is offered
  only on those rows: a shipped detector's logic is Go compiled into
  this binary and keyed by its own id, so a copy of it would list, look
  configurable and evaluate nothing -- the server refuses, and the
  button is not there to press. Overriding a shipped detector's params
  is the operation that exists for it; starting a custom detector *from*
  one needs a conditions editor and is filed as #829.
- **A demo seeder that exercises the whole interface, not just syslog**
  (#687). Every UI review this project has run was hampered by a demo
  that only ever sent syslog: one lane on the fall, no pushed rule/NAT/
  address tables, every stream row on the unnamed-host fallback,
  nothing named on Entities, an empty watchlist, a flat metrics
  hourline -- all read as UI defects when they were data gaps.
  `scripts/seed-demo.py` is a seeder against a running instance, in the
  repo rather than rebuilt from memory in `/tmp` each session: it pushes
  filter/NAT/address tables (with log prefixes, and two rules per
  router that are pushed but never fire) over `POST /api/ingest/
  routeros`, names hosts/rules/ports, creates four watchlist entries
  (two healthy, one paused, one with a genuinely broken ring --
  `internal/engine/coverage.go`'s `out-of-scope`), a user- and a
  viewer-tier account alongside the admin, and drives three admin
  mutations (a cleared flag with a note, an entity rename, a definition
  edit) so the audit log has rows. Its own `feed` subcommand replaces
  the ad hoc `/tmp` traffic generator with one that gives every host in
  its small, consistent estate a single stable MAC for the run --
  the earlier per-line-random-MAC version read as thousands of
  first-ever devices (measured: 4,025 `new_device` episodes from about
  8,000 events), which is what was making the docket, not the traffic,
  laggy. A fourth router is declared and never touched, so Entities'
  "quiet is a fact, not a fault" card is real. Country-flag data needs
  an operator-supplied MaxMind GeoLite2 database (`geoip.dbPath`) that
  this demo instance does not have configured; the seeder picks real,
  geolocatable public addresses so that flag is real the moment one is.
- **The scene bar built to its ratified round-29 design, the stream's
  retired toolbar folded into it, and the filter row rebuilt as one
  quiet row** (#683). Every scene's bar now reads MIKROVIEW · page ·
  strap · LIVE·rate (bare `LIVE` on the stream, whose whisper line
  already carries the rate) · `⚑ N` (docket) · the mockup's own
  eye-glyph watch marker, `N ○M` (watchlist) · account, ported field-
  for-field from `docs/design/concepts/round-29/the-whole.html` rather
  than approximated -- including on the fall, which hand-rolls its own
  bar rather than sharing `SceneBar.svelte`. The retired `[0d 0h 3m
  55s]` uptime counter and the per-router chips (`border (RB5009)` etc)
  are off every bar; round 29 draws neither. The previously-bare pink
  flag count (no `⚑`, no title, hidden at zero) now carries the
  ratified glyph, an accurate title, and shows even at zero
  ("no open flags", ok-coloured) -- the mockup's own "clear all" demo
  state, not invented. The stream's active filter now also shows on the
  bar as one bordered box (`action:drop boundary:iot→lan ⌫`), matching
  `.controls .search` exactly rather than as separate chip pills.
  `lib/watchlist.svelte.ts` gained `heldCount` (the same "enabled and
  not ring-broken" predicate `Watchlist.svelte`'s own `class:watching`
  already used) so the bar's count can never disagree with the page's.

  The filter row (`FilterBar.svelte`) is one quiet row on desktop --
  device · action · chain · proto · source ⇄ destination (scope,
  address and country) · port · interface · rule -- as dim micro-labels
  over hairline underlines, no boxes, no placeholder prose, a single
  `×` to clear and `▸` to fold back, fitting one line well inside
  1600px. `Presets` and `Export to CSV` are off the row: round 29 draws
  neither, so they are not homed here -- `FilterPresetsMenu.svelte` and
  `lib/export.ts` are untouched and still work, just unmounted.

  Two ratified pieces are not built, on the owner's explicit
  instruction not to invent a home for anything round 29 does not
  draw: the stream bar's own SPAN control (`15 m · 1 h · 24 h · 14 d`,
  the same pattern as the fall's), since the app has no existing
  time-window concept matching those four buckets; and the retired
  toolbar's other controls (`17/s`, `2% of buffer used`, `No limit ▾`,
  Autoscroll, Pause, Group, Clear) -- round 29's own `#s5` bar markup
  does not draw any of them, only the filter summary and (unbuilt) SPAN
  control, so building them onto the bar would have been inventing a
  home for them. All are recorded on the issue as gaps, each with what
  it does today and the nearest ratified thing, for the owner to work
  through one by one.

- **The docket's watchlist tab built to its ratified round-29 design**
  (#676). A table -- watch · boundary · window · state · last event --
  sits above the existing add/edit/invert/observe/promote workflow
  (now headed "Manage entries"), reading the same entries rather than
  replacing them. Rows open as drawers, matching the flags tab's own
  grammar: a plain-English story with a standalone headline, the
  entry's verbatim last matching line (from the persisted match log's
  own event, not composed), a detail panel, and pause/resume-watch plus
  open-in-stream actions. Pausing reuses the definition's existing
  `enabled` flag through the generic definition PUT (`setWatchlistEnabled`)
  -- no new route. Two ratified pieces are not built: a per-entry
  time-of-day "window" (every row honestly reads "always," since no
  entry carries a schedule) and the seven-night strip ("five kept
  nights · two empty"), which would need a new persisted nightly
  history and a schedule to call "night" against -- a data-model
  decision left to the issue, not invented here. "Mend — widen window"
  is withheld for the same reason: there is no window to widen.

- **Entities, built to round 29's ratified scene** (#675). Router cards
  lead the page -- one per pushing device, its live/quiet state, RouterOS
  version, rule/zone counts, and either its current push rate or a plain
  "quiet is a fact, not a fault" -- followed by a dashed invite card for
  the next router, carrying the standing promise (mikroview only ever
  receives, never connects out) behind a disclosure of the real RouterOS
  lines to paste. Below that, one table of every named and
  discovered-but-unnamed host -- lane, address, mac (elided; `private`
  when none is known), first seen, last seen, and the docket's own marks
  (new talker, watched, flagged, ring broken) -- renamed inline by
  clicking the name (Enter saves, Esc cancels). Replaces #647's page
  wholesale: the old add-entity form and the separate discovered-rules/
  -ports sections are gone, since the ratified scene has exactly one
  table and no other CRUD surface. mac/first-seen/last-seen had no
  existing source, so `device.MACRegistry` gained `NoteIP` (pairing a
  MAC with the IP it last answered to) and a new read route,
  `GET /api/devices/macs`; `GET /api/devices` gained each device's
  reported RouterOS version. Lane reuses the topography's own
  boundary-derived zones unchanged.
- **Settings' three previously-unbuilt rows** (#677). Detection gains a
  tunable port-scan window (`20 ports / 60 s`, editable next to "flag
  types ... tune..."), reading and writing the port_scan definition's
  own `threshold`/`window` params through the exact PUT
  `/api/definitions/{id}` the watcher bench already uses -- no second
  store. Memory gains a persistence row stating live truth on both
  halves: the ratified copy read `JSON store · 14 d`, but no event
  store with a day-based retention exists (`internal/persist`'s own
  package doc calls the live event stream the one deliberate
  in-memory-only exception) -- `internal/persist` *is* real, though,
  and backs flags/definitions/watchlist entries/entities/tokens, so the
  row instead states which backend is actually live (file, with its
  directory, or Postgres) via a new admin-gated `GET /api/persistence`
  (admin-tier for the same reason `GET /api/config/problems` already
  is -- a filesystem path is infrastructure detail), alongside the fact
  that the event buffer above it stays memory-only regardless. Account
  gains a sessions row -- "this device, signed in 4 d · sign out
  everywhere" -- backed by a new self-serve `POST /api/auth/logout-all`
  (`SessionStore.RevokeAllForUser` against the caller's own account,
  then a fresh session so the calling tab stays signed in, the same
  revoke-then-recreate shape `POST /api/auth/password` already uses),
  and `GET /api/auth/session` now threads the session's own `IssuedAt`
  through as `signedInSince`. The device name in the ratified copy
  ("tom-desktop") was mockup placeholder text -- `auth.Session` tracks
  no device/user-agent -- so the row says "this device" rather than
  inventing one.
- **The docket's flag drawer explains itself: a headline, a story, the
  episode's shape, a note on clear** (#678). Round 29's drawer gains the
  plain-English headline and prose story the mockup ratified -- "One
  source, twenty doors.", "A camera asking for a mail server." --
  generated per flag type from the evidence the flag already carries
  (`lib/flagNarrative.ts`), and the episode's shape -- still arriving,
  stopped, or intermittent, e.g. "first 13:46 · last 13:52 · still
  arriving" or "every ~2 m since 13:28" -- derived purely from the
  flag's own timestamps (`lib/episodeShape.ts`). Clear now opens an
  inline note field before clearing; the note travels to
  `POST /api/flags/{id}/clear` and lands on the same admin-mutation
  audit entry the clear itself already writes (#679's ruling that a
  clear's note belongs in the existing log, not a second one). The
  where value in a flag's collapsed row now opens the topography at the
  host it resolves to, rather than the live stream -- "watch this
  pathway"/"watch this source" is deliberately not part of this change;
  it depends on the watchlist tab #676 is still building.
- **Entities gets a tab strip back -- hosts, rules and ports** (#681).
  Naming a rule or a port in context turned out to have nowhere to
  happen: a rule the router has pushed but that has never fired has no
  row anywhere to click. The owner ruled naming-in-context out and asked
  for a tab strip over the one table instead, reusing the docket's own
  three-tab vocabulary rather than new furniture. hosts is the ratified
  table exactly as #675 built it, and stays the default. rules lists
  every rule in a router's pushed filter-rule table -- name, chain,
  action, last fired -- whether or not it has ever fired; a never-fired
  rule reads as "has not fired," never as a blank. That join needed a
  rule's log-prefix decoded into the slug an event from it would carry
  (`ruleLabelFromLogPrefix`, `lib/routerLookup.svelte.ts`, the inverse of
  the router-lookup popup's own `prefixMatchesLabel`), since `GET
  /api/rules` only ever holds a rule once it has fired. ports lists every
  port seen in traffic plus any already named. Inline rename is one path
  across all three tabs, same store, same `EntityType`, same
  Enter-saves/Esc-cancels/blur-saves behaviour #675 built.
- **Per-hour top talker and top port, answered by the ring itself**
  (#644). `Store.HourTops` computes each axis minute's winning source
  and destination port from the events the ring actually holds, under
  one RLock, walking backward and stopping at the hour's edge. A minute
  is answered only while the buffer still holds every event it received
  in that window -- once eviction reaches in, the minute reads as an
  honest em dash, never a count of the survivors, and ties break on the
  lower label so map order can't flap the answer. Served by its own
  route, `GET /api/stats/tops`, fetched only by the Metrics page while
  open rather than folded into the `/api/stats` poll every open tab
  makes every five seconds. The metrics table gains the two columns as
  plain, unsortable headers, with their hour-total footer cells staying
  em dashes.
- **The whisper commands the live stream, and the filter box folds to a
  thin bar** (#644). The whisper is a quiet full-width strip above the
  live table -- rate curve, drop share, top talker, top port -- that
  commands the stream: clicking the curve seeks it, turning off
  autoscroll and moving the stat line to the clicked minute, and a
  fence toggle plus two clicks dims every row outside the picked range,
  as a display lens rather than a second filter state beside
  FilterBar's own. On desktop, the filter box now defaults folded
  behind a "Filters ▸" trigger and slides out into one quiet row --
  device, action, chain, proto, source/destination, port, interface,
  rule -- with dim micro-labels over hairline underlines, a × to clear
  and a fold back; it still writes only `appState.filters`, the same
  grammar typing does.
- **The drum: Metrics' seismograph is one mirrored stroke per minute**
  (#644). The seismograph is rewritten from per-action horizon lanes
  into the drum -- one mirrored stroke per minute, clickable paper to
  select a minute -- where the outer half of each stroke is every event
  that minute and the inner half its refused share, both on one shared
  scale so a quiet minute's refused sliver can never draw taller than
  its own total. Per-action detail lives on in the register and the
  table, and the cursor's `aria-valuetext` still reads every action's
  figure.
- **The drum drops its leftover per-flag-type panel** (#644). The
  round-13 rewrite replaced the seismograph's per-action lanes with the
  mirrored stroke but left a FLAG EPISODES row per detector type
  underneath, inherited unchanged from the pre-drum `#488` build --
  something no round-13 through round-29 mockup of the seismograph ever
  drew. Removed; flag detail still reads from the register's flag
  columns, the table's flag-episodes column, and the cursor's own "N
  flag episodes" fact. The drum's left margin, which existed only to
  hold that panel's labels, now matches its right margin.
- **The topography map grows a floor, two health dials, an aggregate bar
  per card, and node information cards** (#648). An altitude slider at
  the foot of the map steps between clients, services, zones and survey.
  Two health dials sit top-right for flags and watchers, and clicking
  either one takes you straight to the docket. Each card also carries
  one aggregate bar, clicking through to the watchlist or to a
  pre-filtered view of flags depending on which card it belongs to. And
  a node's information card now opens the same way wherever that node
  appears on the map, rather than only from one place.
- **Every column on the docket now sorts and filters, across all three
  tabs** (#649). Flags, watchlist and audit log all gained the same
  behaviour: click a column to sort by it, click again to reverse, and a
  quiet filter sits under the heading. Flags and watchlist are card
  grids rather than tables, so there the same sorting and filtering
  appears as a toolbar over the same columns instead of column heads
  themselves. Filtering the flags list also renders it flat rather than
  grouped by campaign, so a match is never left hidden inside a
  collapsed group it happens to belong to.
- **A brand-new instance now has a first-run journey, choreographed door
  to wizard** (#646). Where an empty instance used to greet you with the
  ordinary sign-in form, it now opens on a door that offers Enter instead
  of a login box. From there the journey walks you through creating the
  first admin account, an attach step that shows the two commands the
  router needs, a held beat while that connection comes alive, and then
  a choice: skip the tour and go straight to the wizard, or take a
  card-by-card tour of the deck first. Most of what it walks through
  already existed as its own surface -- the journey is what strings them
  together into one continuous first hour, rather than adding a page of
  its own. Either path leads to the same place, the setup wizard, which
  now finishes by taking you to the fall, its landing page, instead of
  leaving you somewhere else.
- **Flags carry an operator's verdict** (#638). Every flag now offers
  Expected, Noise and Real, and the old Clear demotes to a secondary
  affordance beside them. Expected means legitimate traffic; Noise means
  real traffic caught by too sensitive a threshold. Both clear the flag
  on one press, with about five seconds to undo before anything is sent
  -- and a verdict still inside that window is committed rather than
  silently dropped if you navigate away, so the interface never claims a
  judgement it did not record. Real does not clear, because a confirmed
  concern is not something you have finished with. A judged flag shows
  its verdict, who judged it and when, and is never presented as an open
  question again; a flag that later re-fires as a genuinely new episode
  starts unjudged rather than carrying the old badge forward. Noise
  verdicts are recorded against the detector that raised the flag so
  that receipted threshold suggestions (#640) can later reason from
  them -- nothing tunes itself yet.
- **The fall is the landing page** (#616). A band per boundary --
  interface pair by default, named from the pushed rule's own
  src-address-list where a router provides one (e.g. "lan"), ordered
  input-chain/WAN-facing first, observed next, dark last. Since nothing
  yet names a full network-group taxonomy (LAN/SRV/GUEST/IOT) or a WAN
  interface, that ordering and naming stay evidence-based rather than
  invented -- see the #616 PR for the deviation from the ratified
  "group-pair + direction" design. Above a NOW line, a live spectrum
  strip positions a peak per carrier by port; below it, time pours
  downward as a thin dashed carrier per (boundary, port) -- brightness
  for accepted rate, red only for drops, violet for nat -- capped at the
  8 most recently active carriers per band with a "+n quieter"
  affordance, bucketed by span so a real cadence stays visible rather
  than one mark per event. A dark (unlogged) boundary is drawn hatched
  and says so in words, never colour alone; clicking a boundary or a
  carrier opens Stream filtered to it. Retires the interim landing from
  #544 -- Stream keeps its own row, second in the Live group.
  Reduced-motion turns off the NOW-line pulse.
- **A deployment's data can be moved between a bind mount and a named
  volume**, in both directions (#537). `mikroview -migrate-data
  <destination>` copies the whole data directory, verifies it by
  re-hashing what landed, and leaves the source untouched -- so a failed
  migration costs nothing but the copy. It carries the three things
  `-backup` deliberately leaves out (the TLS store, the recovery pepper
  and the Postgres adoption marker), because a backup travels to another
  host and a migration does not.

- **Custom detection definitions can now be created** (#502). An
  operator authors match conditions plus an aggregation -- key mode,
  counting mode, threshold and window -- and a detail sentence, and
  the detector evaluates on the ingest path and raises flags like any
  shipped one. Each definition's own view says whether the dispatch
  index can narrow it to the events it might match, or whether it is
  evaluated against every event instead. `POST /api/definitions`'s
  refusal of `intent=detection` is gone, since it is no longer true.
- **The learning shelf says when the record is still thin** (#640). A
  freshly-deployed mikroview flags plenty before it has learned what is
  normal, and until now nothing said why. While fewer than five
  expectations have been recorded, the shelf now reads *"Flags will be
  noisy until mikroview has learned what is normal here — N
  expectation(s) recorded so far. Judge each flag and the inbox
  settles."* — viewer-readable, since an expectation is not owner-only
  information.

### Changed

- **"Never flag this again" now records how much is normal, and comes
  back when a host outgrows it** (#640, part A: store and engine). It
  used to silence a detector on a host outright, forever, whatever the
  host did next. It now records the size of the firing you judged
  normal -- the measure that detector compares against its threshold,
  such as the number of distinct ports for a port scan -- and absorbs
  later firings up to one and a half times that size. Past that the
  flag returns, carrying both numbers, so it can say what was expected
  and what was actually seen rather than repeating a count you already
  looked at. Saying it is expected again raises the recorded size to
  the new firing; the size only ever goes up.

  Every shipped detector now states what its size is, or states that it
  has none. Detectors with no size -- device silence, known bad IP,
  unexpected mail sender, stale rule, and the two that judge a rate
  against a moving baseline (global spike, rule spike) -- keep the old
  meaning exactly: silencing one of those ignores that host on that
  detector outright. Silences recorded before this release have no size
  and keep that same meaning; nothing needs migrating.

- **Every flag now ends with a judgement: expected, checked,
  investigate or resolved** (#640, part B: API and the verdict row). The
  flags tab offers **expected · checked · investigate** on a fresh flag,
  and **expected · resolved** once something is being investigated. All
  four are available to any signed-in user, not just an admin, through
  `POST /api/flags/{id}/verdict`.

  - **expected** is what records an expectation now (see part A's entry
    below): normal for this host, at the size you just looked at.
  - **checked** means "looked suspicious, checked, fine this time". It
    clears the flag and suppresses nothing, but it is remembered: if
    the same detector fires on the same host again, the flag comes back
    saying *you checked this on 2 Sept and found it fine*.
  - **investigate** leaves the flag open while you work on it.
  - **resolved** means dealt with, normally by a firewall change. It
    clears the flag and deliberately does not suppress anything: a line
    only reaches mikroview if the firewall let it get that far, so a
    correct fix makes the lines stop. If the same circumstances recur
    the flag returns, reading *resolved on 2 Sept -- it's back*, because
    the fix was not what was intended. Wanting to keep logging those
    drops is an expected verdict at that rate, not a resolved one.

  A flag that returns past an expectation says so on its own row --
  "expected up to 30, saw 120", the two real numbers -- and saying
  expected again raises the recorded size. Undo still sits beside the
  stamp for as long as the flag carries the verdict, and undoing an
  expected verdict now withdraws the expectation it recorded rather than
  leaving a suppression standing behind a re-opened flag.

  Verdicts and their undo are audit-logged, carrying the verdict as the
  entry's detail. The exclude-forever action they replace was admin-only
  and logged; these are user-tier, so the record of who decided a pair
  stops being flagged matters more, not less.

  The expectations themselves are not listed anywhere in the interface
  yet -- one made by mistake can only be withdrawn by undoing the
  verdict while the row is still in front of you. The ledger that lists
  them, with absorbed counts, and lets you prune them is the remaining
  part of #640.

- **A verdict now writes to the watchlist as well as to the flag**
  (#641). Recognising traffic as legitimate and recording it as expected
  used to be two separate errands, so the second rarely happened.

  - **expected** records the destinations the flag actually saw -- each
    with the port it was reached on -- as permitted on the device's own
    inverted watchlist entry. If the device has no entry, one is created
    **observing**: it lists where the device goes and fires nothing, so
    an automatic step never arms a fence. No form and no extra click,
    because it is reversible -- undoing the verdict, or changing it to
    something else, takes the permissions back and removes an entry that
    existed only to hold them. Only what that verdict added is removed;
    anything permitted separately stays.
  - **resolved** offers instead of acting. The line it leaves behind
    reads *resolved — undo · watch for this*. Taking the offer opens the
    watchlist's own entry form, prefilled with the host (by MAC where
    the evidence carries one, otherwise by address) and the pairs the
    flag saw, and states where those values came from: which firing
    window, how many of how many pairs, and whether the watch is MAC- or
    IP-bound. Saving *or* discarding puts you back in the flags inbox,
    so declining costs nothing. The flag stays resolved either way.

  Why offer a watcher at all: after a block, the first packet that gets
  through matters more than the detector's threshold being crossed
  again. The detector brings a resolved flag back only when the host
  re-crosses its threshold; a watch fires on the first line that
  reappears.

  **outbound_anomaly and internal_recon now record the port alongside
  each destination**, which is what makes their evidence precise enough
  to permit or watch. Nothing is inferred by crossing a host list
  against a port list -- that would allow combinations the device never
  made -- so a flag from a detector that records no pairs permits
  nothing, and offers no watcher.
- **Metrics: the hourline reads every series, and the ledger sits above
  the table's minutes** (#803, design rounds 36-37). The line under the
  scene bar used to answer the minute under the cursor with a ratio that
  named two series and hid the rest ("9 refused of 61 events"); it now
  reads the whole minute in one line -- a figure per series, refused in
  the refused ink, and the flag-episode count followed by the type names
  behind it. That was already the hourline's job, so the register's
  cross-section aside is gone rather than duplicating it beside the
  paper, and the "Pick a minute on the register to read it across every
  series" instruction it printed when empty went with it.

  On the table view the ledger -- top rules, top talkers, by device, by
  protocol, the hour by action, episodes by flag type -- moves from a
  narrow column down the left side to a full-width band across the head
  of the view, ruled off from the minutes below it, with the table
  taking the whole width instead of what a 320px sidebar left it. Its
  six columns are bars without boxes: the bordered cards around each one
  are gone, since a box inside a ruled band draws the same border twice.

- **The docket's flags tab is the ratified round-29 table, not a card
  grid** (#688). One row per open flag -- flag · where · evidence ·
  count · age -- each wearing its flag type's own family ink as a left
  stripe and mark, and each opening as a drawer directly beneath itself
  rather than expanding a card. The drawer holds the plain-English
  headline and story, the episode's shape drawn as a tick strip, the
  matched log lines, and the two ratified actions: open in stream, and
  clear with a note. Every column still sorts by its head and filters
  from the quiet dashed row beneath it, and `where` still opens the
  topography at its sensible level. Two earlier passes put ratified
  language inside the old card instead of building the row-and-drawer
  structure the record draws; this ports the record's own markup and CSS,
  so the flags and watchlist tabs now read as one surface.

  Things the round-29 scene draws no home for have been taken off this
  tab rather than squeezed into it, and are recorded on #688 to be
  placed one at a time: the Expected/Noise/Real verdict row and its
  undo, per-flag confidence, the Exclusions tab (and the "permanently
  clear" action that fed it), campaign grouping of flags sharing a
  source, the 1/2/3-column density picker, the "Recently cleared" list,
  the "Active flags by type" summary, the per-flag abuse check, the
  reputation panel and the ports/hosts/NAT evidence rows. None of the
  code, API or stores behind them changed -- only what this tab shows.

- **The live table now shows the ratified nine columns, and NAT stopped
  being one of them** (#644). Stream's table is rebuilt around time
  (with milliseconds), action, source name, source address, destination
  name, destination address, proto, port and rule -- a name now carries
  the identity and its address sits dim and right-aligned beside it, and
  an unnamed external address shows bare, with its country code standing
  in for a name it doesn't have. NAT is no longer a column of its own; a
  NAT'd row carries an action badge instead. Rows have gone quiet too --
  no more full-row colour washes, a subtle band instead -- and the
  per-cell information buttons are gone, with that same detail now
  living in the row's own detail sheet, opened from the time cell and
  reachable by keyboard. The filter bar folds away behind a quiet
  trigger too, rather than sitting permanently open.
- **Entities and Settings join the deck, and Fleet's routers move in with
  them** (#647). The card deck now runs to seven: Entities and Settings
  take the two final places, and Fleet's routers table folds into
  Entities rather than staying a page of its own -- Fleet stops being
  where you go for them. The account menu, now that each of those has a
  home of its own, slims down to theme, Run setup, Sign out, and About
  and licence.
- **The fall's port spectra are stroke-only waves now, not filled
  triangles** (#650). Each carrier's live-spectrum peak used to draw as
  a small filled triangle; it now draws as a single stroked wave, with
  no fill and no baseline, so the spectrum strip reads as a line rather
  than a row of solid tents.

### Removed

- **An account with no `role` in the accounts file no longer defaults to
  `user`** (#873). The default existed for accounts persisted before
  roles existed (#653); every account mikroview writes has carried a role
  since. A roleless account can now only be hand-edited in, so it is
  loaded as-is and fails closed -- denied by every role gate, admin and
  user alike. Give the account a role in the file, or set one as an
  admin.

- **The rule-usage store no longer reads the bare-array
  `rule-usage.json` it wrote before `recordingSince` existed** (#873).
  Only the current object shape is read; an array-shaped document is now
  an unparseable document, which is a hard startup error by design
  (#378), so an instance still holding one refuses to start until that
  stale `rule-usage.json` is deleted.

- **A stored definition whose `kind` or `intent` this binary does not
  recognize is now a hard startup failure** (#873), naming the offending
  definition and its unrecognized value. It was previously preserved
  unevaluated rather than dropped -- the case a downgrade, or a shipped
  definition since retired, could produce. Pre-1.0 with no user base,
  downgrade is not a supported operation: delete the named definition
  (its data stays on disk until you do), or restore the binary that
  wrote it.

- **`watchlist.storePath` and `flags.detectorSettingsStorePath` are
  gone, with the boot-time migration that read them** (config keys,
  `MIKROVIEW_WATCHLIST_STORE_PATH` and
  `MIKROVIEW_FLAGS_DETECTOR_SETTINGS_STORE_PATH`). Both documents were
  migration sources only: the stores that owned them
  (`internal/watchlist.Store`, `internal/detect.SettingsStore`) were
  deleted in #405/#407, and nothing has written either since. Watchlist
  entries and detector toggles both live in the definitions store
  (`engine.definitionsStorePath`), which is what `-backup` carries. The
  server no longer opens the two documents at boot, so a read failure on
  either can no longer stop it starting. Remove the keys from
  `config.yaml`; unknown keys are ignored, and the files themselves can
  be deleted once a deployment has upgraded past #407. Upgrading
  straight from a pre-#407 release skips the one-time entry adoption:
  upgrade through a release that still had it, or re-create the entries.

- **The Noise verdict, the plain Clear control, "never flag this again"
  and the exclusions API are gone** (#640), wholesale, with no aliases
  and no stub endpoints. Each is replaced by a verdict, above:

  - **Noise** existed only to feed a threshold-suggestion generator the
    owner dropped: raising a detector's threshold hides real events for
    every host in order to quiet one. Gone from the UI, the API and the
    store; `POST /api/flags/{id}/verdict` now rejects `"noise"` as an
    unrecognised verdict rather than accepting it as anything.
  - **The plain Clear** (`POST /api/flags/{id}/clear`, and the drawer's
    "clear with a note") dismissed a flag without recording what you
    concluded. **checked** is that action with the conclusion kept.
    "Clear all" is unchanged -- it still clears every active flag in one
    request, records nothing, and suppresses nothing.
  - **"Clear and never flag this again"**
    (`POST /api/flags/{id}/clear-permanent`) silenced a (detector,
    target) pair outright, forever. **expected** does the same job
    bounded by the size of the firing you judged, so a host that grows
    past it comes back.
  - **The admin exclusions API** (`GET /api/flags/exclusions`,
    `DELETE /api/flags/exclusions/{id}`) and the Exclusions tab that
    read it are removed. Their replacement -- a ledger of expectations
    with their sizes and absorbed counts -- is the remaining part of
    #640; until it lands there is no screen or endpoint that lists
    recorded expectations.

  Expectations recorded before this release are untouched, and an
  operator's stored data needs no migration. Anything scripted against
  the four removed endpoints will now get a 404 or a 400 rather than a
  quiet no-op.
- **`Definition.Suppressions` is gone** (#640). The per-definition
  suppression list was modelled on the definition envelope in #401 and
  never given matching semantics -- nothing ever consulted it before an
  emission. #640 settles on one suppression mechanism, so the field is
  removed rather than left as a second, silent one: from the definition
  envelope, from `GET /api/definitions`' response, from
  `PUT /api/definitions/{id}`'s accepted fields (a request that sends
  `suppressions` now has that field ignored), and from the frontend
  types. Any value stored in an existing definitions document is simply
  not read back.

- **The `system` and `light` themes are gone** (#708), wholesale. Round
  30 -- the ratified design -- is dark throughout, and dark stops being
  a preference and becomes what the stylesheet simply is. The light/dark
  auto mode picker is removed from the theme menu; the `mikroview-theme`
  storage key, the `ThemePref` type, `cycle()` and the `system` fallback
  in `lib/theme.svelte.ts` are removed outright along with the file
  itself; and every light-mode CSS variable override and the
  `prefers-color-scheme` media query are removed from `app.css`. No
  alias maps `system` or `light` onto dark, and no stored preference is
  read even to migrate it -- a browser with an old `mikroview-theme`
  value in local storage now simply has an unread key. The colourway
  picker (Signal/Pulse/Nebula/Frequency/Mono) is unaffected.

### Fixed

- **`docs/configuration.md`'s API reference table had drifted from the
  authorization matrix** (#847). Several rows still said `admin-only`
  for routes #653's "watchers" bench ruling had widened to user tier
  months earlier (`/api/entities`, `/api/naming/provenance`,
  `/api/coverage/declarations/{key}`, `/api/suggestions*`), one said the
  opposite -- `GET /api/tokens` was narrowed back to admin-only by #657
  after the table was last updated -- and nine registered routes had no
  row at all. The table now states the tier `internal/api/authz_matrix_test.go`
  actually pins for every route in it.
- **The engine room's event-buffer row lost its live count** (#842).
  #823's memory slider replaced "8,412 of 201,000 events · ~9 h window"
  with "120 MiB · ~201 000 events · ~9 h at today's rate" -- the
  configured ceiling and a reckoning from it, with no number anywhere in
  the row that the server actually publishes as traffic arrives.
  `bufferRow()` now takes the live held count and prints it ahead of the
  ceiling ("120 MiB · 8 412 of ~201 000 events · ~9 h at today's rate"),
  and the engine room passes the buffer's current occupancy in.
- **Pages could scroll far past their own content into empty space**
  (#689). Metrics' own sr-only screen-reader region is `position:
  absolute` with no offset of its own, and none of the deck's wrappers
  established a positioning context, so the browser fell back to the
  region's CSS "static position" -- computed from the full, unclipped
  flow height of everything before it, ignoring every
  overflow:hidden/auto ancestor on the way. With nothing positioned
  between it and `<html>`, that became real document coordinates:
  scrolling far enough down left nothing on screen but the deck's fixed
  roll rail, on any scene sharing a card with Metrics (it is a snap
  neighbour of Topography and Stream as well as itself). Deck.svelte's
  `.card` is now `position: relative`, closing the gap for every scene
  that sits inside it rather than papering over Metrics' own case.
- **The stream's table had the ratified nine columns but not the
  ratified measure** (#685). #644 built the columns; this is the rest of
  round 29's stream scene, which the branch that built them never
  touched. Source, destination and rule were three equal flexible
  columns sharing leftover width evenly regardless of what each held --
  on a wide viewport that gave source roughly a third of the table for
  a short IP, while proto/port/rule were pushed toward (and sometimes
  past) the right edge. Rule is now the only flexible column; the rest
  hold fixed widths sized to what they actually carry (worked out by
  hand against the mockup's own rows, since `the-whole.html`'s table
  sets no widths at all and relies on plain browser auto-layout).
  Action badges shrink to the mockup's own small-flat numbers (`10px`
  font, `0 6px` padding, `0.06em` tracking, was `12.5px`/`3px 8px`/
  `0.03em`) -- the colors already matched. The thin vertical strokes
  floating over the header turned out to be the column resize handles:
  their tick mark had an explicit height with no `align-items` to
  center it, so it pinned to the top of the header instead of centering
  in it. Persisted column widths from before this change are
  discarded (`mikroview-column-widths-v4` → `-v5`) since three of the
  nine columns changed from flexible to fixed. A row on a flagged
  pathway carried a ⚑ mark in the time cell where the ratified table
  draws a full-row wash instead (`tr.hl`, `var(--alarm)` at 5%) --
  built as drawn, glyph removed. The name/address pairing and the row
  banding were already built to spec and are untouched.
- **The topography's furniture, landed against its ratified round-29
  scene** (#682). #648 built the dials, the lens selector, the zone
  cards, the edge labels, the altitude control and the ascend link; on
  screen none of it read as designed. The health dials sat on top of a
  top-right Traffic/Policy/Coverage tab strip that round 29 never
  draws there -- the strip is now the scene's own bottom-left bar,
  merged with the same typography the map's other chrome wears, and
  the dials sit alone, inset clear of the deck's roll rail. The
  watcher dial's ring wore a "◉" text glyph for its legend; it now
  draws the scene's own eye (a path and a pupil, ported from the
  mockup's dial markup). Zone cards' coverage badges (`LOGGED BOTH
  WAYS`, `DARK TOWARD WAN`) rendered in flat grey, one crammed
  sentence; they now carry the ratified three-way colour -- green
  logged, red dark, dim otherwise -- as two lines, badge over detail,
  with the aggregate bar moved below the card's own edge (as round 29
  draws it) so the second line never collides with it. Edge labels sat
  directly across the lines they annotated; they now sit off to the
  side, with a backdrop-matched halo for the cases still crossing
  something. The altitude control was a bare browser range input; it
  now wears a custom thin track and diamond thumb with its two
  extremes named ("clients" ... "survey", the middle stops staying
  tick-only). The ascend link floated as a bordered pill over the
  whole card; it now sits inside the map's own flow, top-left of the
  stage, as plain text. The boundary-derived note that used to float
  half off the map's bottom-left corner is now a bounded, backed pill
  stacked above the relocated lens bar, in the scene's own chrome
  rather than over the drawing. Degrading honestly when the `/ip`
  address table hasn't been pushed (#687) was already correct and
  stays untouched -- these fixes change how the furniture reads, not
  what it invents when data is missing.
- **The deck mounted a card's scene a card early and tore it down a
  card late, worst on the docket** (#690). `Deck.svelte` used to mount
  any card within one index of the active one, always -- so sitting on
  Stream paid a full mount of the docket's unvirtualised Flags list,
  and sitting on Entities or Settings paid its teardown, neither ever
  seen. A card's scene now mounts once it's the one actually visited,
  plus whichever neighbour the deck is physically rolling it into or
  out of view (tracked by a low-threshold `IntersectionObserver` with a
  lookahead margin, `lib/deckMount.ts`), so the roll still shows real
  content mid-transit without paying for a neighbour nobody scrolled
  toward. Measured against the live demo: the stream roll dropped from
  ~11 s to under 1 s, the docket's own roll from ~50 s to under 2 s,
  Entities from ~20 s to under 1 s, and Settings from ~14 s to under
  0.5 s.
- **Finishing the setup wizard could land you on a dead view** (#646).
  Its exit still pointed at the stream-as-landing-page arrangement that
  #616 retired once the fall took over as the real landing page, so
  every wizard exit -- not only the new journey's -- closed onto a view
  that no longer served that role. It now closes onto the fall instead,
  on every path into the wizard.
- **Watchlist matching against a real router could silently see nothing**
  (#614). Against a real RouterOS device, several firewall lines logged
  close together in a burst -- an input line and the forward/NAT line
  for the same packet, for instance -- arrived on the TCP syslog
  connection glued together with no delimiter, and got stored as one
  garbled event whose fields came from whichever embedded line the
  parser happened to read last. A watchlist entry scoped to a real
  port or device could go through an entire burst without ever
  recording a match, with nothing in the interface to explain why.
  Recommending `remote-log-format=syslog` in the RouterOS setup steps
  gives every message its own header, which the TCP listener now uses
  to split a burst into its individual messages eagerly rather than
  waiting on a quiet gap that a fast enough burst never has. A sender
  left on the previous default format has no header to split on and
  keeps the old behaviour.
- **A second `make live-check-remote` run could force-push over the
  first's branch and delete the tree it was still standing in** (#809).
  `scripts/gate-remote.sh` used one fixed branch (`gate-run`) and one
  fixed work tree (`~/gate-work`) on the host, with nothing to stop two
  runs overlapping -- the second run's push, reclaim and `rm -rf` landed
  on top of the first mid-run, and the tell was the bare repo's
  `gate-run` ref pointing at someone else's commit. The script now takes
  an atomic `mkdir ~/gate-lock` on the host right before it pushes,
  writing who holds it (host, user, ref, sha, pid, start time) to
  `~/gate-lock/owner`; a second run that cannot take the lock prints the
  owner and refuses rather than racing it. A lock older than 15 minutes
  with no `mv-gate-run` container running is treated as abandoned and
  taken over, printing what was found. Released in a `trap ... EXIT` so
  Ctrl-C, a dropped SSH connection or a normal finish all clear it
  without disturbing the run's real exit status. Separately, the
  checkout is now verified after cloning -- `git status --porcelain`
  and `git ls-files --deleted` must both be empty, or the run refuses
  before building rather than failing confusingly partway through
  `live-check` -- and a `RECLAIM` chown failure against an existing tree
  now prints a warning instead of failing silently.

## [0.4.0] - 2026-08-25

### Added

- **Matches is a tab of Watchlist, and it is one list** (#584). Every
  entry's matches in a single newest-first list, rather than hidden
  behind a per-entry expander you had to open one at a time. Each row
  says which entry it belongs to and which kind it is -- a port you
  watch was reached, or a device went somewhere it should not -- because
  the sentence to read differs and the entry name alone does not say
  which. The newest hundred, with **load older** to go back, the same
  way the live view pages. An empty tab distinguishes the two silences
  that look identical and are not: if any watch you have enabled cannot
  be checked because the rules it needs aren't logged, it says so and
  names it, rather than reporting a clean sheet.
- **A bounded query across every watch** (#586) --
  `GET /api/matches?entries=all`, newest first. Until now every match
  query needed a device identity, so asking "what has broken recently"
  meant already knowing where to look. It also reaches evidence nothing
  else could: a watch with no source set ("any source") had its matches
  recorded but retrievable only by an identity nobody knew to ask for.
- **The broken ring reaches the phone** (#583). A watch that has gone
  blind was invisible at small widths -- the ring had nowhere to live
  once the rail became a bottom bar. It now marks the group in the bar
  and again the page inside the half-sheet, so the next tap tells you
  which page carries the problem. A group is only marked where opening
  it resolves which page is at fault.
- **The NAT popup answers one of two different questions, and says which**
  (#445, design ratified 2026-08-22). RouterOS never states which NAT
  rule performed a translation -- but neither does it state which filter
  rule dropped a packet, and filter events resolve anyway, through the
  `log-prefix` the operator chose to put on the rule. NAT rules take the
  same tag, so tag one (`log=yes log-prefix="N|port-fwd|"`) and the "i"
  beside a NAT cell now names **that rule**: header "NAT rule --
  logged", a `logged` chip, the rule's ordinal, chain, action and
  comment, and the same shared-prefix honesty filter rules already get
  (two rules with one prefix show as two rules, never a pick). Leave it
  untagged and the popup cannot name anything, so it does not pretend
  to: header "NAT table -- *device*", a `not logged` chip, and the
  pushed table split into **"Could have performed it -- 3 of 14"** and
  **"Ruled out by this event -- 11"**, every exclusion carrying its
  reason in one clause (`ruled out: protocol udp ≠ tcp`,
  `ruled out: chain dstnat, event is srcnat`, `ruled out: disabled`).
  The ruled-out half is dimmed but fully readable and stays in the
  accessibility tree, because reading it is how you audit the split
  rather than trust it. A rule is excluded only by positive
  contradiction; anything mikroview cannot evaluate -- an address-list
  name, say -- keeps its rule and is shown as `not evaluable here`.
  There are no scores, no ranking and no "best match": RouterOS's own
  order is preserved because first-match-wins is real evidence, and even
  a sole survivor reads "1 of 14", never "this rule did it". The two
  modes never share a rendering, and an untagged translation never gets
  inline rule decoration on the row. Opening the popup holds the stream
  while it is open, so the row it is anchored to does not slide away
  under newest-at-top. Mobile gets the same thing as a section of the
  event sheet, in the same words. A push script predating the NAT rule
  anatomy shows the whole table and says so. Push your NAT table
  (`docs/routeros-setup.md`, step 4c) for any of it to have data to
  work with; nothing here contacts the router.
- **Rename hosts, ports and rules from the live view itself** (#413).
  The moment you recognise what a row is and the moment you label it are
  now the same moment: hover a row (or tab into it) and a pencil appears
  beside the copy glyph on the address, port and rule tokens, opening a
  small editor anchored to the token you were looking at. Type a display
  name, press Enter, and every row already on screen showing that same
  raw value changes with it -- no navigating to Entities, no reload. The
  raw value stays visible in the editor throughout and stays what
  filters, groups and copies use; leaving the field empty removes the
  name and shows the raw value again. Admins only, and a viewer sees no
  pencil at all rather than one that cannot do anything. The stream holds
  still while any row-anchored popover is open -- this editor and the
  existing lookup popovers -- and lets go when it closes, without
  changing what the Autoscroll button says it will do.

  **It will not accept a rename that would do nothing.** RouterOS still
  wins on host names (#186), so on a host your router already names --
  from a DHCP lease, a DNS static entry or a WireGuard peer comment --
  there is no text field at all. Instead the editor says which of those
  supplies the name, names the router to go and change it on, and, if you
  had already saved a name for that host, tells you it is saved but not
  what is shown. Previously that name was accepted, confirmed, and then
  silently never displayed.

- **The setup wizard is a modal now, and it is a claim ledger** (#487).
  Setup opens over whatever you were already looking at instead of
  taking you to a page, and it auto-opens once on a first admin sign-in
  with no router sending -- after the shell has painted, so you see the
  real (empty) app first. Five steps, always five, each ending in
  exactly one of three states: **done**, with its receipt (what arrived,
  when, from where); **skipped**, quietly, with the consequence stated
  rather than a telling-off; or **forced past**, in amber, recorded.
  Mikroview still never connects to your router, so every check is an
  observation of what arrived here -- which is why Next on a step that
  is still waiting does not fail, it explains that nothing has arrived
  and offers exactly two choices: keep waiting, or go on anyway with the
  exact record quoted on the button before you press it. That record
  reaches the audit log (`setup.step_forced`, `setup.step_skipped`) and
  the setup status every other surface reads, so an empty stream can say
  which decision accounts for its silence. If the evidence turns up
  later the step simply goes green; the audit line stays as history.
  Closing is explicit only -- ✕ or Esc, never a stray click outside --
  and nothing is lost by closing, because the ledger is rebuilt from
  what the server has observed every time you open it. **Admin ▸ Run
  setup…** reopens it at the first step still waiting. On a phone it is
  a full-bleed sheet with a button that flips between the step and the
  ledger, and the commands come pre-broken so nothing scrolls sideways.

  Those decisions **survive a restart**, in a new small JSON document at
  `setup.storePath` (default `/var/lib/mikroview/setup.json`, or
  `MIKROVIEW_SETUP_STORE_PATH`, or a row in Postgres where that is
  configured) -- the same optional-persistence contract as
  `audit.storePath`, and included in `-backup`. A restart is most likely
  at upgrade, which is exactly when someone is looking for the
  explanation, so an in-memory-only record would have gone missing at
  the worst moment. What the wizard has *observed* is deliberately not
  stored: that is re-made from arriving traffic every run.
- **Metrics is three views of one hour** (#488, design ratified 2026-08-23,
  `docs/design/screens/metrics/DESIGN.md`). The page now offers
  **Seismograph** (the default), **Register** and **Table**, chosen in
  the page header; the choice is a per-user preference, remembered and
  applied before the page first paints, the same grammar the rail's
  density already uses. All three read the same hour: seven traffic
  series and all sixteen flag types share one time axis, each with its
  own declared scale printed beside it and floored at 12/min, so a
  series that whispered all hour draws as a thread rather than being
  inflated to look busy. One cursor reads a whole minute across every
  series at once, moves with the arrow keys (Shift for ten, Home and End
  for the ends of the hour), and stays on the same minute when you
  switch views. Colour carries meaning and nothing else: two chart inks,
  traffic blue and refused red, with amber reserved for time -- the
  brink and the cursor. Identity is always the label, so every view
  survives greyscale and the Table proves it.

- **The engine room replaces the settings pages** (#490). Settings are no
  longer filed by noun: mikroview's own signal path is drawn as a live
  vertical diagram -- the door (syslog listener), the store, the
  watchers, the flags desk, the heralds -- with every setting on the
  station it governs, and two side doors beside it for who may look in
  and which machines may speak. Opening a station unfolds it in place
  rather than navigating away, so what feeds it and what it feeds stay
  on screen. Every number on the page is arrived traffic, not a chart:
  events/s at the door, events held in the store, flags open on the
  desk, so a setting is read against the thing it actually governs.
  It lives under Admin and, for the first time, **a non-admin can read
  it** -- the header declares READ-ONLY once and every verb is simply
  absent rather than greyed out.

### Changed

- **Exclusions is now a tab of Flags, and Suggestions is now a tab of
  Watchlist** (#547), closing the gap #544 opened when their own rail rows
  disappeared. Both are reached by the house tablist (arrow keys move
  between tabs) instead of a menu row or a page of their own, and the old
  `exclusions`/`suggestions` routes are gone -- no alias, no redirect.
  Exclusions carries a quiet, outlined count of the current permanent
  exclusions on its tab, distinct from the rail's own red flag count,
  which stays Flags' alone. Access is unchanged either way: Exclusions is
  admin-only inside a Flags page a viewer can otherwise use, and
  Suggestions is admin-only because the whole of Watchlist already is.

### Removed

- **The setup wizard page is gone** (#487), replaced wholesale by the
  modal above. The `setup` route is removed outright -- no alias, no
  redirect, nothing left that answers to it. Setup is reached from
  **Admin ▸ Run setup…**, or by the modal opening itself on a first run.
- **The metrics overlay charts and its three cards are gone** (#488),
  wholesale. The two multi-line charts (event volume by action, flags
  raised by type) and the ranked-count cards they sat above are removed
  outright -- no alias, no stub. Their answers did not go with them: top
  rules, top talkers, by device and by protocol are now the ledger,
  carried under the Register and as the Table's opening section, where
  they own magnitude and stop pretending to own time. The per-flag-type
  chart palette went with the chart that was its only user; flag types
  are named in words on every surface instead of cycled through sixteen
  hues.

- **The Users, Tokens and Detectors pages are gone** (#490), absorbed by
  the engine room: Users and Tokens became its two side doors, and
  Detectors became the watchers station's bench. Everything they did is
  still there -- adding and removing accounts, minting and revoking
  tokens with the same one-time secret banner, running and scoping
  detectors -- but the rail rows and the `users`, `tokens` and
  `detectors` routes are removed outright, with no alias and no
  redirect. The Detect group now holds Flags alone, so on a small screen
  tapping Detect goes straight to Flags instead of raising a half-sheet.

### Security

- **Three settings reads are now open to any signed-in user** (#490):
  the token list, the detector definitions, and the setup status, each
  widened deliberately and recorded as its own row in the route
  authorization matrix. This is what makes the engine room readable by a
  non-admin. **The account list is deliberately not among them** and
  stays admin-only, so the people door is absent for a viewer rather
  than read-only. No write changed: creating or deleting an account,
  minting or revoking a token, and every definitions write still refuse
  a non-admin. The raw value of a token still appears in exactly one
  place, the response to minting it, and in no GET at all.

### Added

- **A bottom bar and half-sheets replace the rail at small widths** (#550).
  The five groups render as a bar of buttons along the bottom of the
  screen, flag badge intact; tapping a group with more than one page
  raises a half-sheet listing them (focus-trapped, closed by Esc or the
  browser's own Back button), and a single-page group goes straight to
  its page. Dock and density stay pointer-width affordances — neither
  renders on the bar in any form.

- **Users, Tokens, Fleet and Entities are pages under Admin now** (#548),
  reached the same way as every other row in the rail rather than through
  a menu overlay. **Run setup…** stays an action, not a page -- since
  #487 (above) it opens the setup modal, retiring the interim view
  switch this shipped with.

- **The live view's filter bar can now filter by everything the table
  shows you** (#438). A few gaps, closed:

  - **Chain** has a proper picker now (`Any chain`, the built-in RouterOS
    chains, plus any custom one your rules use), and clicking a chain in
    the table shows up in it — before this, clicking a chain cell quietly
    applied a filter you had no way to see, change or clear.
  - The single **"IP or CIDR"** box is now two: **Source** and
    **Destination**, each next to its own internal/external switch. Type
    a name, a bare address or a CIDR block into either — a name or
    address fragment matches both the label you see and the underlying
    address, live, so renaming a device updates what it matches. A small
    **⇄** button between them swaps everything you've typed, scopes
    included, for when you filter the wrong side.
  - **Port** now also takes a service name (`https`, `ssh`, `mikrotik
    api`...), not just a bare number.
  - **Rule** now also matches the friendly name you gave a rule, not just
    its raw label.
  - The **NAT** translated address and the **interface** in/out pair are
    click-to-filter now, the same as every other value in the table.
  - The **country flag** next to an address is click-to-filter too, into
    a new country picker (one per side). Where MikroView couldn't
    determine a country, the row still shows and the picker offers an
    **Unknown** entry to find those rows on purpose, rather than them
    just being unreachable from the bar.

  Saved filter presets and bookmarked filter links from before this
  change lose whatever they had in the old IP box — it isn't carried
  over into the new Source/Destination fields automatically, so double
  check a preset that used it still does what you expect.

### Changed

- **The "known bad IP" flag now only fires on traffic your firewall let
  through** (#555). It used to fire on every packet from a blocklisted
  address, and since your firewall blocks most of that, the flag mostly
  reported the firewall working correctly — burying the one case worth
  your attention. A blocklisted address that got *in* still flags; one
  that was dropped or rejected no longer does. Expect to see this flag
  far less often, which is the point.

  Two details. Lines from rules that only log or only tag a packet
  (`L|` and `M|`) no longer flag either, because they do not say what
  happened to the packet — the rule that actually decided does, and that
  is the line now judged. And where MikroView cannot tell what happened,
  including NAT rules and any rule without a log prefix, it still flags:
  it would rather tell you about something that turned out to be blocked
  than go quiet about something that got in. See "Log your accept and
  drop rules" in `docs/routeros-setup.md`.

  Unchanged: a blocklist match still strengthens other flags for the same
  address — port scans and slow scans are built almost entirely from
  blocked traffic, and that evidence is still counted.

### Added

- **The chrome now has its own connection-lost, loading and first-run
  states** (#549). A dropped connection turns a small dot at the top of
  the rail alarm-red, and a banner tops the content column and pushes it
  down rather than covering it — navigation stays fully usable while
  disconnected, and both clear the moment the connection recovers. Hide
  the rail down to the edge tab and the banner alone carries the state;
  the tab was never given a second job. A page whose data hasn't arrived
  yet — the live view, the fleet table — now shows a few placeholder rows
  instead of "Waiting…" text or a spinner, and a table that comes back
  confirmed empty because no RouterOS device has ever sent anything
  points you at **Admin ▸ Run setup…** (or, if you can't reach that
  yourself, at whoever can) instead of leaving you guessing whether
  something is broken.

- **The rail now shows how many flags are waiting for you** (#546). A red
  count sits on the Flags row whenever there are open flags, and
  disappears once there are none left — so an empty count never sits
  there looking like something to deal with. Flags that you cleared, and
  ones whose detector and target you excluded permanently, are not
  counted. Hiding the navigation does not hide the count: it moves to the
  tab on the left edge of the window, so docking the rail never docks the
  alarm. Screen readers hear the row as "Flags — 6 open" rather than a
  word and a loose number.

  Only Flags carries a count of this kind. That is deliberate: a red
  count that appears in several places at once stops meaning "look here".

- **Watchlist now wears a red outline when it can't do its job** (#546).
  The ring appears the moment an enabled watch can't be checked — no
  firewall rule you've pushed logs the traffic it needs — and names why:
  "Watchlist — 3 watches can't be checked: the firewall rules they need
  aren't being logged" (singular wording for one). It clears itself the
  moment you log the missing rule; there is nothing to acknowledge or
  dismiss. A watch mikroview simply has no evidence about, or one that's
  out of scope for every rule that does log, never rings — only a
  definite "this can't work" does. Switching a watch off takes it out of
  the count too. Like the flag count beside it, hiding the navigation
  down to icons only tightens the ring around the icon rather than
  hiding it; unlike the flag count, the ring stays with the page and does
  not follow the rail to the edge tab when navigation is hidden.

- **The navigation rail can now be narrowed or hidden** (#545). It has
  three states you choose from the two buttons at the bottom of the rail:
  full width with icons and labels, a 54px strip of icons alone, or
  hidden entirely. Hidden leaves a small tab on the left edge of the
  window — click it, or press Tab twice from the top of the page and
  Enter, and the rail comes back exactly as you left it. Your choice is
  remembered per browser and applied before the page draws, so the rail
  never flickers between states on load. New installs start at full width
  on wide screens and icons-only on narrower ones; nothing is ever hidden
  for you automatically.

  Bringing the rail back with the edge tab is deliberately temporary: it
  lasts for as long as you are on the page, and a reload returns to
  hidden. Only the two buttons in the rail itself change what is
  remembered. Every rail row also gained an icon, so the icons-only state
  has something to show; hovering **or** keyboard-focusing a row names it.

### Changed

- **Navigation is now a permanent left rail instead of a hamburger menu**
  (#544). Every view is one click away and always visible, grouped as
  Live, Investigate, Detect, Expect and Admin. The menu's other contents
  moved rather than disappearing: **Export to CSV** is now in the live
  view's filter bar next to Clear filters, where it acts on exactly the
  events you have filtered; **account actions** (change password,
  connect SSO, sign out) and **About & licence** are in the rail's
  footer; and on phones the **display duration** selector moved to the
  filter bar too. The version number is no longer shown in the chrome --
  it is in About & licence, `GET /api/healthz` and `mikroview -version`,
  as before.

### Removed

- **The hamburger menu is gone** (#544), wholesale, along with its
  overlay. Nothing is aliased or stubbed behind it; see the moves above
  for where each of its contents now lives.

- **The Users and Tokens account/token-management overlays are gone**
  (#548), wholesale, along with the rail's account-actions-as-a-modal
  pattern that opened them. Nothing is aliased or stubbed behind them —
  see Added above for where the same management now lives.

## [0.3.1] - 2026-08-23

### Changed

- **Mikroview now refuses to start when its data directory is unusable**
  (#536), instead of starting and discarding every change on the next
  restart. Each store used to log an error and carry on -- so the
  interface worked, a watchlist entry you added appeared, and it was
  gone after a restart. Startup now checks every configured store, and
  a refusal names the store, the path and the fix. An unset path is
  still a supported "do not persist" choice, and Postgres deployments
  are unaffected.

- **Mikroview now refuses to start rather than silently replacing a TLS
  certificate authority it cannot use** (#535). The generated CA is what
  your router trusts before it will send logs over TLS, so replacing it
  stops ingest entirely -- and the old behaviour made that invisible:
  a CA that was present but unreadable was treated as no CA at all, so
  mikroview minted a new one, overwrote the original, and logged the
  same line it logs on a first run.

  Two cases now stop startup, each naming the file and the fix: the
  store directory cannot be written, and CA files that are present but
  cannot be read or parsed. An empty directory is still a normal first
  run. To start over with a new CA deliberately, delete the files in
  `tls.storePath` and restart.

  **If mikroview stops starting after this upgrade, it was already
  regenerating your CA on every restart and you had no way to see it.**
  The usual cause is the data directory not being writable by uid
  `65532` -- the shipped `deploy/docker-compose.yml` mounts the
  `mikroview-data` volume at `/var/lib/mikroview` for this; a bind mount
  needs the permissions step in README.md's "Persistent data" section.

- The startup log now says **"reusing the stored local CA"** when it
  reuses one, instead of saying "generated a local CA" every time
  regardless (#535).

## [0.3.0] - 2026-08-23

### Added

- **One definitions API** (#407): `GET /api/definitions` and its
  siblings, the operator-facing half of the evaluation-engine
  unification (`docs/decisions/evaluation-engine.md`). A shipped
  detector and a watchlist entry are the same thing to the engine -- a
  definition -- and they are now the same thing over HTTP: one list,
  with each definition's enabled state, scope, tuned params, declared
  param schema, provenance, suppressions, replayability, and (for an
  expectation) its coverage answer.

  What is new rather than merely moved:

  - **Param overrides, validated against each definition's own declared
    schema.** A threshold outside its bounds is a 400, not a stored zero
    read back later as though it were configured. `POST
    /api/definitions/{id}/reset` discards every override in one call;
    editing a shipped definition keeps it shipped, with the response
    saying exactly how far from stock it now is.
  - **Replay over the API** (`POST /api/definitions/{id}/replay`): "what
    would this have done?", answered from the stored event corpus with
    candidate params, returning the emission count, a bounded evidence
    sample, and -- mandatorily -- the window it actually covered. A
    definition that cannot answer honestly (an inverted expectation,
    whose judgement comes from an observation period measured in days
    against a corpus measured in minutes) declines with its reason
    instead of reporting a misleading zero.
  - **`GET /api/definitions/schema`**, so the UI renders tuning controls
    from the server's own declaration rather than a second copy of every
    definition's knobs written in TypeScript.
  - **The five definitions that never had a toggle** --
    `unexpected_mail_sender`, `stale_rule`, `known_bad_ip`, `netclass`
    and `reputation` -- are listed, switchable and scopable for the
    first time. They ran as always-on passes with no name and no switch
    before v0.3.0's engine port gave them the same envelope as everything
    else; the Detectors page now shows all seventeen.

- **Definition changes take effect on the very next ingested event
  again** (#407). Toggling a detector became restart-effective as each
  one was ported onto the engine during v0.3.0 (the port was documented
  as such at the time, not hidden), because a definition reads its
  enabled/scope when it is built and that happened once at startup. An
  edit now re-registers the affected definition immediately -- and only
  the affected one, so an unrelated save no longer resets any other
  definition's half-full counting window or warming baseline.

- **The RouterOS push schema carries more of each rule** (#408). Pushed
  filter rules now carry `connectionState`, `inInterface` and
  `outInterface`; pushed NAT rules carry the full rule anatomy
  (`toAddresses`, `toPorts`, `dstPort`, `protocol`, `inInterface`,
  `outInterface`, `srcAddress`, `dstAddress`, `disabled`, `dynamic`);
  and every push may state the router's own RouterOS version on the
  payload (`routerosVersion`, read from `[/system/resource get
  version]`).

  Nothing reads any of it yet, on purpose. Each field is input for work
  that is deliberately later — connection-state for the "which rules can
  actually feed this view" answer, the NAT anatomy for a NAT popup that
  separates rules an event could have hit from ones it could not (#445),
  the version for warning that a command was written against a different
  RouterOS (#436) — and all three are worth designing against data that
  has genuinely been flowing rather than data assumed into existence.

  Every field is optional: an older push script against this build
  leaves them unset and is accepted exactly as before. The documented
  upgrade order is unchanged — update MikroView before the script, never
  the other way round. `docs/routeros-setup.md` step 4c and its field
  table carry the new lines, and the setup wizard emits them.

- **Two new action categories, `marked` and `natted`**, so mangle mark
  rules and NAT rules stop being reported as "unknown". RouterOS's
  non-filter rules produce log lines with no accept/drop/reject verdict
  in them, and every one of those used to land in the unknown bucket —
  on a policy-routing deployment (mangle rules steering traffic into a
  VPN tunnel) that is a permanent, steady dribble, which leaves "unknown"
  meaning "not a filter rule" instead of "worth investigating". Both
  categories carry through the parser, the store's `byAction` totals and
  time series, the API, and the live view's action filter and badges.

  The log-prefix convention gains two letters to go with them: `M` for a
  mangle mark rule and `N` for a NAT rule, alongside the existing
  `A`/`D`/`R`/`L`. Tagging is what identifies a mangle rule — RouterOS
  prints nothing in the line that distinguishes one from a filter rule,
  so an untagged mangle rule still shows as "unknown", the same as an
  untagged filter rule always has. See `docs/routeros-setup.md` step 3.

  One case needs no tagging: a `srcnat`/`dstnat` line carrying RouterOS's
  translated-address annotation is read as `natted` directly, because the
  line itself states that the packet was translated. Nothing else is
  inferred — a line the parser cannot genuinely classify stays "unknown",
  which is the point of the change rather than a limitation of it.

  Existing `A|`-tagged NAT rules keep reporting `accept` until they are
  re-tagged with `N|`; nothing about them breaks. (#437)

- **An uptime readout in the toolbar**, next to the connection
  indicator: how long the server has been running, counting live,
  visible on every view rather than tucked into a menu. The number
  comes from the server once and counts on locally, re-syncing every
  minute so a restarted server cannot keep showing its predecessor's
  uptime for long. `/api/healthz` grows an `uptimeSeconds` field
  alongside the existing human-readable `uptime` string.

- **A new persisted store for the evaluation engine's baseline state**
  (`internal/engine.StateStore`, part of the v0.3.0 engine unification
  -- see `docs/decisions/evaluation-engine.md`), so a statistical
  baseline can resume warm across a restart instead of losing its whole
  warm-up every time mikroview restarts. A new `engine-state.json` file
  appears under mikroview's data directory (`engine.storePath` in
  `config.yaml`, `MIKROVIEW_ENGINE_STORE_PATH` as an env var override,
  defaulting alongside every other store under `/var/lib/mikroview`) and
  is carried by `-backup`/`-restore` from day one. Nothing in this
  release registers a definition against it yet, so on a running
  mikroview the file stays empty regardless -- this lands ahead of the
  definitions that will use it (a deliberate sequencing choice, the same
  one `Flag.Provisional` and `matchlog.Record`'s equivalent field
  already took) so the persisted shape and its round trip are proven
  before anything depends on them.

- **The definitions store** (`internal/engine.DefinitionsStore`, issue
  #404, another piece of the v0.3.0 engine unification): one document,
  on both the JSON-file and Postgres backends, holding every definition
  the evaluation engine will run against -- the twelve shipped
  detectors' enabled/scope state and default params, an operator's
  watchlist expectations, and (once the builder UI lands) fully custom
  definitions, all in one place instead of the two separate documents
  `internal/detect` and `internal/watchlist` keep today. A new
  `definitions.json` file appears under mikroview's data directory
  (`engine.definitionsStorePath` in `config.yaml`,
  `MIKROVIEW_ENGINE_DEFINITIONS_STORE_PATH` as an env var override,
  defaulting alongside every other store) and is carried by
  `-backup`/`-restore` from day one, the same day this store exists --
  #372's lesson, applied before an operator could ever be caught out by
  it rather than after.

  The first time a running mikroview reaches this new store with no
  document of its own yet, it is seeded once from whatever is already on
  disk: the detector settings store's enabled/scope toggles land as
  overrides on their matching shipped definition, and every watchlist
  entry becomes an expectation definition -- a non-inverted entry
  becoming a declarative one, an inverted entry (including one still
  mid-observation, with its recorded candidates and promoted
  destinations) becoming a programmatic one, exactly as it was, nothing
  reset. This migration only ever reads the old documents; it does not
  delete or modify either one, and both keep working completely
  unchanged for now -- `internal/detect` and `internal/watchlist` are
  not ported onto the new engine in this release, that is still to come.
  It also only ever writes the new document after everything above has
  already succeeded in memory, so a source document this version cannot
  read or parse refuses the whole migration outright (naming the file)
  rather than starting from a partial or empty result -- the same
  fail-closed policy issue #378 established for every other store in
  this codebase, extended to cover a conversion failure partway through,
  not only a read failure at the start. And nothing operator-authored is
  ever silently discarded: a definition this version of mikroview
  cannot make sense of at all -- the shape of things to come from a
  newer release, encountered after a downgrade, or a shipped definition
  a future release has since retired -- is kept exactly as stored and
  marked unavailable rather than dropped, on every write this store ever
  makes from then on.

### Changed

- **Live view: newest event at the top, not the bottom** (#363). The
  table used to append new rows at the bottom and autoscroll down to
  follow them; it now inserts at the top and autoscroll holds the view
  there instead. Ungrouped rows and Group mode's collapsed rows both
  follow this -- a group still keeps the position of its *first*
  arrival rather than jumping around as it's hit again, so nothing
  reorders while you're reading it. Turning Autoscroll off still holds
  a scrolled-back view exactly where it was, with rows that don't move
  or renumber under you as new events arrive elsewhere in the buffer.

  If you use the live view unfiltered as a moving feed, the newest
  traffic is now at the top of the screen instead of the bottom.

### Fixed

- **The setup wizard's syslog step reported "done" on a bare TCP
  connect, before any TLS handshake completed** (#371). `noteConnection`
  fired from `ServeTCP`'s accept branch in `internal/syslog/tcp_listener.go`
  -- before `handleTCPConn` ever read a byte, and `tls.Listener.Accept`
  negotiates its handshake lazily, on first read, not on accept. A
  router configured with `check-certificate=yes` against a certificate
  that didn't cover its address -- the exact misconfiguration the
  wizard's own CA-trust step exists to catch -- connected at TCP,
  failed the handshake, and sent nothing, yet the wizard still rendered
  "A router has an open syslog connection: done" and sent the operator
  off to fix firewall `log=yes` rules that were never the problem. A LAN
  port scan or a plain TCP health check against the syslog port produced
  the identical false "done". The hook now fires from inside
  `handleTCPConn`, past a completed `tls.Conn.HandshakeContext`, so a
  bare connect or a failed handshake no longer satisfies the step --
  while a genuine handshake with no logging rule configured yet still
  does, keeping that state distinct from "never connected" (see
  `setup.Store.NoteSyslogConnection`'s doc comment).

- **`close-issues-on-dev.yml` no longer closes an issue over a negated or
  code-quoted keyword** (#503). Its closing-keyword regex matched
  `close`/`fix`/`resolve` anywhere in a merged PR's title+body, with no
  regard for what came before or around it -- `Not fixed: #371` and
  `Does not close #363` both read as closes, and so did a keyword
  quoted inside a code span purely to explain that it had been removed
  (`` `Closes #442` ``), even though GitHub's own closing-keyword
  parser skips code spans. All three false positives happened for
  real on 2026-08-22 (PRs #496, #497, #499) and wrongly closed issues
  #371, #363 and #442 while their work was still open; all three were
  reopened by hand. The matching logic is now
  `.github/scripts/close-issues-matcher.js`, tested against the actual
  PR bodies that broke it (`.github/scripts/close-issues-matcher.test.js`,
  fixtures in `.github/scripts/fixtures/`): a keyword immediately
  preceded by a negation ("not", "doesn't", "never", ...) no longer
  matches, and markdown code spans/fenced blocks are stripped before
  matching runs. A genuine `Closes #NNN` trailer, negation-free and
  outside code, still closes as before.

- **A definition update touching only `enabled` or only `scope` could
  silently revert a concurrent change to the other field** (#494, a
  narrower survivor of #380 item 4 that outlived the engine port).
  `handleDefinitionsUpdate` filled in whichever of the two a request left
  unset from a snapshot taken *before* the client-paced request-body read
  -- an admin's enabled-only toggle, mid-flight while another admin's
  scope-only change landed, would write its own stale pre-read scope back
  over that change. Fixed by re-reading fresh state and writing under one
  lock spanning both, the same get-fresh-state/mutate/write shape
  `engine.DefinitionsStore.UpdateExpectation` and `RecordObservation`
  already used for the broader case #380 item 4 originally reported.

- **A router with `remote-log-format=syslog` set and a non-UTC system
  clock had every event's displayed time off by its clock's offset**
  (#379). RouterOS's BSD syslog output carries no timezone at all --
  `internal/syslog/envelope.go`'s parser took the bare wall-clock digits
  as literal UTC, so a router on Europe/London during BST (UTC+1)
  logging 14:00 the instant the message arrived at 13:00 UTC produced an
  event timestamped an hour into the future, in the live view, the CSV
  export, and every timestamp-windowed query. The receiving host's own
  clock is trusted and known accurate, so the parser now infers the
  device's real offset from the gap between it and the device's
  self-reported time -- rounded to the nearest 15 minutes, since every
  real-world UTC offset lands on that grid -- instead of assuming the
  gap is zero. A UTC-clocked router (the documented default deployment)
  sees no change: ordinary network delay still rounds to no correction.

- **One transient accept error no longer permanently deafens the HTTPS
  listener** (#380 item 3). `tlssniff.Listener`'s accept loop treated
  every `Accept` error as terminal -- hitting the process's
  file-descriptor limit (EMFILE/ENFILE, surfaced as a temporary
  `net.Error`) killed the loop's goroutine for good, and `http.Server.
  Serve`, which retries a temporary error by calling `Accept` again
  following its own documented contract, then blocked forever on a
  listener with nothing left to ever deliver a connection or another
  error. The web UI and the whole API stopped accepting connections
  permanently while the process stayed up and logged nothing further.
  Now mirrors `internal/syslog`'s existing capped-exponential-backoff
  accept retry: a temporary error is retried, and only a genuinely fatal
  one ends the loop.

- **`maxTCPConnections` is safe for concurrent access** (#380 item 6).
  It was a plain `int` read by `ServeTCP`'s accept loop on every
  iteration; a test shrinking it to exercise the rejection path raced
  that read with no Go-level happens-before edge between them --
  `go test -race -count=2 ./internal/syslog/` failed 3/3 with a data
  race. Now an `atomic.Int64`, the same fix already applied to its two
  neighbours (`maxTCPConnectionsPerSource`, `tcpIdleTimeoutNS`) and to
  the same bug class in #45.

- **Postgres match-log timestamps come back in UTC, not the server
  process's local zone** (#380 item 7). `pgx` v5's default
  `TimestamptzCodec` scans a `timestamptz` using `ScanLocation`
  `time.Local`, which doesn't change the instant a record's `FirstSeen`/
  `LastSeen` represent but does change how it serializes -- `+01:00`
  instead of `Z` on a non-UTC host -- so a record's own timestamps and
  its embedded event's `receivedAt` (decoded from JSON, always UTC)
  rendered as the same instant two different ways in one response body,
  and a consumer keying or grouping on the timestamp string (the
  birdcage correlation case #29 exists for) matched on the file backend
  and failed on Postgres. `PostgresStore.Query` now normalizes both
  fields with `.UTC()` after `Scan`, matching every other timestamp
  mikroview emits.

- **The watchlist stops claiming "nothing anywhere is watching this"
  when it has not read every router's rules** (#367). Coverage answers
  from the filter tables routers have pushed, and the push is optional —
  so a router that streams syslog and is actively producing matches, but
  never pushed, was silently missing from the evidence. One other router
  with a non-logging table was then enough for every entry to be
  labelled "no firewall rule on any router you have connected has
  logging turned on", while the excluded router's matches were visible
  on the live view next door. Both definite negatives (no-logging and
  out-of-scope) now degrade to saying nothing at all unless every device
  that has actually carried events has pushed its table, and the entry
  list names the devices whose rules went unread. A positive answer is
  unchanged: one router demonstrably logging the right traffic stays
  true however many others went unread.

- **A failed filter refetch (or initial load) no longer reads as "no
  events match"** (#373). `refetchWithFilters()` re-queries the server so
  a filter that misses the client's ~20,000-event buffer (an older
  event, a device that hasn't logged recently) still finds a real match
  in the server's larger store — but a rejected request (a 503, a
  dropped connection) left `events` exactly as it was, with nothing
  recording that the query never completed. The live view then rendered
  that untouched, incomplete buffer as a definite "No events match the
  current filters" (or, on first load, the equally silent "Waiting for
  events…"), telling the operator traffic didn't happen when the truth
  was that mikroview couldn't ask. `appState.fetchFailed` now records the
  failure on both call sites and clears on the next successful one; the
  live view shows an honest "could not load" message instead of asserting
  emptiness whenever it's set.

- **The watchlist page can no longer show torn observation data**
  (#376). `GET /api/watchlist` handed out entry copies whose observed
  and permitted lists still pointed at the live ones, so an entry in
  observe mode with traffic arriving — or a promotion landing at the
  same moment — could render a count from one moment beside a
  last-seen from another, in exactly the list an operator reads to
  decide what to promote. Self-correcting on the next poll, and never
  persisted wrong, but wrong on screen while it lasted.

- **WireGuard peer pushes are no longer refused when a peer has more
  than one allowed address** (#443). RouterOS holds `allowed-address` as
  an array, so the documented push script produced a payload the server
  rejected outright (`cannot unmarshal array into Go struct field
  WireguardPeer.allowedAddress of type string`) — the whole
  `wireguard-peer` kind failed on any real router, taking peer-based
  host naming with it, while the other seven kinds landed fine. The
  schema now takes the array RouterOS actually sends, and a
  comma-joined string as well, so a script written against either
  version of the docs works unchanged. Every allowed address a peer
  holds now names traffic from it, not just the first: a peer routing
  two branch subnets reads "branch office" on both.

- **A stalled storage backend could freeze flag reads, rule-usage
  reads, and the new-device MAC lookup -- all API-served state -- not
  just the write that triggered it** (#377). `internal/flags.Store`,
  `internal/rules.Store` and `internal/device.MACRegistry` each
  persisted synchronously while holding the same lock every read goes
  through: a backend that stopped responding (a blackhole, an
  overloaded database, a long lock wait -- the kind of failure a clean
  disconnect error does not cover) meant every subsequent `Add`/`Touch`/
  `Seen` call, and every read behind it, blocked for as long as the
  backend stayed stuck. A related defect made it worse under load: the
  rate limiter protecting these hot paths stamped its "last write" clock
  *before* attempting the write, so a write that itself ran long bought
  no back-off at all -- a stalled backend under sustained traffic was
  retried once per event, not once per debounce window. Both are fixed
  at the root by a new shared helper (`persist.WriteBehind`): a mutation
  now only ever encodes a snapshot under its own lock before handing it
  off, one writer goroutine does the actual backend call off that lock
  entirely, every backend call carries a deadline, and the debounce
  clock is stamped after an attempt completes, not before. Applied to
  the three stores above plus `internal/detect.SettingsStore` and
  `internal/watchlist.Store`, which had no deadline on their backend
  calls at all before this. Each store's public behaviour is unchanged;
  the practical effect for an operator is that a struggling storage
  backend now degrades to "this change might take a few seconds longer
  to become durable" instead of freezing flags, rules, or watchlist
  reads across the whole API while it lasts.

- **`-backup` silently dropped the entire watchlist -- entries, the
  suggested-entries pool, and the match log -- and `-restore` gave no
  sign anything was missing** (#372). `backedUpStores` (`backup_cli.go`)
  is a hand-maintained list pairing each store with where it lives on a
  JSON deployment, and it had drifted three fields behind
  `config.Config`: `watchlist.storePath`, `watchlist.matchLogPath` and
  `watchlist.suggestionsStorePath` were never added to it, even though
  this file's own doc comment and this document's backup section both
  claimed the envelope carried "everything." An operator following the
  documented disaster-recovery path -- `-backup` before an upgrade,
  `-restore` if it goes wrong -- got a file that looked complete, a
  restore that reported success, and a watchlist that had quietly gone
  back to empty. All three are now carried. Fixing this also surfaced a
  second, latent bug on the way in: the match log is a newline-delimited
  JSON file, not a single JSON document like every other store here, and
  embedding it directly the way the other stores are embedded made
  `-backup` fail outright the moment a second match was ever recorded --
  caught before it shipped, by a round-trip test that populates two
  match-log records rather than the one that would have passed either
  way. It is now base64-wrapped going in and unwrapped coming back out,
  which needed no format change and stays fully compatible with backups
  taken by earlier builds. Two exclusions are now written down explicitly
  rather than being an accident of the list never mentioning them: the
  TLS certificate/key directory (`tls.storePath`) and the GeoIP database
  (`geoip.dbPath`), alongside the pre-existing recovery-pepper exclusion
  (#97) -- see `docs/configuration.md`'s "Backing up and restoring"
  section for what each one is and why. A new test
  (`TestBackupCoversAllConfigPathFields`) now fails the build if a future
  `*Path` config field is added without an explicit backup decision, so
  this can't drift the same way twice. **Every backup taken before this
  fix is missing the watchlist** -- if you have watchlist entries,
  suggestions, or match-log history you care about, re-run `-backup` now
  that you've upgraded; nothing about restoring an old backup was
  destructive, it just wouldn't have brought the watchlist back.

- **A store whose on-disk document exists but can't be read now refuses
  to start mikroview, instead of quietly running on near-empty state and
  then overwriting the real file** (#378). Every persisted store --
  accounts, flags, the MAC registry, rule usage, entities, API tokens,
  the audit log, the watchlist, watchlist suggestions, and detector
  settings -- followed the same pattern: a document that failed to load
  or parse still handed back a store with its backend attached, and
  mikroview logged "continuing with in-memory-only X" and kept running.
  That log line was false. The backend was still live, so the very next
  time that store persisted -- often within seconds of boot -- it wrote
  its near-empty in-memory state straight over the operator's actual
  data. A missing document is unaffected and still boots as a normal
  first run; only a document that exists and can't be loaded now stops
  the process, with an error naming the store, where its document lives,
  the underlying cause, and the remedy: restore from a backup, or
  deliberately move the document aside to start fresh. `mikroview
  -restore` is unaffected by this -- it writes store files directly and
  runs before any store is opened, so it remains the way back in when a
  document has been corrupted.

- **Typing an `/api/...` address into the browser now returns the API's
  JSON instead of loading the interface.** The app's service worker
  answers page navigations from its cached shell so the UI opens
  instantly, and it was doing that for *every* typed address --
  including API endpoints and `/ca.crt`, which belong to the server.
  The UI itself was never affected (its own requests are not
  navigations), which is why this only surfaced when an operator tried
  to read `/api/stats` directly. Reported 2026-08-15.

- **Logging out, changing your password, or deleting an account now also
  ends that account's open live-tail connections**, not just its ordinary
  requests. A `/api/ws` connection was only ever authenticated once, at
  the moment it opened, and was never checked again for the rest of its
  life -- so a session cookie that had just been signed out, rotated by a
  password change, or deleted along with its account kept receiving live
  firewall events over that socket regardless, contradicting the "signed
  out immediately" promise the interface already made for both of the
  first two. The connection now re-validates its session every 30
  seconds (the same interval it already used to ping the browser) and
  closes cleanly the moment that check fails, bounding exposure to at
  most one interval instead of leaving it open indefinitely. Reported by
  the post-release review of v0.2.0 (#375).

- **Auto-discovered router registration no longer collapses log ingest
  once device discovery fills its cap** (#370). `device.Registry`'s
  `pruneLocked` walked and re-sorted the *entire* device map on every
  single `Resolve` call, with no guard at all, and then evicted back to
  exactly the 4096-entry discovery cap -- so once that cap filled, the
  very next newly-seen source IP overflowed it again and paid the full
  walk once more, and so did every one after that. `Resolve` runs
  synchronously on the single ingest goroutine for every ingested event,
  so this sat squarely on the fast path: measured at roughly 108us/call
  at the cap against 0.4us/call empty, enough sustained cost to back up
  the raw ingest channel and start silently dropping real router log
  records -- the tool failing at its one job, quietly, under exactly the
  conditions (an unauthenticated flood of source addresses, or simply a
  busy fleet) it exists to withstand. This is the same
  evict-to-exactly-the-cap defect commit 3d27200 fixed for
  `MACRegistry.pruneLocked` and `rules.Store.pruneLocked` (#285); the
  device registry was missed there because its map interleaves
  non-evictable configured devices with evictable discovered ones,
  which the earlier fix's shared `internal/evict` helper can't prune
  directly. `pruneLocked` now checks the registry's size against the cap
  in O(1) before doing anything else, and, once past it, evicts a batch
  below the cap rather than exactly to it, so a shed leaves headroom and
  the next new source is free. Configured devices are still never
  evicted.

- **A firewall log line's TCP-flags/ICMP-type field is now length-capped
  and stripped of control characters, like every other field mikroview
  extracts -- closing a gap that reopened #285's largest memory finding**
  (#369). That field comes from the parenthetical after `proto` in a
  RouterOS log line -- `proto TCP (SYN)`, `proto ICMP (type 8, code 0)`
  -- and it was the one extracted field the original clamp missed. A
  crafted line could put over 65KB into it alone, retained in every event
  slot, returned in the API's JSON, and quoted in any flag notification
  it triggers: the same **12.5GB resident at the documented 120MiB
  budget** overrun #285 fixed for the raw log line, reopened through a
  field the earlier fix's field-by-field list never named. Real values
  (`SYN`, `SYN,ACK`, `type 8, code 0`) are a few bytes long, so nothing
  genuine is affected.

- **A long log line arriving over syslog-over-TCP could turn into
  several garbage, undecoded events instead of the one real one it
  actually was** (#415). The TCP listener's read loop only recognised a
  message as continuing into the next read when the current one exactly
  filled its 64KB buffer -- but TCP promises nothing about how a message
  gets sliced into reads, so a message *smaller* than 64KB that still
  happened to arrive fragmented across several non-full reads (a TLS
  record boundary, ordinary segmentation under load) had each fragment
  handed to the parser as its own line. None of the fragments carry any
  framing of their own, so each produced noise in the live view and in
  detection rather than the one genuine record. Found concretely: a
  single ~65KB line over the real TLS listener produced 3 stray events
  where it should have produced 1. Fragmentation is a property of the
  network path, not of what was sent, so this could happen to any
  sufficiently long legitimate line -- a verbose NAT detail, a long
  address-list name -- crossing a segment boundary at the wrong moment,
  intermittently and with no pattern an operator could pin down. The
  read loop now reassembles by message framing instead: newline-
  delimited input is accumulated across as many reads as it takes and
  split on the delimiter itself, and RouterOS's undelimited bare
  messages (#202) are still recognised per message, resolved by a brief
  quiet period on the socket rather than by whether one read happened to
  fill a buffer. The existing 64KB per-message bound, and what happens
  when a line genuinely exceeds it, are unchanged.

## [0.2.0] - 2026-08-14

### Removed

- **`/api/detectors` and `/api/watchlist/entries`** (#407), replaced
  wholesale by one definitions surface (see Added below). Every route on
  both is gone -- `GET /api/detectors`, `PUT /api/detectors/{name}`,
  `GET|POST /api/watchlist/entries`, `PUT|DELETE
  /api/watchlist/entries/{id}`, and the promote/observing actions --
  with no alias, no dual reading, and no handler kept alive to return a
  friendlier error, per `AGENTS.md`. A stale caller gets a plain 404.

  The replacements, one for one: list/read a detector or an entry ->
  `GET /api/definitions` (and `GET /api/definitions/{id}`); toggle or
  scope a detector -> `PUT /api/definitions/{id}`; create/update/delete
  an entry -> `POST /api/definitions`, `PUT /api/definitions/{id}`,
  `DELETE /api/definitions/{id}`; promote/observing -> `POST
  /api/definitions/{id}/promote` and `.../observing`.

  Nothing is lost in the move, including on upgrade: every existing
  watchlist entry, every recorded observation and every promoted
  destination is carried into the definitions document on the first boot
  after upgrading, and the match log is untouched. The watchlist
  document is now a migration source only -- still read, never written,
  still carried in `-backup` -- and can be removed after one clean
  upgrade.

- **`GET /api/watchlist/matches`, renamed to `GET /api/matches`** (#407).
  Same query parameters (`mac`/`ip`/`since`/`until`/`limit`), same
  response, same access (any signed-in user, and reachable with a
  read-only API token). It is a query over the match log rather than
  anything to do with entries, and leaving one lone route behind on a
  prefix whose noun had been retired is the kind of half-removal
  `AGENTS.md` is about. Update any external correlation script's URL;
  nothing else about it changed.

- **`internal/watchlist.Store`** (#407) -- the second persisted document
  holding the same entries the definitions document already held. Not an
  operator-facing change beyond the routes above, but it is why the
  entry set now has exactly one home: an entry's enabled flag, scope and
  params lived on its definition while the entry itself lived somewhere
  else, and nothing structurally stopped the two disagreeing.

- **`spamhaus_edrop` as a blocklist source** -- Spamhaus merged EDROP into
  DROP on 2024-04-10, and the endpoint now returns only a "this list has
  been merged" comment with no ranges at all. mikroview had it
  **enabled by default** and fetched it daily, parsed zero entries, and
  logged that as a successful refresh -- so an operator saw a source
  switched on and apparently working while it contributed nothing.
  Coverage is unaffected (DROP absorbed EDROP's data years ago); what
  changes is that the product no longer advertises protection it was not
  providing. Removed wholesale per `AGENTS.md` -- a config still naming
  `spamhaus_edrop` gets the usual unknown-source error rather than a
  silent alias.

  `Blocklist.Refresh` now also warns when *any* feed fetches cleanly but
  yields no usable entries, so the next retired feed is noticed in days
  rather than years.

- **The plaintext syslog listeners** (#189): `listen.syslogUdp`,
  `listen.syslogTcp`, `internal/syslog/udp_listener.go`, and the
  plaintext `ListenTCP` entry point in `tcp_listener.go`, along with
  their config keys, env vars (`MIKROVIEW_LISTEN_SYSLOG_UDP`,
  `MIKROVIEW_LISTEN_SYSLOG_TCP`) and CLI flags (`-syslog-udp`,
  `-syslog-tcp`). Wholesale, per `AGENTS.md` -- no alias, no listener
  kept alive only to log a warning. Syslog over TLS (#188) is now
  mikroview's only syslog listener, and was shown to work repeatably
  against a real router first: sustained real traffic over an hour-plus,
  survives a mikroview container restart, and survives a full router
  reboot, all without manual intervention.

  RouterOS's `remote-protocol=tls` requires 7.18 or later, which is now
  the effective minimum RouterOS version for mikroview's whole setup
  guide, not just this listener.

  The syslog TLS listener is also no longer gated behind `tls.enabled`:
  it now loads/generates its own certificate independently, so a
  deployment that disables mikroview's own HTTP TLS (its own reverse
  proxy terminates TLS for real clients) doesn't lose syslog ingest as
  a side effect -- RouterOS connects to the syslog port directly, never
  through that proxy, so it needs a certificate to trust either way.

- **`store.maxEvents`** (#244), replaced by `store.maxMemory`: a memory
  budget (`"120MiB"`, `"500MB"`) rather than a raw event count, along
  with its env var (`MIKROVIEW_STORE_MAX_EVENTS` → `MIKROVIEW_STORE_MAX_MEMORY`)
  and CLI flag (`-max-events` → `-max-memory`). Wholesale, per
  `AGENTS.md` -- no alias, no dual reading of the old key. An event count
  meant something different by four or more orders of magnitude between
  deployments (MikroTik firewall rules do not log by default, so the
  rate mikroview actually sees is set entirely by which rules an operator
  turned logging on for) -- measured on one real instance, the old
  200,000 default held under three minutes against a configured 24h
  retention, invisible anywhere until someone went looking. A memory
  budget is the thing an operator can actually reason about across that
  spread: it's what they set on a container, and mikroview derives the
  rest and reports it back (`GET /api/stats`, and now the live view's
  toolbar directly). See `docs/configuration.md`'s "How events are
  stored" and CFG-0011/CFG-0012 for the full reasoning.

- **The Control Ports tab and `GET /api/critical-ports`** (#243),
  replaced wholesale by the watchlist (see Added below). The
  `flags.criticalPorts` config key is unchanged and still feeds the
  unrelated `critical_port` behavioral detector -- only the HTTP
  endpoint that re-exposed that same list for the now-deleted tab, and
  the tab itself, are gone. No alias, no dual UI, per `AGENTS.md`.

### Added

- **A Group option in the live view** (#341). A toolbar button, off by
  default, that collapses repeats of the same connection into one row
  carrying a count -- so a host retrying the same thing four hundred
  times costs one line instead of four hundred. Two connections count as
  the same when their source, destination, port, protocol and outcome
  all match, which is strict enough that nothing is merged that an
  operator would want to read separately. Nothing is discarded: the
  counts account for every event the ungrouped view would have shown,
  and clicking a grouped row opens it to reveal its events (the most
  recent 20, with the remainder stated rather than silently dropped).
  Rows belonging to an active flag carry a marker in both modes. The
  view keeps behaving exactly as it always has until the button is
  pressed, and the choice is remembered per browser.

- **A guided setup wizard for connecting a router** (#320). Menu →
  **Connect a router**, admin only. Every command comes out with your own
  values already in it — the address you are reaching MikroView on, the
  syslog port this instance actually listens on, and an ingest token the
  page creates for you — so there is nothing to fill in and no
  placeholder to leave behind by accident. Each step then tells you
  whether it worked, because every step ends with your router arriving at
  MikroView: the certificate download, the syslog connection, the first
  events, each pushed table. MikroView still never connects to your
  router. The first step checks that MikroView's certificate covers the
  address you are using, which is worth its own mention: getting that
  wrong otherwise shows up three steps later as a router-side error about
  name verification, when the fix is on MikroView's side.
  `docs/routeros-setup.md` remains the reference for what the wizard
  emits and why.

- **Ingest tokens can be created in the interface** (#326). The API
  tokens dialog previously made read-only tokens only, while the setup
  guide told you to create an ingest one there — so the documented path
  did not exist and only the `curl` alternative worked. Worse, a
  read-only token pasted into a router push script fails with a bare
  `404` and nothing anywhere says "wrong kind of token". The dialog now
  has a kind chooser and, for ingest, a list of the routers MikroView
  knows about; the token list shows each token's kind and router, so one
  can no longer masquerade under a misleading name.

- **The Watchlist can watch a whole address list from your router**
  (#274). If a firewall rule already scopes by an address list, MikroView
  suggests watching traffic from the addresses in it — and the entry
  follows the list as the router changes it, rather than freezing
  today's members. That matters because RouterOS edits these lists
  itself; an entry built from a snapshot would be wrong the first time
  it did.

  Only lists a rule actually references are suggested. A router has
  plenty of lists for routing and bookkeeping, and suggesting all of them
  would be noise — a rule referencing one is the operator saying that
  group matters.

- **A renewed certificate can be picked up without a restart** (#294).
  Send MikroView `SIGHUP` — `docker kill --signal=HUP mikroview` — and it
  reloads `tls.certFile`/`tls.keyFile` on both the HTTPS listener and the
  syslog listener. Certbot and cert-manager both have a deploy hook for
  exactly this.

  Previously MikroView read the certificate once at startup and never
  looked again, so anyone renewing automatically served an expired
  certificate until they happened to restart — and a router set to
  `check-certificate=yes` stops sending its logs at that point, which is
  the outage you would least want to find out about late.

  It does not watch the files and reload on its own, deliberately: it
  cannot tell a finished renewal from one still being written, and half a
  certificate is worse than an old one. A reload that fails leaves the
  working certificate in place rather than dropping to none.

- **You can change your own password from the interface** (#294). There
  was no way to do it at all before: it meant `-recover-admin-account`
  on the host, so anyone who suspected their password was known could do
  nothing about it themselves. **Menu → Change password.**

  Changing it also signs out everywhere else, immediately — that is the
  point rather than a side effect, since the other half of "someone has
  my credential" is "someone has my session". You stay signed in where
  you made the change. An account that signs in through your identity
  provider has no local password to change and does not see the option.

- **Sessions now have a maximum age** (#294): seven days from signing
  in, however often you use MikroView in between. `auth.sessionTTL` is
  an *idle* timeout, so a session used once a day never expired — a
  browser left signed in on a shared machine stayed valid indefinitely.
  Configurable as `auth.sessionMaxLifetime`; set it to `0` to go back to
  no ceiling.

- **The Watchlist tells you when an entry can never match** (#274). An
  entry showing no matches used to be ambiguous between "nothing
  happened" and "nothing here is even watching" — the second being a
  configuration mistake you had no way to see. Where MikroView can be
  certain, the entry now says so and what to do about it: either no
  firewall rule on any connected router has logging switched on, or your
  rules do log but none of them covers what that entry watches.

  **It stays quiet unless it is certain**, which is most of the time.
  This needs the optional router push (step 4 of the RouterOS setup) to
  say anything at all, and any rule it cannot read — one scoping by an
  address-list name, say — makes it stop claiming rather than guess. A
  wrong "this can never fire" hides a working entry, and a wrong "this
  looks fine" is worse than silence.

- **Routers can push whether a firewall rule actually logs, and which
  addresses it matches** (#274). The filter-rule push gained three
  fields: `log`, `dstAddress` and `srcAddress` — what the check above
  reads.

  **Update MikroView before you update the push script on your routers.**
  MikroView refuses a push containing a field it does not recognise
  rather than ignoring it, so a router sending the newer script to an
  older MikroView gets a `400` and stops pushing. The other order is
  safe. See [docs/routeros-setup.md](docs/routeros-setup.md).

- **`THIRD-PARTY-NOTICES.md`, embedded in the binary and served at
  `GET /api/third-party-notices`** -- the copyright notices and licence
  texts of every Go module statically linked into mikroview and every
  frontend package bundled into its embedded assets. MIT, BSD-3-Clause,
  ISC and Apache-2.0 each require those notices to accompany a *binary*
  distribution, and Apache-2.0 s4(d) requires any `NOTICE` file to be
  passed along; mikroview previously shipped none of it. Because the
  runtime image is distroless -- the binary is the entire artefact -- the
  notices are compiled into it and linked from the About dialog, so a
  user of a running instance actually receives them.

  Generated by `tools/licenses/generate-notices.mjs` and verified in CI,
  rather than hand-maintained: a notices file nothing checks is wrong the
  first time a dependency changes and silently stays wrong. Same
  "record the obligation as something that fails" approach as
  `injection_sinks_test.go` and `internal/api`'s `authzMatrix`.

- **Spamhaus attribution is retained rather than discarded at parse
  time.** Spamhaus's DROP terms require that credit be given and that the
  list's date and (c) text "remain with the file and data". That text
  lives in the feed's leading `;` comment block, which the parser threw
  away; it is now kept alongside the ranges it describes and logged on
  every refresh.

- **The watchlist**, replacing the old Control Ports tab (#243): a
  persisted, admin-managed entry set (`internal/watchlist`) with two
  matching modes -- **record**, "watch attempts against these ports,"
  generalising Control Ports' client-side-only logic to run server-side
  against every ingested event; and **invert**, "this device should only
  ever reach these destinations," which starts a new entry in an
  observe state (nothing fires, candidate destinations are recorded for
  review), lets the operator promote what's expected, and only then
  starts treating anything else as a real match. Matches are persisted
  (`internal/matchlog`, append-only, fsync per write) so they survive
  both the in-memory event ring wrapping and a mikroview restart --
  unlike Control Ports, which only ever saw whatever was still in the
  browser's own capped, volatile event buffer. Matching runs on an async
  evaluation worker mirroring `internal/detect.Detector`'s own
  queue/drop/recover pattern, so a slow or backed-up match evaluation
  never delays store insertion or WebSocket broadcast on the single
  ingest goroutine -- the same failure mode issue #221 already
  demonstrated on the equivalent detection path.

  Managed end to end: an HTTP API (entry CRUD, promote, observe toggle,
  and a windowed match query reachable via a read-only API token) and an
  admin-only **Menu → Watchlist** page for creating entries, reviewing
  an inverted entry's observed candidates, promoting them, and viewing
  an entry's recent matches inline.

  New config: `watchlist.storePath`, `watchlist.matchLogPath`,
  `watchlist.matchLogCapacity` (CFG-0040/CFG-0041), see
  `docs/configuration.md`'s "Watchlist" section.

- **Watchlist entries suggested from pushed router data** (#243 slice
  5): named DHCP leases and ports an existing firewall rule already
  drops or rejects are generated into `internal/suggest`'s candidate
  pool automatically in the background, so there's something to react
  to rather than a blank page. Every candidate is one of three states,
  never a plain accept/reject -- **undecided** (the default),
  **accepted** (a real watchlist entry now exists for it), or
  **hidden** (declined, reversible only by deliberately undoing it from
  the Hidden view; it never reappears on its own). Accepting a device
  suggestion always starts observing with nothing pre-approved, the
  watchlist's own safe default. An accepted suggestion whose original
  reason later disappears (the rule changed, the lease expired) is
  flagged **stale** with a hard-to-miss highlight rather than silently
  reverted. A confirm-gated **"reset everything"** wipes the whole
  watchlist and regenerates suggestions from scratch.

  New admin-only **Menu → Suggestions** page and
  `GET/POST /api/suggestions/...` API. New config:
  `watchlist.suggestionsStorePath`, see `docs/configuration.md`'s
  "Suggested watchlist entries" section.

  `docs/routeros-setup.md` gained the two push-script blocks
  (`dhcp-lease`, `arp`) this feature's device suggestions actually
  depend on -- previously only documented as a reference table row,
  never given as code to paste in, so following the setup guide as
  written produced zero device suggestions no matter how the feature
  itself was configured. Verified against a real RouterOS 7.23.3
  router, including one real surprise: a lease with no client-reported
  hostname yet serializes `host-name` as JSON `null` rather than
  omitting or erroring on it, which MikroView already handles the same
  way it handles an unset `dst-port`.

- **An optional Postgres backend for every persisted store** (#131),
  including accounts: separation between the mikroview host and its
  persisted state, so that compromising the host no longer means
  reading a plaintext file to get every account's data. One shared blob
  table (`store_blob`) holds the same JSON document the file backend
  writes today, per store, with an optimistic-concurrency version
  column -- see `docs/decisions/postgres-backend.md` for the six
  decisions this left open and why. Existing JSON files are migrated
  byte-identically into Postgres on first boot with it configured, and
  left on disk afterward rather than deleted, so reverting is "remove
  `postgres.dsnFile` and restart" rather than an irreversible choice.
  New config: `postgres.dsnFile` -- deliberately no inline `postgres.dsn`
  or CLI flag, since a DSN carries a password. See
  `docs/configuration.md`'s "Postgres (optional)" section.

- **The watchlist match log now has a Postgres backend** (#243 slice
  6): a dedicated, indexed `match_log` table rather than a row in the
  shared document table every other Postgres-backed store uses -- see
  `docs/decisions/postgres-backend.md` §1a for why that doesn't reopen
  the "one blob table" decision. No record-count ceiling on Postgres,
  unlike the file backend; bounded by age instead
  (`watchlist.matchLogRetention`, 7 days by default), enforced by an
  hourly background purge. One asymmetry from every other store: an
  existing `matchlog.jsonl` is **not** migrated when Postgres is
  configured (its append-only format doesn't fit the byte-identical
  migration path the rest of the backend uses) -- it starts empty on
  Postgres, and the startup log says so plainly rather than leaving
  missing history to be discovered. New config:
  `watchlist.matchLogRetention`.

- **Tor and VPN network-class matches now reinforce an already-raised
  flag's confidence** (#114), direction-aware: only a classified source
  reaching *into* your network counts, never your own outbound traffic
  through a VPN or Apple Private Relay. Datacenter and privacy-relay
  matches stay display-only, as measured in #114's research: the broad
  datacenter/cloud lists alone cover more than one in ten routable IPv4
  addresses, so scoring on them would fire constantly on ordinary
  traffic (Google Public DNS, Akamai edge, every Apple Private Relay
  user). A match never raises a flag by itself, only reinforces one a
  behavioral detector already raised — the same floor-only contract
  every other reputation signal in mikroview follows. Whitelisting a
  trusted VPN/VPS uses the existing flag-exclusion tools; no new
  suppression mechanism was added.

- **New `netClass` source: `apple_private_relay`**, Apple's official
  Private Relay egress range list, on by default alongside `x4b_vpn`.
  Not a cosmetic addition -- X4BNet's VPN feed pulls the same Apple
  ranges in via their own build pipeline, so without an authoritative
  source of the same ranges taking priority, an iPhone or Mac's ordinary
  Private Relay traffic would classify as a "known VPN exit," which
  #114's research called out as the single false positive that mattered
  most for a home-network product.

- **`POST /api/ingest/routeros`** (#186 step 3), the RouterOS push-ingest
  endpoint. Reachable only with an ingest token (#186 step 1) -- there is
  no session-based path to it at all, structurally the same "separate,
  minimal mux" guarantee the existing read-only API tokens have, just
  pointed the other way: an ingest token can reach nothing else, and
  nothing else can reach this. Payloads are strictly validated
  (`internal/ingest`, #186 step 2) and rejected whole on any problem,
  rate-limited per token (120 requests/15 minutes, sized around one
  multi-page, multi-kind push rather than a single request), and every
  accepted push is audited by device, recording only its shape (kind,
  page, record count) -- never the pushed content itself.

- **Pushed router state is applied** (#186 step 4): host names and the
  rule/NAT tables. A validated push lands in a new in-memory-only store
  (`internal/routerstate`), keyed by the ingest token's own device --
  never by anything the payload claims about itself.

  **Host names**: a DNS static entry, DHCP lease hostname, or WireGuard
  peer comment pushed by the router now names that address everywhere
  mikroview displays one. RouterOS always wins -- a router-pushed name
  out-ranks a UI-set entity label and the config map for the same
  address, so names in mikroview always match RouterOS with no drift and
  no reconciling. The mikroview-side label isn't destroyed, just
  shadowed: manage router-known hosts in RouterOS, and hand-made labels
  for anything the router doesn't name simply persist (and resurface if
  the router stops naming a host).

  **Rule and NAT tables**: `GET /api/routeros/{device}/rules` and
  `.../nat` serve the pushed tables in RouterOS's own display order,
  reading only mikroview's local store -- nothing ever contacts the
  router. Event-to-rule resolution goes through the operator's
  `log-prefix` only (the ordinal never appears in a log line), and a
  shared prefix honestly returns every matching rule rather than
  guessing at one.

  In the UI, event rows grow a lookup button beside the rule and NAT
  cells -- the same shape as the IP/port investigate buttons. The rule
  popover shows every pushed rule carrying that row's log-prefix, with
  its RouterOS ordinal ("go look at rule 7 in RouterOS"), comment,
  chain, action and src-address-list; the NAT popover shows the whole
  numbered NAT table, since a log line carries the translation result,
  never which rule performed it. Three states are kept honestly
  distinct: no table pushed by this device yet, a table with no rule
  carrying this prefix, and the matches themselves.

  **Structurally incapable of affecting detection**: the store is
  in-memory only (a restart costs one push interval of enrichment,
  nothing more -- and there is nothing to redact from a backup because
  it is never in one), and a build-failing test forbids any import edge
  between it and the flags/detect machinery, in both directions. Pushed
  data can name things and fill tables; it cannot raise, lower, clear
  or suppress a suspicion signal.

- **Syslog over TLS** (#188), `listen.syslogTls` (default `:6514`, RFC
  5425's port), accepting RouterOS's `remote-protocol=tls`. It presents
  the same certificate the HTTPS listener already uses -- the router
  already imports mikroview's generated CA to verify HTTPS ingest, so
  this is that same trust step, not a second one -- and is only started
  while `tls.enabled` is true, since there's no certificate to present
  otherwise. Implementation-wise it's a `tls.Listener` wrapped around the
  same `ServeTCP` the plaintext TCP listener already uses, so the
  connection cap, per-source cap, idle timeout, and RouterOS's
  no-newline framing (#202) all apply unchanged.

  This buys confidentiality for log traffic on the wire and mikroview
  authenticating itself to the router. It does **not** authenticate the
  sender: RouterOS's logging action has no client-certificate option
  (verified against a real router -- see
  `docs/decisions/routeros-ingest-spike.md`), so anything able to reach
  the port can still connect and inject log lines, exactly as with the
  plaintext `syslogUdp`/`syslogTcp` listeners, which stay on alongside
  this one (their removal is #189, deliberately deferred until this
  ships and is proven against a real router).

- **A live abuse-check button on flag cards** (#213). Raw events aren't
  persisted, so an old or cleared flag often has nothing left in the
  live view to click into — this reuses the existing
  `IpInvestigateButton`/`IpLookupPopover` path wholesale (no new
  backend, already cached and rate-limited) to answer "what does this
  IP look like now" without leaving the page. Additive only: the
  frozen reputation snapshot captured at raise time is unchanged, and
  still answers "what did it look like when it fired". Shown for every
  flag type with a real IP-shaped target; `device_silence` is
  explicitly excluded even though an auto-discovered device's ID can
  itself be IP-shaped, since that flag identifies the device that went
  quiet, not a source worth threat-checking.

### Changed

- **Selectable 1/2/3-column layout for the Flags page** (#199),
  persisted per browser. 2 and 3 columns switch to a compact card
  variant — a truncated one-line detail, the split Clear button flowing
  full-width below the content instead of floating in a fixed-width
  corner — rather than forking the card markup per density. Below the
  shared 700px mobile breakpoint the grid collapses to 1 column and
  cards revert to their full, non-compact detail regardless of the
  stored setting, which itself is left untouched by the floor.

- **"Clear all" and a split Clear button on the Flags page** (#198). A
  new Clear all button above the active list clears every active flag in
  one request — click-again red "Confirm" is the safeguard against an
  accidental single click, not a modal — via a new
  `POST /api/flags/clear-all` endpoint, one audit entry per call rather
  than one per flag. Regular clears only; it can never create a
  permanent exclusion. Each flag's own Clear button is now a split
  control: the main segment is unchanged, and its arrow segment (admin
  only) opens "Permanently clear" (renamed from "Clear, never flag
  again"), keyboard-accessible (focus, Enter to open, Tab to the item,
  Escape to close).

- **Permanent exclusions moved to their own page** (#207), reachable
  from the menu alongside Detectors/Entities/Audit log. Reaching and
  reviewing exclusions underneath a potentially large active-flags list
  was a pain; the new Exclusions page adds a count and a filter by
  detector type and target. A pointer stays where the section used to be
  on the Flags page. No backend changes -- it's a client-side filter
  over the existing `GET /api/flags/exclusions` /
  `DELETE /api/flags/exclusions/{id}` endpoints.

- **Appearance is a standalone toolbar control again, and Export moved
  into the menu** (#137). #73's inline-vs-menu split filed theme and
  colorway switching under "everything else" and buried it two clicks
  deep, while Export — an occasional, deliberate action — held an inline
  toolbar slot. The two are now where that split should have put them:
  Appearance one click away on every view at both breakpoints (a bottom
  sheet at phone widths, same as the menu), and Export in the menu on
  desktop as it already was on mobile.

### Changed

- **The stored copy of a raw log line is now capped at 2KiB** (#285).
  MikroView keeps each firewall log line exactly as the router sent it,
  so you can check what it shows against what arrived. Nothing bounded
  that, though, beyond the 64KiB a syslog message may be -- while the
  documented memory budget (~120MiB for 200,000 events) assumed an
  ordinary line. Both could not be true: filled with 64KiB lines, the
  same buffer holds **12.5GB**, and nothing has to log in to send them.
  Real RouterOS lines run 150-400 characters, so 2KiB is about five
  times the longest genuine one and truncation only ever hits input a
  real router does not produce. A row whose line was cut says so on
  hover and in a CSV export, rather than presenting a shortened line as
  though it were verbatim. The per-event cost figure rises from 616 to
  624 bytes, which is the one field this needed.

- **Routine router pushes are no longer written to the audit log**
  (#285). Every push recorded a row, a push script runs every 15-30
  minutes, and the log keeps the most recent 10,000 entries -- so one
  ingest token produced 11,520 rows a day and pushed out the entire
  record of admin actions (accounts created, tokens issued, the admin
  role transferred) in about 21 hours. A successful scheduled push is
  not an accountability event; what it erased was. MikroView now records
  the first push of a kind from a device, a push starting to fail or
  recovering, and a periodic heartbeat -- and applies the same rule to
  refusals, since those are cheaper for an attacker to produce than
  valid pushes.

- **Syslog connection slots are now reserved for the routers you
  declare** (#285). The listener holds 256 connections with 8 per source
  address, so 32 addresses filled it -- easy for one host with a routed
  IPv6 range -- and a real router connecting afterwards was accepted and
  immediately dropped, its log lines never arriving. A total monitoring
  blackout whose only trace was a repeated line in the container log.
  A quarter of the pool is now held for routers listed under `devices:`
  in `config.yaml`, the same protection configured devices already get
  elsewhere, and the live view shows a warning when one of yours has
  been turned away. Declaring no devices reserves nothing, since holding
  capacity back for nobody would only shrink the pool.

- **The HTTP-to-HTTPS redirect no longer trusts the address it was
  asked for** (#283, #284). To build the redirect MikroView reused the
  address the request claimed to be for, without checking it, whenever
  `tls.hosts` was unset -- which is the shipped default. Someone able to
  send a hand-crafted request could get MikroView to reply "go to
  https://somewhere-else". The code had a check for exactly this, with a
  comment saying so, against a list that was empty out of the box.
  MikroView now works out its own valid addresses (its hostname and the
  addresses it is listening on) when you have not listed any, so the
  protection works with nothing configured and reaching MikroView by
  bare IP keeps working.

### Fixed

- **Upgrading no longer leaves the browser showing the previous
  version's interface** (#347). MikroView is installable as a web app,
  which means a service worker keeps a copy of the interface so it opens
  instantly. The server never told browsers how long to keep that copy,
  and a browser left to guess can hold on to the file that detects new
  versions for up to 24 hours — so an operator who pulled a new image,
  restarted, and reloaded could be served the old interface for the rest
  of the day, with no way to tell from inside the app that anything was
  stale. Reported after it cost an hour of hunting for a container
  problem that did not exist: the server was correct and said so, and the
  browser was quietly ignoring it. Files whose names change with their
  contents are still cached indefinitely, so this costs nothing on load.

- **Typing `host:8080` into a browser now works** (#325). MikroView
  usually gets one published port on a host where 80 and 443 belong to
  something else, and a browser given an address with a port tries plain
  HTTP first. That arrived at the encrypted port and produced a bare
  error page. It is now answered with a redirect to the same address over
  HTTPS. Nothing is ever served unencrypted.

- **TLS connection failures are explained instead of quoted** (#321). A
  phone or router that refuses MikroView's certificate produced a line
  like `remote error: tls: unknown certificate`, repeated every few
  seconds by the client's own retries — accurate, unreadable, and enough
  of it to bury anything else. The line now says who rejected whom and
  what to do about it, keeps the original error at the end, and repeats
  at most once a minute per cause with a count.

- **A quiet flood could fill the log** (#322). Rejected syslog
  connections were logged one line per attempt on a port that takes no
  credentials, so anyone able to reach it could write to the log at
  connection speed; a client disconnecting mid-response was logged as a
  warning, which a phone locking its screen does routinely; and once the
  watchlist match log filled, every subsequent match logged the same
  failure again. All are now rate-limited with a running count.

- **An address-attribution feed could freeze permanently** (#324). The
  check that rejects a suddenly-oversized feed compared address counts
  that could overflow: a feed carrying wide IPv6 ranges — Apple Private
  Relay does — reported a nonsensical total, and every later refresh then
  looked like a huge jump and was rejected as possibly poisoned. The feed
  silently kept its first copy for good while the log reported success.
  Counts now saturate instead of overflowing, and are printed in a form a
  person can read.

- **A router declared without an `id` shared an identity with every
  other one** (#332). A device's `id` decides which router pushed state
  belongs to and what an ingest token is allowed to speak for, but it was
  never validated, and the documented examples omitted it — so following
  the documentation produced devices that were indistinguishable
  internally, and two of them broke the token dialog outright. An unset
  `id` now defaults to the router's own address, duplicates and
  misleading values are refused at startup (CFG-0032, CFG-0033), and the
  documentation says what the field is for.

- **MikroView checks the database ran the schema changes it thinks it
  did** (#294). It recorded which schema versions had been applied but
  never what they contained, so anyone able to write to that one table
  could claim a version and MikroView would report "up to date" and run
  against a schema it had never seen. Each schema change now records a
  fingerprint as it is applied, checked on every start. A mismatch stops
  startup rather than guessing at the shape of your data.

  Existing databases are unaffected: rows written before this have
  nothing to compare and are simply not checked, rather than treated as
  suspicious.

- **Restoring an older database backup can no longer bring deleted
  accounts back** (#294). MikroView copied your JSON files into Postgres
  on the first move, but it re-checked on *every* start — so restoring
  the database to a snapshot from before a store was filled, with the
  original files still on the data volume, copied them straight back in.
  A deleted account and its password came back, with a single line in the
  log as the only sign. The one-time move now happens once and never
  again.

  If a first migration is ever interrupted part-way, MikroView says so
  loudly and tells you how to finish it deliberately. It will not guess:
  an interrupted migration and a restored-from-backup database look
  identical from the inside, and guessing wrong is what caused this.

- **A single sign-on issuer written without `https://` is now checked
  properly** (#267). MikroView refuses multi-tenant providers, and that
  check read the hostname — which is empty for a scheme-less string, so
  `login.microsoftonline.com/common/v2.0` passed a check meant to refuse
  it. Setup would have failed later at provider discovery, but a check
  that answers "fine" because it could not read its input is the wrong
  shape.

- **A router with no NAT rules no longer shows an empty box** (#267).
  The rule lookup said so when a pushed table contained no matching
  rule; the NAT lookup had no equivalent message, so a router that has
  pushed its NAT table and simply has no NAT configured showed nothing
  at all, with a footnote explaining how to read rules that were not
  there.

- **Creating a named entity now answers `201`, not `200`** (#267),
  matching every other create endpoint. `POST /api/entities` both
  creates and replaces, and always answering `200` left a caller unable
  to tell which had happened. A replace still answers `200`.

- **Four single sign-on access-policy settings can now be set from the
  environment** (#267): `MIKROVIEW_OIDC_ALLOWED_GROUPS`,
  `MIKROVIEW_OIDC_GROUPS_CLAIM`, `MIKROVIEW_OIDC_ALLOWED_EMAILS` and
  `MIKROVIEW_OIDC_ALLOWED_EMAIL_DOMAINS`. A deployment keeping its OIDC
  block in the environment could previously say who its provider was but
  not who was allowed in.

- **Two named entities can no longer overwrite each other** (#267).
  Entities were stored under `type + ":" + key`, and both parts can
  contain colons — an IPv6 address is a perfectly ordinary host key — so
  `("host", "2001:db8::1")` and `("host:2001", "db8::1")` landed on the
  same key and one silently replaced the other. They are now kept apart.
  The entity type also gets the same check for control characters that
  the key, label and tags already had.

- **MikroView regenerates its certificate if the authority behind it is
  lost** (#267). If `ca.crt` or `ca.key` became unreadable while the
  certificate files survived, MikroView created a fresh authority and
  carried on serving the old certificate — a pair that validates against
  nothing, so every browser and router that had trusted the original
  failed with a certificate error and no other clue. It now checks that
  the stored certificate was signed by the authority in use, and issues
  a new one if not.

- **A Pushover message is no longer cut mid-character** (#267). Long
  batches were trimmed by byte count, which can split a multi-byte
  character and leave invalid text. Trimming now stops at a character
  boundary.

- **`-transfer-admin` no longer names the admin before you prove a
  recovery key** (#267). It printed "Admin is currently ..." as its first
  action, so anyone able to run the binary learned who the admin is by
  starting the command and pressing Ctrl-C — despite the code's own
  comment two lines below stating the key is asked for first precisely
  so that cannot happen. It now asks first and names the account once the
  key is verified. `-recover-admin-account` still names it up front,
  deliberately: it has to say which account it cannot help with when
  that account signs in through your identity provider.

- **A mistyped filter in an API request is now refused instead of
  silently ignored** (#267). `GET /api/events` and `GET /api/audit`
  accepted a malformed `since`, `until`, `limit`, `port`, `around`,
  `window` or `sinceId` and returned `200` with the filter simply not
  applied — so a caller with a typo got *everything* while believing
  they had asked for a window. In a tool whose job is showing you what
  happened in a window, that is the misreading that matters. Both now
  answer `400` and name the parameter, which is what
  `GET /api/watchlist/matches` already did; the three took the same
  parameters and disagreed about this. An absent parameter still means
  "no filter", unchanged.

- **`-validate-config` now checks your single sign-on settings** (#267).
  It performed no OIDC validation at all, so a block missing
  `publicBaseUrl`, or `clientId`/`clientSecret`, or pointed at a
  multi-tenant provider MikroView refuses, passed cleanly — and the
  first sign anything was wrong was the SSO button not being there. New
  CFG-0060, CFG-0061 and CFG-0062. They are warnings, not errors:
  MikroView still starts and local login still works, because taking a
  deployment down over a half-configured optional integration would be
  worse. `-validate-config` exits non-zero on warnings, so a pipeline is
  still told.

- **A rule filter that stops being usable mid-stream now says so**
  (#267). If a regex filter became unevaluable once matching events
  started arriving, the live view kept showing the last match set it had
  worked out — stale and wrong, with nothing to say the filter was no
  longer being applied. Reading a filtered view as complete when it is
  not is the exact misreading this product exists to prevent.

- **Clicking the logo returns you to the live view** (#267), as does a
  Live view entry in the menu. Previously the only way back was to click
  whichever view button you had used to leave it.

- **Failed actions now say so instead of looking like nothing happened**
  (#267). Removing a watchlist entry, turning observe mode on or off,
  promoting a destination, removing a named entity, clearing a flag,
  clearing all flags, permanently clearing one, and removing an
  exclusion all reported nothing when they failed. The row reappeared or
  simply stayed, which reads as the button not having worked rather than
  as an error — and in several cases it was an unhandled promise
  rejection that only the browser console would ever have shown you.
  Each of these now shows the reason, the same way the forms next to
  them already did.

- **Signing out no longer looks successful when it failed** (#267).
  `logout` was the one action that ignored the server's response
  entirely. You are still signed out locally either way — being left
  looking signed in would be worse — but if the server did not confirm
  it, MikroView now says so, since the session may still be live.

- **The live view no longer briefly claims to be disconnected after a
  fast sign-out and back in** (#267). Closing a WebSocket reports the
  closure asynchronously, so the old connection's "closed" arrived after
  the new one was already up, overwriting the indicator and scheduling a
  reconnect that was then abandoned. Handlers now ignore a connection
  that has been superseded.

- **A regex rule filter no longer switches itself off under load**
  (#267). Two overlapping match requests shared one background worker's
  reply handler, so the earlier one could never be answered and gave up
  reporting "too slow" — which drops the filter and shows every event,
  precisely when a busy feed makes filtering matter most. Replies are
  now matched to their own request.

- **`search_path` in a Postgres connection string is honoured again**
  (#273). Pinning the schema so nobody could shadow MikroView's tables
  (#285) was implemented by forcing it to `public`, which also overrode
  an explicit `?search_path=...`. An operator keeping MikroView's tables
  in a schema of their own got `public` regardless, with nothing to say
  so. It is now pinned to what the connection string asks for, falling
  back to `public` — the protection is unchanged, since the schema is
  still what MikroView sets rather than what the role happens to default
  to. Documented under "Which schema the tables go in".

- **ICMP events are read correctly, and are no longer invisible to the
  watchlist** (#273). RouterOS puts a comma after the connection state on
  a TCP log line but not on an ICMP one, and MikroView split the line on
  commas. So for every ping and every unreachable message, the protocol
  came out blank and the connection state came out as the text
  `new proto ICMP (type 8, code 0)`.

  The visible half was a blank Protocol column. The half that mattered:
  MikroView only considers an event for the watchlist when its connection
  state is new (or absent), and that text is neither -- so **no ICMP
  traffic ever reached the watchlist**, including the "this device should
  only ever talk to X" entries whose entire job is noticing a device
  reaching something it should not. Reading the connection state now
  stops at the state itself and hands anything RouterOS appended to it
  back to the normal field handling, so this holds for whatever else a
  future release appends there too.

  Two knock-on effects worth knowing about, both of them the intended
  behaviour rather than new behaviour. Logged ICMP now counts toward a
  host's activity baseline, so a host that pings a lot looks busier than
  it used to -- it always should have. Port-scan and critical-port
  detection are unchanged: both ignore events with no destination port,
  which every ICMP event is.

- **A watchlist entry whose MAC address you typed in lowercase now
  matches** (#273). RouterOS writes MAC addresses in upper case
  (`52:55:0A:00:02:02`), both in its firewall log lines and in the ARP
  and DHCP tables it pushes. MikroView compared them exactly, so an
  entry you set up by typing the address the ordinary way --
  `52:55:0a:00:02:02` -- never matched anything that device did, and
  looking its matches up by that address returned nothing even when
  there were some. Neither failure said anything: the entry looked
  configured and quietly did nothing. Matching now ignores case, the
  same way MikroView's device registry already did. Stored records keep
  the router's own spelling, since they are evidence.

  Found by running MikroView against a real RouterOS router rather than
  against generated log lines -- every example in this project writes
  MACs in lower case, so nothing that fed itself its own test data could
  have shown this.

- **Terminal escape sequences from a syslog line no longer reach saved
  flags or notifications** (#285). Every field MikroView reads out of a
  firewall log line is chosen by whoever sent it, and those fields become
  a flag's target and detail -- which are then written to `flags.json`
  and the watchlist match log, and sent in email and Pushover messages.
  They were length-capped but never checked for control characters, so a
  crafted line could put an ANSI escape sequence somewhere a terminal
  would execute it, for example on `cat flags.json`. MikroView already
  had the function for this and was applying it only to usernames. It is
  now applied once, where fields are read, rather than at each place they
  end up -- a destination is easy to add and easy to forget. The raw log
  line itself is deliberately left byte-for-byte as the router sent it,
  since comparing against it is the reason it is kept.

- **A CSV export can no longer smuggle a formula into a new row**
  (#285). A cell containing a bare carriage return was written unquoted,
  and a spreadsheet reading classic-Mac line endings treats that as the
  end of a record -- so text after it started a new row, and the first
  cell of a new row never went through the formula-neutralising step.
  The payload has to avoid quotes and commas to reach this (anything
  containing them was quoted already), which the classic
  command-execution form does. Carriage returns are now quoted like every
  other separator.

- **A webhook notification will no longer follow a redirect to another
  host** (#285). MikroView supports putting a credential in a custom
  header, because that is what ntfy, Home Assistant and n8n each expect.
  Go strips the standard `Authorization` and `Cookie` headers when a
  redirect crosses hosts but not custom ones -- so the header most likely
  to hold your secret was the one being forwarded. Redirects within the
  same receiver are still followed; anything else fails the send with a
  message saying why. MikroView also now warns at startup if the webhook
  URL is plain `http://`, since flag contents and that header cross the
  network in the clear.

- **Asking the match log for one record no longer builds all of them**
  (#285). `GET /api/watchlist/matches` loaded every record for the
  requested device into memory before applying the limit -- 237 MB and
  1.9 seconds to return a single record from a large log -- and it is
  reachable with a read-only API token and no rate limit. It now reads
  only what it needs to order the results, then fetches the full
  contents of just the records being returned.

- **One router can no longer name hosts on another router's traffic**
  (#285, #283, #284). Names pushed from RouterOS -- DNS static entries,
  DHCP lease hostnames, WireGuard peer comments -- were applied
  deployment-wide rather than to the router that pushed them. The
  holder of one router's ingest token could therefore label any address
  in the world, including one under active attack seen through a
  completely different router, and a single WireGuard peer allowing
  `0.0.0.0/0` became the catch-all name for every otherwise-unlabelled
  address. That contradicted the one-router blast radius the ingest
  token exists to provide and states in its own documentation. Without
  any attacker involved, two independently-administered routers both
  using `192.168.1.0/24` cross-contaminated each other's displayed
  names.

  Every other router-pushed table -- filter rules, NAT rules, DHCP
  leases, ARP -- was already scoped per device, and had a test saying
  so. Host names were the one exception, and the existing test only ever
  used a single router, so nothing exercised the gap. Found
  independently by two reviewers and reproduced by a third.

  If you monitor one router, nothing changes. If you monitor several,
  the `device` named on each ingest token must match that device's `id`
  in `config.yaml` -- which is already required for the rule and NAT
  table lookups to work, so in practice it already does. Labels you set
  inside MikroView are unaffected: they are yours, not a router's, and
  stay deployment-wide.

- **A flood of made-up MAC addresses or rule names can no longer grind
  ingestion to a halt** (#285). Three in-memory indexes are keyed on
  something whoever is sending syslog gets to choose -- the source MAC,
  the firewall rule label, and the rule leaderboard behind the stats
  panel. Each was capped, but the cap was enforced by trimming back to
  exactly the limit, which leaves the index full, so the next new value
  overflowed as well and re-ran the whole scan -- for every event
  thereafter. Measured on the old code: 1,529 ns per event became 16-21
  ms once the MAC registry was full, taking ingestion down to roughly 47
  events a second, and the registry is saved to disk so it came back
  poisoned after a restart. mikroview now sheds a batch and leaves
  headroom, so the cost is spread over the next several thousand events
  instead of being paid on each one. The same treatment mikroview
  already applied to its detectors, now shared rather than re-derived.

  The rule leaderboard had no cap at all: 500,000 made-up rule names cost
  161 MB of memory, permanently displaced the real rules from the "top
  rules" list, and sorting that list held the lock ingestion needs --
  measured at 306 ms per stats request with ingestion blocked for 301 ms
  of it. It is now bounded like the others, keeps the rules that actually
  fire, and the sort no longer holds anything ingestion needs.

- **The login rate limiter now enforces its own memory cap** (#285). The
  4,096-key ceiling was applied when recording a failed login but
  silently skipped on the path an unauthenticated request reaches first,
  because the pruning step created the entry it was about to check for.
  200,000 requests from varying source addresses left 200,006 tracked
  keys and 22.7 MiB retained. The limiter also now forgets a key
  entirely once its attempts age out, instead of keeping an empty entry
  per source address forever.

- **A flood of flags no longer pushes out the alert that matters**
  (#285). At its hard ceiling the flag store shed the *earliest-raised*
  flag first -- which is the first flag of a real incident, the single
  most valuable thing it holds. Since flag targets come from
  unauthenticated syslog, 5,001 junk "new device" flags (about 600 KB,
  7 ms) were enough to erase a genuine alert permanently. Reviewed
  (cleared) flags are still shed first; among the rest mikroview now
  keeps the ones a detector has re-fired for, since a real incident
  re-fires and junk fires once. A bounded store under an unbounded flood
  still has to drop something, so dropping an unreviewed flag is now
  logged with a running total rather than happening silently.

- **Two concurrent writers can no longer corrupt a store document**
  (#285). Every writer shared one temporary filename (`<store>.tmp`),
  written with a truncating open rather than an exclusive one, so two
  overlapping saves landed in the same file and the rename published a
  byte mixture of both payloads. Measured on the old code: 12 of 300
  concurrent write pairs left the document as invalid JSON *after both
  writers had finished* -- settled corruption, not a transient. For the
  accounts store that meant a total lockout, since mikroview
  deliberately refuses to start on an unreadable accounts document
  rather than treat it as a fresh install. Two writers at once is the
  documented recovery workflow (`docker compose exec ...
  -recover-admin-account` against a running server), not a contrived
  case. Each writer now gets its own exclusively-created temporary file,
  and the write is flushed to disk before the rename and the directory
  after it, so "atomically replaced" now also holds across a power loss.

- **A stalled database no longer takes authentication down with it**
  (#282). Every authenticated request, every login and every
  registration attempt re-read the accounts document with no deadline.
  If Postgres stopped answering while its connection stayed open -- a
  network blackhole, a long lock wait, an overloaded server -- each
  request permanently consumed a goroutine and a pooled connection, and
  the HTTP write timeout did not release them. Once the pool was
  exhausted every further request blocked too, including the login an
  operator needed in order to diagnose it, and nothing recovered until
  the database came back. The staleness check is now bounded by a
  five-second deadline, and concurrent callers share a single check
  instead of each opening their own, so an outage costs one connection
  rather than one per request and the server keeps serving from memory
  throughout. On Postgres the check now also asks only for the version
  rather than pulling the whole accounts document back on every request.

- **Autoscroll off now holds the live view still** (#232), rather than
  only skipping the jump-to-bottom. The rendered window is a sliding
  slice of the most recent 800 events, so once the buffer passed that
  cap rows kept falling off the top as new ones arrived regardless of
  the toggle -- the page scrolled itself out from under you, which is
  what the toggle was supposed to prevent. Switching Autoscroll off now
  freezes the event pool, and filters still narrow and widen within what
  was frozen, so an event arriving after the freeze can never appear no
  matter what you do to the filter. Distinct from Pause, which also
  halts the age-based display cutoff and detection bookkeeping; this
  only stops what is on screen from moving.

  The freeze survives navigating to another view and back, and the
  Control Ports table -- which has no Autoscroll control of its own --
  is no longer frozen by the live view's toggle.

- **Network-class attribution silently favored the wrong source on an
  exact-prefix collision between two feeds** (found while implementing
  #114's remaining scope). `buildTable`'s own doc comment claimed
  iterating sources in priority order made the higher-priority source
  win a tie; `bart.Table.Insert` is actually last-write-wins on an exact
  prefix, so the *lower*-priority source (whichever was iterated last)
  silently overwrote the higher-priority one instead. Caught by a new
  test reproducing the concrete case this mattered for: with
  `apple_private_relay` and `x4b_vpn` both covering the same prefixes,
  attribution was resolving to "VPN" instead of the intended,
  higher-priority "Private Relay". Fixed by checking for an existing
  exact-prefix entry before inserting.

- **Syslog over TCP ingested nothing from a RouterOS router** (#202).
  RouterOS sends each message as a bare payload with no trailing newline
  and no octet count. The listener read with a `bufio.Scanner` on the
  default line split, so it waited for a delimiter that never arrived:
  the connection was accepted, held, and silently discarded. Measured
  against a real router, `remote-protocol=tcp` delivered **0** events
  where UDP delivered 3 of the same messages — and nothing was logged, so
  it read as "no traffic".

  Framing is now one read per message, or several if that read contains
  newlines, so a conventional syslog sender that does terminate its lines
  keeps working unchanged.

- **Duplicate events in the client buffer** (#183). The initial
  `GET /api/events` fetch and the WebSocket stream overlap, so an event
  arriving in both was appended twice. `LiveTable`'s keyed `{#each}` then
  had duplicate keys — Svelte logged `each_key_duplicate` and keyed-each
  behaviour is undefined from there, so the row rendered twice and any
  count taken off the buffer was inflated. All three insert paths (the
  initial fetch, the live flush, and the pause-resume splice) now dedupe.

### Added

- **API tokens now have a kind, and ingest tokens are scoped to one
  device** (#186). Alongside the existing read-only API token (#101)
  there is now an ingest token, which the RouterOS push integration will
  use. The two are not interchangeable in either direction: an ingest
  token is refused everywhere a read-only token is accepted, and vice
  versa, because `Authenticate` requires its caller to state the kind it
  expects rather than returning a token for the caller to inspect.

  This matters because an ingest token lives in a script on a router,
  where any RouterOS user holding the `read` policy can print it. Without
  the separation, that value would become a read-everything credential
  for every event, flag, stat and device mikroview holds.

  An ingest token must name the device it is issued for, and only an
  ingest token may. One token per router means a compromised router
  cannot report state for any other.

  `POST /api/tokens` takes optional `kind` (`api` or `ingest`) and
  `device`. Omitting `kind` still issues a read-only API token, so
  existing callers are unchanged and the less privileged option stays the
  default.

  A token whose kind this build does not recognise cannot authenticate at
  all, but stays listed and revocable — guessing that an unknown kind
  meant the read-everything one is the wrong direction to guess in.

## [0.1.0] - 2026-08-07

### Changed

- **Rule-regex filtering runs in a Web Worker** (#157), with a hard
  timeout that terminates the Worker if a pattern overruns. The main
  thread no longer executes a user-supplied regular expression at all, so
  no pattern can hang the tab — including shapes nobody has anticipated,
  which a structural screen can never cover. It replaces
  `isSafeRulePattern`, which rejected the known catastrophic-backtracking
  shapes but, as its own comment said, could not prove an accepted
  pattern was fast.

  It is also less work: the old path ran the pattern against the rule
  label *and* the raw line for every event on every recomputation — up to
  10,000 regex executions across a 5,000-event buffer, repeated per
  top-talker widget. Filtering is now a set lookup.

  A pattern that is invalid, or refused for overrunning, leaves the rule
  filter inactive and says so on the regex toggle rather than looking
  like "no matches".

### Added

- **`-backup` and `-restore`** (#97), producing a single gzipped JSON
  document keyed by store name. Not tar: there are no filenames in the
  format, so path traversal is impossible by construction rather than
  defended against. It carries everything, including accounts and
  recovery-key digests — a backup missing credentials cannot restore a
  working system, and disaster recovery is the wrong moment to find that
  out. Protection is on the output instead: mode 0600, `O_EXCL` at
  creation, and a refusal to write into a world-readable directory
  without `--force`.

  JSON deployments only. On Postgres both commands refuse and point at
  your database's own tooling, which is the expectation that came with
  choosing Postgres.

- **Recovery-key digests now follow the accounts into Postgres**, while
  the pepper stays a local file on the mikroview host. Previously the
  digests stayed in a local JSON file *next to the pepper* even on a
  Postgres deployment — the single-host arrangement, on the one
  deployment that chose Postgres specifically to avoid it.

### Fixed

- **`-transfer-admin` and `-recover-admin-account` work on Postgres
  deployments.** They previously refused outright, which left a Postgres
  deployment with no way to transfer or recover admin at all: neither
  operation can go through the web UI (a compromised session must not be
  able to grant itself admin) nor through an identity provider (an IdP
  account is a login, not an authorisation to escalate). The CLI is the
  only route, so it has to work in every deployment shape.

### Removed

- **`callerIsAdminOrOpen`'s "no accounts yet" bypass**, which treated an
  anonymous caller as an admin on the detector-settings, flags-exclusion
  and config-problems endpoints. It dated from when mikroview could run
  with authentication switched off. Unreachable since that mode was
  removed — `requireAuth` refuses those paths before they route — but it
  read as "anonymous callers are admins under some condition" and would
  have gone live again the moment `requireAuth` was loosened. There is
  now one admin check, `callerIsAdmin`, with no bypass.

- **The option to run mikroview without authentication.** The first-run
  screen offered "No Authentication" alongside creating an admin
  account. An unauthenticated mikroview publishes which hosts are being
  scanned, which rules fire, which ports are under pressure, and which
  accounts exist — a reconnaissance map of the network it is meant to be
  watching. Creating a local account is one form, and it is now the
  floor.

  Gone with it: `POST /api/auth/skip`, the `authDisabled` field on
  `GET /api/auth/session`, and the frontend's skip flow.

  **If your deployment had authentication disabled:** it now starts in
  the create-account state and serves nothing else until you complete
  it. Nothing is lost — there were no accounts to lose. Open the web
  interface and create the admin account. The `"disabled": true` key in
  your accounts file is ignored and disappears the next time the file is
  written.

- **`-enable-auth-setup`.** It re-armed the first-run screen after a
  deployment had skipped authentication. There is no skipping, so there
  is nothing to re-arm.

- **`-reset-password`.** Superseded some time ago by
  `-recover-admin-account`, which is narrower (the admin account only)
  and requires a recovery key. It had been left as a stub that printed
  where to go instead; that stub is now gone too. Use
  `mikroview -recover-admin-account`.

- **Legacy on-disk format fallbacks.** `auth`, `entities` and `flags`
  each accepted a bare JSON array as well as their current object shape,
  for files written by builds that predate those shapes. `auth` also
  carried `migrateHasLocalPassword`, which filled in a missing
  `hasLocalPassword` by inferring it from whether the account had a
  linked SSO identity — the field was a `*bool` purely so that "absent"
  could be told from "false".

  All four are gone, and `User.HasLocalPassword` is a plain `bool` now
  that absence has no meaning. Files written by any current build are
  unaffected; a file in one of the old shapes will fail to load and
  mikroview will refuse to start, naming the path.

### Changed

<!-- Nothing operator-visible changed in this release beyond the
     Added/Fixed entries above. An earlier entry here described an
     internal test-fixture refactor (internal/api's Routes gaining an
     inner mux); it was removed because every other entry in this file
     is written from what an operator would notice, and a reader
     scanning for what changed for *them* has to skip past anything
     that isn't (#268 finding 17). -->
