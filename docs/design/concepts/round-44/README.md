# Round 44 — router backups in Settings

Issue: #394. Round 43's `restart.html` is the ratified whole; `build.py`
copies it and adds one group after the disk group. Nothing else moves.

## The drawing

A `router backups` group in the disk group's idiom, third of the three
things mikroview holds: memory, disk, router backups.

- **Left, what has arrived.** One block per router: its name, a receipt
  (`10 of 10 kept · nightly at 03:00 · the oldest 24 Aug`), a strip of
  ten slots for the ten generations kept with the newest at the right
  and a date at the left, then the newest pair with its sizes and
  `download .backup · .rsc`. A caption says what a pair is, that the
  eleventh lets the oldest go, and that a download is written to the
  audit log with the admin's name.
- **Right, the facts.** `kept` (pairs, routers, bytes); `arrive by`
  (SFTP on 47022, a drop box the router writes into and nothing reads
  out of); `allowed` (10 a router, 16 MiB a file — a fact, not a control,
  in v1); `key` (mounted; every pair encrypted under it, admins read,
  each read audited); `path` — the caveat in amber: the router never
  checks who it is sending to, so only on a network you trust.
- **A missed push is said** (owner, 2026-09-05: once a router has been
  pushing, watch for the next one at its usual interval and say when it
  does not come). The interval is learned from the pushes themselves, so
  hap-ax2's receipt reads `nightly at 03:00 · none since 30 Aug —
  3 missed` in amber, and its newest line ends `is it gone?`, which
  opens the wizard's lost-router step (round 45). This is the one place
  mikroview can tell an admin something is wrong on the backup path: it
  cannot see the path, but it can see the silence.
- The group is admin-only, like `key` and `state`: a viewer never sees it.

## Scenes (`backups.html`)

States are section classes on `#set`; `backups.html?b=<state>#set`
applies one, and `?d=` still applies the disk states.

| Scene | URL | What it shows |
|---|---|---|
| Two routers | `backups.html#set` | rb5009 with its ten; hap-ax2 with four, three nightly pushes missed, `is it gone?` |
| Receiving | `?b=brecv#set` | rb5009's newest slot outlined and pulsing; the pair before it stays until this one is whole |
| Refused, not a backup | `?b=brefused#set` | hap-ax2's newest slot crossed; the first bytes were wrong, nothing kept, the four before it stay |
| Refused, over the cap | `?b=bquota#set` | rb5009 sent 17.2 MiB; nothing kept, the ten before it stay |
| Nothing yet | `?b=bnone#set` | no left column; `kept · nothing — no router has pushed one yet · the wizard's step 6 prints the script` |
| No key | `?b=bnokey#set` | no left column; `key · none mounted — a backup that arrives has nowhere safe to go, so the drop box is closed` |
| Unanswered | `?b=bfail#set` | `unknown — the server did not answer · ask again`, round 43's `dfail` idiom |

Screenshots: `shots/settings.png`, `backups-{rest,recv,refused,quota,none,nokey,fail}.png`,
and `settings-nokey.png` with the disk and backups groups both keyless,
so the one fact is told twice and the two agree.

## Not drawn, deliberately

- **Changing `allowed`.** 10 × 16 MiB is fixed in v1 (owner, #394 item
  22). If it becomes a control it takes the disk group's slider idiom.
- **Restore.** Downloading the newest `.backup` and loading it on the
  replacement is RouterOS's own job; the wizard's lost-router state
  (round 45) points at it.
- **Reading the `.rsc` in the app.** The export is kept for later
  config scanning, not shown; v1 downloads it.
- The disk group's `state` row does not yet say which stores persist
  without a key (#853 rule 6). That is #853's row to word when it lands.

## Evidence

The facts the copy rests on were measured on RouterOS 7.23.3 (CHR) for
#394: no HTTP upload exists in `/tool fetch`; SFTP upload works with
password-only auth and the router never verifies the host key; a
`.backup` starts `88 ac a1 b1` unencrypted. The measurement notes are on
the issue (notes 10466, 10489, 10495, 10510).

## Verdicts (owner, 2026-09-05)

*"Good."* **Ratified as drawn.** Build: #394.

Amended after ratification the same day: the missed-push receipt and the
`is it gone?` link, from the owner's note that mikroview should watch for
the next push at its usual interval and say when it does not come.
