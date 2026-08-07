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
bytes — roughly 130 bytes each on a bare CHR, so order 500 rules fits,
and rules carrying more fields fit proportionally fewer. Comfortable for
config shape; not comfortable for anything list-like, where an address
list or a large DHCP lease table can pass it without much trouble.

## 2. A file on the router does not route around it

The obvious escape is to write the payload to a file and upload that.
The router holds the file happily — a 293KiB file downloaded and stored
without complaint, on a disk with ~69MiB free. But:

```
/tool fetch url="http://.../q2b" upload=yes src-path=big.json http-method=post
failure: only [s]ftp modes support upload
```

`/tool fetch` cannot upload over HTTP or HTTPS at all. `upload=yes` is
FTP and SFTP only, and `http-data` is the sole body source, so the
ceiling stands.

**Why we are not taking the FTP route.** It would mean mikroview running
an inbound FTP or SFTP listener with credentials the router stores —
reintroducing exactly the long-lived stored secret that #186 chose push
to avoid, and adding a file-transfer daemon to the attack surface, to
raise a limit that the config shape does not currently hit.

**The two ways past it, if we ever need them,** in preference order:

1. **Several small documents, not one large one** — interfaces,
   addresses, firewall rules, leases, each self-contained and each under
   the cap. Order does not matter, a dropped one degrades rather than
   corrupts, and the endpoint stays stateless.
2. **Chunking one document across POSTs** — needs reassembly state,
   ordering and partial-payload handling on the mikroview side, which is
   a materially larger attack surface and makes the additive-only
   invariant in step 4 much harder to reason about. Avoid unless (1) is
   genuinely insufficient.

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
0 big.json  .json file  293.0KiB  2026-08-07 20:24:28
[admin@CHR] >
/tool fetch url="http://192.168.11.30:19899/q2b" upload=yes src-path=big.json http-method=post as-value
failure: only [s]ftp modes support upload
[admin@CHR] >
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
 0   name="mv-ingest" start-date=2026-08-07 start-time=20:25:03 interval=30s on-event=/system script run mv-ingest owner="admin" policy=read,test run-count=2 next-run=2026-08-07 20:26:33 
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
             issuer=O=mikroview,CN=mikroview local CA key-type=ec key-size=prime256v1 key-usage=digital-signature,key-cert-sign,crl-sign days-valid=3650 invalid-before=2026-08-07 19:23:34 
             invalid-after=2036-08-04 20:23:34 serial-number="5fe3dd77c26782e3581f235e483e3865" akid="" skid=5F47F5A6CCA03C0D8AD71AAC1F6DEB7A417D4B1D 
             fingerprint="1c238dc1b03c3e481618985df941385d1f233f9c81a27f0a6365885dee324720" expires-after=521w2d23h56m49s 
[admin@CHR] >
:put ([/tool fetch url="https://192.168.11.30:19801/api/healthz" check-certificate=yes output=user as-value]->"status")
finished
[admin@CHR] >
```
