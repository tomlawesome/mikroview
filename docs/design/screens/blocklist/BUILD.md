# Build notes — the blocklist builder (#1360)

Ratified: round 2 (`round-2/README.md`, `catalogue.md`, `builder.html`,
`tail.html`) plus the owner's answers on the issue — 9b, 11a, 12a, 13,
14a, 15, 17a, 18, and 2026-10-01: 1b+1c (seven lists), 2a (the tail as
drawn). Build it as drawn (AGENTS.md, "Building a ratified design"); where
these notes amend the drawing they say so, under "Decisions". Written for
an Opus 5.5 builder (owner, 2026-10-01: "Fable designs, Opus 5.5 builds").

Base the branch on a fresh `gitlab/dev`, not this worktree: dev already
carries `WizardVersion = 5` (#1405), so the mock-up's "wizardVersion 5"
is stale — this build bumps to **6**.

Invariants that bind every part: MikroView never connects to the router
(it prints commands); the router fetches each list from its source and
MikroView never serves, copies or vendors list data (AGENTS.md, "List and
lookup data"; dependencies-and-data skill) — the catalogue is names, URLs,
terms and recipes only; a feed's attribution travels with the data
(Spamhaus: credit and © in each entry's comment).

## Parts, in build order

Each part is one commit with its own tests. Nothing in a later part is
needed to merge an earlier one.

### 1. Version features — `internal/routeros/features.go`

- `Features(version string) (Features, bool)` → `{LoopContinue, FetchFollowsRedirects, SchedulerDays bool}`
  from thresholds 7.22, 7.22, 7.24 (`catalogue.md`, "Sources"), via the
  existing `parseVersion`/`compareVersions`; `false, false` on an
  unparseable version. `AtFloor(version)` = at or above `MinimumVersion`.
- Proves: each flag flips at exactly its threshold; below 7.18 is
  refused. Test shape: `TestVersionStanding` (`versions_test.go`).

### 2. The push: raw rules and list counts — `internal/routeros`, `internal/ingest`, `internal/routerstate`

- `blockSpecs` gains two kinds. `raw-rule`: `/ip/firewall/raw` then
  `/ipv6/firewall/raw` into one record list, record
  `{ordinal, family ("ip"|"ipv6"), comment, chain, action, srcAddressList, dstAddressList, logPrefix, log, disabled, packets, bytes}`.
  `address-list-count`: for each name in the catalogue's fixed list
  (`mv-bl-<key>` and `mv-bl-spamhaus6`, part 3) and each family, one
  record `{list, family, count, loadedAt}` — `count` from
  `print count-only where list=$n`, `loadedAt` from the first entry's
  `creation-time` (verify on CHR that `set list=` keeps it; fall back to
  the fetched file's `creation-time` if not). The names are constant, so
  re-pasting part 1 of the block changes nothing whatever lists are on.
- The `address-list` block's `foreach` gains `where !(list~"^mv-bl-")`:
  15,000 entries would overflow `/tool fetch`'s ~64 KiB POST body and
  stop the whole page. Verify the `~` regex form in `print as-value where`
  on CHR.
- `WizardVersion` → 6; doc comment entry in the same style as 5's.
- `internal/ingest`: `KindRawRule`, `KindAddressListCount`, `RawRule`
  (reuse `FilterRule`'s field types) and `AddressListCount` structs with
  `validate()`, added to `Payload`, `RecordCount` and the decode switch.
- `internal/routerstate`: `RawRules(device)` (sorted by family then
  ordinal, like `FilterRules`) and `AddressListCounts(device)`. Also a
  per-device, per-raw-rule-comment sample ring of `(at, packets)` kept
  from each push, 36 h deep, for part 6's "fired N today".
- `internal/routeros/reference/menus.json`: `ip/firewall/raw`,
  `ipv6/firewall/raw`, `ipv6/firewall/address-list`, `file` (read, get),
  `system/scheduler` gains `start-time`, `days`; `tool/fetch` gains
  `http-max-redirect-count`, `http-method`, `http-data`,
  `http-header-field`. Mark each `verified` honestly once part 4's CHR
  run has happened.
- `docs/routeros-setup.md` §4c: the two new blocks, in the existing
  "verified against a real RouterOS x.y.z" voice, once verified.
- Proves: field renaming (`TestPushBlockRenamesIPServiceFields`); the
  exclusion is in the address-list block and nowhere else; every block
  still stamps version 6 (`TestPushScriptStampsEveryBlockWithTheWizardVersion`);
  decode round-trips and refuses unknown fields
  (`TestIPServiceRoundTripsFields`, `TestIPServiceRejectsUnknownRecordField`);
  routerstate sorts and keeps per-device
  (`TestIPServicesSortedAndAccessibleWithNoAddressRestriction`,
  `TestDevicesAreIsolated`); and **one test per new kind on the real
  body a CHR produced**, pasted in like `TestDecodeRealFilterRulePush`.
  `TestEmittedCommandsUseKnownMenus` and `TestDocSchedulerAndRuleBlocksMatchGenerators`
  must stay green.

### 3. The catalogue — new package `internal/blcatalogue`

- Pure data, no imports beyond stdlib: `List{Key, Name, Short, URL, URL6, Format, Terms, Caveat, Default, DefaultDirection, IPv6, Refresh []Refresh, RefreshDefault, Facts, Guide, FlaggedByMikroView bool}`
  for the seven ratified lists, in the drawn order: `spamhaus` (+
  `drop_v6.json`), `et`, `cins`, `blde` (blocklist.de strongips),
  `greensnow`, `dshield`, `bindef` (Binary Defense). `Caveat` is the
  card's fact: `"no licence stated"` (blde, greensnow),
  `"not for business use"` (dshield, bindef), `""` for the three. The
  URLs, terms and update rates from `catalogue.md`; for the four new
  lists read the source page at build time and record the URL and what
  it said, with the date, in the package doc comment.
- `Format` is one of `spamhaus-json`, `one-per-line`, `dshield` (tab
  `start end mask` → `start/mask`). `LeftOut []LeftOut{Name, Why}` carries
  the "considered and left out" record, now ten entries (the two
  labelled pairs moved up).
- `ListNames()` returns every `mv-bl-*` name (the constant part 2 pushes).
- The two lists `internal/blocklist` flags from keep working from
  `feedRegistry`; add a test there that its ET URL equals the catalogue's,
  so the two cannot drift. Spamhaus differs by design (`drop.txt` vs
  JSON) until the finding below is its own issue.
- Proves: seven keys, unique, slugs fit the log-prefix limit (part 4);
  every URL is https; the three unlabelled lists have `Terms` text and
  the four labelled ones a `Caveat`; defaults are exactly Spamhaus and
  ET on. Shape: `TestKnownSourcesMatchesRegistryOrder`.

### 4. The command generator — `internal/routeros/blocklist.go`

- `BlocklistBlock(req BlocklistRequest) (Block, error)` where the request
  is `{Device, RouterOSVersion, Lists []Choice{Key, Direction ("from"|"both"), IPv6, Log, Refresh}}`
  and `Block` is ordered parts `[]Part{Ordinal, Title, Ink (list key or "push"), Commands, Fold}`,
  numbered as drawn: the push (part 2's re-rendered schedule block, see
  Decisions), then per list: loader script, scheduler, rule(s), then the
  closing "run now" line. Every add is guarded add-or-set
  (`SchedulerAdd`, the droplist rule idiom in `internal/droplist/setup.go`),
  set branches name `disabled=no`.
- Loader script, per list, policy `read,write,test,ftp`, named
  `mv-bl-<key>`: fetch to `mv-bl-<key>.src` (with
  `http-max-redirect-count=2` when `!FetchFollowsRedirects`); clear
  `mv-bl-<key>-next`; read the file in 32 KiB chunks with `/file read`,
  carrying a split line across chunks; per line by `Format` — JSON:
  `:deserialize from=json`, skip objects without `cidr`, keep `sblid` and
  "© The Spamhaus Project" in the comment; text: trim, skip empty and
  `#`/`;` lines; a bad line is skipped with `:continue` when
  `LoopContinue`, else an `:onerror` flag; refuse IPv4 wider than /8 and
  IPv6 wider than /16; add into `-next`; **only if n > 0**: remove the
  live list, `set [find list=…-next] list=…` (the swap in
  `internal/droplist/rsc.go`); then remove the fetched file. A fetch or
  parse failure leaves yesterday's list standing.
- Scheduler: `interval` 1h / 6h / 1d / 7d from `Refresh`; ET
  "weekdays" is `interval=1d days=mon,tue,wed,thu,fri` only when
  `SchedulerDays` — **the `days` syntax is from a changelog line, verify
  it on 7.24.4 before anything else in this part**. Start times fixed
  per list so no two fetch together: spamhaus 04:17, et 04:31, cins
  04:45, blde 04:59, greensnow 05:13, dshield 05:27, bindef 05:41.
- Rules: `/ip firewall raw add chain=prerouting src-address-list=mv-bl-<key> action=drop log=<yes|no> log-prefix="<prefix>" comment="mikroview blocklist: <key> (from)" place-before=0`;
  "both" adds the `(to)` twin with `dst-address-list`; Spamhaus with
  IPv6 adds the `/ipv6 firewall raw` pair on `mv-bl-spamhaus6`. Prefix:
  see owner question 1; until answered render `D|bl-<key>|`.
- `BlocklistUndo(key)` and `BlocklistUndoAll()`: the lines in
  `builder.html`'s `undoSpamhaus`, generalised (raw rules by comment,
  scheduler and script by name, lists by `^mv-bl-<key>`, files by
  `^mv-bl-<key>`). Undo never touches the push script.
- `scripts/routeroscommands` gains `-step=blocklist -version=<v>` (all
  seven lists on, defaults, placeholder address and token) so the CHR
  check reads the same generator the API does.
- **Real-RouterOS check** — `scripts/live-blocklist-chr.sh`, run by hand
  and recorded, like `docs/routeros-chr-exercise.md`: for each version,
  `CHR_VERSION=<v> scripts/live-routeros.sh up`, paste the block via
  `live-routeros.sh run` line by line, run each loader, then assert on
  the console: `print count-only where list=mv-bl-spamhaus` ≥ 1,000 and
  `mv-bl-et` ≥ 100, the raw rules at ordinal 0, scheduler present; paste
  the block again and assert the counts of scripts, schedulers and rules
  are unchanged; paste the undo and assert everything named `mv-bl` is
  gone and the drop-list rule (if any) untouched. Versions: **7.18.2**
  (floor), **7.21.x and 7.22.x** (the `:continue` and redirect
  boundary, both sides), **7.24.4** (`days`). Use the newest patch of
  each line that `download.mikrotik.com/routeros/<v>/` offers and record
  which. Transcripts go to `docs/routeros-verification-logs/<v>-blocklist.log`
  with the real addresses left in (they are the lists' own public
  data, not ours). Spamhaus asks for no automated fetch under an hour
  apart: four one-off fetches in a session are fine; never loop this.
  QEMU user-mode networking gives the CHR outbound internet; if the
  host cannot reach a source, say so in the log rather than skip
  silently.
- Proves: exact text for one list on 7.24.4 and on 7.19.4 (the drawn
  differences: `http-max-redirect-count`, `:onerror` vs `:continue`,
  `days`); below 7.18 returns an error; every add is guarded
  (`TestSetupDocAddsAreAllGuarded`'s walk); pasting twice is idempotent
  (`TestNewSetupCommandsAreSafeToPasteTwice`); re-paste re-enables
  (`TestRePasteResumesDisabledEnforcement`); undo names match forward
  names (`TestUndoBuildersMatchTheirForwardResourceNames`); every prefix
  ≤ `maxLogPrefixLen` and parses back through `stripPrefix` as action
  drop with the list slug as label; the Format recipes against a
  **real header and three real lines** of each source captured at build
  time (the Go tests check the parse recipe's assumptions — comment
  markers, field order — the way `TestParseEmergingThreatsCompromised`
  does; the loader itself is only ever proven on the CHR).

### 5. The builder API — `internal/api/blocklist.go`

- Admin-only, on the session-gated table beside `/api/droplist`:
  `GET /api/blocklist/builder?device=` → `{catalogue, leftOut, device, routerosVersion, reportedAt, standing ("ok"|"below-floor"|"no-push"), lists: [ledger row per list, part 6], ownDroplist: {held, confirmedAt}, undoAll}`;
  `POST /api/blocklist/builder/commands` body `{device, address, token, lists:[Choice]}` → `{parts, copyText, note}`.
  Validate as `validateSetupCommandsRequest` does (#1095): the choices
  are enums, the key must be in the catalogue, address and token through
  the same validators. The version comes from
  `RouterState.RouterOSVersion(device)` — never from the client.
- Part 1 of the block is `ScheduleCommands(PushScript(address, token, kinds ∪ {raw-rule, address-list-count}))`
  with `kinds` = `RouterState.PushedKinds(device)`; blank with
  `blocked: ["no-push"]` when the device has never pushed.
- Proves: handler tests in the shape of `TestHandleSetupCommandsPushRendersOnlyWithTokenAndKinds`,
  `TestHandleSetupCommandsRejectsUnsafeInput`,
  `TestDroplistWriteRoutesAreAdminOnly`; below-floor and no-push
  standings; the version is the pushed one even when the body claims
  another.

### 6. The ledger — `internal/api/blocklist.go`, `internal/routerstate`, `internal/setup`

- Per list a row `{key, state ("off"|"chosen"|"held"|"below-floor"), count, count6, loadedAt, firedToday, flags24h *int, undo}`:
  `held` when `AddressListCounts` has the list with count > 0;
  `firedToday` = latest `packets` minus the earliest sample since local
  midnight (the instance's timezone), floored at 0 and restarted when the
  counter falls (reboot); `flags24h` only for lists `FlaggedByMikroView`
  (read `internal/engine/shipped_known_bad_ip.go` for whether a flag
  names its feed; if it does not, send `null` and the card says
  "flags: not tracked per list" — do not invent a count).
- First-run bookkeeping in `internal/setup`: record number **8** (after
  `register`, documented beside `RECORD_NUMBERS`): `NoteWitnessed(8, receipt)`
  from the ingest handler when an `address-list-count` page first
  reports any `mv-bl-*` count > 0 (next to the `KindLogging` hook in
  `ingest.go`); `NoteMark(8, MarkSkipped)` from the tail's "Not now"
  through the existing `POST /api/setup/mark`.
- Proves: `firedToday` across midnight and across a reboot; `held` →
  `off` when a later push omits the list (`TestChangedPagesTotalDropsStalePages`);
  witness fires once (`TestLoggingReportSurvivesAReopen` for the
  persistence half).

### 7. The page — `frontend/src/components/blocklist/`

- `Builder.svelte` full-screen like `Wizard.svelte` (`blocklistState.open`
  in `lib/blocklist.svelte.ts`), the frame classes from
  `components/wizard/wizard.css`; port `builder.html`'s own rules (cards,
  choices, block, ledger, warnbox, `.more`) into `builder.css` — not
  restyled, not approximated. Pure logic in `lib/blocklistBuild.ts`:
  choices → request, `partsSummary` ("Copy — 2 lists · 8 parts"), the
  "Parts 5–7 are new; re-pasting 1–4 changes nothing" note (new = a list
  not yet `held`), rail rows, foot spec — the `wizardRun.ts` shape.
- The block is re-requested on every click (debounced; guard
  out-of-order responses as `commandsRequestSeq` does). Copy uses
  `lib/clipboard.ts`. Below floor and no-push render the warnbox body
  with the drawn upgrade commands (no-push copy: see Decisions).
- Token: on Settings open, mint once per page session the way
  `wizard.svelte.ts` mints `token` (device-scoped, named
  `blocklist-<device>`, module-lifetime, never persisted); inside the
  wizard's tail reuse `wizardState.token` when it is for this device.
- Proves (`Builder.svelte.test.ts`, `blocklistBuild.test.ts`, in the
  voice of `StepStand.svelte.test.ts`): three default cards on, four
  labelled cards carry their caveat fact; Not now empties a list's parts
  and renumbers; a 7.19.4 version hides "weekdays" and the 7.24 tag;
  below floor shows no card and the upgrade lines; the ledger reads
  held/chosen/not now from the API; Undo reveals that list's lines and
  hides again; "Undo everything" is in the foot; each row wears its
  ink through the custom property.
- Live scenario `frontend/scripts/live-blocklist-builder.mjs` (family
  `blocklist`): sign in as admin, post a synthetic push with
  `raw-rule` and `address-list-count` pages through the real ingest
  endpoint (as `live-routeros-ingest.mjs` does), open Settings ▸ drop
  list → the page, assert the version line, click ET to Not now and see
  the parts drop from 8 to 4, Copy, and the Spamhaus row read
  "held … refreshed". Add to the gate's scenario list.

### 8. The doors — Settings, and the first-run tail

- `Droplist.svelte`: one line in the group, "Block known-bad
  addresses…", opening the page for the group's selected router;
  `sectionLink.ts` gains nothing (the page is not a section).
- The tail (owner, 2a), first run only — the launch walk
  (`!wizardState.addingRouter`, `steps === SETUP_STEPS`) with no mark or
  witness under record 8: `wizardRun.ts` gains `StepId 'block'` after
  `stand`; `railRows` adds the sixth row with `n: '+'`, class `offer`,
  never locked; `ledgerRows` the sixth row ("Known-bad addresses · not
  blocked yet — …", button **Set it up**); `footSpec` on `stand` adds
  "Block known-bad addresses" beside Finish (Finish stays primary); the
  `block` stage renders `Builder.svelte`'s body in the wizard's frame
  with foot Back · Not now · Copy · Finish (`FootAction`s `to-block`,
  `block-not-now`, `block-copy`). After the paste the row is the drawn
  proof with Undo, the track gains "2 lists", the bar a chip. Port
  `tail.html`'s `.offer` rules into `wizard.css`.
- Proves (`wizardRun.test.ts`, `Wizard.svelte.test.ts`, `StepStand.svelte.test.ts`):
  the row and button appear on a launch walk and not on Add another
  router or Re-enrol…; Not now marks 8 skipped and the row reads "not
  now · Settings ▸ drop list"; a witnessed 8 turns the row green with
  the receipt; Run setup… after either shows no sixth row.
  `live-setup-wizard.mjs` gains the tail beat (offer → stage → Not now).

### 9. Docs and changelog

- `docs/configuration.md`: a "Block known-bad addresses on the router"
  section under the drop list's (what it does, that the router fetches
  each list itself, the seven lists with their terms and the two
  caveat labels, the refresh floors, the undo, the 7.18 floor).
- `docs/routeros-setup.md` §4c (part 2) and a short §8 pointing at the
  page; `docs/features.md` one paragraph; `CHANGELOG.md` under
  Unreleased (the page, the two push kinds, WizardVersion 6 and why a
  re-paste is nudged).
- The catalogue's licence findings for the four new lists on the issue
  (dependencies-and-data skill), in the same comment that reports the
  CHR run.

## Decisions made here (the design left them to the build)

- **Part 1 is the whole push script re-set, not a 4-line patch.** The
  push script embeds the ingest token and MikroView keeps only its hash,
  so a patch cannot be rendered later from Settings. The block re-renders
  `ScheduleCommands(PushScript(…))` with a token this page session minted
  (or the wizard's, on the tail). The fold line reads "the push script,
  re-set with the two new kinds · N lines". The old token stays valid
  until revoked in Settings; the page says so under the fold.
- **Version features, not dialect rows.** The builder's tokens key on
  thresholds (part 1), and `dialects.Rows` stays one dialect: a row is
  "renders the same wizard commands", and the wizard's blocks do not
  change by version. Alternative rejected: rows "b" at 7.22 and "c" at
  7.24 would reshape `TestRowsMatchTheContract` and the freshness job for
  something only this page uses.
- **The fetched file is removed after a successful load**; counts and
  `loadedAt` come from the address list, so flash on a small router is
  not held by a 213 KB file. Undo still sweeps `^mv-bl-<key>` files.
- **A load that parses zero entries does not swap**: an empty list is a
  failure, and yesterday's entries stand.
- **Refresh choices for the four new lists**: blde hourly / 6 h / daily
  (default 6 h; the source refreshes every 30 min); greensnow, dshield
  6 h / daily / weekly (default daily); bindef daily / weekly (default
  daily). Direction default "from" for all four; log default yes.
- **"Today" in "rule fired N times today"** is since local midnight
  from in-memory push samples; after a restart it counts from the first
  push since. The receipt says "today" as drawn; a session starting
  mid-day simply counts less.
- **IPv6 is offered only on Spamhaus** (the only source with a v6 list);
  the v6 refusal floor is /16.
- **The no-push state** (a router that has never pushed, so no version)
  is not drawn: render the below-floor warnbox layout with the copy
  "rb5009 has not pushed yet — this page writes for the version the
  push reports. Run the wizard's push step first." Record it on the
  issue as a state for Fable to draw; do not invent a different home.
- **Undo never touches the push script**: the extra kinds cost one
  small page per cycle and let the ledger notice the lists are gone.
- **Record number 8** for the tail's mark and witness; the frontend's
  `RECORD_NUMBERS` gains `block: 8` without joining `SETUP_STEPS`.
- **Flags per list** are shown only where MikroView flags from that
  list and the flag names its feed; otherwise the fact reads "MikroView
  does not flag from it", as the CINS card already says.

## Catalogue copy and defaults (2026-10-01)

Round 2 drew three cards; the four the owner admitted (1b+1c) and
CINS's on-state were not drawn. Read against the built
`internal/blcatalogue` (141f81e2). The `**…**` phrase is the one the
drawn cards set bold; `Guide` carries no markup, so part 7 must bold
it some way (a marker in the string is fine) or the drawn emphasis is
lost on every card.

**The four guide sentences** (the label's meaning for the operator is
the bold clause):

- **blocklist.de strongips:** Hosts that fail2ban on many servers
  reported in the last 48 hours — the small, high-confidence cut of
  blocklist.de. **No licence stated**: the site says nothing about who
  may use it, only that it comes "at your own risk", so whether that is
  enough is your call. MikroView does not flag from it.
- **GreenSnow:** Attacking hosts tracked by one company; about half
  are on blocklist.de too. **No licence stated**: the site says nothing
  about who may use the list and forbids republishing it — a router
  fetching it for itself is neither, but nobody has said yes, so that
  is your call. MikroView does not flag from it.
- **DShield top 20:** The twenty /24 networks that attacked DShield's
  sensors most over the last three days — whole networks, so an
  attacker's neighbours are dropped with it. **Not for business use**:
  the licence is non-commercial, fine on a home router and not on one
  a business runs, and MikroView cannot tell which this is. It does
  not flag from it.
- **Binary Defense banlist:** Attacking hosts on Binary Defense's own
  ban list. **Not for business use**: the header allows public use
  only, not commercial — take it on a home router, leave it off one a
  business runs; MikroView cannot tell which this is. It does not flag
  from it.

**CINS Army: "from", 6 h — confirmed, on its own grounds.** From,
because CINS lists scanners knocking on exposed ports: inbound is the
whole point, and a LAN device reaching one is not a finding the way
it is for Emerging Threats. 6 h, because the list changes hourly (so
daily leaves it stale most of the day) while each load is a minute of
address-list adds on a small router (the reason the card is off by
default) — four loads a day keeps up without hourly churn. The source
states no floor; hourly stays offered.

**Choice hints** (the drawn on-cards put one under each choice; none
are in the catalogue — part 7 carries them):

- log the drops, all five: Spamhaus's line ("Each drop reaches
  MikroView as a line naming the list; a home WAN sees a few an hour.").
- block, all five: "Inbound is enough — these are attackers and
  scanners; nothing on your LAN talks to them on purpose." DShield
  adds: "and blocking *to* a whole /24 would catch its innocent hosts."
- refresh — CINS: "Changes hourly, but each load is 15,000 adds; 6 h
  keeps up without doing that every hour." blde: "The source refreshes
  every half hour; 6 h is current enough for a 48-hour window."
  GreenSnow and Binary Defense: "The source states no cadence; daily
  is plenty, and a failed fetch leaves yesterday's list standing."
  DShield: "A rolling three-day window; daily keeps up with it."

**Where the build departs from the drawing, and the call:**

- CINS's guide says "the router" where the drawing says "rb5009".
  Accepted: catalogue data cannot hold a device name.
- The left-out record is ten rows, one per list, where the drawing
  pairs abuse.ch's four into two rows. Accepted — each gets its own
  reason — but the summary must read "ten lists", not "fourteen".
- The four new facts lines lack the "false positives rare/some" slot
  the three drawn ones carry. Fill it only where the research
  supports it: DShield **likely** (whole /24s; one in the sample was
  Google Cloud). The other three leave the slot out rather than
  invent a rating.
- The facts line's last slot is the terms word on the drawn cards
  (credited · BSD · free). On the four it is the owner's label
  verbatim — "no licence stated" / "not for business use" — so
  DShield's "CC BY-NC-SA" and Binary Defense's "non-commercial"
  change to "not for business use". The `Caveat` field is the same
  text; the page renders it once, in that slot.
- Counts (385, 4,935, 20, 1,514) are the build day's samples, a day
  after `catalogue.md`'s; fine, that is what the field is for.

## For the owner

**1** The ratified log prefix `D|drop|<list>` (12a) cannot be used
literally: MikroView's own convention is `<ACTION>|<label>|` with the
trailing bar as the terminator (`internal/routeros/prefix.go`), and the
whole prefix must stay within 15 characters (`maxLogPrefixLen`).
`D|drop|spamhaus` parses as label "drop" with "spamhaus" glued onto the
message, and `D|drop|spamhaus|` is 16 characters.
- **1a** `D|bl-<key>|` — reads as a drop, the label names the list,
  every one fits (`D|bl-greensnow|` is 15). The fall shows the label
  `bl-spamhaus`. (Recommended; the notes above build this.)
- **1b** `D|<key>|` — shorter, but nothing in the label says blocklist,
  and `et` or `cins` alone is cryptic in the router's own log.
- **1c** Keep `D|drop|<list>` and teach the parser a second convention
  — a parser change for every log line, for one feature.

## What stays out, and where it belongs

- What the fall and the stream show for a `D|bl-*` line: #1360's last
  bullet, its own issue; the drop ink and the label are the whole intent.
- `internal/blocklist` moving from `drop.txt` to Spamhaus's JSON: the
  round-2 finding, its own issue (opened in the same session as this
  build; part 3's URL test marks the difference deliberate).
- Forwarding the `script` topic so a refresh is seen within seconds
  rather than within a push cycle: a wizard change, separate issue.
- Flagging in MikroView from the five non-default lists: the
  `blocklist.sources` menu is its own vetted thing (configuration.md).
- Policy lists (Tor exits, Cymru bogons): `catalogue.md` leaves them out.
- Routers below 7.18 (owner, 18): refused, no fallback.
- Phones: desktop-first (#635), as the wizard.
- IPv6 lists other than Spamhaus's; a per-router "which lists are on"
  on the fleet page (not drawn).

Written by Fable 5.1, 2026-10-01; catalogue copy and defaults added
the same day.

## Built review (2026-10-02)

Reviewed: the eight built screenshots (page 7.24.4, 7.19.4, below
floor, left-out open, undo open; tail 01–03) against `round-2/shots/`,
and the built `frontend/src/components/blocklist/`, `lib/blocklistBuild.ts`,
`lib/wizardRun.ts`. Everything not listed below matches the drawing and
stands. Each item names where it lands; a builder applies it without
asking.

### The builder's reported departures

- **Bar, wordmark and strip from the real `wizard.css`** (upper-case
  chips; one flat strip on the page, the fall's lanes on the tail):
  accepted. The page is not a walk, so a flat strip is honest.
- **Part 1 is the three-command push re-set, `mv-push` named as the
  wizard names it**: accepted as the copy text. The *preview* must
  change (below, "The block's first screen").
- **Eight rail and ledger rows, the body scrolling**: accepted. Two
  consequences fixed below (the undo-all lines, the stand row).

### The parked calls

**1 — ink for the four admitted lists: one shared ink, not neutral,
not four.** Seven inks is noise and the palette has no four free hues
that stay clear of amber (`--drop`), purple (`--marked`), teal
(`--natted`), decision blue (`--log`), accept green and the warn
orange. Neutral is wrong because `--fg-muted` is the *off* colour: a
labelled list switched on reads as off (dot, card border, rail tick,
ledger tick, bar chip). One shared ink says what the owner's 1b+1c
said: these four are the class you take on your own judgement.

- `builder.css` `:root`: replace `--ink-list: var(--fg-muted)` with
  `--ink-labelled: var(--lane-guest)` (rose, `#d76a9e`; the only hue
  not already a meaning on these screens — the wizard's rules ink
  never shares a surface with a list ink, since the wizard's ledger
  stays green).
- `blocklistBuild.ts` `DRAWN_INKS`: add `blde`, `greensnow`, `dshield`,
  `bindef` → `'var(--ink-labelled)'`; `inkFor`'s fallback stays
  `var(--fg-muted)` for a key the catalogue does not know.
- Nothing else changes: the facts line's caveat slot stays `--fg-dim`
  mono as the other facts; the ink is identity, the label text is the
  meaning.

**2 — "Undo everything" opens under the ledger: keep, with three
fixes.** Same home as the wizard's own undo-everything, same toggle
label ("Hide the undo lines"), so the operator meets one idiom.

- `BuilderBody.svelte`: the bare `<pre aria-label="Undo everything">`
  gains the per-row undo's `.undo` wrapper and a note:
  "Paste on the router. Every list goes — rules, scripts, schedulers
  and the lists themselves; the push script stays, so MikroView sees
  them leave at the next push and every row goes back to **not now**."
- `lib/blocklist.svelte.ts`: `toggleUndoAll()` closes an open per-row
  undo (`undoOpen = null`) and `toggleUndo(key)` closes undo-all, so
  the same lines never show twice.
- With eight rows the pre lands below the fold at 1000 px and the foot
  button appears to do nothing: on opening, `scrollIntoView({ block:
  'nearest', behavior: 'smooth' })` the `.undo` wrapper (the page body
  is what scrolls).

**3 — the undrawn states.**

- **3a — observation line when every chosen list is already held:
  show one, not none.** `BuilderBody.svelte`, a third branch, class
  `obs quiet`: "**Nothing waiting.** {name} holds every list in the
  block; a paste now only applies a changed choice — it sets, never
  adds."
  And the state no branch covers, **no list on at all**: the Copy
  buttons (copy row and foot) disabled with label "Copy — no lists
  chosen"; the copy-row note "Turn a list on above; the block is
  empty."; the block pre shows one `.fold` line "# nothing to paste —
  every list is Not now" and no parts; no observation line;
  `blockHead` reads "0 parts". (`partsSummary` keeps its shape for
  every other count.)
- **3b — the drop-list rail row when the router holds nothing.** Two
  states, keyed on `ownDroplist.fetchedAt`, not on `held`:
  set up on the router but empty → tick in `--ink-own`, receipt
  "0 addresses · fetched 14:35 · from Settings" (the existing branch,
  with `held` 0 allowed); never fetched → dash, `off`, receipt
  "not on this router · Settings ▸ drop list" (the tail's own idiom
  for a set-aside thing). "none on this router · from Settings" goes.
- **3c — the no-push foot.** `Builder.svelte` foot hint for
  `standing === 'no-push'`: "MikroView never connects to the router —
  it writes for the version the router's push reports". Below floor
  keeps "— the upgrade is yours to run".
- **3d — the no-push warnbox: BUILD.md's line is the lead, not the
  whole box.** Mirror the below-floor box's four beats:
  `<p><b>{name} has not pushed yet.</b> This page writes its block for
  the RouterOS version the push reports, and nothing has arrived from
  this router.</p>`
  `<p>Run setup first (Admin ▸ Run setup…): its one paste installs the
  push.</p>`
  `<p>The first push tells this page the version, and the lists appear
  here.</p>` — no command box (MikroView has nothing to print until the
  push exists). Bar: `att warn` chip "no push yet — version unknown"
  in place of the push chip. Rail receipts: "after the first push"
  where below-floor says "after the upgrade" (`railRows`, the `below`
  branch, keyed on `standing`). Hint under the title stays "Not yet on
  this router."

### Other differences, screen by screen

Page, 7.24.4 and 7.19.4 (`page-7.24.4.png`, `page-7.19.4.png`):

- **The block's first screen — must change.** Part 1 now fills the
  198 px pre (two comment lines, three wrapped commands, a two-line
  fold): the operator sees only the push and must scroll to find any
  list. Drawn: part 1 is one line and part 2 is on the first screen.
  `internal/routeros/blocklist.go` `pushPart`: `shown` carries only the
  script add-or-set line (source elided as now); the scheduler line
  and `/system script run mv-push` stay in `Commands`/`copyText` and
  are named in the fold, which becomes "   the push script, re-set
  with the two new kinds · N lines · its schedule and run line re-set
  unchanged · a new token; the one before stays valid until revoked in
  Settings". The copy text is unchanged. Same rule as the loaders:
  the preview summarises, the copy is complete.
- **Live pill hidden at 0 lines — must change.** `Builder.svelte`
  `.bar .right` is wrapped in `{#if lines > 0}`; the wizard's bar shows
  "live · 0/s · 0 lines" at zero (`tail-01.png`). Drop the guard.
- **"Considered and left out — 10 lists" — must change** to "ten
  lists": `words()` exists in `BuilderBody.svelte`; the screenshot
  shows the numeral, so verify `data.leftOut.length` reaches it.
- Accepted as data or as the ratified build: no "drop list · N held"
  chip when the router holds none; "fired 0 today"; "catalogue
  reviewed 2026-10-01"; `mv-push`; the 7.19.4 block hiding `days` and
  the 7.24 tag.

Below floor (`page-below-floor.png`): matches, eight rows; the own
drop-list row takes 3b.

Left-out open (`page-more.png`): ten rows, abuse.ch split four ways —
accepted (BUILD.md, 2026-10-01). Summary word above.

Undo open (`page-undo.png`): the lines remove by `(from)` and `(to)`
comment separately and sweep the IPv6 list — accepted, that is the
generator's naming (part 4) and the drawing's single-comment line was
the sketch. Note copy matches.

Tail 01 (`tail-01.png`):

- **Ledger row names the long form — must change.** "MikroView flags
  them from Spamhaus DROP and Emerging Threats compromised IPs" →
  "… and Emerging Threats": `wizardRun.ts`, the tail's ledger receipt,
  use the catalogue's `short`, not `name`, for every list named there.
- Rail row, foot, track: match.

Tail 02 (`tail-02.png`):

- **"Where setup stands" rail row is dim with a dashed disc — must
  change.** Drawn (and `tail-01.png` itself): tick disc, title in
  `--fg-muted` like rows 1–4, clickable — it is where Back goes. On
  stage `block` the stand row must not take the `locked` class or
  `disabled`; `railRows`/`reachedIdx` in `wizardRun.ts` (reached is 4
  on `block`, so find what sets `locked`/`!can` on index 4 and exclude
  the `block` stage as `done` is).
- **Foot Copy renders at half opacity — must change.** Drawn: full
  `--accent`, Finish ghost. `footSpec` sets `disabled: !copyLabel`;
  the label is present in the shot, so the paleness comes from
  elsewhere — bind the button exactly as the page's foot does
  (`disabled={!blocklistState.block?.copyText}`, class `primary`) and
  recapture.
- Copy row, observation line ("Nothing to wait for until you paste…"),
  ledger with amber 1 and 2: match.

Tail 03 (`tail-03.png`): matches — the proof row, Undo, the "2 lists"
station and chip, the rail receipt.

### Carry-over

- The stage's ledger shows eight rows under a 198 px block: accepted
  with the page's.
- A per-row Undo open plus eight rows pushes the last rows under the
  foot: the body scrolls; accepted.
- No owner question arises from this review.

Written by Fable 5.1, 2026-10-02.

## Addendum — the rail's rows 1–4 on the tail (#1417, 2026-10-02)

**Ruling: as drawn — rows 1–4 are not clickable on the tail.** They
keep their `done` look (tick disc and receipt in the row's ink, title
in `--fg-muted`); they take neither `locked` nor any new class. Only
Where setup stands stays live, as Back.

Why. `round-2/tail.html` already draws it: on the build scene every
numbered row renders `disabled` and only the tick row carries
`data-act="goto-stand"`. The build departed from that (a defect under
"Building a ratified design"). It is also the rail's own rule — "once
the router is answering nothing goes back": on Where setup stands
(stage `done`) rows 1–4 are already disabled, and the tail is one step
past it, so a row that moved from the tail could do what the same row
cannot do one step earlier. Making them a second Back was considered
and rejected: clicking **The router** and landing on Where setup
stands is a surprise, and the ledger there is the real way back (each
row's Undo, "undo everything on the router first"). Not `locked`:
that class means "ahead of you, after the step before it"; these rows
are behind.

What to build, without asking:

- `frontend/src/lib/wizardRun.ts`, `railRows`: the `can` expression
  excludes `block` as it excludes `watch`, `tune` and `done` —
  `s.stage !== 'block'` added to the chain, `back ||` left in front
  so the stand row stays clickable. No change to `n`, `state`,
  `locked` or `current`.
- `frontend/src/lib/wizardRun.svelte.ts`, `gotoStep`: keep both
  guards as they are (the stand → `blockBack()` line, then the
  `ask`/`paste` return). The rail no longer calls it for rows 1–4 on
  the tail; the guard stays as the belt.
- `frontend/src/components/wizard/Wizard.svelte`: the rail's comment
  gains one line — "On the tail only Where setup stands is live, as
  Back." No markup change: `disabled={!r.can}` already renders it,
  `aria-disabled` stays unset (not locked), no `title`.
- `wizard.css`: nothing. `.step-row:disabled` (opacity 1, default
  cursor) and `:hover:not(:disabled)` already give the drawn look.
- Tests. `wizardRun.test.ts`, "goes back to nothing once the router is
  answering": add `block` to the stage loop with `ev({ cert, tail:
  offer })`, asserting rows 0–3 `can === false`, row 4 (`stand`)
  `can === true`, row 5 (`block`) `can === true`. `Wizard.svelte.test.ts`,
  "Wizard: the first-run tail": on the stage, `rows().slice(0, 4)` are
  all `disabled` with `aria-disabled` null, `rows()[4]` is enabled, and
  clicking `rows()[4]` returns to Where setup stands.
- Screenshots: `tail-02` needs no recapture; the only visible change
  is that rows 1–4 no longer highlight on hover.

Written by Fable 5.1, 2026-10-02.
