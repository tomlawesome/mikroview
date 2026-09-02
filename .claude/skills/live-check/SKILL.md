---
name: live-check
description: Stand up a real mikroview and drive it in a real browser. Runs continuously on dev after merges, and once clean before dev -> preview; on a branch by choice, not by rule.
---

# Live check

`make live-check` builds the real binary with the real embedded UI, starts
it with real syslog listeners and a real admin account, feeds synthetic
firewall events, and drives it in Chromium via Playwright.

Not the test suite. Run it in addition, not instead.

**It is not a step before a merge.** Since 2026-09-02 a PR merges on green
CI plus review; the gate runs on `dev` in a loop on the second host, and a
new failure against the previous run is filed against that merge window and
fixed forward. It blocks only `dev -> preview`. AGENTS.md, "The gate runs
on `dev`, after the merge", has the rule and the reasoning.

**It does not have to run here.** `make live-check-remote` runs the same gate
on the second host, so this machine stays free for the 35-50 minutes it takes.
Same suite, and `MV_BROWSER=firefox` or `webkit` drives an engine the local
run never does -- which is how #659 shipped a static style attribute Chromium
tolerates and Firefox refuses. Peer sessions share this workstation, so prefer
the remote form when someone else may want the machine.

## Why this exists

Nearly every defect worth finding in this project was found by running it.
None of these were visible from the code or from `go test`:

- Recovery keys reaching the container log — the guard checked
  `term.IsTerminal(stdout)`, which a `docker run -t` pty satisfies while
  the log driver still writes every byte to disk. Keys were recovered
  from the on-disk log and used to take over the admin account.
- `-transfer-admin` and `-recover-admin-account` writing JSON files
  nothing read on a Postgres deployment, reporting success while leaving
  the operator locked out.
- A rule-regex filter that became unevaluable only once matching events
  arrived, leaving a stale match set and quietly hiding them.

## Running it

```sh
make live-check
```

Or drive the environment by hand:

```sh
eval "$(scripts/live-env.sh up)"     # exports MV_URL, MV_USER, MV_PASS, MV_DIR
scripts/live-env.sh syslog 200 my-rule
cd frontend && node scripts/live-smoke.mjs
scripts/live-env.sh down
```

### Which browser it drives

`MV_BROWSER` selects the engine: `chromium` (the default), `firefox` or
`webkit`. It applies to the whole suite, including the handful of
scenarios that open a second browser for a second signed-in session.

```sh
MV_BROWSER=firefox make live-check
```

Run it under Firefox before believing a UI change is safe. The gate has
only ever driven Chromium, and that is how #659 shipped: a static
`style="..."` attribute Chromium tolerates and Firefox refuses under this
app's CSP passed live-check, vitest and every screenshot, and was found
by the owner opening the app. An unrecognised value refuses to start
rather than falling back, because a run that reports PASS believing it
exercised Firefox is worse than no run. Locally the engine must be
installed (`npx playwright install firefox`); the CI image carries all
three.

## Reading a run

Do not judge a run by counting `RESULT: PASS` against `RESULT: FAIL`. The
RESULT line is printed by the scenario itself, so one that *throws* --
a stale selector, an import error -- dies before printing anything, and
the log shows a header, some passing checks, a stack trace, and no
verdict. Counting verdicts cannot see it. That is #661, and it was read
as a clean browser phase across two full runs.

The check that cannot lie about it:

```
grep -c '^== ' run.log                      # scenarios started
grep -cE '^RESULT: |^PASS: ' run.log        # scenarios that reported
```

Equal means every scenario reported. A shortfall is scenarios that died
silently, and the difference is how many. `run-scenarios.sh` now also
writes a `RESULT: FAIL (... without printing a result)` line for those,
but the count comparison is what to reach for on any log, including older
ones and other people's.

The opposite error is as bad: an earlier version of that fix printed a
synthesised failure on *every* non-zero exit, duplicating the verdict a
scenario had already written for itself and denying it had written one.
Over-reporting and under-reporting are the same defect -- a log that is
not a faithful record of what happened.

To tell a regression from a pre-existing failure, run the whole thing on
both trees, a worktree at `origin/dev` and the branch, and take the set
difference. Failures present on both are not yours. Never run the one
scenario twice instead: scenarios share an instance and run in filename
order, so most depend on state an earlier one left.

## Running two live checks at once

Safe by default. `MV_DIR` and every port are derived from a hash of the
checkout path, so each `git worktree` gets its own data directory and its
own port block, and repeated runs in the same tree stay on the same slot.

It was not safe until #660, and the way it failed is worth knowing. This
said "safe by default" while it was only true of the browser phase: the
standalone scripts hardcoded ports that sat inside the band `live-env.sh`
hands out, so whichever checkout hashed to slot 21 owned a port two of
them also claimed. `scripts/live-slot.sh` is now the single allocator for
both phases, and this claim is true of the whole gate.

**A new check that binds a port takes it from `live-slot.sh`.** Source it
and use one of its variables, or add a band there with a comment. Never
write a number into the script -- that is precisely how the two-allocator
bug happened, and nothing but this line stops it happening again.

### Ports are safe; the CPU is not

Two runs no longer collide, but they still share one machine, and a
loaded host breaks a specific class of assertion: **the ones waiting for
something to disappear.** An empty state, a cleared list, a count
returning to zero -- a feeder that keeps arriving refills what the
assertion is waiting to stop seeing, so it times out while the app is
behaving correctly. Assertions waiting for something to *appear* mostly
just get slower.

Observed 2026-08-31: `live-flags-clearing.mjs` failed its post-reload
check ("the cleared state survived a reload") on a host running two
gates, and passed the same check on an idle one. The re-run to establish
that cost 35 minutes.

