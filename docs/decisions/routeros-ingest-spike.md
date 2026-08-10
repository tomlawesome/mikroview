# RouterOS push ingest: what a real router does

Date: 2026-08-07. Answers the Step 0 spike on #186 before any of the
implementation steps were started, because the payload schema and the
whole router-side script depend on capabilities that were being guessed
at.

Regenerate every line of this with `scripts/live-routeros-step0.sh`
against a booted CHR — see `.claude/skills/live-check/SKILL.md`. Evidence
nobody can reproduce is a claim with better formatting, and two of these
answers are the kind that a RouterOS release can move quietly.

## 1. The 64KiB ceiling is real, and it is the schema's constraint

`/tool fetch` accepts a 65,430-byte POST body and refuses 65,440 with
`failure: maximum message size exceeded`. The refusal is **client-side**:
nothing reaches the server at all, so a router whose payload outgrows the
limit goes silent rather than reporting an error.

It is not an artifact of typing into a console. The same refusal comes
back from `/system script run`, which is the path a scheduler uses.

**Consequence for step 2:** the bounded total size is set by the router,
not chosen by us. A schema that can exceed ~64KiB produces a deployment
that silently stops reporting, which is the worst of the available
failure modes.

**What the budget buys:** 60 firewall filter rules serialise to 7,828
bytes, and 200 rules to 26,370 — about 132 bytes each on a bare CHR, so
the cap lands near 500 rules. **500 is not a large deployment**, and
rules carrying comments and more match conditions fit proportionally
fewer. Treat the cap as something a real site will hit, not a
theoretical ceiling.

### Pagination handles it, and it is not chunking

The worry with "split it up" is having to reassemble a byte stream:
ordering, partial pieces, state on the receiving side. That is chunking,
and it is genuinely awkward. Pagination of whole records is a different
thing, and the router does it natively — `:pick` slices the array that
`print as-value` returns, and each slice serialises independently:

```
:local all [/ip/firewall/filter print as-value]
:local chunk [:pick $all ($p * $size) $upper]
:local body [:serialize to=json value={kind="firewall-rules"; page=($p + 1); of=$pages; rules=$chunk}]
```

Run against 200 rules at 50 per page:

```
page 1/4  6568 bytes  valid_json=True  rules=50  rule-1..rule-50
page 2/4  6603 bytes  valid_json=True  rules=50  rule-51..rule-100
page 3/4  6701 bytes  valid_json=True  rules=50  rule-101..rule-150
page 4/4  6701 bytes  valid_json=True  rules=50  rule-151..rule-200
```

Every page is **a complete, valid JSON document containing whole rules**,
self-describing as `page N of M`, at a tenth of the cap. There is nothing
to rebuild: mikroview applies whole records as they arrive, so no
ordering guarantee is needed and no partial-record state exists.

**Additive-only makes a missing page safe.** Because step 4 forbids
pushed data from clearing a flag or reducing suspicion, the absence of
page 3 can never be read as "those rules were deleted" — it means less
enrichment, which is the degradation #186 already asks for under "fails
closed". That invariant, written for a different reason, is what makes
pagination cheap.

**Consequence for step 2:** page size is a schema decision, and it should
be a record count chosen to keep a page well under the cap even for wide
records — not a byte-slicing routine.

## 2. A file does route around it — over SFTP, at a price

Over HTTP there is no file path at all:

```
/tool fetch url="http://.../q2b" upload=yes src-path=big.json http-method=post
failure: only [s]ftp modes support upload
```

`upload=yes` is FTP and SFTP only, and `http-data` is the sole body
source over HTTP.

**Over SFTP it works, and it works well.** A 293KiB file and then a 5MB
file both uploaded byte-exact (md5 verified against the original), so
there is no meaningful size ceiling on that path. The 64KiB limit is a
property of `http-data`, not of the router.

So the question is not whether the router *can*. It is what the SFTP path
costs, and two measured properties decide it.

**It supports key authentication — this document said otherwise, and was
wrong.** `/tool fetch` has no `keyfile=` parameter (`keyfile=`,
`private-key=`, `identity=` and `key=` are all rejected), and that was
read as "the router cannot present a key". It does not need a parameter:
it uses the private key in the router's own store. Against a server with
`PasswordAuthentication no`:

