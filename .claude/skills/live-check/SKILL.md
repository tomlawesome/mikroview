---
name: live-check
description: Stand up a real mikroview and drive it in a real browser. Use before opening any PR that touches the server, the UI, or the CLI.
---

# Live check

`make live-check` builds the real binary with the real embedded UI, starts
it with real syslog listeners and a real admin account, feeds synthetic
firewall events, and drives it in Chromium via Playwright.

Not the test suite. Run it in addition, not instead.

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

## Adding a scenario for your change

One short file per change, `frontend/scripts/live-<thing>.mjs`, importing
the helpers. `make live-check` picks it up automatically.

```js
import { session, feedSyslog, check, responsive, done } from './live-browser.mjs'

const { page, consoleErrors } = await session({ waitForEvents: 100 })
check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, 'no console errors')
done()
```

`check(ok, message)` records a failure without aborting, so one run
reports everything rather than stopping at the first problem.

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
