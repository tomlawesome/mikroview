# Settings surfaces (#490, under #518) — round 1

One direction this round (letters run on; X and Y were entities
round 2). The grammar itself is already ratified — #486's record:
read-only declared once, in words, in the header chip; edit
affordances absent, never disabled — and was proven on Entities
(round 1, scene 3). This round is that grammar **applied to the
settings estate**, settling the translations it forces and the
estate's edges, so the build (#490) needs no new design judgement.

## Direction Z — the reading room

The viewer walks into the **same room**: nothing rearranged, nothing
dressed down, nothing greyed. The hands are simply absent.

The three translations, stated as the mechanical table the build
applies everywhere:

1. **A control is a fact wearing a handle.** The viewer keeps the
   fact, loses the handle — a detector's checkbox becomes the word
   "running · paused" (same quiet dot the admin sees); a scope form
   becomes a sentence in the card body ("watches external sources
   only"); an action button becomes nothing. Never a disabled ghost:
   a disabled control advertises an action the viewer can't take
   (#198, already the rail's shipped rule) and hides whether the
   thing is off or merely untouchable.
2. **Explanations of absent affordances are absent too.** The admin
   row's "role transfer is a console operation" note explains a
   missing control; in a room with no controls it has no job.
3. **Permission-absent ≠ busy-disabled.** Permission never renders a
   disabled control. A mid-save control may briefly disable, with its
   own word ("saving…") — the only disabled state the page ever
   shows, and it never means "not yours".

Scenes, dark and light (admin's room left, viewer's reading room
right in each):

1. **Users** — the pattern, twice: form/Remove vs the bare true
   list; the rail change (viewers keep Users · Tokens · Fleet ·
   Entities as places to read; only "Run setup…", an action, is
   absent; foot wears kai · user).
2. **Detectors** — the hard translation: checkbox → state word,
   scope form → scope sentence, no-scope prose unchanged; the
   busy-disabled contrast on the admin side.
3. **Tokens** — secrets: the one-time mint banner is a moment in the
   admin's room, not a field the viewer's page lost — no GET serves
   a secret to anyone, ever; and the estate's edges (below).

## The estate, settled

- **Read-only for viewers**: Users, Tokens, Detectors, Entities
  (per #489), Watchlist, Flags' exclusions — same table applied.
- **Already reading rooms**: Fleet, audit log — gain the chip only.
- **Admin-only outright**: Run setup… (it mints credentials; nothing
  in it to read) — absent from the viewer's rail, per the ratified
  rail record.
- **Every user's own, unchanged**: change password, connect SSO.
- **Client-side prefs** (theme, density, columns, presets…): personal,
  not settings surfaces; out of #490's scope.

## API-side (in #490's scope, stated in the record)

- Writes are already 403 for non-admins at the server everywhere
  (`callerIsAdmin`, authz matrix).
- The viewer pages exist by **widening the settings GETs**
  (users, tokens, definitions, entities, suggestions, audit) from
  admin to signed-in user — deliberately, one authz-matrix row at a
  time, which is exactly the deliberate widening the matrix's own
  comment (authz_matrix_test.go) reserved for #385/#490.
- Token values appear in no GET at all; the mint response is the
  secret's whole life.

## Palette

No charts on this page — no data palette; the dataviz categorical
validator does not apply. Colour stays meaning-only per the house
grammar: accent for interactive, amber solely as time's tick (and
the one-time secret's amber moment, which is a time fact: "now or
never"), alarm only on the rail's flag badge. Text wears text
tokens throughout.

## Verdicts

Owner, 2026-08-23, on direction Z (verbatim):

> Again... same issue, it's fine but it's boring. Please go wild with
> two out of the box concepts instead of direction z

**Z killed as a direction** — round 2 (`../round-2/`) replaces it with
two out-of-the-box concepts. What survives the kill as requirements
any direction must still carry: the three translations (control →
worded fact; explanations of absent affordances absent too;
busy-disabled ≠ permission-absent), the estate's edges, and the API
clause (writes 403 regardless of client; settings GETs widened
deliberately per authz-matrix row; secrets in no GET) — those are
facts about the build, not Z's styling, and the wild passes inherit
them.