```
/user/ssh-keys/private import user=admin private-key-file=mvkey
/tool fetch url="sftp://…/drop/keyauth.json" user=mvingest src-path=big.json upload=yes

sshd: Accepted publickey for mvingest … RSA SHA256:kS5gs62Ylhcd…
-rw-r--r-- 1 mvingest mvingest 300011 keyauth.json
```

No password is stored on the router at all. **A MITM on the path cannot
harvest a reusable credential from an SSH public-key exchange**, which is
a materially better position than the password case.

**And unlike a bearer token, a `read` user cannot get at the key.** This
is the finding that most changes the balance, because finding 5 below
shows a token in a script is readable by anyone with `read`:

- The key **file is consumed on import**. `/file print` shows nothing
  afterwards — there is no copy left on disk to fetch.
- There is **no export command**. A `read` user querying
  `/user/ssh-keys/private print detail` sees
  `user=admin key-type=rsa bits=2048` and no key material.
- The key is **bound to a RouterOS user**. `mv-read` running the same
  `/system ssh-exec` that admin runs gets `failure: authentication
  failure`, and the server logs a failed pre-auth. Admin, identical
  command, gets `Accepted publickey`.

The script that performs the upload is still readable by any `read`
user — but it now contains a username and a path rather than a secret.
**The credential moves out of the script entirely**, which is not
something the bearer-token design can offer.

Worth noting alongside this: the built-in `read` group carries the
`sensitive` policy by default, so a `read` user on a stock router can
already see WireGuard private keys and IPsec PSKs. That is an argument
for keeping secrets out of script bodies, not a reason to relax about it.

