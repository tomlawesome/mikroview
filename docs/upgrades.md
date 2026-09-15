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
   says so, naming both versions and the way back: start the newer
   build again, or restore the copy it took before it upgraded
   (below). It never writes an older shape over newer data.
3. If the data is older, MikroView first copies every document it is
   about to change into `<data>/upgrade-backup/<version it came
   from>/`, then runs each pending migration in order, one at a time,
   stamping the new schema version only when the last one has landed.
   An upgrade interrupted part-way resumes from the first migration
   that did not finish; nothing is left half-written.
4. It records the build version it is now running, so the next start
   can tell whether it was an upgrade.

On the Postgres backend the migrations are SQL, each in its own
transaction; MikroView does not copy the database first. Take a
`pg_dump` before upgrading — that is the copy to restore if you need to
go back.

## What you see afterwards

After an upgrade, MikroView tells you in the interface that it upgraded,
from which version, and what — if anything — you have to do by hand. The
one recurring item is the router: the setup wizard's pasted script
changes between versions, and the router does not update itself.

The wizard's logging block is safe to paste again at any time: it updates
what is already there in place. Paste step 1 again on each router after
an upgrade, and the notice goes away once every router has reported the
current setup.

## Going back

Restore the copy under `<data>/upgrade-backup/<version>/` (or your
`pg_dump`) and start the older build. Do not start the older build on
the upgraded data: it will refuse, and it is right to.

The copy stays until you delete it or the next upgrade replaces it.

## How this is tested

For every released version there is a recorded data directory — what
that version actually wrote after a scripted session — under
`testdata/upgrade/<version>/`, with made-up users and tokens. Every
change to MikroView opens each of them with the current build and
checks the result: the migrations run, the users can still sign in, the
watchlist, flags, entities and coverage declarations are still there
and still mean what they meant.

A new release adds its own recording. The gate never boots an old
image; that happens once, when the recording is made.
