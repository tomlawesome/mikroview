# Settings surfaces (#490, under #518) — round 3

Round 2's verdict (verbatim in `../round-2/README.md`): the engine
room wins, "but it's weak and needs development". One direction this
round — **AE, the engine room developed** — the development itself.
The organising idea is unchanged: settings live on the machine.

## What "developed" adds over the winning sketch (AC)

1. **The full roster.** Every settings surface now has a place in
   the room:
   - the **fleet** folds into the door — a router is *who speaks
     there* (name, source, totals on the door itself);
   - the **expectations** (watchlist) join the path between the
     watchers and the flags desk — each carrying its whole rule
     (source, destination, ports, the observing knob), and the
     broken one wearing the **same alarm ring the rail wears**;
   - the flags desk owns its **exclusions drawer** — permanent
     quiets kept where anyone can read why something stays silent;
   - the **dictionary** (entities) joins the side rooms as a
     counts-and-door pointer to #489's ratified page — never a copy
     of it: one surface, one owner;
   - the **logbook** (audit) joins the side rooms, its entries one
     glance from the stations they describe.
2. **Working flows.** Letting someone in and minting a key happen
   inside their doors with **consequences written beside the button
   before it is pressed** (new accounts view; the admin role moves
   only by console; Remove names whose sessions end). A watcher's
   scope opens as a popover on its bench holding only what the
   scope can truthfully say. The broken expectation offers the one
   act that helps: **read the traffic** at the minute it broke
   (the fall, via #29's bounded lookback). Every act writes the
   logbook.
3. **The room at every size.** The viewer's complete room (values
   as bold facts in the stations' sentences, all verbs absent,
   the broken ring still ringing — a viewer is exactly who needs
   it) and **the pocket room**: the path was always vertical, so a
   phone stacks it — no drawer needed; side rooms fold to headers
   beneath.

Alarm red keeps exactly the two jobs it already holds in the app:
the broken expectation's ring and the rail's flag badge. Nothing
else in the room may wear it.

## API-side additions to the record

Watchlist's GET joins the deliberate widening list (one authz-matrix
row); the logbook is the existing audit GET read in place; writes
remain 403 for non-admins everywhere; secrets in no GET. No new
stores.

## Palette

No charts — no data palette; colour stays meaning-only per the
house grammar (accent = the admin's amendable ink and interactive
colour; amber = time and the mint's one moment; ok green = the
receiving pulse; alarm as above). Text wears text tokens; the page
survives greyscale.

## Verdicts

None yet — awaiting the owner.
