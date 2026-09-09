# Settings surfaces (#490) — round 4: the conditions editor (#829)

Round 3's room stands. This round draws one thing inside it: the
expanded custom-detector row once it can write its own conditions,
and what Clone means on a shipped row. One file,
`conditions-editor.html`, four scenes; `capture.mjs` shoots them
dark and light into `shots/`.

## The rules the round proposes

- **A condition is a sentence**, one per line: field · verb · value.
  Verbs are worded per field (is · is not · is one of · is not one
  of · is within · is between · is classed as — the engine's seven
  operators) and the value control follows the verb (one box, chips,
  a CIDR box, two boxes, a choice). Every line must hold; the gutter
  says "and" because the engine has no "or".
- **Counting is one sentence** built from the engine's real axes (per
  source · per source and port · per target · per destination port ·
  everywhere; each event or distinct values of a field) with the
  threshold and window inline — the numbers Try already replays.
- **Reads as** is the honesty line: the exact spec to be saved, read
  back as prose. It is the viewer's whole row.
- **Clone on a shipped row says beforehand what it carries.** The
  four shipped *declarative* detectors (port scan, critical port,
  repeated drops, distributed brute force) copy with their conditions
  already written; the code detectors copy scope and numbers only and
  the sentence beside the button says so. The copy always starts
  paused.
- **Wrong is said in words on the line**, in amber (time's ink,
  borrowed for "not yet"). Save waits and says why; Try does not wait.
- Alarm appears nowhere on the bench. No charts, no data palette.

## Scenes

| Scene | Shows |
|---|---|
| s1 | Clone on a shipped row, consequence beside the button |
| s2 | The copy just made: Port scan (copy), conditions written, editable |
| s3 | A copy of a code detector written from nothing; one line wrong; tried |
| s4 | The viewer's row as prose; the pocket bench |

Data story: round 55's garage — the retired 10.0.70.0/24 range's
stragglers (garage-cam .14, ev-charger .31), watched a second way.

## For the build

- Clone from a shipped declarative detector needs the server to hand
  back that detector's conditions (`BuildShippedDeclarativeDefinition`
  has them; the clone response or a GET must expose them). That is an
  API addition the build issue owns.
- Detail-template placeholders are the engine's closed set per key
  mode (`{Count}` plus the key's fields); the "can use" chips list
  exactly that set and insert on click.

## Open questions

1. Should "is between" exist for ports only, or also for time of day
   (the engine allows both)? Drawn for ports.
2. Should Try be offered while a line is incomplete (drawn: yes,
   on the lines that hold) or wait like Save?

## Verdicts

_(none yet)_
