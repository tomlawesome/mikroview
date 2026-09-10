#!/usr/bin/env python3
"""Round 43: what outlives a restart (#921). Built on round 42's
disk.html verbatim; two rows change and one state is added. Reads
round-42/disk.html, writes round-43/restart.html.

The memory group's `persistence` row carried two facts about two
different things: which store keeps flags, definitions, watchlist
entries, entities and tokens, and what happens to the event buffer on a
restart. With the disk group one storey down, the second fact has a
counterpart ("27 days · since 7 Aug") that the old sentence contradicts.

So the row is split by subject:

- memory's `persistence` becomes `on restart`, about the buffer alone.
  The buffer itself always clears -- nothing refills the ring from disk
  -- and what outlives it depends on the disk group's state, so the row
  reads per state: on, off with a key, no key, and unanswered.
- the state store moves to the disk group as `state`, beside `key`: it
  is the other thing mikroview keeps on disk, and both rows are
  admin-only facts.

The fourth state, `dfail`, is round 42's gap 9: the settings GET did not
answer (an older server, or an error). The group stays, says so, and
offers to ask again, rather than vanishing.

Data story: round 42's own -- today is 2 Sep, 27 days on disk since
7 Aug, a file store under /var/lib/mikroview.
"""
import pathlib

root = pathlib.Path(__file__).resolve().parent.parent
s = (root / 'round-42/disk.html').read_text()
out = root / 'round-43/restart.html'

s = s.replace('<title>Round 42', '<title>Round 43', 1)


def sub(old, new, count=1):
    global s
    assert s.count(old) == count, (s.count(old), old[:60])
    s = s.replace(old, new)


# ---------------------------------------------------------------------
# MEMORY · on restart -- the buffer alone, per the disk group's state
# ---------------------------------------------------------------------
ON = 'rest dgrow dshrink dcap doff dcapped'
sub('<div class="orow"><span>persistence</span><span class="ov">JSON store · 14 d</span></div>',
    '<div class="orow"><span>on restart</span><span class="ov">'
    f'<span data-d="{ON}">the buffer clears — the 27 days on disk stay; trying a watcher reads them</span>'
    '<span data-d="dstopped">the buffer clears — nothing outlives it; days can be kept on disk below</span>'
    '<span data-d="dnokey">the buffer clears — nothing outlives it</span>'
    '<span class="dim" data-d="dfail">the buffer clears</span>'
    '</span></div>')

# ---------------------------------------------------------------------
# DISK · state -- the other thing kept on disk, beside the key
# ---------------------------------------------------------------------
KEY_ROW = ('<div class="orow"><span>key</span><span class="ov"><span class="dim" data-d="rest dgrow dshrink dcap doff dcapped dstopped">mounted at start</span>'
           '<span data-d="dnokey">none mounted — nothing is kept on disk without one · <a class="olink">how to mount one</a></span></span></div>')
sub(KEY_ROW,
    '<div class="orow" data-dhide="dfail">' + KEY_ROW[len('<div class="orow">'):] + '\n'
    '      <div class="orow" data-dhide="dfail"><span>state</span><span class="ov"><span class="dim" data-d="rest dgrow dshrink dcap doff dcapped dstopped dnokey">file store · /var/lib/mikroview — flags, definitions, watchlist, entities, tokens</span></span></div>')

# the unanswered state: the row says so and offers to ask again
sub('<span class="dim" data-d="dstopped dnokey">nothing</span>',
    '<span class="dim" data-d="dstopped dnokey">nothing</span><span data-d="dfail">unknown — the server did not answer · <a class="olink">ask again</a></span>')
sub('<div class="orow" data-dhide="dnokey"><span>allowed</span>',
    '<div class="orow" data-dhide="dnokey dfail"><span>allowed</span>')

# dfail joins the section classes; like dnokey it has no left column
sub('  #set.dnokey [data-dhide~="dnokey"] { display: none; }\n',
    '  #set.dnokey [data-dhide~="dnokey"] { display: none; }\n'
    '  #set.dfail [data-d~="dfail"] { display: revert; }\n'
    '  #set.dfail [data-dhide~="dfail"] { display: none; }\n'
    '  #set.dfail #diskg { display: block; }\n  #set.dfail #diskg > .wleft { display: none; }\n')
# the "rest" selector must also exclude the new class
sub(':not(.dnokey) [data-d~="rest"]', ':not(.dnokey):not(.dfail) [data-d~="rest"]')

out.write_text(s)
print(out, len(s))
