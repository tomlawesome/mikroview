# Interface visioning — round 28

Under #634. The clear-all verdict, verbatim:

- **"Clear all is a no, make it the bubble, it's orange, 'clear
  all', click once, it turns red 'confirm' click again to actually
  clear all."**

Built here, in `the-whole.html`:

- **The bubble.** A filled orange bubble ("clear all", the app's
  standing amber `--now`, void-dark text) on the tab row, flags tab
  only. One click arms it: the same bubble turns red (`--alarm`)
  and reads "confirm". A second click actually clears — the honest
  empty state and the green ⚑ 0 chrome from round 26 are unchanged.
  Clicking anywhere else disarms back to orange, so an armed bubble
  cannot ambush a later stray click. The two-button inline confirm
  from round 26 is removed wholesale.

Verified in the capture run: click-away disarms; the second click
clears (chrome reads ⚑ 0); the apparatus restore still works. Shots
of both states in `shots/`.

If the orange should be punchier than the standing amber, it is one
value (`.cabtn { background }`).

## Owner verdicts

- Pending: the bubble; round 27's journey (five beats).
