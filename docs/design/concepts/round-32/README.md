# Interface visioning — round 32: Settings, its two doors

Under #691 (backend capability with no front-end home), phase 1, item 2.
Round 30's Settings card carries forward **verbatim** — same stack, same
groups, same rows — and gains the two doors the app has behind flags
(`EngineRoomDoors.svelte:40-41`) and round 30 drew nowhere: **people**
(who may look in) and **keys** (which machines may speak). Both in
`settings-doors.html`. Nothing accepted is redrawn.

The rule the placements follow: **a settings group is rows, and a door
is a group.** No panel, no modal, no second page. Each door lists in the
card's own row grammar (name · chips · fact · quiet verb), adds with a
form row that appears where the next row would go, and removes with
round 28's arm-then-confirm gesture. The form is round 31's — dashed
inputs, segmented choices, pill actions — so it is the same form
wherever it is met.

## What is placed

1. **`keys`** sits directly under `ingest`, because the ingest drawing
   already names the two routers the ingest keys speak for. Rows:
   `rb5009 · ingest · speaks for rb5009 · spoke 41 min ago — the hourly
   state push`, `hap-ax2 · … · spoke 3 d ago` (matching the ingest
   drawing's *quiet 3 d*), `grafana · read-only · spoke 4 min ago`.
   An ingest chip names its router (`GET /api/tokens` → `kind`,
   `device`, `lastUsedAt`); a key that has never spoken says so.
2. **`+ mint a key`** opens the form row: name · `read-only | ingest` ·
   and for ingest, `for rb5009 | hap-ax2`. `mint it` is `POST
   /api/tokens {name, kind, device}`.
3. **The reveal** is the one moment the secret exists on screen. It
   stands in for the new key's row, accent-edged: name · kind · the
   value in a selectable code span · `copy` · `done`, with *shown once —
   mikroview keeps only its fingerprint, so copy it now*. An ingest key
   adds *the router lines, with this key already in them: copy for
   RouterOS* — the setup wizard's `pushScript` (`setupsteps.ts:127`),
   reached from here so a replacement key never sends the operator back
   through setup. `done` lets the ordinary row take its place.
4. **`revoke`** ends every key row, quiet. One click arms it red as
   `confirm — it stops speaking now`; a second click revokes (`DELETE
   /api/tokens/{id}`); any other click disarms.
5. **`people`** sits directly under `account` — your account, then
   everyone else's. Rows: `tom · admin · this is you · signed in 4 d ago
   · console-only`, `anna · signed in 2 h ago · remove`, `mia · can only
   look · sso · signed in 12 d ago · remove`. Only the read-only tier is
   chipped (#653's rule: *can change things* is the ordinary case and
   needs no label); `sso` is a fact chip. `GET /api/auth/users` →
   `role`, `sso`, `lastLogin`.
6. **`+ let someone in`** opens the form row: their name · a first
   password (*they change it*) · `can change things | can only look`.
   `let them in` is `POST /api/auth/users {username, password, role}`
   with `user` or `viewer` — the backend refuses `admin`, which is why
   the hint says *the admin is made at the console, never here* and the
   admin's own row ends `console-only` instead of `remove`.
7. **`remove`** uses the same armed gesture: `confirm — signs them out,
   revokes their keys`. That is what `DELETE /api/auth/users/{id}`
   does; the count of keys is not known before the call, so the text
   names the consequence rather than a number.

## Deliberately not here

- No viewer's view. The people group is admin-only end to end (`GET
  /api/auth/users` is gated), so for a viewer it is absent, not empty;
  the keys group lists for anyone signed in but only an admin sees
  `+ mint a key` and `revoke`. One data story, one signed-in admin.
- No read-only declaration for the viewer (#700's gap): not this item.
- No key expiry, scopes or rename — the backend has none (a #750-kind
  of gap if drawn).
- No password-change flow for someone else — the backend has none.

## Screenshots

`shots/` — captured by `capture.mjs`, viewed and clean: `settings`,
`people-form`, `people-let-in`, `people-remove-armed`,
`keys-form-ingest`, `keys-reveal-ingest`, `keys-reveal-readonly`,
`keys-revoke-armed`.

No new data palette: the accent, the `now` ink for the read-only tier,
the `ok` ink for ingest and the alarm ink for an armed verb are round
30's, so the dataviz validator has nothing new to check.

## Verdicts

Awaiting the owner.
