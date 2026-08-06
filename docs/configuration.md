# Configuration

## Precedence

`defaults < YAML file < environment variables < CLI flags` — each layer
overrides the one before it. The list of monitored devices only comes
from the YAML file (see below for why).

## config.yaml

Copy `deploy/config.example.yaml` to `deploy/config.yaml` and edit it —
`docker-compose.yml` mounts that path into the container at
`/etc/mikroview/config.yaml`.

```yaml
listen:
  syslogUdp: ":1514"
  syslogTcp: ":1514"
  http: ":8080"
  httpRedirect: ":8081"

store:
  retention: 24h
  maxEvents: 200000

devices:
  - id: core-router
    name: "Core Router"
    sourceIp: 192.168.1.1
```

- `store.retention` — how far back `/api/events` and the UI will show,
  as a Go duration string (`24h`, `12h30m`, ...).
- `store.maxEvents` — the hard cap on events held in memory at once (a
  fixed-size ring buffer); this bounds memory use regardless of traffic
  volume. Once full, the oldest events are overwritten first.
- `devices` — maps a syslog source IP to a friendly name. Routers
  sending logs from an IP *not* listed here still appear in the UI and
  `/api/devices`, labelled by their raw IP with `configured: false`, so
  you can identify and add them rather than silently losing their events.

**File permissions**: the container runs as a fixed non-root user (uid
65532, distroless `nonroot`) that is unrelated to any user on the Docker
host. If `config.yaml` isn't world-readable (or owned by a matching
uid/gid), the container will fail to start with a permission error —
`chmod 644 deploy/config.yaml` after editing it.

## Logging

Mikroview's own server output (not event data -- see `store.retention`
above) is leveled and colorized, one line per entry:

```
18:43:44 INFO  auth        │ no decision made yet -- showing the first-run choice screen
18:43:45 WARN  flags       │ permission denied opening flags.json -- continuing in-memory-only
18:43:46 ERROR syslog-udp  │ listen udp :1514: bind: address already in use
```

```yaml
log:
  level: info
```

**On boot**, before the leveled log lines start, mikroview prints its
ASCII wordmark once (plain text, not a log line -- always shown,
regardless of `level` or whether stdout is a terminal), followed by a
`version` line identifying which build is running -- the short commit
SHA it was built from (see `.github/workflows/docker.yml`), the
practical stand-in for a registry digest since a binary can't know its
own digest at build time. If the persisted version marker
(`/var/lib/mikroview/version`) doesn't match the running build, that
line reads `upgraded from <old> to <new>` instead -- confirming a
`docker compose pull`/image update actually took effect, versus a
routine restart on the same build. This boot sequence doesn't apply to
the CLI recovery commands (`-healthcheck`, `-list-users`, etc.) --
only the real server-start path.

- **`level`** — one of `debug`/`info`/`warn`/`error` (case-insensitive).
  Defaults to `info`. Anything unrecognized (a typo, an empty value)
  falls back to `info` silently, the same way every other malformed
  value in this app degrades rather than failing startup over a log
  setting.
