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
users and tokens. The recordings are not in the repository (a data
directory holds password and token hashes and TLS keys, and a secrets
scanner rightly cannot tell a made-up one from a real one); they live in
the project's package registry, and CI fetches them before the test.
Every change to MikroView opens each of them with the current build and
checks the result: the migrations run, the users can still sign in, the
watchlist, flags, entities and coverage declarations are still there
and still mean what they meant.

A new release adds its own recording. The gate never boots an old
image; that happens once, when the recording is made.
