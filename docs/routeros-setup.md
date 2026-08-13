# RouterOS setup

MikroView never talks to RouterOS's API and needs no credentials on the
router. Instead, RouterOS pushes to MikroView: firewall log lines over
syslog (steps 1–3, required), and optionally a copy of its own
config for host names and rule lookups (step 4). Either way, the
router always initiates; MikroView never connects to it. This is a
one-time configuration on each router you want to monitor.

## 1. Point RouterOS at the container over TLS

MikroView's only syslog listener speaks `remote-protocol=tls` (RFC
5425's syslog-over-TLS port, `6514`, mapped straight through by
`deploy/docker-compose.yml`) — confidentiality for firewall log traffic
on the wire, and MikroView authenticating itself to the router.
Replace `203.0.113.10` with the IP of the Docker host running
MikroView, and `mikroview-host` with its hostname or IP.

**Requires RouterOS 7.18 or later.** `remote-protocol=tls` is rejected
on older releases (verified against booted CHR images: 6.49.18 and
7.1–7.17 all reject it; 7.18 and later accept it) — this is MikroView's
only syslog listener, so it's the effective minimum for this whole
guide, not just this step.

The router is the one *initiating* the connection here, so — unlike a
browser, where you visit MikroView and get a one-time warning — the
router needs to be told explicitly to trust MikroView's certificate,
or every syslog push will fail closed. Skipping this step is not a
shortcut: without it, `check-certificate=yes` below refuses with

```
failure: SSL: ssl: no trusted CA certificate found (6)
```

which is the honest, safe failure — a router that will send your
firewall logs to *anything* claiming to be MikroView is not a router
you want. Import the certificate once:

```
/tool fetch url="https://<mikroview-host>/ca.crt" check-certificate=no dst-path=mikroview-ca.crt
/certificate import file-name=mikroview-ca.crt passphrase=""
```

