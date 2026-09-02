#!/usr/bin/env python3
"""Round 39: the settings memory bar gains its slider (#796). Built on
round 38 verbatim; only the settings memory group changes.
Reads round-38/the-whole.html, writes round-39/the-whole.html."""
import pathlib

root = pathlib.Path(__file__).resolve().parent.parent
s = (root / 'round-38/the-whole.html').read_text()
out = root / 'round-39/the-whole.html'
out.parent.mkdir(exist_ok=True)

s = s.replace('<title>Round 38', '<title>Round 39', 1)


def sub(old, new, count=1):
    global s
    assert s.count(old) == count, (s.count(old), old[:60])
    s = s.replace(old, new)


# =====================================================================
# SETTINGS · MEMORY — the buffer size is set here, not in a file
# (owner, 2026-09-02: "let the user change the maxMemory using the
# slider in settings"). The hours bar stays as drawn: it is what is
# held. Under it a second track is what is allowed: 32 MiB up to what
# this host can spare, on a doubling scale so 120 MiB is not jammed
# against the left end. Dragging is a proposal until "apply": the row
# on the right reads what the proposed figure buys at today's rate,
# the handle's old place stays as a dotted ghost, and a shrink is
# shown on the hours bar itself — the hours that would let go dim,
# with the new oldest time marked. Growing has nothing to show on the
# bar yet, so the sentence says how long the reach takes to fill.
# Positions: x = 8 + 500 · log2(v / 32) / log2(3584 / 32).
# =====================================================================

# the hours bar: hour labels take a class so the shrink marker can
# replace them; the shrink overlay is drawn in place and hidden
sub('<text x="288" y="50" text-anchor="middle" class="sp-n" opacity="0.75">10:00</text>\n'
    '        <text x="400" y="50" text-anchor="middle" class="sp-n" opacity="0.75">12:00</text>',
    '<text x="288" y="50" text-anchor="middle" class="sp-n mtk" opacity="0.75">10:00</text>\n'
    '        <text x="400" y="50" text-anchor="middle" class="sp-n mtk" opacity="0.75">12:00</text>\n'
    '        <g class="mcut" data-m="shrink">'
    '<rect x="8" y="20" width="233" height="10" rx="5" fill="var(--void)" opacity="0.75"/>'
    '<line x1="241" y1="15" x2="241" y2="35" stroke="var(--ink-2)" stroke-width="1.2"/>'
    '<text x="245" y="50" class="sp-n">09:16 — the oldest that 64 MiB would keep</text></g>')

# the control, after the bar's hint
CTL = '''      <p class="oghint">the oldest hour falls away as the newest arrives; darker hours held more</p>
      <svg class="stmemctl" viewBox="0 0 520 54" role="slider" aria-label="Event buffer size" aria-valuemin="32" aria-valuemax="3584" aria-valuenow="120" aria-valuetext="120 MiB of the 3.5 GiB this host can spare">
        <line x1="8" y1="24" x2="508" y2="24" stroke="var(--hair-2)" stroke-width="2" stroke-linecap="round"/>
        <line x1="82" y1="27" x2="82" y2="31" stroke="var(--hair-2)"/><line x1="155" y1="27" x2="155" y2="31" stroke="var(--hair-2)"/><line x1="228" y1="27" x2="228" y2="31" stroke="var(--hair-2)"/><line x1="302" y1="27" x2="302" y2="31" stroke="var(--hair-2)"/><line x1="375" y1="27" x2="375" y2="31" stroke="var(--hair-2)"/><line x1="449" y1="27" x2="449" y2="31" stroke="var(--hair-2)"/>
        <g data-m="rest"><line x1="8" y1="24" x2="148" y2="24" stroke="var(--accent)" stroke-width="2" stroke-linecap="round" opacity="0.55"/><circle class="mh" cx="148" cy="24" r="6.5" fill="var(--raised)" stroke="var(--accent)" stroke-width="1.6"/><text x="148" y="11" text-anchor="middle" class="sp-k">120 MiB</text></g>
        <g data-m="grow"><line x1="8" y1="24" x2="295" y2="24" stroke="var(--accent)" stroke-width="2" stroke-linecap="round" opacity="0.55"/><circle cx="148" cy="24" r="4.5" fill="none" stroke="var(--ink-3)" stroke-dasharray="2 2"/><circle class="mh" cx="295" cy="24" r="6.5" fill="var(--raised)" stroke="var(--accent)" stroke-width="1.6"/><text x="295" y="11" text-anchor="middle" class="sp-k">480 MiB</text></g>
        <g data-m="shrink"><line x1="8" y1="24" x2="82" y2="24" stroke="var(--accent)" stroke-width="2" stroke-linecap="round" opacity="0.55"/><circle cx="148" cy="24" r="4.5" fill="none" stroke="var(--ink-3)" stroke-dasharray="2 2"/><circle class="mh" cx="82" cy="24" r="6.5" fill="var(--raised)" stroke="var(--accent)" stroke-width="1.6"/><text x="82" y="11" text-anchor="middle" class="sp-k">64 MiB</text></g>
        <text x="8" y="46" class="sp-n">32 MiB</text>
        <text x="302" y="46" text-anchor="middle" class="sp-n" opacity="0.75">512 MiB</text>
        <text x="508" y="46" text-anchor="end" class="sp-n">3.5 GiB — all this host can spare</text>
      </svg>
      <p class="oghint memnote" data-m="grow">480 MiB would hold ~36 h at today's rate, filling over the next 27 h — <a class="olink">apply</a> · <a class="olink">keep 120 MiB</a></p>
      <p class="oghint memnote" data-m="shrink">64 MiB holds ~4.8 h at today's rate — everything before 09:16 lets go — <a class="olink">apply</a> · <a class="olink">keep 120 MiB</a></p>
'''
sub('      <p class="oghint">the oldest hour falls away as the newest arrives; darker hours held more</p>\n', CTL)

# the row on the right reads the proposed figure
sub('<div class="orow"><span>event buffer</span><span class="ov">120 MiB · ~201 000 events · ~9 h at today\'s rate</span></div>',
    '<div class="orow"><span>event buffer</span><span class="ov">'
    '<span data-m="rest">120 MiB · ~201 000 events · ~9 h at today\'s rate</span>'
    '<span data-m="grow">480 MiB · ~806 000 events · ~36 h at today\'s rate</span>'
    '<span data-m="shrink">64 MiB · ~107 000 events · ~4.8 h at today\'s rate</span></span></div>')

# the memory group gets a handle for the capture script
sub('    <div class="og wide">\n      <h3>memory</h3>', '    <div class="og wide" id="memg">\n      <h3>memory</h3>')

# states are section classes, as everywhere else in the drawing
sub('  .stpath, .stmem { display: block; width: 100%; height: auto; margin: 4px 0 2px; }',
    '  .stpath, .stmem { display: block; width: 100%; height: auto; margin: 4px 0 2px; }\n'
    '  .stmemctl { display: block; width: 100%; height: auto; margin: 10px 0 0; }\n'
    '  .stmemctl .mh { cursor: grab; }\n'
    '  #set .memnote { color: var(--ink-2); margin-top: 2px; }\n'
    '  #set [data-m]:not([data-m="rest"]) { display: none; }\n'
    '  #set.mgrow [data-m], #set.mshrink [data-m] { display: none; }\n'
    '  #set.mgrow [data-m="grow"], #set.mshrink [data-m="shrink"] { display: revert; }\n'
    '  #set.mshrink .mtk { display: none; }')

out.write_text(s)
print(out, len(s.splitlines()), 'lines')
