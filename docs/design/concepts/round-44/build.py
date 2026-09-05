#!/usr/bin/env python3
"""Round 44: the router backups group in Settings (#394). Built on round
43's restart.html verbatim; one group is added and nothing else moves.
Reads round-43/restart.html, writes round-44/backups.html.

What the group says, in the disk group's own idiom: on the left, what
has arrived -- one strip per router, ten slots for the ten generations
kept, the newest at the right; on the right, the facts an admin needs
without reading the docs: how a backup arrives, how many are kept, how
they are stored, and the one caveat that matters (the router never
checks who it is sending to).

The states are the group's honest set, section classes on #set like the
disk group's d-states: rest (two routers, both with backups), brecv (one
arriving now), brefused (the last one was not a RouterOS backup),
bquota (the last one was over the cap), bnone (no router has pushed one
yet), bnokey (no key mounted, so nothing can be kept), bfail (the
server did not answer).

Data story: round 42's own -- today is 2 Sep. rb5009 pushes nightly at
03:00 and has its ten; hap-ax2 has four, the newest 30 Aug, which is
why the ingest group calls it quiet 3 d.
"""
import pathlib

root = pathlib.Path(__file__).resolve().parent.parent
s = (root / 'round-43/restart.html').read_text()
out = root / 'round-44/backups.html'

s = s.replace('<title>Round 43', '<title>Round 44', 1)


def sub(old, new, count=1):
    global s
    assert s.count(old) == count, (s.count(old), old[:60])
    s = s.replace(old, new)


# ---------------------------------------------------------------------
# the generations strip: ten slots, the newest at the right
# ---------------------------------------------------------------------
def strip(kept, label, aria, newest='today', receiving=False, refused=False):
    """kept filled slots of ten; the newest slot may be receiving
    (outlined, pulsing) or refused (crossed, alarm ink)."""
    w, gap, x0 = 46.0, 4.6, 8.0
    parts = ['<rect x="8" y="20" width="500" height="10" rx="5" fill="rgba(157,184,232,0.10)"/>']
    first = 10 - kept
    for i in range(10):
        x = x0 + i * (w + gap)
        if i < first:
            continue
        op = 0.10 + 0.017 * (i - first)
        parts.append(f'<rect x="{x:.1f}" y="20" width="{w}" height="10" rx="3" fill="var(--accent)" opacity="{op:.2f}"/>')
    if receiving:
        x = x0 + 9 * (w + gap)
        parts.append(f'<rect class="brpulse" x="{x:.1f}" y="19" width="{w}" height="12" rx="3" fill="none" stroke="var(--accent)" stroke-width="1.2"/>')
    if refused:
        x = x0 + 9 * (w + gap)
        parts.append(f'<rect x="{x:.1f}" y="20" width="{w}" height="10" rx="3" fill="none" stroke="var(--alarm)" stroke-width="1.2" stroke-dasharray="3 2"/>')
    parts.append('<rect x="504" y="15" width="3" height="20" rx="1.5" fill="var(--now)"/>')
    parts.append(f'<text x="8" y="50" class="sp-n">{label}</text>')
    parts.append(f'<text x="508" y="50" text-anchor="end" class="sp-k">{newest}</text>')
    return (f'<svg class="stmem brstrip" viewBox="0 0 520 58" role="img" aria-label="{aria}">'
            + ''.join(parts) + '</svg>')


DL = '<a class="olink">download .backup</a> · <a class="olink">.rsc</a>'

# one router's block: name and receipt on one line, the strip, the newest pair
def router(name, receipt, strip_svg, newest_line, cls=''):
    return (f'      <div class="brtr{(" " + cls) if cls else ""}">\n'
            f'        <div class="brhead"><b>{name}</b><span>{receipt}</span></div>\n'
            f'        {strip_svg}\n'
            f'        <p class="oghint brnewest">{newest_line}</p>\n'
            f'      </div>\n')


REST = 'rest brecv brefused bquota'

RB_REST = router('rb5009', '10 of 10 kept · nightly at 03:00 · the oldest 24 Aug',
                 strip(10, '24 Aug — the oldest pair kept; the next push lets it go',
                       'Ten backups kept for rb5009, the oldest at the left; the newest arrived today'),
                 f'today 03:00 · rb5009.backup 412 KiB · rb5009.rsc 38 KiB · {DL}')
RB_RECV = router('rb5009', '10 of 10 kept · nightly at 03:00 · receiving now',
                 strip(9, '25 Aug — the oldest pair kept',
                       'Ten backups kept for rb5009; the newest is arriving now', receiving=True),
                 'receiving now · 212 KiB so far · the pair before it stays until this one is whole')
RB_QUOTA = router('rb5009', '10 of 10 kept · nightly at 03:00 · the last push refused',
                  strip(9, '25 Aug — the oldest pair kept',
                        'Ten backups kept for rb5009; the newest was refused', refused=True),
                  '<span class="brwarn">refused today 03:00 — rb5009.backup was 17.2 MiB, over the 16 MiB a file can be · nothing kept, the 10 pairs before it stay</span> · <a class="olink">why 16 MiB</a>')
# the overdue router: mikroview learned the interval from the pushes
# themselves, so a missed one is a fact it can state (owner, 2026-09-05)
HAP_REST = router('hap-ax2', '4 kept · nightly at 03:00 · <span class="brwarn">none since 30 Aug — 3 missed</span>',
                  strip(4, 'the first pair arrived 27 Aug',
                        'Four backups kept for hap-ax2, the newest 30 Aug; three nightly pushes have not arrived', newest='30 Aug'),
                  f'30 Aug 03:00 · hap-ax2.backup 96 KiB · hap-ax2.rsc 11 KiB · {DL} · <a class="olink">is it gone?</a>', cls='brquiet')
