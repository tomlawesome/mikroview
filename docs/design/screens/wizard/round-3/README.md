# Setup wizard, the built component — round 3 (#1374)

2026-09-26, based on batch branch `763e1dee` (feature/m25-routers-signin,
already carrying the #1364/#1365/#1369/#1370/#1372 fixes).

Round 1 and round 2 (this same directory tree) were mockups — hand-drawn
HTML standing in for the wizard before it was built. This round is not a
mockup: every screenshot below is `frontend/src/components/SetupWizard.svelte`
itself, built with the project's normal `npm ci && npm run build` and
rendered in real Chromium, driven against a small fake HTTP server
(`fake-api.mjs`) rather than a running `mikroview` binary. The point is to
see the component as built, in every state its own status logic can
produce, before the owner reviews it.

**Dark only.** The app has had no light theme since #708 retired it
("Dark is the only theme") — `app.css` hardcodes `color-scheme: dark` and
nothing in the frontend responds to `prefers-color-scheme`. Round 1/2's
mockups could still draw a light variant of their own CSS; the real
component cannot, so every shot here is `*-dark.png` and there is no
light counterpart to link.

No verdicts recorded yet — the owner has not seen this round.

## The story

One fictional deployment throughout: the instance answers on
`192.168.13.15:8080`, the router is named `rb5009`, declared in
config.yaml at `sourceIp: 192.168.13.1`. Where a source-address split is
shown, the undeclared address arriving instead is `192.168.254.1`.

## How this was built

`fake-api.mjs` is a plain `node:http` server, no new dependency: it serves
`frontend/dist` (built the normal way) and answers the handful of
`/api/...` routes the app shell's boot sequence and the wizard actually
call — `/api/auth/session` (a signed-in admin, TOTP already enrolled, no
forced password change, so the app opens straight to the deck),
`/api/setup/status`, `/api/setup/commands`, `/api/devices` and its
`enrolment`/`registration` sub-routes, `/api/router-backups`,
`/api/tokens`, and a handful more. Everything else answers a safe empty
body (`[]` or `{}`), never a 404 or 500, since the deck's own scenes
render underneath the wizard modal and must never crash it. It also
answers `/api/ws` with a bare WebSocket handshake and then goes silent,
so the live-tail socket's reconnect loop never spins for the whole
capture run — the wizard itself never reads that socket at all.

A scenario is chosen by visiting `/scenario/<name>` first, which sets a
cookie and resets that scenario's state fresh; `capture.mjs`
(Playwright, already a frontend dependency) then drives the real UI —
opening the wizard from the account menu's "Run setup…" row, or, for the
one state that needs it, from Settings ▸ router backups' own "is it
gone?" link — and screenshots the wizard dialog (`.modal.setup-wizard`,
`role="dialog"`) at 1600×1000, device scale 2.

Some states are one static fixture (a `SetupStatus`/`devices`/
`router-backups` snapshot chosen to read as blocked/waiting/partial/
done). Others need a real interaction the fixture can't fake in one
response — naming a router, minting an enrolment token, clearing the
address field, registering — so `capture.mjs` actually clicks through
those, the same as an operator would; see the scenario table below for
which is which.

## Scenarios

