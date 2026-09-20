#!/usr/bin/env python3
"""Round 58: the confidence number leaves the flag row and moves into the
drawer, drawn as a rating rather than a bare figure. Built on round 47's
campaigns.html verbatim (the flags tab as ratified on 2026-09-06) and
nothing else in it is redrawn. Reads round-47/campaigns.html, writes two
directions that differ only in how the rating is coloured:

  one-hue.html     one blue, three steps: dim / accent / near-white
  three-bands.html cool / amber / hot, one hue per band

Why: the owner (2026-09-13) read the row's `▲ ACTIVITY SPIKE 72` as an
event count -- it sits beside a type, in the same column family as
COUNT's `26×`, with nothing saying what it is. The ask: take it out of
the row, put it in the drawer, say plainly that it is a confidence
rating, and use colour.

One data change. DROP SURGE's score moves from 71 to 46 so the round
shows two of the three bands in real drawers (moderate and high); its
story gains a clause saying why it scores lower than the spike -- the
wan boundary's own volatility -- which is exactly what a moderate score
means (internal/engine/baseline.go, emaConfidence). bands.html shows all
three side by side at 18 / 46 / 72 so the colours can be ratified as a
set.
"""
import pathlib
import re

root = pathlib.Path(__file__).resolve().parent.parent
base = (root / 'round-47/campaigns.html').read_text()
here = root / 'round-58'

# The three bands and their words. Thresholds are the drawing's, not the
# engine's: emaConfidence gives 0..100 with no bands of its own.
BANDS = [('low', 0, 39), ('moderate', 40, 69), ('high', 70, 100)]


def band(n):
    for word, lo, hi in BANDS:
        if lo <= n <= hi:
            return word
    raise ValueError(n)


# The rating block that goes in the drawer's side column, under the
# sparkline. Label, number and word on one line; a meter beneath; one
# quiet line saying what the number is made of.
def block(n, subject, days):
    w = band(n)
    return (
        f'<div class="conf c-{w}" role="group" aria-label="confidence {n} of 100, {w}">'
        f'<span class="clab">confidence</span>'
        f'<span class="cval"><b>{n}</b><em>{w}</em></span>'
        f'<span class="cbar" aria-hidden="true"><span style="width:{n}%"></span></span>'
        f'<span class="cwhy">how far this hour sits from {subject} usual × how much history backs that ({days} days here). '
        f'The detector\'s number, not a verdict.</span>'
        f'</div>'
    )


PALETTES = {
    # one hue, three lightness steps -- confidence is a magnitude, so it
    # is drawn as a sequential ramp of the accent blue, distinct from
    # every (warm) type ink and from the status greens and reds
    'one-hue': {'low': '#5c6f9c', 'moderate': '#9db8e8', 'high': '#e2ecff'},
    # one hue per band, cool to hot: reads as a rating at a glance, at
    # the cost of the hot step sharing the alarm ink
    'three-bands': {'low': '#7f93bd', 'moderate': '#f5a623', 'high': '#ff5470'},
}

CSS = r'''
<style>
  /* ============================================================
     ROUND 58 — the confidence rating moves into the drawer (#1231).
     Additive to round 47; the row loses its bare number and the
     story loses its "Scored N." sentence, nothing else moves.
     ============================================================ */
  .fmark .conf { display: none; }
  .story .scored { display: none; }
  .dwr-in .side .conf { --ci: var(--ink-2); display: grid; grid-template-columns: 1fr auto; gap: 4px 10px; align-items: baseline; margin-top: 14px; padding-top: 10px; border-top: 1px solid var(--hair); }
  .dwr-in .side .conf .clab { font: 600 9px var(--mono); letter-spacing: 0.14em; text-transform: uppercase; color: var(--ink-3); }
  .dwr-in .side .conf .cval { justify-self: end; font: 700 13px var(--mono); color: var(--ci); font-variant-numeric: tabular-nums; }
  .dwr-in .side .conf .cval em { font: 600 10px var(--mono); font-style: normal; letter-spacing: 0.1em; text-transform: uppercase; margin-left: 8px; }
  .dwr-in .side .conf .cbar { grid-column: 1 / -1; display: block; height: 4px; border-radius: 2px; background: color-mix(in srgb, var(--ci) 16%, transparent); }
  .dwr-in .side .conf .cbar span { display: block; height: 100%; border-radius: 2px; background: var(--ci); }
  .dwr-in .side .conf .cwhy { grid-column: 1 / -1; font: 10.5px var(--mono); color: var(--ink-3); line-height: 1.5; white-space: normal; }
  .dwr-in .side .conf.c-low { --ci: @low@; } .dwr-in .side .conf.c-moderate { --ci: @moderate@; } .dwr-in .side .conf.c-high { --ci: @high@; }
  tr.drawer.open .dwr { max-height: 340px; }
</style>
'''