HAP_REFUSED = router('hap-ax2', '4 kept · the last push refused',
                     strip(4, 'the first pair arrived 27 Aug',
                           'Four backups kept for hap-ax2; the newest was refused', refused=True),
                     '<span class="brwarn">refused today 03:00 — hap-ax2.backup was not a RouterOS backup (its first bytes are wrong) · nothing kept, the 4 pairs before it stay</span> · <a class="olink">what was sent</a>')

LEFT = (
    '      <div class="wleft">\n'
    '      <div data-b="rest brefused">' + RB_REST + '</div>\n'
    '      <div data-b="brecv">' + RB_RECV + '</div>\n'
    '      <div data-b="bquota">' + RB_QUOTA + '</div>\n'
    '      <div data-b="rest brecv bquota">' + HAP_REST + '</div>\n'
    '      <div data-b="brefused">' + HAP_REFUSED + '</div>\n'
    '      <p class="oghint" data-b="' + REST + '">each push is a pair — the binary .backup that restores the router whole, and the .rsc export it can be read from · the eleventh pair lets the oldest go · a download is written to the audit log with your name</p>\n'
    '      </div>\n'
)

ROWS = (
    '      <div class="wrows">\n'
    '      <div class="orow"><span>kept</span><span class="ov">'
    '<span data-b="rest">14 pairs · 2 routers · 5.6 MiB</span>'
    '<span data-b="brecv">14 pairs · 2 routers · 5.6 MiB — one arriving</span>'
    '<span data-b="brefused bquota">14 pairs · 2 routers · 5.6 MiB — the last push refused</span>'
    '<span class="dim" data-b="bnone">nothing — no router has pushed one yet · <a class="olink">the wizard\'s step 6 prints the script</a></span>'
    '<span class="dim" data-b="bnokey">nothing</span>'
    '<span data-b="bfail">unknown — the server did not answer · <a class="olink">ask again</a></span>'
    '</span></div>\n'
    '      <div class="orow" data-bhide="bnokey bfail"><span>arrive by</span><span class="ov"><span class="dim">SFTP on port 47022 · a drop box the router writes into and nothing reads out of</span></span></div>\n'
    '      <div class="orow" data-bhide="bnokey bfail"><span>allowed</span><span class="ov"><span class="dim">10 pairs a router · 16 MiB a file · the oldest lets go</span></span></div>\n'
    '      <div class="orow" data-bhide="bfail"><span>key</span><span class="ov">'
    '<span class="dim" data-b="' + REST + ' bnone">mounted at start — every pair is encrypted under it; admins read them, and each read is audited</span>'
    '<span data-b="bnokey">none mounted — a backup that arrives has nowhere safe to go, so the drop box is closed · <a class="olink">how to mount one</a></span>'
    '</span></div>\n'
    '      <div class="orow" data-bhide="bnokey bfail"><span>path</span><span class="ov"><span class="brwarn">the router never checks who it is sending to</span> — anyone on the path could read the pair and the token, so only on a network you trust · <a class="olink">why</a></span></div>\n'
    '      </div>\n'
)

GROUP = (
    '    <div class="og wide" id="bakg">\n'
    '      <h3>router backups</h3>\n'
    + LEFT + ROWS +
    '    </div>\n'
)

# straight after the disk group: memory, disk, router backups -- the
# three things mikroview holds, in the order they outlive a restart
STATE_ROW_END = 'flags, definitions, watchlist, entities, tokens</span></span></div>\n      </div>\n    </div>\n'
sub(STATE_ROW_END, STATE_ROW_END + GROUP)

# ---------------------------------------------------------------------
# states: b-classes on #set, the d-state idiom
# ---------------------------------------------------------------------
BSTATES = ['brecv', 'brefused', 'bquota', 'bnone', 'bnokey', 'bfail']
css = ['  #set [data-b] { display: none; }',
       '  #set' + ''.join(f':not(.{b})' for b in BSTATES) + ' [data-b~="rest"] { display: revert; }']
for b in BSTATES:
    css.append(f'  #set.{b} [data-b~="{b}"] {{ display: revert; }}')
    css.append(f'  #set.{b} [data-bhide~="{b}"] {{ display: none; }}')
for b in ('bnone', 'bnokey', 'bfail'):
    css.append(f'  #set.{b} #bakg {{ display: block; }}\n  #set.{b} #bakg > .wleft {{ display: none; }}')
css += [
    '  .brtr { padding: 6px 0 2px; }',
    '  .brtr + .brtr { border-top: 1px solid var(--hair); margin-top: 6px; }',
    '  .brhead { display: flex; justify-content: space-between; gap: 14px; font: 12px var(--sans); color: var(--ink-2); }',
    '  .brhead b { font: 600 11px var(--mono); color: var(--ink); }',
    '  .brhead span { color: var(--ink-3); }',
    '  .brquiet .brhead span { color: var(--ink-3); }',
    '  #set .brnewest { color: var(--ink-2); margin-top: 0; }',
    '  .brwarn, .brhead span .brwarn { color: var(--now); }',
    '  .brpulse { animation: brpulse 1.6s ease-in-out infinite; }',
    '  @keyframes brpulse { 0%, 100% { opacity: 0.35; } 50% { opacity: 1; } }',
]
sub('  .oin { font: 600 11px var(--mono);', '\n'.join(css) + '\n  .oin { font: 600 11px var(--mono);')

# the URL applies b-states like d-states: backups.html?b=bnokey#set
sub('for (const c of (new URLSearchParams(location.search).get("d") || "").split(","))',
    'for (const c of ((new URLSearchParams(location.search).get("d") || "") + "," + (new URLSearchParams(location.search).get("b") || "")).split(","))')

out.write_text(s)
print(out, len(s))