| Scenario | Step / state | What the fixture (or the script) does |
|---|---|---|
| `fresh-install` | ca waiting, name quiet, rules waiting, push/backup no-token, register error, finish (nothing done) | Nothing set anywhere: no address answered, no devices, no marks. Auto-launch opens the wizard on pane 1 by itself (the record's own rule: a fresh admin session with no router and no marks opens it unasked). |
| `ca-blocked` | ca blocked | Address already saved as the story's `192.168.13.15:8080`, but `tls.hosts` still reads `["localhost","127.0.0.1"]` — the certificate doesn't cover it. |
| `source-split` | syslog partial (source-address split) | rb5009 declared at `192.168.13.1`, configured, silent; `192.168.254.1` streams instead, undeclared (#442). |
| `rules-partial-undecoded` | rules partial | 15234 events, 0 decoded — rules log without the letter convention at all. |
| `rules-partial-some` | rules partial | 15234 events, 6031 decoded — some rules tagged, some not. |
| `mostly-done` | ca/syslog/rules/push/backup done; name/register stay quiet; finish (mostly done) | A full "day two" story: rb5009 enrolled, pushing, backed up (2 generations), registered — all as server-side fact. Opened plainly from the account menu, so `ledgerDevice` is empty this walk; see the note below on why name/register still read quiet here. |
| `backup-blocked` | backup blocked | `router-backups.enabled: false` — no retention key mounted, so the step mints one in-browser rather than describing one twice (#1133). |
| `lost-router` | backup, lost router | Same as `mostly-done`, but rb5009's backup row carries `missed: 6`. Reached only through Settings ▸ router backups' "is it gone?" link (EngineRoom.svelte/RouterBackups.svelte) — this is the one door into that shape; Run setup… never offers it. |
| `walkthrough` | name done; syslog waiting → token fresh; push with-token → no-address; backup with-token; register done | One continuous session: name "rb5009" (creates it live), mint its enrolment token for `192.168.13.1`, let push/backup auto-mint their shared ingest token once a router is known, clear and restore the address field for the no-address shot, then register. |
| `walkthrough-expired-token` | syslog, token expired | Same walk, but the fixture's mint endpoint always returns an `expiresAt` already in the past, so "Reroll to mint another" shows on the very next render. |
| `walkthrough-refused` | syslog, refused senders | Same walk; the fixture's `/api/devices/refused` always reports `192.168.254.1` refused since the mint. |

## Re-running it

```
cd frontend
node ../docs/design/screens/wizard/round-3/capture.mjs
```

Builds nothing itself — run `npm ci && npm run build` first if
`frontend/dist` is stale. It starts `fake-api.mjs` on `127.0.0.1:8791`,
drives all eleven scenarios in one pass (~30 shots into `shots/`), and
kills the server on exit.

## Could not reach, or deliberately not faked

- **Backup's two "blocked on key" sentences never both show.** The
  ledger's own generic "No key file is mounted…" body appears whenever
  `router-backups.enabled` is false — which is the *only* real-server
  condition under which `POST /api/setup/commands` would ever set
  `no-retention-key`/`retention-key-unreadable` on the backup block's
  `blocked` list. But that granular list is only ever rendered in a
  *different* body (the no-script list), which the component only shows
  once the ledger's own `blocked` state has already been left behind —
  so a real server can never actually put those two sentences on
  screen. Reaching them would need feeding `/api/router-backups` and
  `/api/setup/commands` two answers a real server would never disagree
  on at once; that felt like faking a state the app cannot really be
  in, so `backup-blocked-dark.png` shows the one sentence a real
  deployment can actually reach instead.
- **Name/register reading quiet beside an obviously-existing, fully-set-up
  router** (`name-quiet-existing-router`, `register-quiet-existing-router`
  in `mostly-done`) is not a gap — it's real, documented behaviour worth
  the owner's attention regardless: both steps read `quiet` unless
  *this walk* named or registered a router (`wizardState.ledgerDevice`
  is a client-only, per-session field, never restored from the fleet).
  Opening the wizard plainly from the account menu, with a router that
  demonstrably already exists, still shows "nothing to wait for" on
  both. `docs/design/screens/wizard/DESIGN.md`/`setupsteps.ts`'s own
  comments say this is intentional (naming/registering are the
  operator's own act, not a fact read back), but it is easy to mistake
  for the wizard "forgetting" a router mid-review.
- **No light theme to capture** — see above; not a gap, a fact about the
  app since #708.

## Serving it

```
docker run -d --name mikroview-design-host -p 8307:80 \
  -v /home/codex/projects/.worktrees/mikroview/1374-round-3/docs/design:/usr/share/nginx/html:ro \
  nginx:alpine
```

<http://192.168.11.30:8307/screens/wizard/round-3/index.html>