So: one gate at a time per host, and a browser-phase failure on a shared
host is not evidence until it is reproduced alone. `live-inline-editing`
is the standing counter-example in the other direction -- it fails
intermittently on an idle host too (#611), so a single clean run does not
clear it either.

This matters because the collision used to be destructive, not noisy:
`up` runs `down` and then `rm -rf "$MV_DIR"`, so a second live check on
shared defaults killed the first one's server and deleted its data
mid-scenario. The run that got trampled failed with an unexplained
scenario timeout and nothing in its own log to account for it.

Two checkouts can still hash to the same slot. `up` therefore refuses to
start if either port is held after its own teardown, naming the ports and
telling you to override, rather than proceeding into the `rm -rf`. The
standalone scripts now refuse the same way, naming the port and the
process holding it; before #660 they said only "server never came up",
and one collision surfaced as five failures across two scripts that
mentioned no port at all. To run alongside another check deliberately,
set `MV_DIR`, `MV_HTTP_PORT`, `MV_SYSLOG_PORT` and `MV_SYSLOG_TLS_PORT` —
explicit values always win over the derived ones.

An interrupted run used to leave its instance behind for good: `up`
detaches deliberately and `down` is the only thing that stops it, so a
run killed mid-scenario held its slot's port until someone found the
process by hand. The `live-check` recipe now traps INT and TERM. If you
drive `live-env.sh up` yourself rather than through `make live-check`,
that trap is yours to set.

### The standing lanes (owner, 2026-08-30)

Use up to three lanes, one worktree each, and never share a worktree
between concurrent agents — they share one git index, so one agent's
commit sweeps another's staged files:

1. **Suite lane** — the branch worktree, running `make live-check`.
2. **Driving lane** — a detached worktree at the same commit, for
   hand-driving and screenshots while the suite runs.
3. **Baseline lane** — a worktree at `origin/dev`, for telling a
   regression from a pre-existing failure.

The server embeds the frontend at build time: rebuilding `frontend/dist`
changes nothing a running server serves. To see a frontend change,
`scripts/live-env.sh down` then `up` (it rebuilds), or you will verify
against a stale bundle without noticing.

## Driving a real router

`make live-routeros` boots MikroTik's own CHR image under QEMU, stands
mikroview up on the host's LAN address with TLS on, and imports
mikroview's generated CA into the router. Opt-in, not part of
`make live-check`, because it boots a VM and only RouterOS-facing changes
need it.

```sh
eval "$(MV_BIND=$(scripts/live-routeros.sh host-addr) scripts/live-env.sh up)"
eval "$(scripts/live-routeros.sh up)"
scripts/live-routeros.sh trust "$MV_URL"
scripts/live-routeros.sh run '/system resource print'
scripts/live-routeros.sh down && scripts/live-env.sh down
```

No root and no host packages: QEMU runs in a container, with TCG when
`/dev/kvm` is not usable. CHR reaches a login prompt in about 15s.

`make live-routeros-container` goes further: the shipped container *and*
a real CHR, with the router configured by
`docs/routeros-setup.md`'s own steps and made to log real firewall
traffic in every chain. That is what runs
`frontend/scripts/live-routeros-real.mjs`, the only scenario whose input
mikroview did not write itself.

Use it whenever a change touches what mikroview reads out of a log line
or a pushed table. Synthetic feeds and the parser agree with each other
by construction, so they cannot disagree with RouterOS -- which is how a
real router emitting `src-mac` in upper case went unnoticed until this
target existed, silently breaking every watchlist entry whose MAC had
been typed the conventional way.

```sh
make live-routeros-container
scripts/live-routeros.sh setup "$MV_URL" "$MV_BIND" "$MV_SYSLOG_TLS_PORT"
scripts/live-routeros.sh traffic 5     # real events, input/forward/output
scripts/live-routeros.sh push "$MV_URL" "$TOKEN"   # real ARP/lease/rule tables
```

## Adding a scenario for your change

One short file per change, `frontend/scripts/live-<thing>.mjs`, importing
the helpers. `scripts/run-scenarios.sh` picks it up automatically, and
every `live-*` target runs that. Add it to that script's exclusion list
only if it needs something the plain targets do not stand up (a booted
router, say).

```js
import { session, feedSyslog, check, responsive, done } from './live-browser.mjs'

const { page, consoleErrors } = await session({ waitForEvents: 100 })
check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, 'no console errors')
done()
```

`check(ok, message)` records a failure without aborting, so one run
reports everything rather than stopping at the first problem.

- Playwright derives visibility from `getBoundingClientRect()`, which for
  SVG returns the geometry box rather than the stroked one, so a plain
  vertical or horizontal `<line>` resolves as hidden however clearly it is
  drawn. Assert on geometry or a wrapping element instead.

## What it cannot cover -- check before assuming

Be suspicious of "we cannot test that here". It was claimed about the
browser (Playwright and Chromium were installed; the check only looked at
`PATH`) and about RouterOS (it virtualises, and `make live-routeros` now
boots one). Both claims were wrong, and both times the real thing found
defects.

Be equally suspicious of the correction. The note that replaced the
RouterOS claim said hardware virtualisation was available because
`/dev/kvm` is present — but it is `root:kvm` mode 0660, and this account
is not in that group, so it is present and unusable. Checking that a
thing exists is not checking that you can use it. The fixture works
anyway, on TCG, which is why the wrong reason went unnoticed for a while.

Look properly first. Where something genuinely cannot be exercised, say
so plainly in the PR rather than letting "tested" imply more than was
observed.