def css(colours):
    out = CSS
    for k, v in colours.items():
        out = out.replace(f'@{k}@', v)
    return out


def build(name, colours):
    s = base

    def sub1(old, new, count=1):
        nonlocal s
        assert s.count(old) >= 1, old[:90]
        s = s.replace(old, new, count)

    sub1('<title>Round 47', f'<title>Round 58 · {name}')

    # the row: no number beside the type
    s, n = re.subn(r'<span class="conf" title="scored \d+ of 100 by the detector">\d+</span>', '', s)
    assert n == 2, n

    # the story: no "Scored N." sentence; the span line: no "· scored N"
    s, n = re.subn(r'<span class="scored"><b>Scored \d+\.</b>[^<]*</span>', '', s)
    assert n == 2, n
    s, n = re.subn(r' · scored \d+</span>', '</span>', s)
    assert n == 2, n

    # the one data change: DROP SURGE scores 46, and its story says why
    sub1('the shape background scanning takes. Nothing inside answered anything.',
         'the shape background scanning takes — and this boundary is volatile, so 3.1× clears the line without being far outside its own spread. Nothing inside answered anything.')

    # the rating block, under each scored drawer's sparkline
    sub1('<span class="span">baseline dashed · climbing since 13:20</span></div>',
         '<span class="span">baseline dashed · climbing since 13:20</span>' + block(46, "the wan boundary's", 14) + '</div>')
    sub1('<span class="span">baseline dashed · 11:10 → 11:50 · settled</span></div>',
         '<span class="span">baseline dashed · 11:10 → 11:50 · settled</span>' + block(72, "cam-porch's own", 14) + '</div>')

    # open straight onto the flags tab with ACTIVITY SPIKE's drawer out,
    # so the thing this round is about is on screen without a click
    OPEN = ('<script>window.addEventListener(\'load\', function () {'
            'location.hash = \'#s7\';'
            'var t = document.querySelector(\'#dtabs span[data-p="flags"]\'); if (t) t.click();'
            'var r = document.querySelector(\'tr.frow[data-d="d7"]\'); if (r) r.click();'
            'setTimeout(function () { var d = document.getElementById(\'d7\'); if (d) d.scrollIntoView({block: \'center\'}); }, 350);'
            '});</script>')
    sub1('</body>', css(colours) + OPEN + '</body>')
    (here / f'{name}.html').write_text(s)


for name, colours in PALETTES.items():
    build(name, colours)

# bands.html: the three bands side by side, both palettes, for
# ratifying the colours as a set. Same tokens and fonts as the drawer.
head = re.search(r'<head>.*?</head>', base, re.S).group(0)
head = re.sub(r'<title>.*?</title>', '<title>Round 58 · the three bands</title>', head)
cards = ''
for name, colours in PALETTES.items():
    cards += f'<section><h2>{name}</h2><div class="row">'
    for n, subject in ((18, "lan's"), (46, "the wan boundary's"), (72, "cam-porch's own")):
        cards += f'<div class="side">{block(n, subject, 14)}</div>'
    cards += '</div></section>'
bands = f'''{head}
<body>
<style>
  body {{ background: var(--void); color: var(--ink); padding: 40px; }}
  h2 {{ font: 600 11px var(--mono); letter-spacing: 0.14em; text-transform: uppercase; color: var(--ink-2); margin: 0 0 12px; }}
  section {{ margin-bottom: 40px; max-width: 1100px; }}
  .row {{ display: grid; grid-template-columns: repeat(3, 1fr); gap: 36px; }}
  .side {{ max-width: 330px; }}
  .side .conf {{ margin-top: 0; padding-top: 0; border-top: 0; }}
</style>
{cards}
'''
for name, colours in PALETTES.items():
    k = list(PALETTES).index(name) + 1
    bands += css(colours).replace('.dwr-in .side', '.side') \
        .replace('.side .conf.c-low', f'section:nth-of-type({k}) .side .conf.c-low') \
        .replace('.side .conf.c-moderate', f'section:nth-of-type({k}) .side .conf.c-moderate') \
        .replace('.side .conf.c-high', f'section:nth-of-type({k}) .side .conf.c-high')
bands += '</body></html>'
(here / 'bands.html').write_text(bands)