`check-certificate=no` here is the one and only place it belongs in
this whole setup: there is nothing to verify against yet, since this
fetch is *getting* the thing to verify against. Every fetch and logging
action after this uses `check-certificate=yes`. If your MikroView
deployment uses your own certificate rather than the self-generated one
(see [configuration.md](configuration.md#tls)), skip this step — your
CA is presumably already trusted some other way.

Then point the router's logging at MikroView:

```
/system logging action add name=mikroview target=remote remote=203.0.113.10 remote-port=6514 remote-protocol=tls check-certificate=yes
```

This does **not** authenticate the router to MikroView — RouterOS's
logging action has no client-certificate option, so anything able to
reach the port can still connect and inject log lines. The trust here
is one-directional: the router verifying MikroView, not the reverse.

## 2. Forward firewall log events to it

RouterOS tags firewall rule matches with both the `firewall` category and
`info` severity — forward both:

```
/system logging add topics=firewall,info action=mikroview
```

## 3. Tag your firewall rules

RouterOS firewall log lines don't say "accepted" or "dropped" — that's
only known from *which rule* logged the packet. MikroView decodes this
from the rule's `log-prefix`, using a compact convention:

```
<ACTION>|<rule-slug>|
```

- `A` = accept, `D` = drop, `R` = reject, `L` = log-only/passthrough
- `rule-slug` is a short, human-meaningful label (lowercase, hyphens)
- **the trailing `|` is required** — RouterOS concatenates the log-prefix
  directly onto the log message with no guaranteed separating space, so
  without a hard terminator there'd be no reliable way to tell where the
  prefix ends and the rest of the message begins. A prefix missing the
  trailing `|` won't be recognized at all — every event will show up as
  action "unknown" (`?`), un-colored, un-labeled.

**Keep the whole prefix to 15 characters or fewer** (trailing `|`
included). RouterOS's log formatter is known to corrupt/overwrite
adjacent fields once a log-prefix gets much past ~20 characters — 15
stays safely clear of that.

### The universal part: tag rules you already have

You almost certainly already have a firewall rule set. The only thing
that matters is adding `log=yes` and a `log-prefix` to the rules you
want visible — don't recreate your rules, edit them in place. Find the
rule number with `/ip firewall filter print`, then:

```
/ip firewall filter set <rule-number> log=yes log-prefix="A|wan-in|"
```

(`A` here because that example rule accepts traffic — use `D`/`R`/`L`
for a drop/reject/log-only rule instead.) Repeat for whichever rules you
want to see in the live view — your default drop rule and a couple of
accept rules is a good starting point.

Rules without a `log-prefix` (or without `log=yes` at all) still work —
they show up with action "unknown" and no rule label, since MikroView has
no way to know what RouterOS decided without one.

### If you're starting from a blank firewall

The rules below are an *illustrative example*, not a universal script —
they assume things like a `mgmt` address-list existing and specific
chain/interface naming that won't match your setup. Treat them as a
reference for the pattern (match condition + `action=` + `log=yes
log-prefix=...`), not something to paste in blind:

```
/ip firewall filter add chain=input connection-state=established,related action=accept log=yes log-prefix="A|est-rel|"
/ip firewall filter add chain=input connection-state=invalid action=drop log=yes log-prefix="D|invalid|"
/ip firewall filter add chain=input protocol=tcp dst-port=22 src-address-list=mgmt action=accept log=yes log-prefix="A|mgmt-ssh|"
/ip firewall filter add chain=input action=drop log=yes log-prefix="D|input-def|"
/ip firewall filter add chain=forward action=accept log=yes log-prefix="A|lan-wan|"
/ip firewall filter add chain=forward action=drop log=yes log-prefix="D|fwd-def|"
```

### NAT rules (optional)

The same `log=yes log-prefix="..."` convention works on `/ip firewall nat`
rules too — no separate setup needed. Add the topic forward for NAT the
same way as step 2 covers firewall/info, then tag the NAT rules you care
about:

```
/system logging add topics=firewall,info action=mikroview
/ip firewall nat set <rule-number> log=yes log-prefix="A|port-fwd|"
```

Events from a NAT rule show up with `chain` set to `srcnat` or `dstnat`
(whichever the rule belongs to). If RouterOS includes its translated-
address annotation in the log line, MikroView shows the post-NAT address
alongside the original source/destination. RouterOS doesn't document a
fixed format for that annotation, so MikroView parses it defensively
(diffing against the already-known address pair rather than assuming a
fixed layout) — if a translated address ever looks wrong for your
RouterOS version, the untouched raw line is still available in the row's
tooltip for comparison.

## 4. Push router state for names and rule lookups (optional)

Everything above — syslog, rule tagging — is the complete setup.
**Walking away here is a perfectly good outcome**: MikroView already
shows every event, action, and (once you've tagged rules per step 3)
which rule matched. Nothing below changes that.

What this section adds: RouterOS *pushes* a copy of its own config —
address lists, firewall/NAT rules, DNS static entries, DHCP leases,
ARP, WireGuard peers — to MikroView every 15–30 minutes. Three things
follow from that:

- **Host names.** An address MikroView already logs shows up as a name
  (`camera.lan`, not `203.0.113.2`) wherever the router has named it —
  a DNS static entry, a DHCP lease, or a WireGuard peer comment.
- **Rule and NAT lookup buttons.** Click the "i" beside a rule or NAT
  cell on an event row to see the full rule — its comment, chain,
  action — not just the short `log-prefix` slug from step 3.
- **Suggested watchlist entries.** Named devices and ports an existing
  rule already blocks show up as review-and-accept suggestions (Menu →
  Suggestions) instead of the watchlist starting as a blank page — see
  4c-ii below and
  [configuration.md](configuration.md#suggested-watchlist-entries-issue-243).
  This is the one item on this list that needs more than the filter-rule
  push in 4c: without 4c-ii's DHCP-lease/ARP blocks too, there is
  nothing to suggest a device from.

This never gives MikroView a RouterOS password, and MikroView never
connects to your router — the router connects to MikroView, on its own
schedule, the same direction syslog already flows. It authenticates
with an *ingest token*: a credential minted in MikroView and pasted
into the script below, distinct from the read-only API tokens covered
in [configuration.md](configuration.md#api-tokens-read-only), and
scoped to exactly one router.

**Requires RouterOS 7.13 or later** for `:serialize to=json`, which the
script below uses to build its payload, and does not exist before 7.13
(`bad command name serialize`) — moot in practice, since step 1's
`remote-protocol=tls` already requires 7.18 or later for the whole
guide.

### 4a. Certificate trust

Already done in step 1, if you set up syslog first — this push uses the
same imported CA, not a second trust step. If you're somehow doing this
before step 1, go do that CA import now, then come back here.

### 4b. Mint an ingest token

In MikroView, sign in as an admin, open the menu → **API tokens**, set
the kind dropdown to **Ingest**, and pick the device the token speaks
for — this is what scopes it. The list offers every router MikroView
knows about: those declared under `devices:` in `config.yaml`, and any
that has simply sent syslog (marked *not in config.yaml*, identified by
its source IP).

Both work, with one thing to know if you pick an undeclared router: the
token's scope is the device id as it was at that moment. Declaring that
router later under `devices:` **with an explicit `id`** renames it, and
the token then scopes to an identity nothing uses any more — pushes keep
returning `200` while the enrichment silently stops. Declaring it with
no `id` is safe: the id defaults to its `sourceIp`, which is exactly
what it already had. Otherwise, reissue the token after declaring it.
See [configuration.md](configuration.md).

Or via the API:

```
curl -k -b <your session cookie> -X POST https://<mikroview-host>/api/tokens \
  -H 'Content-Type: application/json' -H 'X-Requested-With: mikroview' \
  -d '{"name":"office-router","kind":"ingest","device":"office-router"}'
```

The response's `value` field is the token — shown exactly once, the
same one-time-display every MikroView token uses. **One token per
router, never shared.** This isn't caution for its own sake: any
RouterOS user holding the built-in `read` policy can print a script's
source in full, token included (`/system script get <name> source`),
so the credential is only ever as private as the router's own user
list. Scoping it to one device means a compromised or careless router
can misreport its own state, never another router's.

### 4c. The push script

This example pushes your firewall filter table — the one the rule
lookup button reads. Each field below is renamed from RouterOS's own
property name to MikroView's schema on the way out (`log-prefix`
becomes `logPrefix`, `src-address-list` becomes `srcAddressList`) —
that renaming is what the `:local rec {...}` line is doing, one field
at a time, and it's the one place a typo silently breaks the feature
without RouterOS complaining, so copy it carefully. Verified against a
real RouterOS 7.23.3 router before writing this down:

```
:local recs [:toarray ""]
:foreach i,v in=[/ip/firewall/filter print as-value] do={
  :local rec {"ordinal"=$i; "comment"=($v->"comment"); "chain"=($v->"chain"); "action"=($v->"action"); "srcAddressList"=($v->"src-address-list"); "logPrefix"=($v->"log-prefix"); "dstPort"=($v->"dst-port"); "protocol"=($v->"protocol"); "log"=($v->"log"); "dstAddress"=($v->"dst-address"); "srcAddress"=($v->"src-address")}
  :set recs ($recs, {$rec})
}
:local payload [:serialize to=json value={"kind"="filter-rule"; "page"=1; "pages"=1; "records"=$recs}]
/tool fetch url="https://<mikroview-host>/api/ingest/routeros" http-method=post http-data=$payload http-header-field=("Content-Type: application/json,Authorization: Bearer <your ingest token>") check-certificate=yes output=none
```

**Update MikroView before you update this script.** MikroView refuses a
push containing a field it does not recognise, rather than ignoring the
extra — that strictness is deliberate (it is how a typo in a field name
becomes an error instead of a silently missing column), but it means a
router sending the newer script to an older MikroView gets a `400` and
stops pushing. The other order is safe: an older script against a newer
MikroView just leaves the new fields unset.

`log`, `dstAddress` and `srcAddress` were added for issue #274: they are
what lets MikroView tell you that a watchlist entry can never match
because no rule on this router logs traffic in its scope. `log` is the
important one — a rule with `log=no` sends nothing at all, whatever else
it matches, and without this field MikroView had to guess from whether a
`log-prefix` happened to be set, which is wrong in both directions.

`dstPort`/`protocol` were added for issue #243's suggested-watchlist-entries
feature: without them mikroview has no way to know which ports a rule
that's already blocking traffic actually covers. RouterOS's own
`dst-port` is unset (empty) on most "drop everything on this chain"
rules and a list or range ("22,23", "1000-2000") on ones that scope by
port — both push through as a plain string, unparsed, since MikroView
only needs to display and match it, never compute on it.

Line by line:

- `:local recs [:toarray ""]` starts an empty list. `{}` on its own is
  *not* an empty array in RouterOS script — it's read as an empty code
  block — so this is the working idiom, not a stylistic choice.
- `:foreach i,v in=[...] do={...}` walks every filter rule; `i` is its
  position (which becomes `ordinal` — the number you'd see in
  `/ip firewall filter print`), `v` is the rule's own data.
- `:local rec {"ordinal"=$i; ...}` builds one renamed record. `($v->"comment")`
  reads a field off `v` by RouterOS's own property name.
- `:set recs ($recs, {$rec})` appends `rec` to the list. **The `{$rec}`
  wrapping is required** — `:set recs ($recs, $rec)` without it looks
  identical but silently *merges* each record's keys into one giant
  map instead of building a list of separate records, and MikroView's
  strict decoder refuses the result outright (an unexpected shape,
  not a helpful error naming which field).
- `:local payload [:serialize to=json ...]` turns the whole thing into
  the JSON body — `kind` names which table this is, `page`/`pages` are
  `1`/`1` here since one filter table comfortably fits one push (see
  pagination below for a large rule set).
- `/tool fetch ... output=none` sends it. `output=none` because a
  scheduled script has no console to print to; drop it if you're
  testing this by hand and want to see the result.

### 4c-ii. DHCP leases and ARP -- what issue #243's suggestions feature needs

MikroView's watchlist can *suggest* entries from named devices it
already knows about (Menu → Suggestions -- see
[configuration.md](configuration.md#suggested-watchlist-entries-issue-243)),
but only once it's actually been sent DHCP leases and ARP entries.
Without this section pushed, that feature has nothing to suggest from --
it isn't a separate opt-in, it's this data or nothing.

Same pattern as filter rules, two more independent blocks (each is its
own push, since a payload carries exactly one `kind`). Verified against
a real RouterOS 7.23.3 router, including the one genuinely surprising
part: a lease nobody's DHCP client has actually requested yet -- a
static lease you typed in by hand, say -- has no `host-name` at all, not
an empty one. `($v->"host-name")` on that lease still evaluates fine and
serializes as JSON `null`, which MikroView already treats as "no
hostname" (the same way it already treats an unset `dst-port`) -- so
this reaches MikroView correctly either way, named or not.

```
:local leaseRecs [:toarray ""]
:foreach i,v in=[/ip/dhcp-server/lease print as-value] do={
  :local rec {"hostname"=($v->"host-name"); "mac"=($v->"mac-address"); "address"=($v->"address")}
  :set leaseRecs ($leaseRecs, {$rec})
}
:local leasePayload [:serialize to=json value={"kind"="dhcp-lease"; "page"=1; "pages"=1; "records"=$leaseRecs}]
/tool fetch url="https://<mikroview-host>/api/ingest/routeros" http-method=post http-data=$leasePayload http-header-field=("Content-Type: application/json,Authorization: Bearer <your ingest token>") check-certificate=yes output=none

:local arpRecs [:toarray ""]
:foreach i,v in=[/ip/arp print as-value] do={
  :local rec {"address"=($v->"address"); "mac"=($v->"mac-address")}
  :set arpRecs ($arpRecs, {$rec})
}
:local arpPayload [:serialize to=json value={"kind"="arp"; "page"=1; "pages"=1; "records"=$arpRecs}]
/tool fetch url="https://<mikroview-host>/api/ingest/routeros" http-method=post http-data=$arpPayload http-header-field=("Content-Type: application/json,Authorization: Bearer <your ingest token>") check-certificate=yes output=none
```

Each block uses its own variable names (`leaseRecs`/`leasePayload`,
`arpRecs`/`arpPayload`) rather than reusing `recs`/`payload` from 4c --
all three blocks end up in the same scheduled script (4e), in the same
top-level scope, and RouterOS script doesn't scope a `:local` to just
its own `:foreach`.

ARP has no name of its own, but pushing it still earns its keep: a
device's DHCP lease can go stale between the router's own renewal
cycles, while ARP reflects what's actually answering right now.
MikroView prefers ARP's address over a same-MAC lease's when both are
pushed, for exactly that reason (the choice is made in
`internal/suggest`'s candidate generation; `internal/routerstate` just
holds what each router pushed).

**Other tables** follow the identical pattern, swapping the source
command and the field names for MikroView's schema names -- worth
adding if you want the rule/NAT lookup buttons or host-name resolution
to cover more than filter rules and DHCP/ARP:

| `kind` | Source command | Fields |
|---|---|---|
| `address-list` | `/ip/firewall/address-list print as-value` | `list`, `address`, `comment`, `dynamic` |
| `filter-rule` | `/ip/firewall/filter print as-value` | `ordinal` (loop index), `comment`, `chain`, `action`, `srcAddressList` ← `src-address-list`, `logPrefix` ← `log-prefix`, `dstPort` ← `dst-port`, `protocol`, `log`, `dstAddress` ← `dst-address`, `srcAddress` ← `src-address` |
| `nat-rule` | `/ip/firewall/nat print as-value` | `ordinal` (loop index), `comment`, `chain`, `action` |
| `dns-static` | `/ip/dns/static print as-value` | `name`, `address` |
| `dhcp-lease` | `/ip/dhcp-server/lease print as-value` | `hostname` ← `host-name`, `mac` ← `mac-address`, `address` |
| `arp` | `/ip/arp print as-value` | `address`, `mac` ← `mac-address` |
| `wireguard-interface` | `/interface/wireguard print as-value` | `name`, `comment`, `publicKey` ← `public-key`, `listenPort` ← `listen-port` |
| `wireguard-peer` | `/interface/wireguard/peers print as-value` | `publicKey` ← `public-key`, `allowedAddress` ← `allowed-address`, `endpointAddress` ← `endpoint-address`, `comment` |

For host names, `dns-static` and `dhcp-lease` (above) are the two worth
adding first — they're what turns a raw IP into `nas.lan` everywhere
MikroView shows one. **A name pushed by the router always wins** over a
label you've set inside MikroView for the same address — manage names
for anything the router already knows about *in RouterOS*, not in
MikroView's UI; anything the router doesn't cover stays exactly as
you set it there.

**A pushed name only applies to that router's own traffic.** If you
monitor more than one router, a name `office-router` pushes is used on
events MikroView received from `office-router`, and nowhere else. That
is the same one-router blast radius the ingest token itself has (step
4b): a compromised or careless router can misname the hosts it sees,
never the hosts another router sees. Two sites both using
`192.168.1.0/24` therefore don't contaminate each other's names either.

For this to line up, the `device` you name on the token must be the
same identifier the device has in MikroView — the `id` of its entry
under `devices:` in `config.yaml`. That is already true of the rule
and NAT table lookups, so if those work for a router, host names will
too. If you push under a name MikroView doesn't otherwise know, that
router's traffic simply shows unnamed hosts.

No `read,write` or `sensitive` policy is needed for any of this —
`read,test` (below) is enough, and WireGuard *private* keys never
appear in a `read`-policy script's view at all, only public ones.

### 4d. Pagination, for a large rule set

`/tool fetch` refuses a POST body over roughly 64KiB — measured
against a real router, not documented by MikroTik. A single filter
table (or lease table, or ARP table) stays well under that for most
deployments (a few hundred rules serialize to tens of kilobytes), so
the one-page script above is enough until it isn't. If `[:len $recs]`
(or `$leaseRecs`/`$arpRecs`) output starts running into the low
hundreds, split it with `:toarray` slicing and increment `page`/`pages`
accordingly — each page is a complete, independent JSON document;
MikroView never reassembles pages, so partial delivery degrades to
"less enrichment," never a corrupted table.

### 4e. Schedule it

One script, all the blocks from 4c and 4c-ii concatenated in order —
`:local` names don't collide between them (see 4c-ii's own note on
why), and each block's `/tool fetch` is an independent push, so one
failing (say, a momentary network blip) doesn't stop the others in the
same run.

```
/system script add name=mv-push policy=read,test source="<the filter-rule block from 4c, then the dhcp-lease and arp blocks from 4c-ii, each with your host and token filled in>"
/system scheduler add name=mv-push interval=20m policy=read,test on-event="/system script run mv-push"
```

`policy=read,test` only — no `write`, no `sensitive`. The scheduler
entry stores no credential of its own; the only secret involved is the
bearer token embedded in the script's own source, held to the same
`read`-policy-can-read-it caveat as step 4b describes. 15–30 minutes
is plenty: this data changes when you edit your firewall, not every
few seconds.

### If you're doing this in WinBox instead

The two commands above map to **System → Scripts → +** and **System →
Scheduler → +**. The script dialog has options the CLI commands never
mention, and its defaults are not the CLI's:

- **Policy** — WinBox pre-ticks *every* policy for a new script.
  Untick everything except **read** and **test**: `read` is what lets
  the script print the tables it pushes, `test` is what `/tool fetch`
  needs, and nothing in this script changes config (`write`), manages
  users (`policy`, `password`), or reads secrets (`sensitive`). The
  scheduler entry needs the same two ticked — RouterOS refuses to run
  a script whose policies the scheduler doesn't also hold, so a
  `read,test` script under a default scheduler runs, but not the
  other way round.
- **Don't Require Permissions** — leave unchecked. Checking it lets
  any user able to run scripts run this one with the *script's*
  permissions instead of their own. This setup has no use for that.
- **Owner** — filled in automatically with the account that saves the
  script; nothing to set.
- **Last Time Started / Run Count** — read-only status, empty until
  the first run. After **Run Script** (or the scheduler firing), Run
  Count incrementing tells you the script ran — it does *not* tell
  you the pushes landed, which is what step 5 checks.

And paste the source with `<mikroview-host>` and the token already
filled in — the dialog saves placeholders without complaint, and the
failure only surfaces later as `failure:` lines in `/log print`.

## 5. Verify

On the router, confirm entries are being generated and sent:

```
/log print where topics~"firewall"
```

Then check MikroView picked it up — either watch the live view in the
browser, or:

```
curl -k https://<mikroview-host>/api/devices
```

(`-k` skips certificate verification — expected against MikroView's
self-generated certificate; see the main README's Quickstart.)

A router that's sending traffic but isn't yet in `config.yaml` still
shows up here, labelled by its source IP with `"configured": false` —
that's how you find the IP to add. See [configuration.md](configuration.md).

If you set up step 4, run the script once by hand (`/system script run
mv-push`) rather than waiting for the scheduler, then check the rule
lookup button on an event row whose rule you tagged — it should show
that rule's comment and RouterOS ordinal instead of "no data pushed
yet". `/system script run mv-push` with no output means it worked;
`/tool fetch` failing prints a `failure:` line to the console the same
way step 4a's own certificate check does, including the same
untrusted-CA text if step 4a was skipped or the `<mikroview-host>`
placeholder wasn't replaced consistently between the two.

If you also set up 4c-ii, check **Menu → Suggestions**: a named device
or an already-blocked port should show up under the Undecided filter
within a few minutes of the push landing (suggestions regenerate in the
background periodically, not instantly on push -- see
[configuration.md](configuration.md#suggested-watchlist-entries-issue-243)).
Nothing showing up there usually means no lease on your network has a
reported hostname yet, or no rule has both `action=drop`/`reject` and a
specific `dst-port` -- both real, common states, not a broken push.