Operational detail worth documenting: RouterOS wants **PEM** format.
An OpenSSH-format private key (ssh-keygen's default since 7.8) is
rejected with `unable to load key file (wrong format or bad passphrase)`.
`ssh-keygen -m PEM` imports cleanly.

*The stored-secret framing separates nothing either way.* A bearer token
is also a long-lived reusable secret on the router, and finding 5 below
shows any `read` user can print either one out of the script source.
#186's objection was to mikroview holding *RouterOS* credentials, which
neither design does.

**It does not verify the server's host key.** This is what still
separates them, though with key auth it costs less than it would with a
password. The SFTP server's host key was replaced entirely — a different
key, a different fingerprint — and the router uploaded to it anyway,
silently, with no warning and no failure:

```
256 SHA256:yt7fhRNUgSpVMoWL6uq1AQg1R/hjA9NWw8scdtx6FVo (before)
256 SHA256:D1sta/wr+XyoZMIa0NlbtFHhGDtdh7xWHAekTdyb6YY (after)
-rw-r--r-- 1 mvingest mvingest 300011 /drop/after-keychange.json
```

Anyone in the network path can therefore impersonate mikroview and
receive whatever the router uploads. With key auth they cannot walk away
with a credential, but they do get the payload, and they can return
whatever they like. There is no host-key pinning to turn on.

The HTTPS path has a real answer to exactly this: after importing
mikroview's CA, `check-certificate=yes` authenticates the server, so a
MITM cannot impersonate mikroview at all. That is why #186 refuses to
document `check-certificate=no`. SFTP has no `check-certificate=yes`
equivalent — the difference is now about payload interception rather than
credential theft, but the asymmetry is real.

**A failed upload leaves a truncated file behind.** Observed, not
theorised: one run reported `failure: connection timeout` and left
exactly 65,536 bytes of a 300,011-byte file on the server, with a
different md5 to the original. Nothing on the server side distinguishes
that from a complete small payload. A file-drop ingest therefore needs
its own completeness protocol — upload-then-rename, a sidecar checksum,
or similar — where an HTTP request arrives with a `Content-Length` and
either completes or does not.

**And a file drop bypasses machinery that already exists.** Step 3 wants
a per-token rate limit, an `authzMatrix` row, and an audit entry per
ingest. Those are properties of a request arriving at an HTTP handler. A
file landing in a directory has none of them, and rebuilding them around
a filesystem watcher — plus partial writes, ordering, quarantine and
cleanup — is a second ingest path with its own failure modes, next to one
that already works.

### The payload is the asset, and that is the deciding axis

The comparison below spent most of its length on credentials. But the
payload is reconnaissance-grade config data in its own right — step 5 of
#186 says so — so what happens to it *in transit* matters more than what
happens to the credential.

All three transports encrypt. They differ in **which end gets
authenticated**, and that is measured, not assumed:

| Transport | Encrypted | Server authenticated | Client authenticated |
|---|---|---|---|
| HTTPS + bearer token | TLS | **Yes** — `check-certificate=yes` once the CA is imported | Yes — bearer token |
| SFTP + SSH key | SSH | **No** — no host-key verification, nothing to enable | Yes — SSH public key |
| Syslog over TLS | TLS | **Yes** — `check-certificate=yes`, and it genuinely refuses an untrusted CA | **No** — no client-certificate option exists |

**Syslog can do TLS, which this document did not know when it called that
route unauthenticated.** `remote-protocol=tls` is accepted and brings a
`check-certificate` setting with it. With verification on and the CA
untrusted, the router refuses:

```
sink: [SSL: TLSV1_ALERT_UNKNOWN_CA] tlsv1 alert unknown ca
```

After importing the CA and reconnecting, the message arrives over TLS:

```
{"event": "tls-established", "peer": "192.168.11.30"}
{"event": "msg", "len": 37, "data": "script,info mv-tls-RETRY-after-import"}
```

But the action's full property list is `check-certificate, remote,
remote-log-format, remote-port, remote-protocol, src-address, target,
vrf` — **no client certificate**. So mikroview could verify nothing about
who sent the data, and anything able to reach the port could inject. It
would also need a TLS syslog listener (RFC 5425), which it does not have.

**This is what settles SFTP.** Its weakness was framed above as "a MITM
gets the payload but cannot steal a credential", and then the credential
discussion buried it. With the payload as the asset, that *is* the
finding: SFTP is the only one of the three where an active attacker on
the path can transparently receive reconnaissance-grade config, and
there is no setting to prevent it.

**HTTPS is the only option that authenticates both ends.**

### And what the credential is worth

This document spent most of its length comparing how well each transport
protects its credential, which over-weights that axis considerably.

**To reach either credential, an attacker must already hold the router.**
At that point they have the source data — mikroview holds what the router
told it. Reading mikroview gains them nothing they do not already have.
What the credential buys is the ability to *write* router state into
mikroview, and #186 says so in its own words: "the residual threat is a
stolen ingest token letting an attacker POST false router state — a
data-integrity attack, bounded, on infrastructure we control."

That is push working as designed. The pull design that #110 dropped would
have put a *RouterOS* password in mikroview, which is a high-value
credential; the push credential is low-value by construction. It is the
whole reason the design was chosen.

Two things keep it bounded, and both are transport-independent:

- **Per-device scoping** (step 1) stops one compromised router's
  credential becoming a window into another. Worth keeping precisely
  because mikroview aggregates across routers and holds syslog history,
  flags and audit that no single router has.
- **Additive-only application** (step 4) means the credential cannot be
  used to *hide* anything, only to add noise.

So credential exposure is the least decisive axis here, not the most.
What actually deserves the attention is the parser: that endpoint takes
attacker-shaped input either way, which is what step 2's strict schema
and fuzz target are for.

**Where that leaves it — honestly, and not where this document started.**

SFTP with key auth buys three things: an uncapped payload, a credential
that no `read` user on the router can extract, and a credential a MITM
cannot harvest. The first is not currently needed at measured config
sizes. The second and third are real, and this document initially argued
the opposite — but per the section above, they are advantages on the axis
that matters least, since an attacker holding either credential already
holds the router.

It costs: no server authentication (a MITM still receives the payload and
answers for mikroview, with no pinning available); a completeness
protocol, because failed uploads leave truncated files; an SSH/SFTP
listener mikroview does not currently have; and rebuilding the per-token
rate limit, `authzMatrix` row and audit entry — which step 3 requires —
around a filesystem watcher rather than getting them from an HTTP handler.

HTTPS + bearer token costs a script-readable secret and a 64KiB ceiling,
and buys real server authentication plus the existing authz/audit/rate-
limit machinery on a listener that already exists.

**With the payload weighed as the asset, HTTPS wins on the axis that
matters** — it is the only transport that authenticates both ends, so it
is the only one where an active attacker on the path cannot read the
config data. SFTP's advantages are real but sit on the credential axis,
which an attacker reaches only by already holding the router.

The remaining case for another transport is payload size, and at measured
config sizes the 64KiB cap does not bind. If it ever does, the cheaper move
before revisiting transports is **several small self-contained documents
rather than one large one** — order-independent, a dropped one degrades
rather than corrupts, and the endpoint stays stateless. Chunking one
document across POSTs is the last resort: it needs reassembly state and
makes step 4's additive-only invariant much harder to reason about.

## 2d. The other transports RouterOS scripting offers

This spike anchored on `/tool fetch` because #186 named it, and spent its
effort on that one primitive's limits rather than asking what else can
push. The survey it should have started with:

**Remote syslog (`/system logging action target=remote`).** The most
interesting option, because **mikroview already listens on it** — no new
endpoint, no new credential, no new listener. `:log info "…"` from a
script reaches a remote collector, confirmed against a UDP sink.

Two findings decide how usable it is. RouterOS **fragments long messages
automatically**, at about 1,024 characters of payload — a 65KiB `:log`
arrived as ~64 datagrams of 1,037 bytes. And **the continuations carry no
sequence marker**: every fragment looks like `script,info <text>`, so
nothing distinguishes fragment 40 from a new message. Over UDP, with no
ordering guarantee either, reassembly is guesswork.

That is survivable — a script can emit its own chunks under the fragment
threshold with its own `id/seq` framing, which puts reassembly under our
control rather than RouterOS's. The harder problem is that **syslog is
unauthenticated**, and RouterOS scripting exposes no HMAC primitive to
sign chunks with. Config data arriving over syslog would be exactly as
trustworthy as anything else that can reach that port, which is a step
down from a scoped, revocable, hashed token — the thing #186 chose push
to gain.

**`/tool e-mail`** exists with `tls` and, notably, a
`certificate-verification` setting — so unlike SFTP it can authenticate
the server. It sends attachments, which sidesteps the `http-data` cap.
Not characterised further here.

**`/system ssh-exec`** exists and uses the same `/user/ssh-keys/private`
store that made key-authenticated SFTP work.

**MQTT is not available.** CHR 7.23.3 ships the `routeros` package only;
`/iot` and its MQTT publisher are not present, so anything built on it
would require operators to install an extra package.

**Reproducer gap, stated plainly:** the key-authenticated SFTP result and
this survey were run by hand and are not yet in
`scripts/live-routeros-step0.sh`. Everything in sections 1, 3, 4, 5 and 6
is. Closing that gap is outstanding work, not a completed claim.

**Update, 2026-08-08: the production TLS syslog listener from #188 was
verified end-to-end against a real router**, not just the manual probe
above that established the transport was *possible*. With
`internal/syslog.ListenTLS` running (via `MIKROVIEW_LISTEN_SYSLOG_TLS`,
started because `tls.enabled` is true), a booted CHR 7.23.3 was pointed
at it:

```
/system logging action set remote remote=192.168.11.30 remote-port=16803 remote-protocol=tls check-certificate=yes
/system logging add topics=info action=remote
:log info "mv-tls-live-check-1786178557"
```

after importing mikroview's CA the same way the HTTPS path above does
(`/tool fetch .../ca.crt check-certificate=no`, then
`/certificate import`). Both the rule-creation system log line and the
marker message arrived intact through `GET /api/events`:

```json
{"raw": "system,info log rule added by console:admin+ct200w@ttyS0 (*5 = /system logging add action=remote topics=info)"}
{"raw": "script,info mv-tls-live-check-1786178557"}
```

`sourceIp` for both is the fixture's own host address, not the router's
-- an artifact of QEMU's user-mode networking (SLIRP) NATing the guest's
outbound connections through the host, the same quirk the HTTPS checks
above are subject to, not something specific to syslog. This is a
fixture-networking property, not a finding about RouterOS.

## 3. `:serialize to=json` is native from 7.13, and absent before it

7.12 answers `bad command name serialize`; 7.13 does not. **RouterOS 7.13
is the minimum version,** and it is `:serialize` that sets it —
`/tool fetch` with `http-method`, `http-data` and `http-header-field`
works on both.

From 7.13 it is correct and useful: it escapes embedded quotes, handles
nested maps and lists, and serialises `print as-value` output directly,
so no hand-rolled string concatenation is needed. Keys come out in
alphabetical order, not insertion order.

## 4. A scheduler needs `read,test` and stores no credential

An entry with `policy=read,test` fires unattended on its interval and
POSTs each time. `owner="admin"` is recorded; nothing resembling a
password is stored. No `write`, no `policy`, no `sensitive` — so the
docs' promise that WireGuard keys, IPsec PSKs and PPP secrets stay masked
holds.

## 5. Any user with `read` can read the ingest token

A user in the built-in `read` group prints the whole script source,
bearer token included. #186 guessed this was "probably acceptable given
its narrow scope"; it is now confirmed rather than assumed.

**Consequence:** one token per device is not a nicety. The blast radius
of one router's token has to stop at that router, because every
read-capable account on that router can read it.

## 6. RouterOS accepts mikroview's generated CA, and this is the failure text

Before the import:

```
failure: SSL: ssl: no trusted CA certificate found (6)
```

That is the string an operator will actually see, and it is what the step
6 docs should quote rather than a guess at it. After fetching `/ca.crt`
and `/certificate import`, `check-certificate=yes` reports `finished`.
RouterOS takes the ECDSA P-256 CA without complaint: `key-type=ec
key-size=prime256v1`, `trusted=yes`, `trust-store=all`.

The bootstrap needs exactly one `check-certificate=no` fetch — the CA
download itself, which has nothing to verify against yet. That is the
honest shape of the exception and the only place it belongs.

## 7. Incidentals worth knowing

- `/system license get level` returns `free` on a booted CHR with no key
  applied, confirming the free tier first-hand.
- CHR ships with a **blank admin password**, and 7.23.3 offers
  `Change your password (Ctrl-C to skip)` on first login. A factory CHR
  on a reachable network is wide open.
- The `User-Agent` is `RouterOS 7.23.3` on 7.23.3 but `MikroTik 7` on
  7.12 and 7.13. Nothing should key off it.
- Without an explicit `Content-Type`, `/tool fetch` sends
  `application/x-www-form-urlencoded` regardless of what the body is.
- A POST to `/api/ingest/routeros` currently returns **503**, not 404 —
  worth knowing before step 3 decides what a wrong-path push looks like.

## Full transcript

Captured from `scripts/live-routeros-step0.sh` against CHR 7.23.3.

```
# RouterOS Step 0 transcript

Router: CHR 7.23.3, QEMU. mikroview: https://192.168.11.30:19801. Sink: http://192.168.11.30:19899


### 0. Version and licence tier

== :put [/system resource get version]
:put [/system resource get version]
7.23.3 (stable)
[admin@CHR] >
== :put [/system license get level]
:put [/system license get level]
free
[admin@CHR] >


### 1. Authenticated POST with a JSON body

== /tool fetch url="http://192.168.11.30:19899/q1" http-method=post http-data="{\"hello\":\"router\"}" http-header-field="Content-Type: application/json,Authorization: Bearer spike-token" output=user as-value
/tool fetch url="http://192.168.11.30:19899/q1" http-method=post http-data="{\"hello\":\"router\"}" http-header-field="Content-Type: application/json,Authorization: Bearer spike-token" o
utput=user as-value
[admin@CHR] >
received at /q1:
  User-Agent: RouterOS 7.23.3
  Content-Type: application/json
  Content-Length: 18
  Authorization: Bearer spike-token
  Accept-Encoding: deflate, gzip
  body (18 bytes): {"hello":"router"}


### 2. Payload size limit

--- 65430 bytes
== :local d ""; :for i from=1 to=6543 do={:set d ($d . "0123456789")}; /tool fetch url="http://192.168.11.30:19899/q2-65430" http-method=post http-data=$d output=user as-value
:local d ""; :for i from=1 to=6543 do={:set d ($d . "0123456789")}; /tool fetch url="http://192.168.11.30:19899/q2-65430" http-method=post http-data=$d output=user as-value
[admin@CHR] >
received at /q2-65430:
  User-Agent: RouterOS 7.23.3
  Content-Type: application/x-www-form-urlencoded
  Content-Length: 65430
  Accept-Encoding: deflate, gzip
  body (65430 bytes): 01234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789
--- 65440 bytes
:local d ""; :for i from=1 to=6544 do={:set d ($d . "0123456789")}; /tool fetch url="http://192.168.11.30:19899/q2-65440" http-method=post http-data=$d output=user as-value
failure: maximum message size exceeded
[admin@CHR] >


### 2b. Whether a file on the router routes around the size limit

  downloaded: 292KiB  
       total: 292KiB  
    duration: 0s      
[admin@CHR] >
== /file print where name~"big"
/file print where name~"big"
Columns: NAME, TYPE, SIZE, LAST-MODIFIED
# NAME      TYPE        SIZE      LAST-MODIFIED      
0 big.json  .json file  293.0KiB  2026-08-07 20:38:33
[admin@CHR] >
/tool fetch url="http://192.168.11.30:19899/q2b" upload=yes src-path=big.json http-method=post as-value
failure: only [s]ftp modes support upload
[admin@CHR] >
/tool fetch url="sftp://192.168.11.30:19822/drop/from-router.json" user=mvingest password=ingest-pass-123 src-path=big.json upload=yes as-value
failure: connection timeout
[admin@CHR] >
uploaded, server side:
-rw-r--r--    1 mvingest mvingest     65536 Aug  7 20:38 /drop/from-router.json
42dc39b2946382b07b902460653ffa83  /drop/from-router.json
--- is there a key-auth parameter? (an sftp password is a reusable secret)
expected end of command (line 1 column 46)
[admin@CHR] >
--- does it verify the server's host key? changing it should break the transfer
ssh-keygen: generating new host keys: RSA ECDSA ED25519 
256 SHA256:OcY018LRr3NXiEN9fm1nNGMpJYsPan1mMDIUQvf35KY root@8e8114672855 (ED25519)
== /tool fetch url="sftp://192.168.11.30:19822/drop/after-keychange.json" user=mvingest password=ingest-pass-123 src-path=big.json upload=yes as-value
/tool fetch url="sftp://192.168.11.30:19822/drop/after-keychange.json" user=mvingest password=ingest-pass-123 src-path=big.json upload=yes as-value
[admin@CHR] >
landed despite the server identity changing?
-rw-r--r--    1 mvingest mvingest    300011 Aug  7 20:39 /drop/after-keychange.json
65440
failure: maximum message size exceeded
[admin@CHR] >


### 2c. What the budget buys, in records

== :put ([:len [/ip/firewall/filter find]] . " rules serialise to " . [:len [:serialize to=json value=[/ip/firewall/filter print as-value]]] . " bytes")
:put ([:len [/ip/firewall/filter find]] . " rules serialise to " . [:len [:serialize to=json value=[/ip/firewall/filter print as-value]]] . " bytes")
60 rules serialise to 7828 bytes
[admin@CHR] >


### 3. Scheduler policy set, and whether it stores a credential

== /system scheduler print detail where name=mv-ingest
/system scheduler print detail where name=mv-ingest
Flags: X - DISABLED 
 0   name="mv-ingest" start-date=2026-08-07 start-time=20:39:38 interval=30s on-event=/system script run mv-ingest owner="admin" policy=read,test run-count=2 next-run=2026-08-07 20:41:08 
[admin@CHR] >
received at /q3:
  User-Agent: RouterOS 7.23.3
  Content-Type: application/json
  Content-Length: 289
  Authorization: Bearer sched-token
  Accept-Encoding: deflate, gzip
  body (289 bytes): {"ifs":[{".id":"*2","actual-mtu":1500,"disabled":false,"mac-address":"52:54:00:12:34:56","name":"ether1","running":true,"type":"ether"},{".id":"*1","actual-mtu":65536,"disabled":false,"mac-address":"0
received at /q3:
  User-Agent: RouterOS 7.23.3
  Content-Type: application/json
  Content-Length: 289
  Authorization: Bearer sched-token
  Accept-Encoding: deflate, gzip
  body (289 bytes): {"ifs":[{".id":"*2","actual-mtu":1500,"disabled":false,"mac-address":"52:54:00:12:34:56","name":"ether1","running":true,"type":"ether"},{".id":"*1","actual-mtu":65536,"disabled":false,"mac-address":"0


### 4. JSON assembly

== :put [:serialize to=json value={a=1;b="two"}]
:put [:serialize to=json value={a=1;b="two"}]
{"a":1,"b":"two"}
[admin@CHR] >
== :put [:serialize to=json value={s="quote\"inside"}]
:put [:serialize to=json value={s="quote\"inside"}]
{"s":"quote\"inside"}
[admin@CHR] >
== :put [:serialize to=json value={outer={inner={deep=1}};list={1;2;3}}]
:put [:serialize to=json value={outer={inner={deep=1}};list={1;2;3}}]
{"list":[1,2,3],"outer":{"inner":{"deep":1}}}
[admin@CHR] >
== :put [:serialize to=json value=[/ip/address print as-value]]
:put [:serialize to=json value=[/ip/address print as-value]]
[{".id":"*1","address":"10.0.2.15/24","disabled":false,"dynamic":true,"interface":"ether1","invalid":false,"network":"10.0.2.0","slave":false,"vrf":"main"}]
[admin@CHR] >


### 5. Whether a read-only user can read the script source

== :put [/system script get [find name=mv-ingest] source]
:put [/system script get [find name=mv-ingest] source]
:local p [:serialize to=json value={ver=[/system resource get version];ifs=[/interface print as-value]}]; /tool fetch url="http://192.168.11.30:19899/q3" http-method=post http-data=$p http-header-field="Content-Type: application/json,Authorization: Bearer sched-token" output=none
[mv-read@CHR] >


### 6. TLS against mikroview's generated CA

--- with the CA removed, which is the state an operator starts in
:put ([/tool fetch url="https://192.168.11.30:19801/api/healthz" check-certificate=yes output=user as-value]->"status")
failure: SSL: ssl: no trusted CA certificate found (6)
[admin@CHR] >
--- after fetching and importing /ca.crt
       decryption-failures: 0
  keys-with-no-certificate: 0

[admin@CHR] >
== /certificate print detail
/certificate print detail
Flags: K - PRIVATE-KEY; L - CRL; C - SMART-CARD-KEY; A - AUTHORITY; I - ISSUED, R - REVOKED; E - EXPIRED; T - TRUSTED; a - ACME-MANAGED; D - DYNAMIC 
 0       T   name="mikroview-ca.crt_0" trust-store=all digest-algorithm=sha256 trusted=yes common-name="mikroview local CA" organization="mikroview" subject-alt-name="" 
             issuer=O=mikroview,CN=mikroview local CA key-type=ec key-size=prime256v1 key-usage=digital-signature,key-cert-sign,crl-sign days-valid=3650 invalid-before=2026-08-07 19:37:39 
             invalid-after=2036-08-04 20:37:39 serial-number="3584f1ed917127e1488180f0248e1045" akid="" skid=4B3865816845B1D032754FB7545F44A3855E6B07 
             fingerprint="a0a7875756fd459330e0ba81642e10587442c71b71f827df0646133fcb9efd98" expires-after=521w2d23h56m20s 
[admin@CHR] >
:put ([/tool fetch url="https://192.168.11.30:19801/api/healthz" check-certificate=yes output=user as-value]->"status")
finished
[admin@CHR] >
```
