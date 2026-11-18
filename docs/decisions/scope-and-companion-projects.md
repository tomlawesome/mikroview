# Scope: what MikroView is for, and what belongs in a companion project

Date: 2026-10-10. Moved out of `AGENTS.md`, which keeps the test and links
here for the reasoning. The decisions it records were made earlier (the
OpenCanary outcome around #29; the licence position on 2026-09-16, #1229).

## The decision

MikroView is a **firewall-log interrogation helper**: it ingests the log
stream a MikroTik router exports, and helps someone view, filter and make
sense of it. It is not a security suite and does not grow into one.

When a security-adjacent feature is proposed, the test is where its signal
comes from:

- **Derived from MikroView's own event stream** -- belongs inside
  MikroView. The behavioural flags subsystem and its detectors qualify:
  every judgement they make is computed from traffic MikroView already
  sees.
- **Produced by a separate tool, and merely correlated alongside
  MikroView's data** -- belongs in a companion project. Default
  recommendation for anything in this category is a separate repo, not an
  integration here.

## The worked example: OpenCanary and `birdcage`

Centralising honeypot alerts went through several rounds and settled on
**no OpenCanary-specific logic in MikroView at all**: a rare honeypot hit
flushed into the live view is lost in a stream of real firewall lines, and
a multi-instance honeypot dashboard is a different product -- it became
`birdcage`. MikroView's only part is deliberately generic: a bounded
before/after lookback query by IP and timestamp (#29) that *any* external
trigger can call to pull surrounding traffic context. Not honeypot-aware,
not coupled to birdcage.

## Why this is not tidiness

`README.md` is a public advert for the app rather than a personal readme,
and an app that does one job well is a different proposition from one
accreting adjacent integrations. Absorbing a neighbouring tool also
inherits its failure modes, its dependencies and its data licensing -- see
the next two sections for why the last of those is not free.

## Why MikroView suggests, and why there is a wizard

A real deployment's accept-rule logging was ~90% of volume, burying the
deny signal; dropping it cut volume 97-99%
([docs/routeros-setup.md](../routeros-setup.md)). Fixing what an operator
logs is the product's opening argument, not onboarding decoration.

Coverage is about watching the right things in the right places, not about
accepts versus denies. An accept where there should not be one is as much
signal as a deny that keeps repeating; which matters is a property of the
boundary, not the rule's action. The volume story above is about noise, not
about which action to log -- the owner has corrected the "log denies, not
accepts" reading more than once (last 2026-09-03, designing #435).

## Why list and lookup data is fetched, never vendored

The global rule (fetch feeds at runtime, never vendor) applies; what is
project-specific is that for MikroView it is a licensing constraint before
it is a design preference.

MikroView ships under the **GNU AGPL-3.0** (see [LICENSE](../../LICENSE))
and nothing else: the commercial licence that used to sit beside it was
withdrawn on 2026-09-16 (#1229). Vendoring is still out. CC BY-SA sources
(IP2Location LITE, IPinfo's free tier, older GeoLite2) carry share-alike
and attribution terms of their own that would travel with every copy, and
the cloud providers grant no redistribution licence on their published
IP-range documents at all. Fetching at runtime is uncontroversial; shipping
a copy is not.

It also means a stale release cannot ship stale security data -- a failure
nobody would notice, because everything would appear to work.

Consequences for dependencies and feeds:

- A new third-party dependency needs the owner's approval on an issue
  before it is added, with the provenance case the global dependencies rule
  asks for (#1288).
- Before adopting any feed or dependency, check its licence and record what
  you found in the issue. Permissive (MIT/BSD/Apache/ISC) is fine. GPL-family
  code is compatible with the AGPL, but a copyleft or share-alike dependency
  still needs the explicit consideration the global dependencies rule asks
  for, written on the issue.
- Attribution terms still bind data that is fetched rather than shipped:
  Spamhaus DROP requires credit, and its date and copyright text must
  travel with the data.
