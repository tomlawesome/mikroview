# MikroView observes; it never scans or connects

Date: owner decision ratified 2026-08-15; moved out of `AGENTS.md`
2026-10-10, which keeps the invariant and the "print the command" shape and
links here for the reasoning.

## The decision

A design invariant, not a current limitation: **MikroView never probes,
scans, or initiates a connection to any host on the operator's network --
the router included.** It ingests what is pushed to it (syslog,
router-state pushes) and fetches only its own external reference feeds
(blocklists, network-class data, and their kin). The wizard's long-standing
promise -- "MikroView still never connects to your router" -- is the special
case of this general rule.

## Why

Two reasons, either sufficient alone.

- **Character and trust model.** An observer that starts probing changes
  character: it appears in other tools' logs as a scanner, and its trust
  model shifts from "a place logs go" to "a thing with reach into the
  network", which is a different product with a different security posture.
- **Honesty.** MikroView's evidence is what *arrived*, not what was
  elicited. A claim built on traffic the product itself provoked is a
  different kind of claim, and the interface has no way to mark the
  difference.

## The permitted shape

For anything identification- or diagnosis-flavoured (#410 is the worked
example): assemble the evidence already flowing, state conclusions with
their confidence, and where an active step would genuinely help, **print the
command for the operator to run** -- never run it. A feature that needs
MikroView itself to touch the network is either redesigned around pushed or
passive data, or it belongs in a different tool.
