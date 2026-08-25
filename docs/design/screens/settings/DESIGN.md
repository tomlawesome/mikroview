# Settings surfaces — the ratified design (#490, under #518)

Ratified by the owner 2026-08-23 across three rounds (`round-1/` to
`round-3/` beside this file carry the mockups, screenshots and the
verbatim verdict trail; the same trail is on #518). The ruling: the
**engine room in its round-2 form** — the simple sketch — is the
design; the round-3 development was killed ("sometimes simple is
better"). The round-2 mockup (`round-2/direction-ac-engineroom.html`)
is the visual reference; where this text and a mockup detail
disagree, this text wins.

## The model

Settings is **one page — The engine room** — in the rail's Admin
section: mikroview's own signal path drawn as a live vertical
diagram, with every setting on the station it governs and the two
side doors beside the path. It replaces per-noun settings pages for
exactly the surfaces that had no better home:

| In the room | As |
|---|---|
| Detectors (enable + scope) | the **watchers** station's bench |
| Users | the **who may look in** door |
| API/ingest tokens | the **which machines may speak** door |
| Listener, retention, notifications facts | stated on the **door**, **store** and **heralds** stations |

**Everything that already has a good page keeps it** — this is the
"simple" the verdict chose: Watchlist stays the Expect page;
Entities stays its own ratified page (#489); the audit log stays
under Investigate; Flags' exclusions stay reachable from Flags;
Run setup… stays the admin-only workshop; password/SSO stay each
person's own doors. The room never duplicates them — its stations
may state counts as context (the flags desk says "6 open"), never
re-implement.

## The room

- **The path, top to bottom** — the direction the app reads:
  **the door** (syslog listener: ports and TLS as facts, "set in
  config.yaml — the room shows what is, the file decides"; only
  ingest-key holders may speak) → **the store** (keeps its hours as
  the one amendable knob if/when the server exposes it; count held)
  → **the watchers** (the detector bench: checkbox to run, dashed
  scope knob, worded states — running · paused; a no-scope detector
  says so in prose) → **the flags desk** (open count; exclusions
  noted as facts) → **the heralds** (how word goes out; config.yaml
  honesty).
- **The side doors** govern entry, not flow: users (add with
  role-fixed viewer accounts, remove naming consequences, the
  single admin's console-transfer fact stated inline) and tokens
  (mint with kind and, for ingest, device; the one-time secret
  banner is the secret's whole life; Revoke the only verb on a
  circulating key).
- **Every number on the room is arrived traffic** (events/s at the
  door, events held, flags open) — the room doubles as a status
  page, and a setting is read against what it governs. The numbers
  are context for knobs, never charts.
- **Opening a station zooms, not navigates** — it unfolds in place
  with the path dimmed above and below. A scope opens as a small
  form holding only what the scope can truthfully say.
- **Honesty about reach**: stations the UI cannot amend say so in
  the config-problems banner's remediation voice; no knob is drawn
  that does not exist.
- On a phone the path simply stacks — it is already vertical — with
  the side doors beneath. No new pattern needed.

## Shared grammar (Z's build facts, inherited by verdict trail)

- A control is a fact wearing a handle: the viewer keeps the fact
  in words ("running · paused", scope as a sentence), never a
  disabled ghost; explanations of absent affordances are absent
  too; busy-disabled ("saving…") is the only disabled state and
  never means "not yours".
- **Viewer (#490):** READ-ONLY — ADMINS EDIT declared once in the
  header chip; the dashed knob ink and every verb absent; the
  machine and its live numbers identical — the room at rest.
- **API:** every settings write is 403 for non-admins server-side
  regardless of client; the viewer page exists by widening the
  settings GETs (tokens, definitions, setup status) from
  admin to signed-in user deliberately, one authz-matrix row at a
  time; token values appear in no GET — the mint response is the
  secret's whole life.
  **Amended 2026-08-24, by the owner:** the account list
  (`GET /api/auth/users`) is **not** widened and stays admin-only.
  The clause originally named it among the four. The rationale that
  had guarded it — "who holds an account, and which one is the admin
  … is the map of whose account is worth attacking" — was written
  when a non-admin could read every page, and the ratified viewer
  grammar removed that premise; the owner's ruling is that it stays
  closed regardless. The consequence for the room is that the
  **people door is absent for a viewer**, not read-only, per the
  absent-never-disabled grammar above. The machines door remains
  viewer-readable.
- **Colour is meaning**: no charts, no data palette; accent is the
  admin's amendable ink and interactive colour; amber is time (the
  seen/spoke ticks, the mint's now-or-never moment); ok green only
  as the receiving pulse; alarm only where it already lives (the
  rail's flag badge). Text wears text tokens; greyscale-safe.

## Superseded (considered and closed)

- **Round 1, Z — the reading room** (killed: "fine but boring"):
  its three translations and the API clause survive above as build
  facts; its per-noun pages do not.
- **Round 2, AB — the charter** (killed; the room won): noted
  options on record, not commitments — the amendment-margin posture
  and a printable "how is this network kept?" export.
- **Round 3, AE — the developed room** (killed: "the old version
  was better… sometimes simple is better"): its roster expansion is
  replaced by the keep-their-pages rule above; its flow details are
  replaced by the round-2 drawings plus this text.
