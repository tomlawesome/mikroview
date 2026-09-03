#!/usr/bin/env python3
"""Round 42: the disk group -- the on-disk history's switch, beside the
memory slider (#910). Built on round 39's the-whole.html verbatim; only
the settings column gains a group. Reads round-39/the-whole.html, writes
round-42/disk.html.

The shape: the memory group already reads as two things -- a bar for
what is HELD (the hours), a track for what is ALLOWED (the slider). The
disk group is the same two things one storey down: a bar of the days
held on disk, a track for the days allowed, the byte cap as a figure in
the row on the right, and off as a link. Every change that would delete
something is a proposal until a link that names the deletion is taken,
exactly as the slider's shrink is (round 39, ratified).

Data story (the whole's own): today is 2 Sep, now is 14:02. History was
turned on 7 Aug, so 27 days are on disk, 812 MiB of them, at about
30 MiB a day once compressed. 30 days are allowed under a 1 GiB cap, so
at today's rate the cap would bite at ~34 days -- the days setting is
the one that decides. The capped scene lowers the cap to 768 MiB to show
the other case: 25 days held, the cap deciding, not the 30.

Positions: the days bar has one cell per day held, x = 8 + i * 500 / n.
The days track is a doubling scale from 1 d to 365 d,
x = 8 + 500 * log2(v) / log2(365).
"""
import math
import pathlib

root = pathlib.Path(__file__).resolve().parent.parent
s = (root / 'round-39/the-whole.html').read_text()
out = root / 'round-42/disk.html'

s = s.replace('<title>Round 39', '<title>Round 42', 1)


def sub(old, new, count=1):
    global s
    assert s.count(old) == count, (s.count(old), old[:60])
    s = s.replace(old, new)


def tx(days):
    return round(8 + 500 * math.log2(days) / math.log2(365))


def days_bar(n, first_label, states, cut=None, cut_label=None):
    """The bar of days held: one cell per day, darker held more, the
    newest at the right against the now mark. `cut` dims the days a
    proposal would let go, with the new oldest day marked."""
    w = 500 / n
    # a plausible run of daily volumes, quiet weekends included
    vol = [0.08, 0.11, 0.10, 0.14, 0.12, 0.07, 0.06, 0.13, 0.15, 0.12,
           0.16, 0.14, 0.08, 0.07, 0.15, 0.17, 0.13, 0.18, 0.16, 0.09,
           0.08, 0.17, 0.19, 0.15, 0.21, 0.20, 0.27, 0.24, 0.22, 0.25]
    cells = ''.join(
        f'<rect x="{8 + i * w:.1f}" y="20" width="{w:.1f}" height="10" opacity="{vol[i]}"/>'
        for i in range(n))
    ticks = ''.join(
        f'<line x1="{8 + i * w:.1f}" y1="31" x2="{8 + i * w:.1f}" y2="35" stroke="var(--hair-2)"/>'
        for i in range(7, n, 7))
    overlay = ''
    if cut is not None:
        x = 8 + cut * w
        overlay = f'<g class="dcut"><rect x="8" y="20" width="{min(x, 502) - 8:.1f}" height="10" rx="5" fill="var(--void)" opacity="0.75"/>'
        if cut_label:
            overlay += (f'<line x1="{x:.1f}" y1="15" x2="{x:.1f}" y2="35" stroke="var(--ink-2)" stroke-width="1.2"/>'
                        f'<text x="{x + 4:.1f}" y="50" class="sp-n">{cut_label}</text>')
        else:
            overlay += '<text x="8" y="50" class="sp-n">all 27 days would let go</text>'
        overlay += '</g>'
        first = ''
    else:
        first = f'<text x="8" y="50" class="sp-n">{first_label}</text>'
    return (f'<svg class="stmem" viewBox="0 0 520 58" role="img" data-d="{states}" aria-label="The days held on disk, the oldest at the left; the newest is today">\n'
            f'        <rect x="8" y="20" width="500" height="10" rx="5" fill="rgba(157,184,232,0.10)"/>\n'
            f'        <g fill="var(--accent)" clip-path="url(#memclip)">{cells}</g>\n'
            f'        {ticks}\n'
            f'        <rect x="504" y="15" width="3" height="20" rx="1.5" fill="var(--now)"/>\n'
            f'        {overlay}{first}\n'
            f'        <text x="508" y="50" text-anchor="end" class="sp-k">today</text>\n'
            f'      </svg>')


