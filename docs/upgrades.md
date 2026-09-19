# Upgrading MikroView

What you can expect when you replace a running MikroView with a newer
build, and what MikroView does for you at that moment. The reasoning
behind these rules is in `docs/decisions/upgrade-framework.md`; the
issues that implement them are linked from there.

## The promise

**Any released version upgrades to the current one in a single step.**
Stop the old build, start the new one on the same data, and it brings
the data up to date itself. You never have to install an in-between
version.

A "released version" is any `v*` tag: `v0.1.0` onwards. An install that
predates the first release is treated as the oldest state MikroView
knows how to read, and upgrades the same way.

## Upgrading to 0.6.0: chown your bind mount first

0.6.0 changes the container's user from uid/gid `65532` to `1000`
(#1210) — 1000 is the first account on most Linux hosts, so a file you
mount in is now readable as it stands, where `65532` needed a `chown`
first. This is the one step the single-step promise above does not
cover by itself: a data directory or a secret file (the Postgres DSN,
`history.keyFile`, a TLS key) written by the old image is owned by
`65532`, and the new container cannot write to it until you hand it
over.

Stop MikroView, then hand it over:

```sh
# bind mount
sudo chown -R 1000:1000 /path/on/host/data
```

```sh
# named volume
docker run --rm -v mikroview-data:/data alpine:3.22 chown -R 1000:1000 /data
```

Mounted secret files want the same treatment — `sudo chown 1000:1000
postgres-dsn` and so on — unless you already own them as uid 1000. Skip
this and MikroView's own startup check refuses to start rather than
silently losing data (see [docs/install.md](install.md#persistent-data)'s
"Persistent data" section for what that refusal looks like); chown the
directory it names and restart.

## Upgrading to 0.6.0: the compose file mounts one app folder, not a named volume

If you run `deploy/docker-compose.yml` as shipped (or copied it), 0.6.0
changes what it mounts. Before, your config was one file
(`./config.yaml:/etc/mikroview/config.yaml:ro`) and your data lived in a
Docker-managed named volume (`mikroview-data`). Now it mounts one app
folder, `./mikroview` — read-only for config, and `./mikroview/data`
underneath it read-write for data — and the file no longer declares the
`mikroview-data` volume at all.

**What you'll see if you just pull and run `docker compose up -d`:**
Docker creates an empty, root-owned `./mikroview` because nothing is
there yet. Your old named volume is untouched, but nothing in the new
file mounts it any more. The container finds no `config.yaml` where it
now looks and can't write to the read-only folder either, so it exits at
boot instead of starting on your old data.

**What to do first, before `docker compose up -d`:**

1. Create the folder and put your config where the container now looks
   for it: `mkdir mikroview && cp config.yaml mikroview/config.yaml`.
2. Bring your data across, either way:
   - **Move it into the new bind mount** (matches the shipped file): use
     `mikroview -migrate-data` to copy the named volume's contents into
     `./mikroview/data` — the full command and the ownership it needs
     first are in
     [docs/configuration.md](configuration.md#moving-the-data-directory)'s
     "Moving the data directory" section — then do the chown above.
   - **Keep the named volume**: uncomment the `volumes:` block at the
     end of `deploy/docker-compose.yml` and swap the data line above it
     back to `mikroview-data:/var/lib/mikroview`, exactly as the comment
     there says. Nothing else about your setup changes, and you can skip
     the chown above too, since nothing moved.
3. Run `docker compose up -d`.

This only affects the shipped compose file. If you mount config and data
separately yourself — the old way, still supported, see
docs/configuration.md — nothing here changes for you.

## Upgrading to 0.6.0: routers must be enrolled

0.6.0 stops trusting a router's own pushed configuration to say which
address its syslog comes from (#1281). From this version on, a syslog
source is accepted only once it is the address you declared under
`devices:` in config.yaml (`sourceIp`), or once it has redeemed a
one-time enrolment token you mint for it from the Entities screen. Until
then, its lines are refused and shown under "Refused senders" rather
than counted at all.

**Plainly: if a router was only ever sending logs, and you never gave it
a `sourceIp` in config.yaml, its logs stop being accepted the moment you
upgrade, and stay refused until you enrol it.** A router you declared
with `sourceIp` needs nothing — it keeps working exactly as it did.

MikroView tries to spare you the manual step once, automatically, at the
first push after this upgrade: if a router's own pushed address table
names an address that no other router also claims, that router is
enrolled at it there and then, and it is logged (at Info) so you can see
it happened. This only ever fires once per router, and only when the
evidence is unambiguous — two routers pushing the same address are both
left unenrolled rather than guessed at.

For anything the automatic step does not settle, enrol it by hand:

1. Open the Entities screen and find the router beside your others (or
   press **+ add a router** first if it has never pushed anything at all
   — routers that only send logs, never a push, have no entry to enrol
   until you add one by name).
2. Press **Re-enrol…** to open the setup ledger at **Send logs**.
3. Give the router's own address, and your password. MikroView opens
   the syslog port for that one address while the token is pending, and
   asks who you are because minting the token is what opens it — holding
   an admin session is not enough on its own.
4. Paste the one extra line the step shows — it is appended to the
   syslog action commands you already have on the router — and let the
   router run it once.
5. The router's next log line carrying that token is what enrols it;
   everything after that is accepted normally.
6. Finish the ledger's last step, **Register the router**, to record that
   this is a router you meant to add.

If you gave the wrong address, the router is turned away and its real
address is listed under the step's refused senders. Press it to point
the enrolment window at it — the token you already pasted stays as it
is, so there is nothing to paste into the router again.

Until you do this, that router's traffic is refused, not silently
dropped: it is listed under "Refused senders" beside your routers on the
Entities screen so you can see exactly which addresses are waiting on
you. The syslog port itself now refuses the connection outright from an
address it does not recognise, except for the one address an enrolment
token is currently pending for — that window is what lets the router's
own enrol line reach the port in the first place.

### Routers you enrolled before this release

Registering (step 6 above) is new in this release. A router that was
already enrolled when you upgraded is treated as registered, dated when
it enrolled: its operator did everything the ledger asked of them at the
time, and reopening the ledger on it would be asking for work that did
not exist yet. A router that never enrolled is not swept along with it —
there is no evidence anyone confirmed it, so it still has the Register
step to walk.

## What happens at start

1. MikroView reads the schema version its data was last written by.
   No version on disk reads as the oldest schema.
2. If the data is **newer** than the build — you have started an older
   MikroView on data a newer one wrote — it **refuses to start** and
   says so: which version wrote the data, and that you need that build
   or a newer one. It never writes an older shape over newer data.
3. If the data is older, it runs each pending migration in order, one at
   a time, stamping the schema version after each one lands. A
   migration writes a new document beside the old and swaps it in only
   when it is complete, so an upgrade interrupted part-way leaves the
   old document as it was and resumes from that migration next start.
4. It records the build version it is now running, so the next start
   can tell whether it was an upgrade.

On the Postgres backend the migrations are SQL, each in its own
transaction, with the same effect: a migration either lands whole or
not at all.

## Where the schema version is kept

On a file-backed install it is `schema.json` in the data directory, next
to the stores — a number and the build that wrote it, in plain JSON. It
holds no data of yours, so it is readable without a key: MikroView has to
read it before it knows whether it may open anything else.

A data directory with no `schema.json` is schema 0, which is every
install from before this existed. Nothing to do: MikroView stamps it on
the next start.

On Postgres it is the `schema_version` table, as it has always been. The
migration numbers are the same list for both, so "schema 3" means the
same thing either way, even though some migrations only have work to do
on one of them.

`-backup` and `-restore` (see docs/configuration.md, "Backing up and
restoring") carry `schema.json` along with every other store, so a
restore comes back stamped at the schema it was actually taken at rather
than reading as a fresh, unmigrated install and repeating migrations
that already landed. A backup taken by a build from before this existed
has no `schema.json` to carry, and restores as schema 0 — correct for
data that old.

The refusal in step 2 above looks like this, with the paths and versions
of your install:

```
schema │ the data in /var/lib/mikroview is at schema version 4, but this build of MikroView
         only knows schema version 3 -- it was last written by MikroView v0.6.0, so you need
         that build or a newer one. Refusing to start rather than writing: an older build
         would overwrite newer data in shapes it does not understand, and there is no way
         back from that
```

Start the version it names (or anything later) and it comes up on the
same data.

## Interrupting an upgrade is safe

Each migration publishes its new document by writing it alongside the old
one and renaming it into place, and the schema version is stamped only
once that has happened. So a MikroView killed mid-upgrade — a power cut,
`docker kill`, an OOM — is always in one of two states per migration:
finished and stamped, or not started as far as anything on disk can tell.
The next start picks up at the first migration that did not finish. There
is nothing to repair by hand, and no half-written document to find.

A migration that fails (rather than being killed) stops the start, names
itself, and leaves the data at the last version that did land.

## A note for the curious: migrations carry their own copy of old shapes

A migration that reads a document written by an older MikroView keeps its
own frozen copy of what that document looked like, rather than reusing
the store's current one. The store's shape is free to change with the
next release; the migration's copy is not, so a migration written today
still reads a 2026 document the way it was actually written. It is the
rule that makes "any version to current" hold as the code moves on, and
it is written at the top of the migration list in the source so it is
read by whoever adds the next one.

## What you see afterwards

After an upgrade, MikroView tells you in the interface that it upgraded,
from which version, and what — if anything — you have to do by hand. The
one recurring item is the router: the setup wizard's pasted script
changes between versions, and the router does not update itself.

A line appears at the top of the page, under any connection or
configuration banner:

> upgraded from v0.4.0 · paste step 1 of the setup again on each router

Once your routers report their own setup, it counts them:

> upgraded from v0.4.0 · 2 of 3 routers still on the old setup · paste
> step 1 again on each

It has two controls. **open setup** takes you to step 1 of the setup
wizard, with the script for this version ready to copy. The wizard's
logging block is safe to paste again at any time: it updates what is
already there in place, so pasting it on a router that is already set up
changes nothing it should not.

**done** says you have dealt with it, and the line goes for good. It is
recorded on the server, not in your browser, so one admin pressing it
settles it for everyone and it stays settled across restarts. The next
upgrade raises a new line of its own; this one does not cover for it.

Only an admin sees the line, because only an admin can act on it. There
is no ✕ — `done` is the dismissal, and it says what it claims.

You will not always need to press it. A router running a current script
tells MikroView what the setup wizard left on it, so once every router
reports the current setup the line clears itself and nobody has to
confirm anything. While every router is reporting, `done` is not offered
at all: the count is the real answer, and the remedy is the paste, not a
dismissal. `done` stays available whenever some router has never
reported — a router still running a script pasted before this reporting
existed — because then MikroView genuinely cannot tell, and your word is
the only thing that can settle it.

The line never appears on a first install (there is no version to have
upgraded from), and never for a downgrade — an older build refuses to
start on newer data, as above.

## Going back

There is no downgrade. Once a migration has landed, the data belongs to
the build that wrote it, and an older build will refuse it. If a new
build has a problem, the fix is a newer build. If you want the option of
returning to the old version anyway, take your own copy of the data
directory (or a `pg_dump`) before you upgrade; MikroView does not take
one for you.

## How this is tested

For every released version there is a recorded data directory — what
that version actually wrote after a scripted session, with made-up
users and tokens. There is also a database dump per schema version,
taken from the first release that carried each migration, because the
database's stages are the migrations rather than the releases. The
recordings are not in the repository (a data directory holds password
and token hashes and TLS keys, and a secrets scanner rightly cannot
tell a made-up one from a real one); they live in the project's package
registry, and CI fetches them before the test.

Every change to MikroView opens each of them with the current build and
checks the result: the migrations run, the users can still sign in, the
watchlist, flags, entities and coverage declarations are still there
and still mean what they meant. The database dumps get the same
treatment — each is restored into a database of its own, opened, and
asked the same questions.

Tagging a release records both halves by itself. The job waits for that
version's image to be published, runs the recording, and uploads it to
the package registry along with a small manifest saying what it
created. It cannot commit that manifest — the credential CI runs with
can write a package and nothing else — so the manifest travels with the
recording and the tests read it from there until someone commits it.
That keeps the next change tested against the release that just
shipped, with nothing to remember.

The gate never boots an old image; that happens once, when the
recording is made.
