# RouterOS setup

> **There is a guided version of this page inside MikroView.** Sign in as
> an admin and open **Admin ▸ Run setup…**. It generates every
> command below with your own address, port and a token it mints for you
> — nothing to fill in — and tells you as each step lands, because each
> one ends with your router arriving at MikroView.
>
> This page remains the reference: what the wizard emits, and why. Use it
> if you prefer working from documentation, if you are scripting a fleet,
> or when you want the reasoning behind a step.

MikroView never talks to RouterOS's API and needs no credentials on the
router. Instead, RouterOS pushes to MikroView: firewall log lines over
syslog (steps 1–3, required), optionally a copy of its own config for
host names and rule lookups (step 4), and optionally a nightly config
backup MikroView keeps encrypted (step 7, issue #394). Either way, the
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

If MikroView runs with `tls.enabled: false` — the reverse-proxy
deployment, where the proxy terminates TLS for browsers and the syslog
TLS listener still runs for this router — use `http://` in that URL
instead. MikroView still generates and serves the CA in that case (it is
what the syslog listener presents), but plain HTTP is what is answering
on that port.

`check-certificate=no` here is the one and only place it belongs in
this whole setup: there is nothing to verify against yet, since this
fetch is *getting* the thing to verify against. Every fetch and logging
action after this uses `check-certificate=yes`. If your MikroView
deployment uses your own certificate rather than the self-generated one
(see [configuration.md](configuration.md#tls)), skip this step — your
CA is presumably already trusted some other way.

Then point the router's logging at MikroView:

```
/system logging action add name=mikroview target=remote remote=203.0.113.10 remote-port=6514 remote-protocol=tls remote-log-format=syslog check-certificate=yes
```

This does **not** authenticate the router to MikroView — RouterOS's
logging action has no client-certificate option, so anything able to
reach the port can still connect and inject log lines. The trust here
is one-directional: the router verifying MikroView, not the reverse.

`remote-log-format=syslog` gives every message its own standard header
(timestamp and topic), so MikroView can tell where one firewall log line
ends and the next begins even when several arrive at once — a burst of
matching traffic, one connection attempt logged from two different rules
— rather than only when RouterOS happens to send them far enough apart.
Without it, a fast-enough burst can be read as a single garbled line and
the traffic in it silently mismatched (#614).

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

- `A` = accept, `D` = drop, `R` = reject, `L` = log-only/passthrough,
  `M` = mangle mark rule, `N` = NAT
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
for a drop/reject/log-only rule instead, and `M`/`N` for the mangle and
NAT rules covered below.) Repeat for whichever rules you want to see in
the live view — your default drop rule and a couple of accept rules is a
good starting point.

Rules without a `log-prefix` (or without `log=yes` at all) still work —
they show up with action "unknown" and no rule label, since MikroView has
no way to know what RouterOS decided without one.

**Log your accept and drop rules, not only bare `log` rules.** A rule
with `action=log` writes a line and then hands the packet to the next
rule, so it never says what happened in the end — and some flags are
deliberately quiet about traffic your firewall blocked, which they can
only tell from the rule that did the blocking. If the only rules you log
are `L|` ones, those flags have nothing to go on and stay silent.

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
/ip firewall nat set <rule-number> log=yes log-prefix="N|port-fwd|"
```

`N`, not `A`: a NAT rule translates an address, it does not decide
whether the packet lives, so it gets its own action — `natted` — rather
than borrowing a filter verdict it never made.

**This is what buys you an exact answer.** RouterOS never says which NAT
rule performed a translation, so a `log-prefix` you set is the only thing
that can name one. Tag a rule and the "i" button beside a NAT cell shows
you *that rule*. Leave it untagged and the same button can only show the
table split into the rules the event could have come from and the rules
it rules out, with the reason against each — useful, but not an answer.
Push the NAT table too (step 4c) for either to have anything to show.

Events from a NAT rule show up with `chain` set to `srcnat` or `dstnat`
(whichever the rule belongs to). If RouterOS includes its translated-
address annotation in the log line, MikroView shows the post-NAT address
alongside the original source/destination. RouterOS doesn't document a
fixed format for that annotation, so MikroView parses it defensively
(diffing against the already-known address pair rather than assuming a
fixed layout) — if a translated address ever looks wrong for your
RouterOS version, the untouched raw line is still available in the row's
tooltip for comparison.

### Mangle rules and policy routing (optional)

Policy routing — mangle rules marking connections, routes or packets to
steer traffic into a VPN tunnel or across a second WAN — logs the same
way, with `M`:

```
/ip firewall mangle set <rule-number> log=yes log-prefix="M|vpn-route|"
```

Those events show up with action `marked`, and the action filter will
narrow the live view to them.

`M` is not optional decoration here, it is the *only* thing that
identifies the rule. A mangle log line is byte-for-byte the shape of a
filter line — the same chain names, the same fields — and RouterOS
prints neither the action nor the mark it set. Without the prefix
MikroView has nothing to read the answer off, so the events land in
"unknown" alongside genuinely unparseable ones. (The one exception,
which needs no tagging, is a `srcnat`/`dstnat` line carrying RouterOS's
translated-address annotation: that line states the translation, so
MikroView reads `natted` straight off it.)

**Mind the volume before you turn this on.** `mark-packet` rules match
every packet rather than every connection, which is your whole traffic
throughput arriving as log lines — the same trap as logging the
established/related accept rule. Start with the `mark-connection` or
`mark-routing` rule at the head of the chain, not all of them.

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
  action — not just the short `log-prefix` slug from step 3. For a NAT
  translation the button answers one of two different questions, and
  says which: a rule you tagged with a `log-prefix` is named outright,
  and an untagged one can only be narrowed down — see "NAT rules" above
  for why tagging is worth the two minutes.
- **Suggested watchlist entries.** Named devices and ports an existing
  rule already blocks show up as review-and-accept suggestions
  (**Expect ▸ Watchlist ▸ Suggestions**) instead of the watchlist
  starting as a blank page — see
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

In MikroView, sign in as an admin, open **Admin ▸ The engine room**,
find "Which machines may speak" among the side doors, set the kind
dropdown to **Ingest**, and pick the device the token speaks
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

Underneath all of this is one rule: the token's device must equal the
event's deviceId, or the push and the traffic it's meant to enrich land
under two different identities and nothing tells you why. A multi-homed
router — one with an address on every subnet it routes, which is most
of them — is the usual reason it doesn't: syslog is stamped with
whichever interface's address faces MikroView, and that's frequently
not the address you declared as `sourceIp`. If pushes return `200` in
the audit log but the "i" popups still say no data has been pushed,
check whether the device id the token is scoped to actually matches the
`deviceId` on the events you're looking at — a mismatched source
address is the most likely cause. The setup wizard's step 2 names it
when it sees it (a declared router that has sent nothing while an
undeclared address streams) and prints the fix. Keeping the declared
address is the recommended one, because the token and the tables it
pushes follow that identity, so nothing has to be reissued:

```
/system logging action set mikroview src-address=<the address you declared as sourceIp>
```

The alternative is changing `sourceIp` to the arriving address and
restarting — then reissue any token minted for the old identity.

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
  :local rec {"ordinal"=$i; "comment"=($v->"comment"); "chain"=($v->"chain"); "action"=($v->"action"); "srcAddressList"=($v->"src-address-list"); "logPrefix"=($v->"log-prefix"); "dstPort"=($v->"dst-port"); "protocol"=($v->"protocol"); "log"=($v->"log"); "dstAddress"=($v->"dst-address"); "srcAddress"=($v->"src-address"); "connectionState"=($v->"connection-state"); "inInterface"=($v->"in-interface"); "outInterface"=($v->"out-interface"); "packets"=($v->"packets"); "bytes"=($v->"bytes")}
  :set recs ($recs, {$rec})
}
:local payload [:serialize to=json value={"kind"="filter-rule"; "page"=1; "pages"=1; "routerosVersion"=[/system/resource get version]; "records"=$recs}]
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

`connectionState`, `inInterface` and `outInterface` were added for issue
#408. Nothing in MikroView reads them yet, deliberately: they are the
input a later "which rules can actually feed this view" answer is built
from, and that answer is only worth designing against rule data that has
genuinely been pushed for a while. Sending them now costs one line and
means the history exists when it's wanted. `connection-state` is a *set*
— `established,related` is two values — and MikroView takes it either as
the array RouterOS sends or as a comma-joined string, so
`($v->"connection-state")` can go straight in with no conversion.

`packets` and `bytes` were added for issue #435: RouterOS keeps a
per-rule hit counter whether or not the rule logs, so the "Tune logging"
helper can show a rule's real cost — "fired 41,000 times in the last
day" — beside its tick-box before you switch logging on for it. Same
shape as every other RouterOS integer here: `:serialize to=json` emits
them as a float, which MikroView's decoder already expects.

`routerosVersion` on the payload (not on a record — it describes the
router, not a rule) is the router telling MikroView which RouterOS it is
running, so MikroView can warn when a command it shows you was written
against a different version. It is read straight from
`[/system/resource get version]`. Nothing warns yet; the field is what
that warning will be derived from, and deriving it is why MikroView
never has to ask you. Leave it out and everything still works — you just
get no version-mismatch warning later.

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
  pagination below for a large rule set), and `routerosVersion` is the
  router's own version, the one field on the payload rather than on a
  record. It is optional, and it is the same line in every block.
- `/tool fetch ... output=none` sends it. `output=none` because a
  scheduled script has no console to print to; drop it if you're
  testing this by hand and want to see the result.

### 4c-ii. DHCP leases and ARP -- what issue #243's suggestions feature needs

MikroView's watchlist can *suggest* entries from named devices it
already knows about (**Expect ▸ Watchlist ▸ Suggestions** -- see
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
| `filter-rule` | `/ip/firewall/filter print as-value` | `ordinal` (loop index), `comment`, `chain`, `action`, `srcAddressList` ← `src-address-list`, `logPrefix` ← `log-prefix`, `dstPort` ← `dst-port`, `protocol`, `log`, `dstAddress` ← `dst-address`, `srcAddress` ← `src-address`, `connectionState` ← `connection-state` (a set — send it as-is), `inInterface` ← `in-interface`, `outInterface` ← `out-interface`, `disabled`, `packets`, `bytes` |
| `nat-rule` | `/ip/firewall/nat print as-value` | `ordinal` (loop index), `comment`, `chain`, `action`, `logPrefix` ← `log-prefix`, `toAddresses` ← `to-addresses`, `toPorts` ← `to-ports`, `dstPort` ← `dst-port`, `protocol`, `inInterface` ← `in-interface`, `outInterface` ← `out-interface`, `srcAddress` ← `src-address`, `dstAddress` ← `dst-address`, `disabled`, `dynamic` |
| `dns-static` | `/ip/dns/static print as-value` | `name`, `address` |
| `dhcp-lease` | `/ip/dhcp-server/lease print as-value` | `hostname` ← `host-name`, `mac` ← `mac-address`, `address` |
| `arp` | `/ip/arp print as-value` | `address`, `mac` ← `mac-address` |
| `ip-address` | `/ip/address print as-value` | `address`, `network`, `interface`, `comment` |
| `wireguard-interface` | `/interface/wireguard print as-value` | `name`, `comment`, `publicKey` ← `public-key`, `listenPort` ← `listen-port` |
| `wireguard-peer` | `/interface/wireguard/peers print as-value` | `publicKey` ← `public-key`, `allowedAddress` ← `allowed-address` (**send the array as-is**), `endpointAddress` ← `endpoint-address`, `comment`, `lastHandshake` ← `last-handshake` (absent if never handshaken), `currentEndpointAddress` ← `current-endpoint-address`, `rx`, `tx`, `disabled`, `interface` ← `interface` (which WireGuard interface this peer belongs to) |
| `ppp-active` | `/ppp/active print as-value` | `name`, `service`, `address`, `callerId` ← `caller-id`, `uptime` -- covers L2TP, PPTP, SSTP and OVPN alike; a session's presence in the push is itself the up/down signal |

Every block's payload may carry `"routerosVersion"=[/system/resource get
version]` alongside `kind`/`page`/`pages`, exactly as 4c's does. It is
optional and it is the same line everywhere; there is nothing per-kind
about it.

Two fields are **sets**, not single values, and RouterOS sends them as
arrays: a WireGuard peer's `allowed-address` (a peer can route several
CIDRs) and a filter rule's `connection-state`. Pass them straight
through — `"allowedAddress"=($v->"allowed-address")` — and MikroView
takes the array. Joining them into a comma-separated string by hand
still works, so a script written against an earlier version of this page
does not have to change, but there is no reason to write a new one that
way.

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

If you also set up 4c-ii, check **Expect ▸ Watchlist ▸ Suggestions**: a named device
or an already-blocked port should show up under the Undecided filter
within a few minutes of the push landing (suggestions regenerate in the
background periodically, not instantly on push -- see
[configuration.md](configuration.md#suggested-watchlist-entries-issue-243)).
Nothing showing up there usually means no lease on your network has a
reported hostname yet, or no rule has both `action=drop`/`reject` and a
specific `dst-port` -- both real, common states, not a broken push.

## 6. Recommended logging posture: log connections, not traffic

Steps 1–3 make MikroView show what your router logs. This section is
about choosing *what to log* — because the default instinct on both
ends of the spectrum is wrong, and the difference between a good
posture and a bad one is roughly two orders of magnitude of volume
with no loss of signal.

The failure mode on the noisy end: logging on broad accept rules that
match `established`/`related` traffic. Every packet-bearing flow then
logs its tail over and over — lines that carry no connection-level
information at all, because the interesting fact (this connection was
opened, by whom, to where) was only ever present at its birth. On a
real deployment this measured as ~90% of all volume from two such
rules, ~51M events/day at ~594 events/sec average — compressing the
default 120MiB event buffer to **4–6 minutes** of visible history.
Removing logging from those two rules alone (nothing else) cut
sustained volume by 97–99%, to ~12–14 events/sec measured across the
following days — and stretched the same buffer to **several hours**,
while the deny signal that had been drowned (a steady ~14 unsolicited
WAN drops/minute) became visible in the top rules for the first time.

The failure mode on the quiet end: logging only drops. A drops-only
log is blind to every attack that *works* — successful inbound is an
accept, a compromised device phoning home is an accept, lateral
movement between segments is an accept. The things most worth seeing
are all accepts; they just need logging at the connection level, not
the packet level.

The posture, as a starting point any deployment can adapt:

| Rule | Log? | Why |
|---|---|---|
| accept established/related (or fasttrack) | **no** | the tail of a connection already seen at birth — zero detection signal |
| accept **new** WAN→LAN (port-forwards) | **always** | inbound success: the highest-signal line there is, at tiny volume |
| accept **new** LAN→WAN | yes | each device's first contact with each destination — the compromise/exfiltration signal |
| accept new LAN→LAN / inter-VLAN | yes | lateral movement |
| known-chatty internal services (DNS to the local resolver, NTP, mDNS) | quiet accept rules **above** the loggers | a *chosen, named* blind spot instead of drowning |
| drops | yes | cheap, and already the norm |

Three caveats that belong next to that table, not in a footnote:

- **Never add `connection-state=new` to an existing accept rule as the
  "fix".** That changes what the rule *accepts*, not just what it
  logs — established traffic that matched the rule yesterday stops
  matching it today, and you have black-holed live connections. The
  safe shape is an earlier no-log accept for
  `connection-state=established,related`, so the broad rule below it
  only ever sees — and therefore only ever logs — connection opens.
  (This is also why a log-only `action=passthrough` rule placed above
  an accept is a safe way to add logging without touching policy at
  all.)
- **Fasttrack changes what the filter chain sees.** With a
  `fasttrack-connection` rule in place, established packets bypass most
  of the chain entirely — which is fine for this posture (the
  connection open still traverses and still logs), but it means a
  logging rule placed *below* the fasttrack rule may see far less than
  you expect. Check with `/ip firewall filter print stats`: a logger
  whose counters barely move while traffic flows is being bypassed,
  not idle.
- **Per-packet volume and flow duration are bandwidth questions, and
  NetFlow is the right tool for them** (`/ip traffic-flow`). MikroView
  is a log interrogator on purpose: it answers *who connected to what,
  when, and what the firewall decided*. If the question is "how many
  gigabytes did this flow move", logging more firewall lines will
  never answer it — export flow data to a NetFlow collector instead,
  and keep the firewall log for decisions.

Section 3's tagging convention applies to everything this posture
logs: give each logging rule a distinct `log-prefix`, and MikroView's
per-rule counts will tell you — in numbers, within a day — whether any
single rule is dominating your volume and deserves the same scrutiny
the established-accept rules got above.

## 7. Back up the router's configuration (optional)

Issue #394. A router can push a copy of its own configuration to
MikroView every night — the binary `.backup` that restores it whole on
a replacement, and the plain-text `.rsc` export kept for reading.
**MikroView is the place you turn to when the router is gone**, so this
is worth setting up before that day, not after. See
[configuration.md](configuration.md#router-backups-over-sftp-optional-off-by-default)
for the server side (`backup.enabled`, the retention and quota rules,
the missed-push receipt in Settings) and [SECURITY.md](../SECURITY.md)
for the trust caveat below.

### 7a. Turn the drop box on

Set `backup.enabled: true` in `config.yaml` and restart — this opens a
second listening port (`backup.listen`, default `:47022`), only once
you have decided to use it. Nothing here needs the wizard, but the
wizard's step 6 is what actually prints the script below with your own
values filled in, which is the easier path for most people.

### 7b. The token

The same ingest token that pushes syslog/state (step 4b) authenticates
this too — `internal/backupsftp` checks the SFTP login against the same
token store, not a second credential. If you have already minted one
for this router, reuse it; nothing here needs a token of its own kind.

### 7c. The script

```
/system script add name=mv-backup policy=read,write,test,sensitive source="
  /system backup save name=mv-backup dont-encrypt=yes
  /export file=mv-backup
  /tool fetch mode=sftp upload=yes address=<mikroview-host> port=47022 user=<device> password=\"<token>\" src-path=mv-backup.backup dst-path=<device>.backup
  /tool fetch mode=sftp upload=yes address=<mikroview-host> port=47022 user=<device> password=\"<token>\" src-path=mv-backup.rsc dst-path=<device>.rsc
  /file remove mv-backup.backup
  /file remove mv-backup.rsc
"
/system scheduler add name=mv-backup interval=1d start-time=03:00:00 policy=read,write,test,sensitive on-event="/system script run mv-backup"
/system script run mv-backup
```

`<device>` is both the SFTP username and the destination file stem —
it must be the router's own device id, the same identity the token is
scoped to. The binary save is asked for `dont-encrypt=yes` on purpose:
that copy is the true restore copy and MikroView never holds a second
password to open an encrypted one; the export is taken without
`show-sensitive`, so it carries no secrets and is safe to read or scan
later. The last line runs the script once immediately, so the first
pair does not wait for 03:00.

`policy=read,write,test,sensitive` is wider than the push script's
`read,test` (4e): `write` and `sensitive` are what `/system backup
save` and `/file remove` need, `test` is what `/tool fetch` needs, and
`sensitive` on the script itself is also what stops a `read`-only
RouterOS user printing the token back out of the saved source — the
same caveat step 4b's own token warning describes.

### Only on a network you trust

RouterOS's SFTP client never verifies MikroView's host key (measured on
RouterOS 7.23.3) — an attacker on the path between the router and
MikroView could pose as MikroView and receive the pair and the token in
plain sight. Run this over a LAN or a VPN you control, never across the
open internet. See [SECURITY.md](../SECURITY.md) and issue #955, which
tracks an HTTPS-based path that does verify.

### 7d. Verify

Settings' `router backups` group (admin-only) lists what has arrived
per router, with `download .backup` / `.rsc` and a note of when each
was last seen. A router that has pushed at least twice and then misses
its usual interval shows an amber receipt there — mikroview learns the
interval from the pushes themselves, never from this scheduler line,
since an operator could change that on the router without mikroview
knowing.

Restoring is your own act on the replacement router
(`/system backup load`) — MikroView never connects to a router to apply
one; it only ever reads the header to confirm what arrived.