def track(states, at, ghost=None, cap_days=34, cap='1 GiB', label=None):
    """The days-allowed track. The handle rides at `at` days; a proposal
    leaves the old place as a dotted ghost. The cap's bite at today's
    rate is a dashed mark with its own line under the track."""
    x = tx(at)
    xc = tx(cap_days)
    ticks = ''.join(f'<line x1="{tx(2 ** k)}" y1="27" x2="{tx(2 ** k)}" y2="31" stroke="var(--hair-2)"/>' for k in range(1, 9))
    g = f'<circle cx="{tx(ghost)}" cy="24" r="4.5" fill="none" stroke="var(--ink-3)" stroke-dasharray="2 2"/>' if ghost else ''
    return (f'<svg class="stmemctl" viewBox="0 0 520 54" role="slider" data-d="{states}" aria-label="Days kept on disk" aria-valuemin="1" aria-valuemax="365" aria-valuenow="{at}" aria-valuetext="{at} days, under a {cap} cap">\n'
            f'        <line x1="8" y1="24" x2="508" y2="24" stroke="var(--hair-2)" stroke-width="2" stroke-linecap="round"/>\n'
            f'        {ticks}\n'
            f'        <line x1="{xc}" y1="14" x2="{xc}" y2="34" stroke="var(--ink-3)" stroke-dasharray="2 2"/>\n'
            f'        <line x1="8" y1="24" x2="{x}" y2="24" stroke="var(--accent)" stroke-width="2" stroke-linecap="round" opacity="0.55"/>{g}'
            f'<circle class="mh" cx="{x}" cy="24" r="6.5" fill="var(--raised)" stroke="var(--accent)" stroke-width="1.6"/>'
            f'<text x="{x}" y="11" text-anchor="middle" class="sp-k">{label or f"{at} d"}</text>\n'
            f'        <text x="8" y="46" class="sp-n">1 d</text>\n'
            f'        <text x="{xc}" y="46" text-anchor="middle" class="sp-n" opacity="0.75">~{cap_days} d — where {cap} runs out at today\'s rate</text>\n'
            f'        <text x="508" y="46" text-anchor="end" class="sp-n">365 d</text>\n'
            f'      </svg>')


