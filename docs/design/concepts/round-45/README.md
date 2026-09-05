# Round 45 — the wizard's sixth step

Issue: #394. Round 44's `backups.html` is the whole; `build.py` copies it
and adds one scene after Settings: the setup wizard on step 6, `Back up
the router`. Nothing else moves.

The built wizard has five ratified steps (`setupsteps.ts` `STEP_TITLES`)
and this adds a sixth. Same shape as step 4: a lead sentence, the token
note, the script, copy, the scheduler lines, then the observation line
that says what has arrived. Two things are new to it.

## The drawing

- **The caveat**, in the amber the heavy warning already uses, before
  the script rather than after: *Only on a network you trust.* RouterOS
  never checks who it is sending a backup to; anyone on the path could
  read the pair and the token. The step says it cannot tell a LAN from
  the internet from here, which is the truth.
- **The script** is one `/system script add` whose source saves the
  binary backup unencrypted (`dont-encrypt=yes`), exports the plain
  config (no `show-sensitive`), pushes both by `/tool fetch mode=sftp
  upload=yes` to port 47022 with the router's own name and token, and
  removes both files. Then the scheduler at 03:00 nightly, and one run
  now to test it. Nothing is left on the router; nothing is sent back.
- **The observation line** waits on the first push like step 4 waits on
  the first table, and reads `arrived today 03:00 · rb5009.backup
  412 KiB + rb5009.rsc 38 KiB · kept under the key` when it lands.
- **Lost router** (owner, #394 item 19: make recovery easy). The same
  step re-worded when the router that pushed these is gone: the old
  token still opens the drop box so the script stands, `mint a new one`
  retires it, and the observation line points at the newest `.backup`
  to restore the replacement from. The other five steps re-print as
  they always have.

## Scenes (`wizard.html`)

States are section classes on `#wiz`; `wizard.html?w=<state>#wiz`.

| Scene | URL | What it shows |
|---|---|---|
| Waiting | `wizard.html#wiz` | the script, the caveat, `waiting for the first push` |
| Arrived | `?w=warrived#wiz` | the receipt in the step list and the observation line; `next` |
| No key | `?w=wnokey#wiz` | no script — `the script prints here once a key is mounted`; the step list says `needs a key mounted first`; `next — skip for now` |
| Lost router | `?w=wlost#wiz` | `rb5009 is gone`; the script as it stands; download the newest .backup, then run it |

Screenshots: `shots/step6-{rest,arrived,nokey,lost}.png`.

## For the build to settle, not the drawing

- The script's `policy=` list is drawn as `read,write,test,sensitive`;
  the build proves the minimum on CHR before printing it.
- Whether a skipped step 6 gets a `SKIP_CONSEQUENCES` line like the
  other five (`no backups are kept until the script runs`).
- The lost-router state is reached from the backups group in Settings:
  a router that has missed its usual push shows `is it gone?` on its
  newest line (round 44, amended 2026-09-05), and that opens step 6 in
  this shape. The wizard itself never decides a router is gone.

## Verdicts (owner, 2026-09-05)

*"Good, and the sixth wizard step is right."* **Ratified as drawn**,
the sixth step included. Build: #394. The lost-router state is reached
from the missed-push line in Settings (question 42; see round 44).
