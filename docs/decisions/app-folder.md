# One folder, two mounts: the app-folder deployment contract

Date: 2026-09-15. Issue #1209; the owner's words that started it, 2026-09-13,
while adding a fourth mount to their own compose file: "Instead of asking
users to mount 3-4 lines of volumes."

## The problem

`deploy/docker-compose.yml` and `docs/configuration.md` asked an operator to
mount each file MikroView reads separately — `config.yaml`, the GeoIP
database, the data directory — and every optional feature (the history key,
a TLS certificate, router backups) added one more line to both the compose
file and the docs. Each added line was a separate instruction to follow, a
separate chance to get a host path wrong, and a separate thing an upgrading
operator never heard about (#1205, #1206, #1207). The owner ran into this
directly while wiring up a fourth mount by hand.

## The decision

The contract is **one folder on the host, two lines in compose**:

```yaml
volumes:
  - ./mikroview:/etc/mikroview:ro        # what you own: config, keys, certs, GeoIP
  - ./mikroview/data:/var/lib/mikroview  # what MikroView owns
```

with a fixed layout inside the folder:

```
mikroview/
  config.yaml                   optional -- defaults run without it
  GeoLite2-Country.mmdb         optional -- country flags appear when present
  keys/history.key              optional -- history encryption on when present
  certs/tls.crt, certs/tls.key  optional -- your own certificate instead of the self-signed one
  data/                         MikroView's store; never edit
```

MikroView looks for each file at that path when config does not name one, and
says at boot which optional files it found. Adding a feature is dropping a
file in and restarting.

## The five questions, answered

1. **Two lines, not one.** The line for `/etc/mikroview` is read-only so the
   container can never write to config, keys or certs; that is worth one
   extra line. The data folder is nested inside the same host folder so the
   operator still has one thing to back up, move or `ls`. Inside the
   container `/etc/mikroview/data` is visible read-only and ignored;
   `/var/lib/mikroview` is the writable view of the same directory.
2. **`data/` lives inside the folder on the host; `/var/lib/mikroview` stays
   the container path.** Existing installs change nothing.
3. **The key-outside-data rule stands, restated:** the key must not live
   inside the data store — what MikroView writes and what a data backup
   carries. `keys/` beside `data/` satisfies it. The docs say plainly that a
   copy of the whole folder is a copy of the keys too, and that a backup of
   `data/` alone is what the rule protects.
4. **Old per-file mounts keep working, forever.** They are just paths; a
   value set in config or environment wins over the folder default. Nothing
   to migrate, nothing for the upgrade notice (#1240) to say. The compose
   file in the repo and the docs move to the folder layout; an operator who
   never reads them notices nothing.
5. **Yes.** The wizard's compose block and the GeoIP, history-key and TLS
   instructions collapse to "put the file at `<folder>/...` and restart".
   The one-line install (#1242) uses a named volume for data and no config
   folder at all — defaults alone must run — so the folder is the step up,
   not the entry.

## Superseded

- *A single writable mount over the whole folder.* One line instead of two,
  but the container could then write to `config.yaml`, the history key and
  certificates it should only ever read — giving up a real hardening
  property to save one line of compose.
- *Four per-file mounts as the documented default.* Still works (answer 4
  above), but stops being what the repo's compose file and the docs show:
  every new optional feature grew the contract by another line in both
  places, which is the problem this issue exists to fix.

## Implementation

The code side — defaults that look in `/etc/mikroview/` and the boot line
listing what was found — is #1243, in M17. This issue covers documentation
and the compose file only.