# =====================================================================
# SETTINGS · DISK — the same two things as memory, one storey down
# =====================================================================
DISK = '''    <div class="og wide" id="diskg">
      <h3>disk</h3>
      <div class="wleft">
      ''' + days_bar(27, '7 Aug — the oldest day on disk', 'rest dgrow') + '''
      ''' + days_bar(27, '', 'dshrink', cut=13, cut_label='20 Aug — the oldest that 14 days would keep') + '''
      ''' + days_bar(27, '', 'dcap', cut=10, cut_label='17 Aug — the oldest that 512 MiB would keep') + '''
      ''' + days_bar(27, '', 'doff', cut=27, cut_label='') + '''
      ''' + days_bar(25, '9 Aug — the oldest the 768 MiB cap keeps', 'dcapped') + '''
      <p class="oghint" data-d="dstopped">nothing on disk — events live in memory only, ~9 h of them at today's rate; on keeps each day from now</p>
      <p class="oghint" data-d="rest dgrow dshrink dcap doff dcapped">one encrypted file a day; the oldest day lets go when the days or the cap is reached, whichever first</p>
      ''' + track('rest doff dstopped', 30) + '''
      ''' + track('dshrink', 14, ghost=30) + '''
      ''' + track('dgrow', 90, ghost=30) + '''
      ''' + track('dcapped', 30, cap_days=25, cap='768 MiB') + '''
      ''' + track('dcap', 30, cap_days=17, cap='512 MiB') + '''
      <p class="oghint memnote" data-d="dshrink">14 days holds ~420 MiB at today's rate — the 13 days before 20 Aug let go — <a class="olink">delete 13 days</a> · <a class="olink">keep all 27</a></p>
      <p class="oghint memnote" data-d="dgrow">90 days would need ~2.7 GiB at today's rate; the 1 GiB cap would hold ~34 of them — <a class="olink">apply</a> · <a class="olink">keep 30 days</a></p>
      <p class="oghint memnote" data-d="dcap">512 MiB holds ~17 days at today's rate — the 10 days before 17 Aug let go — <a class="olink">delete 10 days</a> · <a class="olink">keep 1 GiB</a></p>
      <p class="oghint memnote" data-d="doff">off deletes all 27 days on disk, back to 7 Aug, and keeps nothing after — <a class="olink">delete 27 days</a> · <a class="olink">keep them</a></p>
      </div>
      <div class="wrows">
      <div class="orow"><span>on disk</span><span class="ov"><span data-d="rest dgrow dshrink dcap doff">27 days · since 7 Aug · 812 MiB — filling</span><span data-d="dcapped">25 days · since 9 Aug · 768 MiB — full</span><span class="dim" data-d="dstopped dnokey">nothing</span></span></div>
      <div class="orow" data-dhide="dnokey"><span>allowed</span><span class="ov"><span data-d="rest">30 days · at most <a class="olink">1 GiB</a> · <a class="olink">turn off</a></span><span data-d="dshrink">14 days · at most <a class="olink">1 GiB</a> · <a class="olink">turn off</a></span><span data-d="dgrow">90 days · at most <a class="olink">1 GiB</a> · <a class="olink">turn off</a></span><span data-d="doff">30 days · at most <a class="olink">1 GiB</a> · <span class="dim">off</span></span><span data-d="dcap">30 days · at most <input class="oin" value="512" aria-label="Byte cap, MiB"> MiB · <a class="olink">turn off</a></span><span data-d="dcapped">30 days · at most <a class="olink">768 MiB</a> · <a class="olink">turn off</a></span><span data-d="dstopped">30 days · at most <a class="olink">1 GiB</a> · <a class="olink">turn on</a></span></span></div>
      <div class="orow"><span>key</span><span class="ov"><span class="dim" data-d="rest dgrow dshrink dcap doff dcapped dstopped">mounted at start</span><span data-d="dnokey">none mounted — nothing is kept on disk without one · <a class="olink">how to mount one</a></span></span></div>
      </div>
    </div>
'''

sub('    <div class="og">\n      <h3>account</h3>', DISK + '    <div class="og">\n      <h3>account</h3>')

# states are section classes, as the memory group's are
STATES = ['dshrink', 'dgrow', 'dcap', 'doff', 'dcapped', 'dstopped', 'dnokey']
rest = '#set' + ''.join(f':not(.{c})' for c in STATES)
css = ('  #set [data-d] { display: none; }\n'
       f'  {rest} [data-d~="rest"] {{ display: revert; }}\n'
       + ''.join(f'  #set.{c} [data-d~="{c}"] {{ display: revert; }}\n' for c in STATES)
       + '  #set.dnokey [data-dhide~="dnokey"] { display: none; }\n'
       + '  #set.dnokey #diskg { display: block; }\n  #set.dnokey #diskg > .wleft { display: none; }\n'
       + '  .oin { font: 600 11px var(--mono); color: var(--ink); background: var(--raised); border: 1px solid var(--hair-2); border-radius: 4px; width: 5ch; padding: 1px 4px; text-align: right; }\n')
sub('  #set.mshrink .mtk { display: none; }\n', '  #set.mshrink .mtk { display: none; }\n' + css)

# a direct URL per state, so each can be handed over as a link:
# disk.html?d=doff#set
sub('</body>', '<script>for (const c of (new URLSearchParams(location.search).get("d") || "").split(",")) if (c) document.getElementById("set").classList.add(c);</script>\n</body>')

out.write_text(s)
print(out, len(s))
