# MikroView never writes config.yaml, in any form

Date: 2026-09-22. Issue #1278; the owner's words that started it,
2026-09-18: "Why can't we just update the file for the user?"

## The problem

Settings ▸ Upgrade (#1218) shows an operator the new config sections a
build understands as a copyable YAML block, comment and all, and asks
them to paste it in themselves. The owner asked, while looking at that
screen, why MikroView could not just put the text in the file for them
-- as a suggested file MikroView writes itself, or by editing
`config.yaml` in place.

## Decisions

MikroView never writes `config.yaml`, and never writes a suggested copy
of it either. The operator's config file stays theirs alone. After an
upgrade the app keeps showing the new settings as a copyable block in
Settings ▸ Upgrade, and that is the permanent answer, not a stand-in
for something better later. The read-only mount of the operator's
folder (`deploy/docker-compose.yml:77`, `./mikroview:/etc/mikroview:ro`)
is a ratified product property now, not merely how the compose file
happens to be written.

Getting the ten seconds of text into the file was never the hard part.
The upgrade blocks are whole top-level sections, already commented and
position-independent, with a Copy button -- there is no merging and no
finding the right spot. The real work is deciding whether the operator
wants the feature and supplying what it needs: an AbuseIPDB key, an
SMTP password, a history key file. No file the app writes can do that
part. Every option below automates the part that was already easy and
leaves the hard part exactly where it was.

Operators who never open Settings ▸ Upgrade will keep missing new
settings. Nothing here fixes that, and no rejected option fixed it
either.

## Superseded

- *Writing `config.yaml.suggested` into the app's writable data
  directory.* `config.yaml` legitimately carries secrets -- an
  AbuseIPDB API key and an SMTP password, both visible in
  `deploy/config.example.yaml` -- so a file holding the operator's
  current settings would put those credentials at rest inside `data/`,
  the directory MikroView writes and operators copy around as a
  backup. That is the same reason `app-folder.md` keeps `keys/` beside
  `data/` rather than inside it. It also goes stale the moment the
  operator edits the real file, forcing answers about regeneration and
  cleanup and about which of the two files is live; and it is more
  work to use, not less, since reconciling a file on the host is a
  worse instruction than clicking Copy.
- *Letting MikroView edit `config.yaml` in place.* Loses on security
  posture. The read-only mount is a hardening property `app-folder.md`
  already paid an extra compose line to keep; loosening it would let a
  compromised container rewrite the settings that define the app's own
  security -- `listen`, auth, key paths. Every existing install is
  `:ro`, so the write would fail and fall back to the paste block
  anyway, meaning the feature would only work for operators who had
  already weakened their own mount -- the product would be nudging
  people toward the less safe configuration. The objection raised in
  #1218 -- that rewriting the file would reorder its keys and lose its
  comments -- is retired, not the reason this loses: MikroView already
  reads config with `go.yaml.in/yaml/v3`'s `yaml.Node`, whose API
  preserves comments and key order, so a faithful in-place edit is
  achievable. It loses on posture instead.
- *A generate-and-download button in the Settings ▸ Upgrade card*,
  building the operator's config with the new sections appended in the
  browser and offering it as a file to save. The only file shape found
  acceptable -- no staleness, nothing written to disk, no secrets at
  rest -- and deliberately not built, for want of evidence that the
  paste block is failing anyone. Reach for this shape if that evidence
  ever appears.

## Implementation

There is no implementation: the decision is to keep what already
ships. See #1218 for the issue that built the paste block.
