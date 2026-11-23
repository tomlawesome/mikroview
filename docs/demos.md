# Demos the owner reviews

Reference for building a demo instance. `scripts/demo-up.sh` runs the whole
sequence; this explains why each step is there. Moved out of `AGENTS.md`
2026-10-10.

## Run it

`scripts/demo-up.sh` generates the history envelope, brings the instance up
with the demo flags, restores the envelope, seeds `push`, `entities` and
`accounts`, starts the `feed`, and prints the build stamp to quote. Read the
sections below for what each step is for; `scripts/demo-up.sh --dry-run`
prints the commands without running them.

After the feed has produced real flags, `scripts/seed-demo.py mutate` runs
the last step by hand.

## History: seven nights from a restored envelope, not a week of waiting

The demo cannot wait seven nights for real watchlist streaks and a week of
retained corpus, and faking them by ingesting back-dated events would break
what MikroView promises: a night is only ever claimed once this process
watched it end to end, or it was already recorded
(`internal/engine/definitions_nights.go`). Owner decision on #959: **restore
it, don't ingest it**. `-restore` writes a store straight to disk -- it is
not the live ingest path the honesty check guards, so history arriving that
way breaks nothing.

`cmd/demo-history` builds the envelope: a `definitions` store with seven
nights of streaks for the same six watchlist entries `scripts/seed-demo.py`
seeds (#738's feeder characters, so the story continues into today once the
feed starts), and a `retained_events` store of matching back-dated traffic
-- both ending yesterday, so today is genuinely the live feed's. `-restore`
seals `retained_events` into `history.dir` under this deployment's own key,
the same encrypted per-day files a live process writes; it never travels as
ciphertext in the envelope itself.

Generate it before seeding, and restore it into the instance `live-env.sh`
just brought up, stopping the server for the write: generate the envelope,
bring the instance up, restore the envelope, then seed everything else and
start the feeder.

## Seed it: a demo on bare syslog is not a demo

`scripts/seed-demo.py` (#687) gives a running instance a whole story rather
than a log stream: pushed filter/NAT/address tables, named entities, a user
and a viewer account, and a watchlist with a healthy entry, a held one and a
deliberately broken ring. One estate throughout, so a name on the stream is
the same thing on the topography and in Entities.

**Bring the instance up with `MV_DEMO_DEVICES=1`**, or the seeding is
half-wasted. A pushed rule/NAT/address table is keyed by device id;
`seed-demo.py` mints its tokens against router names and streams syslog from
one loopback address per router. Unless `live-env.sh` declares those
addresses, the registry invents a discovered device per source IP and the
pushed tables sit under ids no device has. Both halves report success and
never meet (#709).

It also sets `networks.home` to the estate's inside address blocks (#1481), so
the demo's inside hosts read as inside; unset, the instance has no home networks.

`MV_DEMO_BUILD=1` matters here too: it ships the self-destroying service
worker (#713) that stops a browser serving back a stale precached build
across the many rebuilds a demo goes through. `live-env.sh up` no longer
sets it by default -- the live-check gate needs a real, non-self-destroying
worker to prove its own navigation scenario (#753) -- so a demo has to ask
for it explicitly.

Use `push`, `entities` and `accounts`, never `all`: `all` also runs
`watchlist`, which would create six more, blank watchlist entries beside the
six the restored envelope already carries with their seven nights of
history.

`feed` is the piece that takes time: the metrics hourline, the register and
the fall's memory stay flat until real time has passed under them.

Without this the fall's bands read "other traffic -- not in a pushed rule
table", the topography degrades to boundary-derived zones, and the empty
surfaces get reported as UI defects. Every UI review this project ran before
the seeder existed was hampered that way, and reviews after it existed were
still handed bare-syslog instances because nobody checked.

## Stamp it: a demo must say which build it is

**Show a version on every demo instance the owner is asked to look at, and
quote the same version when handing it over.** Round 30 lost most of a day to
this: fault after fault was fixed in the tree while the running instance had
been built before the fixes, so the owner kept finding faults that were
already fixed and reasonably concluded the work had been lost. Nothing in the
browser said which build it was, so neither side could tell.

A demo whose version cannot be read off the screen is not evidence of
anything. If the owner reports a fault that the tree says is fixed, compare
the demo's stamp against the branch before touching the code -- the usual
answer is a stale build, not a lost fix.