- **Color** follows the [NO_COLOR](https://no-color.org) convention and
  auto-disables when stdout isn't a terminal -- piping to a file,
  `docker logs | grep`, or a log collector all see plain text, not raw
  ANSI escapes.
- The component column (`auth`, `tls`, `flags`, `syslog-udp`, `http`,
  ...) identifies which part of mikroview logged the line -- the same
  names used throughout this doc and SECURITY.md for the pieces they
  refer to.
- This does **not** apply to the CLI recovery commands' own output
  (`-list-users`' table, `-reset-password`'s password prompts and
  success message, etc.) -- those print directly to stdout/stderr for
  scripting/piping, not through this leveled path.

## GeoIP country flags (optional)

mikroview can show a country flag next to public source/destination
addresses, using a MaxMind GeoLite2 (or paid GeoIP2) **Country** or
**City** database. This is entirely opt-in: mikroview doesn't bundle a
database or call out to MaxMind at runtime, since their license requires
you to create your own free account to obtain one.

1. Sign up for a free [MaxMind GeoLite2 account](https://www.maxmind.com/en/geolite2/signup)
   and download `GeoLite2-Country.mmdb` (or generate a license key and use
   their `geoipupdate` tool to keep it current).
2. Mount the `.mmdb` file into the container and point mikroview at it
   with `MIKROVIEW_GEOIP_DB_PATH` (or `geoip.dbPath` in `config.yaml`, or
   `-geoip-db` for local development).

If the path is unset, empty, or the file can't be opened/parsed, mikroview
logs a note at startup and simply shows no flags — this is never a fatal
error.

## IP reputation lookup (optional)

Clicking the "investigate" affordance next to a public source/destination
IP in the live view queries a threat-intel source and shows the result in
a popover (open ports, hostnames, known CVEs, abuse score). This proxies
through the backend so no key ever reaches the browser, and caches each
IP briefly to conserve free-tier quota.

- **Shodan InternetDB** — free, keyless, always used, no configuration
  needed.
- **AbuseIPDB** — optional, needs an API key (free tier: 1000 lookups/
  day). Set `reputation.abuseIPDBKey` in `config.yaml` or the
  `MIKROVIEW_ABUSEIPDB_KEY` env var. Adds abuse score, report count,
  country, and ISP to the result.

Unconfigured, the feature still works with Shodan-only results; private/
loopback/link-local addresses are rejected server-side regardless of
configuration.

## Port lookup

Clicking the "i" affordance next to a source/destination port shows what
that port is commonly used for (`frontend/src/lib/commonPorts.ts`) --
standard/well-known services, common databases, common self-hosted apps,
remote access, VPN, messaging, and a few ports historically associated
with malware/backdoors. Unlike the IP lookup above, this is pure local
static data with no network call: only shown for ports with a known
entry, and there's no way to configure or disable it.

It's a curated reference, not an exhaustive IANA registry dump -- if a
port you care about is missing, add an entry to `commonPorts.ts`.

**`tools/docker-ports/list-exposed-ports.sh`** is a separate, standalone
companion script (not part of the running app) for self-hosters: run it
directly on a Docker host to list every running container's published
ports, flagging which are bound to a public interface (`0.0.0.0`/`::`)
versus loopback-only (`127.0.0.1`/`::1`) -- useful for cross-referencing
"what does mikroview say this port usually is" against "what's actually
listening on it, on this host."

## Friendly names (optional)

RouterOS auto-generates a rule identifier (e.g. `r13`) in the log line
when a firewall rule has no `comment=` set, and every host only ever
shows up as a raw IP. `config.yaml`'s `ruleNames`/`hostNames` maps let
you give either a friendly display name, shown in place of the raw value
everywhere it appears (live table, CSV export) -- the raw value (rule
label, IP) is always still what filtering, grouping, and the hover
tooltip use, this is display-only.

```yaml
ruleNames:
  r13: "Block known scanners"
hostNames:
  192.168.1.50: "Living room NAS"
```

Both are YAML-only (like `devices`) rather than env-var-configurable --
a map doesn't translate cleanly to an env var without an awkward
numbered-key scheme, the same reasoning `devices` already follows. A
rule or host not listed still displays exactly as it does today (the
raw label/IP), so this is purely additive.

Setting a `comment=` on the rule in RouterOS itself is the more durable
fix for rule names specifically, if you're able to -- `ruleNames` is for
when you can't or don't want to edit RouterOS config directly, or want a
different name in mikroview than the RouterOS comment.

## Entities: UI-managed host/rule/port labels and tags (optional)

`ruleNames`/`hostNames` above are YAML-only: a label survives a restart,
but changing one means editing `config.yaml` and restarting the
container. **Entities** are the same idea (a label attached to a rule
label, host IP, or -- issue #109 -- a port number), plus open-ended
tags, managed live from the UI (**Menu → Entities**, admin-only) with no
restart needed -- the shared foundation two features build on (a
mail-sender allowlist, and this IP/port/rule aliasing UI), so the record
shape is deliberately generic (`type`, `key`, `label`, `tags`) rather
than shaped around either one specifically. `type` is a free-form
string, not a closed set -- `host`, `rule`, and `port` are just the
values mikroview's own display sites (the live table, CSV export,
`internal/naming.Resolver`) know to look up, not a validation allowlist.

```yaml
entities:
  # Where entity records are persisted, as a small JSON file. Same
  # optional-persistence contract as flags.storePath: left unset,
  # entities still work, they just don't survive a restart. If you set
  # this in the container, mount a volume for its parent directory --
  # see deploy/docker-compose.yml.
  storePath: "/var/lib/mikroview/entities.json"
```

**One-time migration**: the very first time mikroview boots against a
given entities store, if `ruleNames`/`hostNames` are non-empty it
imports each entry as an entity (`type: rule`/`type: host`, `key` = the
map key, `label` = the map value) so an existing deployment's aliases
become UI-editable instead of disappearing on upgrade. This is tracked
with a marker persisted alongside the entities themselves, not by
"is the store currently empty" -- so it really is one-time: deleting
every entity later (one at a time, from the UI) does not cause them to
reappear on the next restart. `ruleNames`/`hostNames` stay supported
afterward as a YAML-only fallback for a rule/host with no matching
entity. Ports have no `config.yaml`-level equivalent -- entities are the
only way a port ever gets a friendly name.

Entities are managed via `GET`/`POST`/`DELETE /api/entities`
(admin-gated the same way `POST /api/auth/users` is -- see
[API reference](#api-reference)), or the **Entities** panel in the menu.
That panel is also where you'd go to name something *without* already
knowing its raw IP/rule label/port number: a **Discovered** section
lists hosts, rules, and ports seen in live traffic that don't have a
label yet (mirroring the auto-discovered-device pattern the **Fleet**
view already uses for RouterOS sources), each with a one-click "Name it"
action. Discovered rules come from mikroview's own unbounded-time
per-rule usage record (`GET /api/rules`); discovered hosts/ports are
derived from the events currently loaded in your browser tab, so that
list is only as complete as what's been seen there so far -- the entity
itself, once named, is fully persisted regardless.

## Behavioral flags (optional, on by default)

mikroview watches the ingested event stream for a small set of patterns
worth a human's attention, and raises a **flag** (visible in the UI, in
`GET /api/flags`) for each one -- never an automatic action. This is an
"interrogation helper," not an intrusion-prevention system: nothing here
blocks, drops, or reports traffic anywhere. If you're running a proper
IPS alongside mikroview (e.g. CrowdSec), this is meant to complement it,
not duplicate it -- see the detector descriptions below for what each one
is actually good at spotting.

Detection itself is always on and needs no configuration; every
threshold below has a sensible default and is only worth changing for an
unusually quiet or unusually busy network.

The port-scan, activity-spike, critical-port, distributed-brute-force,
internal-recon, outbound-anomaly, low-and-slow-port-scan, and
off-hours-activity detectors only count events whose
RouterOS-reported connection state is `new` (or absent, for setups that
don't log connection state at all) -- if your ruleset logs both
directions of an established connection on the same rule, a busy host's
ordinary return traffic would otherwise look identical to new connection
attempts and trigger false positives purely from being legitimately
busy.

```yaml
flags:
  storePath: "/var/lib/mikroview/flags.json"
  portScanThreshold: 15
  portScanWindow: 60s
  activitySpikeThreshold: 200
  activitySpikeWindow: 60s
  criticalPorts: [21, 22, 23, 445, 3389, 5900, 8291, 8728, 8729]
  criticalPortThreshold: 5
  criticalPortWindow: 5m
  globalSpikeMultiplier: 4
  globalSpikeMinEPS: 5
  globalSpikeWarmupSamples: 20
  distributedBruteForceThreshold: 10
  distributedBruteForceWindow: 5m
  outboundAnomalyThreshold: 25
  outboundAnomalyWindow: 5m
  internalReconThreshold: 10
  internalReconWindow: 60s
  ruleSpikeMultiplier: 5
  ruleSpikeMinRate: 0.2
  ruleSpikeWindow: 60s
  ruleSpikeWarmupSamples: 20
  repeatedDropsThreshold: 10
  repeatedDropsWindow: 15m
  hostActivityMultiplier: 3
  hostActivityWarmupSamples: 20
  lowSlowScanWindow: 3h
  lowSlowScanPortThreshold: 8
  lowSlowScanHostThreshold: 5
  lowSlowScanMinObservation: 45m
  lowSlowScanDropRatio: 0.8
  lowSlowScanBaselineMultiplier: 3
  offHoursStartHour: 23
  offHoursEndHour: 6
  offHoursMinSampleDays: 14
  offHoursMinCount: 5
  deviceStaleAfter: 15m
  ruleUsageStorePath: "/var/lib/mikroview/rule-usage.json"
  staleRuleDays: 30
  staleRuleCheckInterval: 1h
```

- **`storePath`** — where raised/cleared flags are persisted, as a small
  JSON file. This is the one deliberate exception to mikroview's
  otherwise in-memory-only design (see [SECURITY.md](../SECURITY.md)): a
  flag is meant to stay visible until a human clears it, so unlike
  everything else it survives a restart. Left empty (the default),
  flags still work, they just reset like everything else does. If you
  set this in the container, mount a volume for its parent directory —
  see `deploy/docker-compose.yml`.
- **Port scan** — one source touching `portScanThreshold`+ distinct
  destination ports within `portScanWindow`. Applies to any source,
  internal or external.
- **Activity spike** — one source's own event rate vs. a slow-moving
  baseline *of that specific host* (same EMA technique the global-spike
  and rule-spike detectors use, just scoped per source), at
  `hostActivityMultiplier`× or more, and still gated by an absolute
  floor of `activitySpikeThreshold`+ events within `activitySpikeWindow`
  so a nearly-idle host doesn't "spike" from one extra event. A host
  that's always busy is judged against its own normal rather than one
  number applied to every host equally — fixes the false-positive
  pattern where a legitimately busy server (e.g. a database with many
  clients) got flagged just for being itself. `hostActivityWarmupSamples`
  is how many observations a host needs before a flag can reach full
  confidence (see below) — a brand-new source with almost no history
  can't produce a high-confidence flag no matter how extreme its first
  few readings look.
- **Critical-port attempts** — `criticalPortThreshold`+ attempts against
  one of `criticalPorts` within `criticalPortWindow`, from an *external*
  source only (a LAN device reaching your own router's Winbox port is
  normal; the same from the internet usually isn't). The default port
  list covers SSH/Telnet/FTP/SMB/RDP/VNC and RouterOS's own
  Winbox/API ports (8291/8728/8729) — worth watching precisely because
  they're MikroTik-specific and a common target once a scanner has
  fingerprinted a device as RouterOS.
- **Global volume spike** — current events/sec vs. a slow-moving
  baseline of itself (an exponential moving average, not a fixed
  number), so it adapts to your network's real traffic level over time
  rather than needing to be hand-tuned. `globalSpikeMinEPS` is a floor
  below which a "spike" isn't worth flagging (e.g. 2 events/s against a
  0.5 events/s baseline is technically 4x, but not meaningfully busy).
  Flags carry a confidence score (same z-score-against-baseline approach
  as activity spike) rather than firing on the multiplier alone;
  `globalSpikeWarmupSamples` is how many `Check()` cycles the baseline
  needs before a flag can reach full confidence -- this only affects the
  confidence number, never whether or when a flag fires.
- **Distributed brute-force** — `distributedBruteForceThreshold`+
  *distinct* external source IPs hitting the *same* critical port within
  `distributedBruteForceWindow`. The inverse of critical-port attempts
  (which is one source hitting a port repeatedly): this is many
  different sources hammering one port, the signature of a
  botnet/credential-stuffing campaign rather than a single noisy
  scanner.
- **Outbound anomaly** — a LAN source contacting
  `outboundAnomalyThreshold`+ distinct *external* destinations within
  `outboundAnomalyWindow`. One of the strongest signals of a
  compromised/malware-infected device (C2 beaconing, botnet
  participation) available without a separate security product —
  nothing else notices "this device just started talking to 30 IPs it's
  never touched before."
- **Internal reconnaissance** — a LAN source contacting
  `internalReconThreshold`+ distinct *internal* destinations within
  `internalReconWindow`. A network sweep: the classic lateral-movement
  signature of an attacker who already has a foothold on the LAN,
  probing for what else is reachable.
- **Rule hit-rate spike** — a firewall rule's own hit rate vs. a
  slow-moving baseline of *that rule specifically* (same EMA technique
  as the global spike, just scoped per rule), at `ruleSpikeMultiplier`×
  or more. Catches a normally-quiet rule suddenly lighting up even when
  it's nowhere near large enough to move the network-wide total —
  `ruleSpikeMinRate` (events/sec) is the same kind of noise floor as
  `globalSpikeMinEPS`. Flags carry a confidence score the same way
  global volume spike does; `ruleSpikeWarmupSamples` plays the same role
  as `globalSpikeWarmupSamples` -- how many observations this specific
  rule's baseline needs before a flag can reach full confidence, not a
  factor in whether or when it fires.
- **Repeated drops on a port** — the same (source, destination port)
  pair getting dropped/rejected `repeatedDropsThreshold`+ times within
  `repeatedDropsWindow`, against a *locally-hosted* service (unlike
  critical-port attempts, not restricted to a curated port list or to
  external sources). Aimed at self-hosters: this is very often a
  misconfigured port-forward or firewall rule — the real client keeps
  retrying a port that isn't actually open the way you think — rather
  than necessarily an attack, so treat it as "worth a look," not
  "critical."
- **Low-and-slow port scan** — a scan deliberately paced to stay under
  the fast port-scan detector's short `portScanWindow`. Judged over the
  much longer `lowSlowScanWindow` (hours, not seconds), and deliberately
  *not* a single "distinct ports per hour" threshold — that alone is
  exactly the kind of thing container orchestration, health checks, and
  browsers legitimately trip. Instead, all of the following must hold at
  once before anything fires:
  - **Destination breadth on both axes** — `lowSlowScanPortThreshold`+
    distinct destination ports *and* `lowSlowScanHostThreshold`+ distinct
    destination hosts within the window. A real scan spans many
    host:port pairs, not many ports probed against one already-known
    host (that's the fast port-scan detector's job) or one port probed
    against many known hosts.
  - **A source's own destination-breadth rate vs. its own baseline** —
    same per-source EMA technique as activity-spike, at
    `lowSlowScanBaselineMultiplier`× or more, so a source that always
    talks to many destinations isn't flagged just for continuing to do
    what it always does.
  - **A high drop/reject ratio** — at least `lowSlowScanDropRatio` of the
    source's tracked attempts within the window must have been refused.
    Paced scan traffic against a target mostly gets dropped/rejected;
    legitimate low-rate access to real services mostly gets accepted.
  - **A minimum observation floor** — a source must have been under
    observation for at least `lowSlowScanMinObservation` before it's
    eligible to fire at all, so a source only seen briefly can't produce
    a flag no matter how its first few readings look.

  Confidence is the *minimum* of four sub-scores (port overshoot, host
  overshoot, drop-ratio strength, baseline deviation) — the
  weakest-clearing signal bounds the overall score, consistent with
  requiring several independent signals rather than trusting the
  strongest one alone.
- **Device silence (issue #98)** — a *configured* device (`devices` in
  this file, `Configured: true` in `GET /api/devices`) whose `lastSeen`
  hasn't advanced in `deviceStaleAfter`. Unlike every other detector
  above, this isn't a per-event pattern — it's the *absence* of events —
  so it's checked periodically (same ticker-based shape as global volume
  spike) rather than as events arrive. Only devices that have sent at
  least one event are eligible: a freshly configured device that hasn't
  said anything yet is "never seen," a distinct state `GET /api/devices`
  also reports, not "gone silent." Auto-discovered sources (seen on the
  wire but not listed in `devices`) are never eligible either — there's
  no expected cadence to fall silent from. Set `deviceStaleAfter: 0` to
  disable this detector entirely; the default (15m) sits well above
  RouterOS's normal bursty syslog gaps so an ordinarily quiet stretch
  never false-positives. The flag's target is the device's configured
  `id`, not an IP.

- **Stale rule (issue #102)** — a firewall rule that hasn't fired in
  `staleRuleDays`+ days, either dead weight or, worse, an unnecessary
  hole — flagging it for review closes attack surface at essentially no
  cost. Unlike every other detector above, this one doesn't watch the
  live event stream: it reads a separate, long-lived per-rule
  `firstSeen`/`lastSeen`/`count` record (persisted to
  `ruleUsageStorePath`, same optional-persistence contract as
  `flags.storePath`) that's updated on every ingested event alongside
  `internal/store`'s own in-memory rule counters, then periodically
  swept every `staleRuleCheckInterval` for anything past the threshold.
  That separate record exists specifically because `internal/store`'s
  counters are windowed to `store.retention` (24h by default) — nowhere
  near long enough to notice "hasn't fired in a month." **Accepted
  trade-off:** mikroview only sees a rule when it fires in syslog, with
  no visibility into the router's actual configured rule set (it's
  passive-syslog-only) — so a rule you've already removed will keep
  surfacing as stale until you manually clear the flag. Harmless: the
  implied suggestion ("consider removing this") is a no-op if it's
  already gone, and the alternative failure mode — a genuinely
  forgotten, still-open rule going unflagged — is worse. Unlike every
  other detector, stale-rule doesn't currently support the live
  enable/scope toggle described in [Per-detector
  toggles](#per-detector-toggles-and-scope-restrictions-optional), and
  doesn't attach a confidence score (see below) — set `staleRuleDays`
  high enough (or clear flags as they come up) if it's too noisy for
  your ruleset.

- **Unexpected mail sender (issue #108)** — a LAN source originating an
  outbound connection to an external destination on an SMTP port (25,
  465, or 587) that isn't tagged `trusted-mail-sender` on its host entity
  (see [Entities](#entities-ui-managed-hostrule-labels-and-tags-optional)
  above). Deterministic, like new-device and stale-rule detection — no
  threshold or window to tune, it fires the first time a given untagged
  source does this at all. Distinct from **Outbound anomaly** above,
  which only fires on distinct-*destination-count* spread over a window
  — a single new SMTP connection to one destination wouldn't trip that.
  If you self-host your own outbound mail server, tag its host entity
  `trusted-mail-sender` once (**Menu → Entities**, admin-only, or `POST
  /api/entities`) and mikroview never flags it for this again. Like
  stale-rule, this doesn't currently support the live enable/scope
  toggle described in [Per-detector
  toggles](#per-detector-toggles-and-scope-restrictions-optional), and
  doesn't attach a confidence score (see below).

- **Off-hours activity** — a source active during a fixed clock window
  (`offHoursStartHour`-`offHoursEndHour`, wrapping past midnight, 23:00-
  06:00 by default) it has no established history of being active in.
  Extends the same per-host EMA baseline technique activity-spike uses,
  but tracked 24 times over — one independent baseline per hour of the
  day — rather than once per host, so "usual for this host at 3am" and
  "usual for this host at 3pm" are judged separately. Deliberately
  *not* "any activity inside the configured window": that alone fires
  on trivial noise (a phone syncing, a scheduled job, a clock-skewed
  IoT device) with nothing to judge it against. Two independent floors
  gate every flag, both required:
  - **`offHoursMinSampleDays`** — that specific hour must have been
    observed on at least this many distinct *prior days* before a flag
    can fire for it at all, no matter how extreme the count. A single
    busy night isn't a baseline; this is what makes firing on one
    impossible rather than just unlikely.
  - **`offHoursMinCount`** — an absolute floor on top of the z-score
    check. A host never before seen at some hour has a near-zero
    baseline for it, so even a handful of events there would look like
    a huge deviation by z-score alone — this is the same role
    `activitySpikeThreshold` plays alongside `hostActivityMultiplier`
    for activity-spike.

  Every hour's baseline is tracked continuously regardless of the
  configured window — only whether a flag can *fire* is restricted to
  hours inside it, so widening or narrowing the window later doesn't
  discard history already being collected. "Off-hours" here is a fixed,
  operator-set window rather than a per-host-learned quiet period — an
  explicit design choice (the issue that scoped this feature left it
  open): a learned window has its own bootstrapping problem (deciding
  which hours are "quiet" needs the same history this detector is
  already gated on) and is harder for a human reviewing a flag to
  sanity-check than a plain clock range. Revisit if real-world use shows
  a fixed window doesn't fit typical deployments well.

**Confidence score.** Every detector except global-volume-spike and
rule-hit-rate-spike attaches a `confidence` percentage (0-100) to each
flag it raises, shown in the UI as e.g. "73% confidence" — but not all
of them mean the same thing by it, and it's worth knowing which kind
you're looking at:

- **Statistical (activity-spike, off-hours activity).** These two make a
  real statistical judgment call rather than a deterministic threshold
  crossing: each combines (1) how much history backs the relevant
  baseline and (2) how far the current reading deviates from it (a
  z-score). Off-hours activity applies this per hour-of-day rather than
  once per host — "how much history" is that specific hour's distinct
  prior days (`offHoursMinSampleDays`), not overall event count. Both
  flags' own detail text spells out the actual baseline value, observed
  value, and sample count behind the score.
- **Overshoot-based (port-scan, critical-port, distributed
  brute-force, outbound anomaly, internal reconnaissance, repeated
  drops, device silence).** These seven are plain threshold crossings
  with no history or baseline behind them, so their confidence instead
  measures *how far past the threshold* the observed count is: just
  crossed reads low, three times the threshold or more reads 100%. For
  device silence specifically, the "count" is how long the device has
  gone quiet relative to `deviceStaleAfter`. This is a materially
  weaker claim than activity-spike's statistical score — it says
  nothing about whether the pattern is unusual for that specific
  source, only how large it is relative to the configured line. Treat a
  95% overshoot-based flag as "well past the configured threshold," not
  "95% likely to be malicious."
- **Composite (low-and-slow port scan only).** The minimum of four
  sub-scores — port overshoot, host overshoot, drop-ratio strength, and
  baseline deviation (the last computed the same way as activity-spike's
  statistical score). Reflects "how convincingly did the *weakest*
  required signal clear," not an average or the strongest signal alone —
  deliberately conservative, since this detector already requires
  several independent things to be true before it fires at all.
- **Not yet scored (global volume spike, rule hit-rate spike).** Both
  already track a slow-moving EMA baseline internally (same technique
  activity-spike uses), just without a confidence score attached yet —
  a known gap, filed separately.
- **Not scored (stale rule, unexpected mail sender).** Both are
  deterministic "has this happened at all" checks — stale rule's "has
  this rule's `lastSeen` crossed `staleRuleDays`," unexpected mail
  sender's "has this untagged source ever done this before" — with no
  baseline or overshoot concept behind either one, so there's nothing to
  score.

`confidence` is `null` in `GET /api/flags` for a detector that doesn't
score at all, never an implied 100%.

**Reputation-informed floor (optional, requires an AbuseIPDB key).**
When `reputation.abuseIPDBKey` is configured (see
[IP reputation lookup](#ip-reputation-lookup-optional)), `critical_port`,
`port_scan`, `activity_spike`, `repeated_drops`, `low_slow_scan`, and
`off_hours_activity`
additionally get an
async, best-effort AbuseIPDB lookup against the flag's source IP the
first time it's raised (not on every re-fire). If the IP has a known
abuse score, that score becomes a *floor* on the flag's confidence —
`finalConfidence = max(existingConfidence, abuseScore)` — never a
replacement and never a reduction: a clean or unavailable reputation
result is absence of evidence, not evidence of innocence, so it's never
allowed to pull an already-higher score down. `distributed_brute_force`
and `outbound_anomaly` get the same treatment, but against a *sampled
group* rather than one IP (they're keyed by many distinct source IPs or
destinations, not a single one) — up to 10 members are sampled, and at
least 3 of them must actually return reputation data before the
aggregate (their mean score, discounted by how much of the sample was
actually filled) is trusted enough to apply. `internal_recon` is
excluded because its destinations are internal LAN IPs and reputation
data doesn't exist for private ranges at all; `rule_spike` is excluded
because its target is a rule label, not an IP. This reuses the same
`internal/reputation.Client` and 15-minute cache the on-demand IP-lookup
popover already uses, so a manual lookup you've just done and an async
detector check for the same IP share results rather than double-querying
AbuseIPDB.

The same lookup also captures a **reputation snapshot** on the flag
(abuse score, report count, ISP, Shodan ports/hostnames/vulns where
available) *as of when it resolved* -- not fetched live later, since the
point is what the target looked like when it fired, not what it looks
like now. It's stored regardless of whether AbuseIPDB is configured
(the keyless Shodan source alone is still worth capturing) -- only the
confidence floor specifically needs an AbuseIPDB score. Only ever set
for the six single-IP detectors above; the sampled-group detectors
have no single coherent snapshot to attach. Expandable in the UI
alongside the target's country (from the same GeoIP lookup already
applied to the underlying event) and, for `port_scan`,
`distributed_brute_force`, `outbound_anomaly`, `internal_recon`, and
`low_slow_scan`, the specific ports/hosts actually involved -- and for any detector, NAT
translation info when the triggering event had one. `GET /api/flags`
exposes all of this as `reputation`, `country`, and `evidence` fields.

**Tor/hosting-provider signal (issue #58).** AbuseIPDB's response
already includes an `isTor` flag and a `usageType` classification (e.g.
"Data Center/Web Hosting/Transit") that mikroview now captures alongside
the abuse score. Either one also contributes to the confidence floor,
using the same floor-raise-only reasoning as the abuse score itself,
with the *strongest* of the two independent signals applied: a Tor exit
node raises the floor to 60, a hosting/data-center address to 30 --
deliberately smaller than a real abuse score, since neither is proof of
malice on its own (Tor use isn't illegal, and plenty of legitimate
scanners/CDNs/bots run from hosting providers too). Both are starting
points, not calibrated values. Shown in the expanded reputation detail
alongside ISP/country whenever present.

A flag is raised once per (detector, source) pair and updated in place
on re-firing (count/last-seen bumped, not duplicated) until a human
clears it via the UI or `POST /api/flags/{id}/clear`. Clearing an
already-active-again source re-raises it as a fresh entry rather than
silently resurrecting the old one.

Alongside the plain Clear action, "Clear, never flag again" (`POST
/api/flags/{id}/clear-permanent`) clears the flag *and* permanently
excludes that exact (detector, target) pair -- from then on it never
raises again, silently, until the exclusion is removed. This is
deliberately permanent rather than a timed snooze: a time-limited mute
either re-fires once it expires (nothing was solved) or it doesn't
(permanent exclusion was what was wanted all along), so there's no
in-between "snooze" option. Because "permanent" shouldn't mean
"unrecoverable by mistake," every current exclusion is listed (and can
be removed, re-enabling that pair) from the Flags tab's "Manage
exclusions" panel -- admin-only once an account exists, open to anyone
while mikroview is still in its fully-open zero-account state, same as
every other admin-gated endpoint (see [Authentication](#authentication)).

## New-device detection (optional, on by default)

Raises a `new_device` flag the first time mikroview ever sees a given
LAN client MAC address (`store.Event.SrcMAC`). Unlike every detector
above, this isn't a threshold-crossing or a statistical judgment -- it's
a deterministic "have I ever seen this MAC before," so there's nothing
to tune: it fires exactly once per genuinely-new MAC, the moment it's
first observed, and never again for that same MAC.

```yaml
deviceMac:
  storePath: "/var/lib/mikroview/mac-registry.json"
```

- **`storePath`** — where the registry of every MAC address mikroview
  has ever seen (just a MAC plus first/last-seen timestamps) is
  persisted, as a small JSON file. This needs its own store, separate
  from `flags.storePath` above: it must survive a restart for "new" to
  mean anything at all -- the 24h event-retention window alone is
  nowhere near long enough. Left empty (not the default), detection
  still runs, it just starts with an empty registry -- and so treats
  every MAC as new again -- on every restart. Same optional-persistence
  contract as `flags.storePath`; if you set this in the container, mount
  a volume for its parent directory -- see `deploy/docker-compose.yml`.

**Coverage.** `SrcMAC` is only present on RouterOS log lines from
LAN-side/bridge-aware firewall rules -- by the time traffic reaches a
WAN-side rule, its Layer 2 source-MAC information is already gone
(that's how routing works, not a mikroview limitation). If your
firewall ruleset only logs WAN-side traffic, this detector simply never
fires; that's a data-availability gap, not a bug.

**No confidence score.** "Is this MAC new" is a deterministic yes/no,
but a brand-new device on your network is very often entirely benign (a
phone joining the Wi-Fi) -- there's no meaningful numeric judgment call
to attach on top of the deterministic fact. `confidence` is always
`null` for `new_device` flags in `GET /api/flags`, same as the two "not
yet scored" detectors above.

## Notifications (optional)

Flags are only visible if someone has the mikroview UI open. `notify`
sends an alert through one or more external channels whenever a new
flag *episode* is raised (a first-ever raise, or a revival after a human
clears an already-cleared flag -- never a plain re-fire of an
already-active one, so a noisy detector doesn't re-alert on every
event). Each channel below is independently enabled by its own
identifying field being set (`smtp.host`, `pushover.token`,
`webhook.url`) -- configure any combination of zero, one, two, or all
three; every enabled channel shares one `batchWindow`.

```yaml
notify:
  # batchWindow: how often pending flags are flushed to every enabled
  # channel -- a fixed interval, not a quiet-period debounce, so a
  # sustained flood of flags during a real incident still gets a
  # bounded max delay before alerting rather than the window
  # continuously resetting.
  batchWindow: 60s
  smtp:
    host: "smtp.example.com"
    port: 587
    # username left empty means no AUTH is attempted, for an open local
    # relay (e.g. a Postfix instance on the same host/network).
    username: "alerts@example.com"
    password: "changeme"
    # "" (plaintext, local relay only), "starttls" (upgrade after
    # connecting, typically port 587), or "implicit" (TLS from the
    # first byte, typically port 465).
    tlsMode: "starttls"
    from: "mikroview@example.com"
    to: ["ops@example.com", "oncall@example.com"]
  pushover:
    # From your Pushover application (https://pushover.net/apps) and
    # account/group, respectively -- no VAPID keys, no service worker,
    # no per-browser subscription management, just this pair.
    token: "your-application-token"
    user: "your-user-or-group-key"
  webhook:
    # Any HTTPS endpoint that accepts a JSON POST -- ntfy, Discord
    # (via a webhook URL's own payload adapter or a receiving proxy),
    # Slack (same), Home Assistant's webhook trigger, n8n, or a
    # bespoke receiver of your own. No bespoke per-service integration
    # here on purpose -- this is the "everything else" channel.
    url: "https://ntfy.example.com/mikroview-alerts"
    # headers: arbitrary extra headers sent with every POST -- most
    # commonly auth, since ntfy/Home Assistant/n8n-style receivers each
    # expect it in a different header rather than agreeing on one
    # convention. Optional; omit entirely for a receiver that needs no
    # auth (e.g. an unauthenticated local ntfy topic).
    headers:
      Authorization: "Bearer changeme"
      # X-Custom-Header: "some-other-receiver-specific-value"
```

Left with an empty `smtp.host`/`pushover.token`/`webhook.url` (the
default), that channel is simply never dispatched to -- no relay, app,
or endpoint is assumed. `smtp.password`/`pushover.token`/`pushover.user`/
`webhook.url` can also be set via env vars instead of the config file
(see the table below), same secret-via-env precedent as
`MIKROVIEW_ABUSEIPDB_KEY`; `webhook.headers` is YAML-only (a map doesn't
map cleanly onto one env var, same reasoning `flags.detectors` and the
device/naming maps already give).

One notification covers every flag raised within a `batchWindow`: title/
subject `mikroview: N new flag(s)`, one line per flag (type, target,
detail, confidence, first-seen) -- Pushover's message additionally caps
at 10 lines with a "...and N more" trailer, since its message field is
much smaller than an email body. The webhook channel instead POSTs a
JSON body shaped `{"title": "...", "count": N, "flags": [...]}`, where
each entry in `flags` is the same flag record the UI itself renders
(type, target, detail, count, first/last seen, confidence) -- a generic
consumer gets the full structured batch to template off of, rather than
a pre-rendered string. Built around a shared
`internal/notify.Notifier`/`Dispatcher` so every channel reuses the same
batching rather than each implementing their own; true device push (web
push API, VAPID keys, service worker) is a separate, not-yet-built
target scoped alongside PWA feasibility, since a lot of that plumbing
overlaps with making the frontend a PWA at all.

## Per-detector toggles and scope restrictions (optional)

Every detector above defaults to enabled and unscoped -- this section
lets you turn one off entirely, or narrow where it applies, without
touching its thresholds. Settings live in two places that layer
together:

- **`config.yaml`** sets the *starting point* on boot (`flags.detectors`
  below).
- **A live, admin-only UI** ("Detectors" in the toolbar, visible once
  signed in as an admin) can override that starting point without a
  restart -- takes effect on the very next ingested event. A live change
  persists to `detectorSettingsStorePath` (same optional-persistence
  contract as `flags.storePath` above: left unset, a live toggle still
  works, it just resets to `config.yaml`'s values on restart) and, once
  persisted, is what future restarts seed from -- `config.yaml`'s values
  are only ever consulted the *first* time that file doesn't exist yet.

```yaml
flags:
  detectorSettingsStorePath: "/var/lib/mikroview/detector-settings.json"
  detectors:
    critical_port:
      enabled: true
      scope:
        hosts: ["203.0.113.0/24"]
        hostsMode: deny
        ports: [22, 3389]
        portsMode: allow
        classification: external
    rule_spike:
      enabled: false
```

Each detector under `detectors` is optional -- an omitted detector keeps
today's default (enabled, unscoped). Within one detector's `scope`, every
field is also optional and independently combines with the others by
**AND**: an event only counts toward that detector if it satisfies every
restriction you've actually set. Within one field (`hosts`, `ports`, or
`rules`), `*Mode` is either `allow` (only listed entries are admitted) or
`deny` (listed entries are excluded) -- never both directions on the same
field at once. `hosts` entries accept a bare IP or a CIDR.

Not every field means something to every detector -- a detector only
consults the axes relevant to how it's keyed:

| Detector | `hosts` + `classification` restrict | `ports` restrict | `rules` restrict |
|---|---|---|---|
| `port_scan` | which source IPs are tracked at all | which distinct ports *count* toward the scan total (not which events are tracked) | -- |
| `activity_spike` | which source IPs are tracked at all | -- | -- |
| `critical_port` | source IP | the effective subset of `criticalPorts` this instance reacts to | -- |
| `distributed_brute_force` | which source IPs count toward a port's distinct-source total | the effective subset of `criticalPorts` | -- |
| `outbound_anomaly` | which source (LAN) IPs are watched | -- | -- |
| `internal_recon` | which source (LAN) IPs are watched | -- | -- |
| `rule_spike` | -- | -- | which rule labels this detector reacts to |
| `repeated_drops` | source IP | destination port | -- |
| `global_spike` | -- (network-wide, not keyed by anything per-source) | -- | -- |
| `low_slow_scan` | which source IPs are tracked at all | which distinct ports *count* toward its own breadth total | -- |
| `off_hours_activity` | which source IPs are tracked at all | -- | -- |
| `device_silence` | -- (a per-configured-device sweep, not keyed by host/port/rule) | -- | -- |

`global_spike` and `device_silence` only ever consult `enabled` -- the
former is a single network-wide aggregate, the latter a periodic sweep
over the whole configured device list, neither tied to any particular
host, port, or rule, so scoping either wouldn't mean anything.

## Authentication

The first time mikroview loads with no accounts and no prior decision,
it shows a one-time choice screen instead of the live view: **create
the admin account**, or **continue without an account** ("skip"). See
[SECURITY.md](../SECURITY.md) for the full threat-model writeup; this
section is the configuration reference.

```yaml
auth:
  storePath: "/var/lib/mikroview/users.json"
  secureCookie: true
  sessionTTL: 24h
  tokensStorePath: "/var/lib/mikroview/tokens.json"
```

- **`storePath`** — where accounts (and the skip/disabled decision) are
  persisted, as a small JSON file (usernames + Argon2id password hashes,
  never plaintext). Defaults to `/var/lib/mikroview/users.json`, which
  the Dockerfile creates and owns -- no configuration needed for the
  zero-config case. Mount a volume over `/var/lib/mikroview` if you want
  the decision (and any accounts) to survive container recreation, not
  just process restarts -- see `deploy/docker-compose.yml`.
- **`secureCookie`** — sets the session cookie's `Secure` flag. On by
  default, matching [TLS](#tls) being on by default -- there's no other
  kind of connection to have a session on. Only turn this off if you've
  also set `tls.enabled: false`, or sessions won't work at all.
- **`sessionTTL`** — the idle timeout: a session's expiry slides forward
  on each authenticated request, so this is "how long you can go without
  activity before needing to log in again," not a fixed session
  lifetime.
- **`tokensStorePath`** — where [API tokens](#api-tokens-read-only) are
  persisted, as a small JSON file (names + SHA-256 hashes, never the raw
  bearer values). Defaults to `/var/lib/mikroview/tokens.json`. Unlike
  `storePath`, this really is optional: a deployment that never creates
  a token doesn't need it.

**If you create an account**, every request except `GET /api/healthz`
and the login/session endpoints requires a valid session, permanently,
from then on. Whoever completes the form becomes the admin.

**If you skip**, mikroview stays fully open indefinitely -- the same
behavior an older mikroview had by accident, but now a deliberate,
persisted choice rather than "nobody got around to setting up auth
yet." Before deciding, weigh who can reach the deployment: skipping is
reasonable on a network you already trust as much as the router itself,
not on anything broader.

**Reversing a skip** is CLI-only, by design: nothing in the web UI or
API can re-enable auth once skipped, so a visitor to an open deployment
can never unilaterally impose a login requirement on everyone else.

```sh
mikroview -enable-auth-setup
```

re-arms the choice screen (it does not create an account itself -- the
next person to load mikroview, or you, still completes the create-
account form). Requires container/host access, the same trust anchor as
the recovery commands below.

**Adding more accounts** afterward is admin-only, either via the "Add
user" control in the toolbar or `POST /api/auth/users`.

**Account recovery** is a CLI command, deliberately outside the web
UI/API entirely -- container/host access is the trust anchor, so a
locked-out admin isn't dependent on the system they're locked out of:

```sh
mikroview -list-users             # usernames + roles, no password hashes
mikroview -reset-password admin   # prompts for a new password (no echo), twice to confirm
```

A password reset immediately invalidates every existing session for that
account, including on an already-running server -- you don't need to
restart mikroview after running `-reset-password` for the new password
to take effect.

## API tokens (read-only)

For a separate service to pull mikroview's data over the network with
no browser involved -- e.g. a companion OpenCanary-dashboard project
cross-referencing incidents against mikroview's event/flag history -- a
session cookie doesn't work: there's no login flow to hold one. API
tokens are a long-lived bearer credential for exactly that case,
admin-created from the "API tokens" panel in the menu's Account section
(or directly via the API below).

**Scope is deliberately narrow: read-only, four endpoints, nothing
else.** A valid token grants `Authorization: Bearer <token>` access to:

- `GET /api/events`
- `GET /api/flags`
- `GET /api/stats`
- `GET /api/devices`

and *nothing* else -- no clearing a flag, no changing a detector, no
managing users or other tokens, regardless of the method or path
requested. This isn't a per-request permission check that a future
handler could accidentally bypass: a bearer-authenticated request is
routed to a completely separate, minimal internal router that simply
has no other handler registered on it, so there is no code path from a
valid token to anything else, structurally rather than by convention.

A token's raw value is shown exactly once, at creation, and is not
recoverable afterward -- only the SHA-256 hash of it is ever persisted
(not Argon2id: that cost exists to slow down guessing a low-entropy,
human-chosen password, and a token's value is already a 128-bit random
string, well outside brute-forceable range). Losing the value means
issuing a new token; there's no way to view an existing one again.

There is no expiry -- like sessions and accounts, a token stays valid
until explicitly revoked from the same panel (or `DELETE
/api/tokens/{id}`).

```sh
curl -H "Authorization: Bearer <token>" https://mikroview.example.com/api/events
```

## Single sign-on (OIDC/SSO)

Optional, additive on top of [local authentication](#authentication) above
-- local login keeps working unmodified whether or not this is
configured. Tested against [Authentik](https://goauthentik.io/), but
works with any standard OIDC provider (generic discovery, no
Authentik-specific behavior).

```yaml
oidc:
  issuerUrl: "https://authentik.example.com/application/o/mikroview/"
  clientId: "mikroview"
  clientSecret: "changeme"
  publicBaseUrl: "https://mikroview.example.com"
  scopes: ["openid", "profile", "email"] # this is the default if omitted
```

- **`issuerUrl`** — the provider's issuer URL (Authentik: your
  application's **OpenID Configuration Issuer**, found on the
  provider's overview page -- it ends in `/application/o/<slug>/`, not
  just your Authentik host). Empty (the default) means SSO is not
  configured at all -- no separate `enabled` flag, since there's no
  scenario where a fully-populated block should be silently inert.
- **`clientId`/`clientSecret`** — from the provider's confidential
  OAuth2/OIDC client registration (see the Authentik walkthrough
  below). `clientSecret` can also be set via
  `MIKROVIEW_OIDC_CLIENT_SECRET` instead of `config.yaml`, the same
  secret-via-env precedent `notify.smtp.password`/`reputation.abuseIPDBKey`
  already have.
- **`publicBaseUrl`** — mikroview's own externally-reachable origin,
  used to build the `redirect_uri` registered at the provider
  (`publicBaseUrl` + `/api/auth/oidc/callback`). Required whenever
  `issuerUrl` is set. Deliberately never inferred from a request's
  `Host` header -- doing so is a known `redirect_uri`-confusion
  vulnerability class. Covers either deployment mode this app
  supports: mikroview's own self-signed TLS on a LAN IP/hostname
  (`https://192.168.1.10:8443`), or fronted by a reverse proxy
  terminating a real domain (`https://mikroview.example.com`).
- **`scopes`** — defaults to `openid`, `profile`, `email` if omitted.
  `openid` is always required regardless of what's listed.

**Identity**: an account is matched by the immutable `(issuer, subject)`
pair from the verified ID token, never by email or username -- an
identity provider reassigning someone's email must never silently
inherit a different mikroview account. A first-ever login via SSO
just-in-time creates a local account (no pre-registration step), using
the ID token's `preferred_username`/`email` claim as a display name
only if it's free; otherwise a stable synthetic username is generated.
The very first account overall (local or SSO, whichever happens first)
becomes admin; every account after that is a regular user.

**Security**: only asymmetric-signed ID tokens are ever accepted
(RS256/ES256/PS256) -- HS256 and `none` are rejected outright,
regardless of what the provider's discovery document claims to
support. The Authorization Code flow always uses PKCE. A misconfigured
or unreachable provider degrades to "SSO unavailable" (logged, once,
at startup) rather than affecting local login in any way.

**Authentik setup walkthrough**:

1. **Applications → Providers → Create**, type **OAuth2/OpenID
   Provider**.
   - **Client type**: Confidential.
   - **Redirect URIs**: strict match, `https://<publicBaseUrl>/api/auth/oidc/callback`.
   - **Signing Key**: pick a certificate (Authentik ships a self-signed
     one out of the box) -- **do not leave this unset**. Authentik's
     `id_token_signing_alg_values_supported` in its discovery document
     depends entirely on this being assigned; without it, token signing
     may not use an algorithm mikroview's allowlist accepts.
   - Note the generated **Client ID**/**Client Secret**.
2. **Applications → Applications → Create**, bind it to the provider
   above. Its slug is what appears in the issuer URL
   (`/application/o/<slug>/`).
3. Under the provider's **Property mappings**, make sure the standard
   `openid`, `profile`, and `email` scope mappings are attached (they
   usually are by default) -- these are what put `email`/
   `preferred_username` in the ID token for mikroview's username hint.
4. Set mikroview's `oidc.issuerUrl` to
   `https://<your-authentik-host>/application/o/<slug>/` (the exact
   value shown as **OpenID Configuration Issuer** on the provider's
   overview page -- confirm by fetching
   `<issuerUrl>/.well-known/openid-configuration` and checking
   `id_token_signing_alg_values_supported` includes `RS256`).

Verified end-to-end against a real, freshly bootstrapped Authentik
instance (provider + application configured via Authentik's own API):
the full redirect → real Authentik login form → PKCE code exchange →
RS256 ID token verification → JIT account provisioning → mikroview
session flow, including a second login correctly reusing the same
account rather than creating a duplicate.

## TLS

Mikroview serves TLS by default on its main listener -- the
application itself is never served over plain HTTP. See
[SECURITY.md](../SECURITY.md#tls) for the full reasoning; this section
is the configuration reference.

A second listener, `listen.httpRedirect` (default `:8081`, mapped to
host port 80 by `deploy/docker-compose.yml`), exists only to redirect a
plain HTTP request to HTTPS -- it never serves the application itself.
It's only started while `tls.enabled` is true (nothing to redirect to
otherwise), and only started at all if non-empty -- set it to `""` to
disable it, e.g. if your own reverse proxy already handles the
HTTP->HTTPS redirect. The redirect target is built by stripping any
port off the request's `Host` header and assuming HTTPS is reachable on
the browser-default 443, which holds for the default compose port
mapping (host `443` -> this listener); if you've remapped the HTTPS
port to something else externally, either disable this listener and
redirect at your reverse proxy instead, or accept that the `Location`
header will still point at `:443`. If `tls.hosts` is set, the `Host`
header is validated against it (falling back to the first configured
host on a mismatch) rather than echoed unconditionally -- only
relevant if something other than a real browser navigation reaches
this listener directly.

```yaml
tls:
  enabled: true
  # Your own cert (a real domain + ACME, a corporate CA, etc) -- takes
  # priority over the self-generated one below if both are set.
  certFile: ""
  keyFile: ""
  # Hostnames/IPs a generated certificate should cover (SANs) --
  # whatever you'll actually use to reach mikroview. Defaults to
  # localhost/127.0.0.1 if empty -- still fully encrypted either way,
  # just only strictly verifiable under those names unless you add your
  # own.
  hosts: []
  # Persists the self-generated CA + certificate across restarts, so
  # the trust step only happens once. Optional, same contract as
  # flags.storePath.
  storePath: "/var/lib/mikroview/tls"
```

- **`enabled`** — on by default. The one supported reason to set this
  `false` is a deployment where mikroview's listener is *provably* only
  reachable from your own reverse proxy over an isolated docker network
  -- never published to a LAN or the internet at all. In that specific
  topology the RP already owns TLS termination for real clients, and
  there's no bypass surface for mikroview to additionally protect on
  that internal hop. Never set this `false` if mikroview's port is
  reachable from a LAN or the internet in any other way -- doing so
  serves the app, credentials included, in cleartext. Logged clearly at
  startup whenever it's off, so it's never a silent state.
- **`certFile`/`keyFile`** — your own certificate. Skips local-CA
  generation entirely when both are set.
- **`hosts`** — SANs for a self-generated certificate. Left empty, the
  generated cert only covers `localhost`/`127.0.0.1` -- connections from
  any other name/IP are still fully encrypted, just not strictly
  verifiable against that name without adding it here.
- **`storePath`** — where a generated CA + certificate persist across
  restarts. Left unset, TLS still works, it just regenerates (and needs
  re-trusting) every restart -- the same optional-persistence contract
  `flags.storePath` has.

**Zero-config default**: with no `certFile`/`keyFile`, mikroview
generates its own local certificate authority and a leaf certificate on
first start (`internal/servertls`). This CA is trust-on-first-use, not a
globally trusted root -- your browser will show an untrusted-certificate
warning on first visit until you import it, the same one-time step
Proxmox/TrueNAS/pfSense's own self-signed admin UIs already ask for. The
CA is served at `GET /ca.crt`, unauthenticated (only when one was
actually generated), specifically so a browser or reverse proxy can
fetch it to establish trust; its fingerprint is also logged at startup
if you'd rather verify it out-of-band than trust-on-first-use blindly.

**Reverse proxy in front, with your own single ingress**: point your
RP's *upstream/backend* target at `https://mikroview:PORT` instead of
`http://mikroview:PORT` -- same host, same port, no new port opened
anywhere. Your RP's client-facing side is completely untouched. Every
mainstream reverse proxy supports backend/upstream TLS (Caddy's
`reverse_proxy https://...`, Traefik's backend TLS transport, nginx's
`proxy_pass https://...`), typically via either skipping strict
verification for that specific upstream (reasonable here, since you
configured that upstream address yourself) or trusting mikroview's
local CA explicitly (more correct, and what `/ca.crt` is for).

## Environment variables

Override individual scalar settings without a mounted file:

| Variable | Overrides |
|---|---|
| `MIKROVIEW_CONFIG` | path to the YAML config file to load |
| `MIKROVIEW_LISTEN_SYSLOG_UDP` | `listen.syslogUdp` |
| `MIKROVIEW_LISTEN_SYSLOG_TCP` | `listen.syslogTcp` |
| `MIKROVIEW_LISTEN_HTTP` | `listen.http` |
| `MIKROVIEW_LISTEN_HTTP_REDIRECT` | `listen.httpRedirect` |
| `MIKROVIEW_STORE_RETENTION` | `store.retention` |
| `MIKROVIEW_STORE_MAX_EVENTS` | `store.maxEvents` |
| `MIKROVIEW_LOG_LEVEL` | `log.level` (see [Logging](#logging)) |
| `MIKROVIEW_GEOIP_DB_PATH` | `geoip.dbPath` (see [GeoIP country flags](#geoip-country-flags-optional)) |
| `MIKROVIEW_ABUSEIPDB_KEY` | `reputation.abuseIPDBKey` (see [IP reputation lookup](#ip-reputation-lookup-optional)) |
| `MIKROVIEW_FLAGS_STORE_PATH` | `flags.storePath` |
| `MIKROVIEW_FLAGS_PORT_SCAN_THRESHOLD` | `flags.portScanThreshold` |
| `MIKROVIEW_FLAGS_PORT_SCAN_WINDOW` | `flags.portScanWindow` |
| `MIKROVIEW_FLAGS_ACTIVITY_SPIKE_THRESHOLD` | `flags.activitySpikeThreshold` |
| `MIKROVIEW_FLAGS_ACTIVITY_SPIKE_WINDOW` | `flags.activitySpikeWindow` |
| `MIKROVIEW_FLAGS_CRITICAL_PORTS` | `flags.criticalPorts` (comma-separated, e.g. `22,3389,8291`) |
| `MIKROVIEW_FLAGS_CRITICAL_PORT_THRESHOLD` | `flags.criticalPortThreshold` |
| `MIKROVIEW_FLAGS_CRITICAL_PORT_WINDOW` | `flags.criticalPortWindow` |
| `MIKROVIEW_FLAGS_GLOBAL_SPIKE_MULTIPLIER` | `flags.globalSpikeMultiplier` |
| `MIKROVIEW_FLAGS_GLOBAL_SPIKE_MIN_EPS` | `flags.globalSpikeMinEPS` |
| `MIKROVIEW_FLAGS_GLOBAL_SPIKE_WARMUP_SAMPLES` | `flags.globalSpikeWarmupSamples` |
| `MIKROVIEW_FLAGS_DISTRIBUTED_BRUTE_FORCE_THRESHOLD` | `flags.distributedBruteForceThreshold` |
| `MIKROVIEW_FLAGS_DISTRIBUTED_BRUTE_FORCE_WINDOW` | `flags.distributedBruteForceWindow` |
| `MIKROVIEW_FLAGS_OUTBOUND_ANOMALY_THRESHOLD` | `flags.outboundAnomalyThreshold` |
| `MIKROVIEW_FLAGS_OUTBOUND_ANOMALY_WINDOW` | `flags.outboundAnomalyWindow` |
| `MIKROVIEW_FLAGS_INTERNAL_RECON_THRESHOLD` | `flags.internalReconThreshold` |
| `MIKROVIEW_FLAGS_INTERNAL_RECON_WINDOW` | `flags.internalReconWindow` |
| `MIKROVIEW_FLAGS_RULE_SPIKE_MULTIPLIER` | `flags.ruleSpikeMultiplier` |
| `MIKROVIEW_FLAGS_RULE_SPIKE_MIN_RATE` | `flags.ruleSpikeMinRate` |
| `MIKROVIEW_FLAGS_RULE_SPIKE_WINDOW` | `flags.ruleSpikeWindow` |
| `MIKROVIEW_FLAGS_RULE_SPIKE_WARMUP_SAMPLES` | `flags.ruleSpikeWarmupSamples` |
| `MIKROVIEW_FLAGS_REPEATED_DROPS_THRESHOLD` | `flags.repeatedDropsThreshold` |
| `MIKROVIEW_FLAGS_REPEATED_DROPS_WINDOW` | `flags.repeatedDropsWindow` |
| `MIKROVIEW_FLAGS_HOST_ACTIVITY_MULTIPLIER` | `flags.hostActivityMultiplier` |
| `MIKROVIEW_FLAGS_HOST_ACTIVITY_WARMUP_SAMPLES` | `flags.hostActivityWarmupSamples` |
| `MIKROVIEW_FLAGS_LOW_SLOW_SCAN_WINDOW` | `flags.lowSlowScanWindow` |
| `MIKROVIEW_FLAGS_LOW_SLOW_SCAN_PORT_THRESHOLD` | `flags.lowSlowScanPortThreshold` |
| `MIKROVIEW_FLAGS_LOW_SLOW_SCAN_HOST_THRESHOLD` | `flags.lowSlowScanHostThreshold` |
| `MIKROVIEW_FLAGS_LOW_SLOW_SCAN_MIN_OBSERVATION` | `flags.lowSlowScanMinObservation` |
| `MIKROVIEW_FLAGS_LOW_SLOW_SCAN_DROP_RATIO` | `flags.lowSlowScanDropRatio` |
| `MIKROVIEW_FLAGS_LOW_SLOW_SCAN_BASELINE_MULTIPLIER` | `flags.lowSlowScanBaselineMultiplier` |
| `MIKROVIEW_FLAGS_OFF_HOURS_START_HOUR` | `flags.offHoursStartHour` |
| `MIKROVIEW_FLAGS_OFF_HOURS_END_HOUR` | `flags.offHoursEndHour` |
| `MIKROVIEW_FLAGS_OFF_HOURS_MIN_SAMPLE_DAYS` | `flags.offHoursMinSampleDays` |
| `MIKROVIEW_FLAGS_OFF_HOURS_MIN_COUNT` | `flags.offHoursMinCount` |
| `MIKROVIEW_FLAGS_DEVICE_STALE_AFTER` | `flags.deviceStaleAfter` |
| `MIKROVIEW_FLAGS_RULE_USAGE_STORE_PATH` | `flags.ruleUsageStorePath` |
| `MIKROVIEW_FLAGS_STALE_RULE_DAYS` | `flags.staleRuleDays` |
| `MIKROVIEW_FLAGS_STALE_RULE_CHECK_INTERVAL` | `flags.staleRuleCheckInterval` |
| `MIKROVIEW_FLAGS_DETECTOR_SETTINGS_STORE_PATH` | `flags.detectorSettingsStorePath` (see [Per-detector toggles](#per-detector-toggles-and-scope-restrictions-optional)) |
| `MIKROVIEW_AUTH_STORE_PATH` | `auth.storePath` (see [Authentication](#authentication)) |
| `MIKROVIEW_AUTH_SECURE_COOKIE` | `auth.secureCookie` |
| `MIKROVIEW_AUTH_SESSION_TTL` | `auth.sessionTTL` |
| `MIKROVIEW_ENTITIES_STORE_PATH` | `entities.storePath` (see [Entities](#entities-ui-managed-hostruleport-labels-and-tags-optional)) |
| `MIKROVIEW_AUTH_TOKENS_STORE_PATH` | `auth.tokensStorePath` (see [API tokens](#api-tokens-read-only)) |
| `MIKROVIEW_TLS_ENABLED` | `tls.enabled` (see [TLS](#tls)) |
| `MIKROVIEW_TLS_CERT_FILE` | `tls.certFile` |
| `MIKROVIEW_TLS_KEY_FILE` | `tls.keyFile` |
| `MIKROVIEW_TLS_HOSTS` | `tls.hosts` (comma-separated) |
| `MIKROVIEW_TLS_STORE_PATH` | `tls.storePath` |
| `MIKROVIEW_OIDC_ISSUER_URL` | `oidc.issuerUrl` (see [Single sign-on](#single-sign-on-oidcsso)) |
| `MIKROVIEW_OIDC_CLIENT_ID` | `oidc.clientId` |
| `MIKROVIEW_OIDC_CLIENT_SECRET` | `oidc.clientSecret` |
| `MIKROVIEW_OIDC_PUBLIC_BASE_URL` | `oidc.publicBaseUrl` |
| `MIKROVIEW_OIDC_SCOPES` | `oidc.scopes` (comma-separated) |
| `MIKROVIEW_NOTIFY_BATCH_WINDOW` | `notify.batchWindow` (see [Notifications](#notifications-optional)) |
| `MIKROVIEW_NOTIFY_SMTP_HOST` | `notify.smtp.host` |
| `MIKROVIEW_NOTIFY_SMTP_PORT` | `notify.smtp.port` |
| `MIKROVIEW_NOTIFY_SMTP_USERNAME` | `notify.smtp.username` |
| `MIKROVIEW_NOTIFY_SMTP_PASSWORD` | `notify.smtp.password` |
| `MIKROVIEW_NOTIFY_SMTP_TLS_MODE` | `notify.smtp.tlsMode` |
| `MIKROVIEW_NOTIFY_SMTP_FROM` | `notify.smtp.from` |
| `MIKROVIEW_NOTIFY_SMTP_TO` | `notify.smtp.to` (comma-separated) |
| `MIKROVIEW_NOTIFY_PUSHOVER_TOKEN` | `notify.pushover.token` |
| `MIKROVIEW_NOTIFY_PUSHOVER_USER` | `notify.pushover.user` |
| `MIKROVIEW_DEVICE_MAC_STORE_PATH` | `deviceMac.storePath` (see [New-device detection](#new-device-detection-optional-on-by-default)) |
| `MIKROVIEW_NOTIFY_WEBHOOK_URL` | `notify.webhook.url` |

## CLI flags (local development)

`-syslog-udp`, `-syslog-tcp`, `-http`, `-http-redirect`, `-retention`,
`-max-events`, `-geoip-db` — see `go run . -h`. Devices, rule/host
names, and auth config can only be set via YAML/env, not flags.

`-healthcheck`, `-list-users`, `-reset-password <username>`,
`-enable-auth-setup` are standalone modes -- each does its one job and
exits, rather than starting the server. See
[Authentication](#authentication) for the latter three.

## API reference

| Endpoint | Description |
|---|---|
| `GET /api/healthz` | liveness/uptime check |
| `GET /ca.crt` | mikroview's self-generated CA certificate, unauthenticated -- only present when TLS is on and mikroview generated its own CA (never for a supplied cert or `tls.enabled: false`); see [TLS](#tls) |
| `GET /api/events` | filtered, windowed historical query (see below) |
| `GET /api/devices` | known devices (configured + auto-discovered), each with a `status` of `live`/`stale`/`never_seen` (issue #98, see [Behavioral flags](#behavioral-flags-optional-on-by-default)'s "Device silence" entry) -- feeds the Fleet view |
| `GET /api/critical-ports` | the configured `flags.criticalPorts` list -- feeds the "Control ports" tracking tab (issue #34), open to any signed-in user, not admin-gated |
| `GET /api/rules` | every rule label mikroview has ever seen fire, with first/last-seen time and count (`internal/rules.Store`) -- the "discovered but unnamed rules" source for the Entities panel (see [Entities](#entities-ui-managed-hostruleport-labels-and-tags-optional)), open to any signed-in user, not admin-gated |
| `GET /api/stats` | totals, per-action counts, rolling events/sec |
| `GET /api/ws` | live-tail WebSocket feed |
| `GET /api/lookup/ip/{ip}` | on-demand reputation/threat-intel lookup for one public IP (see [IP reputation lookup](#ip-reputation-lookup-optional)) |
| `GET /api/flags` | active + cleared behavioral flags, plus the last hour of newly-raised-episode counts by type at 1-minute resolution (issue #100, feeds the dashboard's flags-over-time chart) (see [Behavioral flags](#behavioral-flags-optional-on-by-default)) |
| `POST /api/flags/{id}/clear` | mark one flag as cleared |
| `POST /api/flags/{id}/clear-permanent` | clear one flag *and* permanently exclude its (detector, target) pair going forward |
| `GET /api/flags/exclusions` | admin-only (open while zero accounts exist): every currently-excluded (detector, target) pair |
| `DELETE /api/flags/exclusions/{id}` | admin-only (open while zero accounts exist): remove one exclusion, letting that pair raise again |
| `GET /api/detectors` | admin-only (open while zero accounts exist): every detector's live enabled+scope (see [Per-detector toggles](#per-detector-toggles-and-scope-restrictions-optional)) |
| `PUT /api/detectors/{name}` | admin-only (open while zero accounts exist): replace one detector's enabled+scope wholesale |
| `GET /api/entities` | admin-only (**not** open while zero accounts exist -- see [Entities](#entities-ui-managed-hostruleport-labels-and-tags-optional)): every persisted entity |
| `POST /api/entities` | admin-only: create or replace (upsert) one entity, identified by `(type, key)` in the JSON body |
| `DELETE /api/entities` | admin-only: remove the entity identified by `(type, key)` in the JSON body |
| `GET /api/auth/session` | current auth state (setup-required / authenticated / not) -- always 200, never gated |
| `POST /api/auth/register` | create the first (admin) account -- only while zero accounts exist |
| `POST /api/auth/skip` | explicitly disable auth for this deployment -- only while zero accounts exist; reversing later is CLI-only (`-enable-auth-setup`) |
| `POST /api/auth/login` | sign in, sets the session cookie |
| `POST /api/auth/logout` | sign out, clears the session cookie |
| `POST /api/auth/users` | admin-only: create an additional account |
| `POST /api/tokens` | admin-only: create a read-only API token (see [API tokens](#api-tokens-read-only)) -- returns the raw value once |
| `GET /api/tokens` | admin-only: list tokens (name/created/last-used, never the value or hash) |
| `DELETE /api/tokens/{id}` | admin-only: revoke a token |
| `GET /api/auth/oidc/login` | start the SSO flow -- a top-level browser redirect to the configured provider, only present when [OIDC](#single-sign-on-oidcsso) is configured |
| `GET /api/auth/oidc/callback` | the provider's redirect target completing the SSO flow -- see [Single sign-on](#single-sign-on-oidcsso) |

Every route above `/api/auth/session`/`/register`/`/login`/`/logout` and
`/api/healthz` requires a valid session once an account exists -- see
[Authentication](#authentication). `GET /api/events`, `/flags`, `/stats`,
and `/devices` additionally accept a valid `Authorization: Bearer
<token>` header instead of a session (see [API tokens](#api-tokens-read-only));
no other route accepts one.
Every mutating (`POST`/`PUT`/`DELETE`) request also requires an
`X-Requested-With: mikroview` header once an account exists (a CSRF
mitigation, see SECURITY.md).

`/api/events` query parameters: `device`, `action` (`accept`/`drop`/
`reject`/`log`/`unknown`), `protocol`, `chain`, `interface`, `ip` (exact
or CIDR, matches source or destination), `port` (matches source or
destination), `srcScope`/`dstScope` (`internal` or `external`, restricts
that side of the connection to a private/LAN or public address
respectively -- an address that can't be parsed satisfies neither),
`rule` (substring match), `since` (RFC3339), `until` (RFC3339, an
optional upper bound -- paired with `since` this gives a bounded
before/after window instead of an open-ended tail to now), `sinceId`
(cursor), `limit` (default 500, max 5000).

**Bounded before/after lookback (issue #29).** `around` (RFC3339) +
`window` (a duration, e.g. `5m`, default `5m`) is sugar for `since`/
`until` centered on a timestamp -- overrides them if both forms are
given. Meant for pulling "what was this IP doing right before/after X"
from an external signal (a honeypot hit, a manual investigation
trigger), combined with `ip`:

```
GET /api/events?ip=203.0.113.9&around=2026-01-15T14:32:00Z&window=10m
```

"Before" context is inherently capped by the in-memory retention window
(`store.retention`) -- older history simply doesn't exist. Rather than
silently truncating, the response's `windowStart` field always reports
the *actual* applied lower bound, so a caller comparing it against the
`since` (or `around`-`window`) it requested can tell whether it got
truncated by retention.

## Live updates: server vs. client filtering

The initial page load and any "load older" request go through
`GET /api/events`, filtered **server-side** against the full retained
buffer (up to `store.maxEvents` / `store.retention`) — this keeps the
browser from ever having to download more than it needs.

The WebSocket at `/api/ws`, by contrast, pushes **every** new event to
every connected client, unfiltered, batched into one frame every ~50ms.
The frontend applies the active filters client-side against its own
capped in-memory buffer. This is deliberate: it means changing a filter
in the UI is instant, with no round-trip to the server, and it keeps the
server-side WebSocket handling simple (one fan-out list, no per-connection
filter state to maintain).
