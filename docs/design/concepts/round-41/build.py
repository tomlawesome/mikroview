#!/usr/bin/env python3
"""Build warm-restart.html from round 39's the-whole.html.

Round 39 is the ratified whole; this round adds one thing to two of its
scenes -- the statement a surface makes while its counters were restored
from a snapshot (#795) -- and clones the metrics scene twice to show the
cold-start statement and the relative-time wording for comparison.
Everything else is carried verbatim, so the only difference between the
scenes is the thing under review.
"""
import re
from pathlib import Path

HERE = Path(__file__).parent
SRC = HERE.parent / 'round-39' / 'the-whole.html'
OUT = HERE / 'warm-restart.html'

html = SRC.read_text()

# The data story: the axis on screen runs 12:26 -> 14:02 (the brink at the
# right edge). Mikroview restarted at 13:18. The newest snapshot had been
# taken at 13:14, so 12:26-13:14 are restored minutes, 13:14-13:18 the
# four minutes the process was down (blank -- an absence of ours, drawn as
# one), and 13:18 onward is live.
FIRST_MINUTE = 12 * 60 + 26
RESTORED_TO = 13 * 60 + 14
LIVE_SINCE = 13 * 60 + 18
X0, STEP = 30, 14


def blank_minutes(svg: str, lo: int, hi: int) -> str:
    """Remove the seismograph strokes for minutes lo..hi-1 (absolute)."""
    out = svg
    for m in range(lo, hi):
        x = X0 + STEP * (m - FIRST_MINUTE)
        out = re.sub(rf'\s*<line x1="{x}" y1="\d+" x2="{x}" y2="\d+"[^>]*/>', '', out)
    return out


def scene(html: str, sid: str) -> str:
    m = re.search(rf'<section class="scene" id="{sid}".*?</section>\n', html, re.S)
    assert m, sid
    return m.group(0)


STMT_ABS = ('<span class="sep">·</span>\n    '
            '<span class="fact stmt">restored to 13:14 · live since 13:18</span>')
STMT_REL = ('<span class="sep">·</span>\n    '
            '<span class="fact stmt">counters from a snapshot 4 m old · live from 13:18</span>')
STMT_COLD = ('<span class="sep">·</span>\n    '
             '<span class="fact stmt">counting since 13:18 — nothing before</span>')

s4 = scene(html, 's4')
brink = '<span class="brinkmark">the brink · 14:02</span>'
assert brink in s4

# Restored, absolute times (the recommendation).
s4_abs = s4.replace(brink, brink + STMT_ABS, 1)
s4_abs = blank_minutes(s4_abs, RESTORED_TO, LIVE_SINCE)
s4_abs = s4_abs.replace('id="s4"', 'id="s4"').replace(
    'aria-label="Metrics: seismograph, register, table — the page as built"',
    'aria-label="Metrics after a warm restart: the hour restored to 13:14, four minutes down, live since 13:18"')


def clone(sec: str, suffix: str, label: str) -> str:
    c = re.sub(r'id="([^"]+)"', rf'id="\1-{suffix}"', sec)
    c = re.sub(r'aria-label="Metrics[^"]*"', f'aria-label="{label}"', c, count=1)
    return c


# Cold start: nothing before 13:18 at all.
s4_cold = s4.replace(brink, brink + STMT_COLD, 1)
s4_cold = blank_minutes(s4_cold, FIRST_MINUTE, LIVE_SINCE)
s4_cold = clone(s4_cold, 'cold', 'Metrics after a cold start: counting since 13:18, nothing before')

# Relative wording, for comparison against the absolute one.
s4_rel = s4.replace(brink, brink + STMT_REL, 1)
s4_rel = blank_minutes(s4_rel, RESTORED_TO, LIVE_SINCE)
s4_rel = clone(s4_rel, 'rel', 'Metrics after a warm restart, relative wording: counters from a snapshot 4 m old')

html = html.replace(s4, s4_abs + s4_cold + s4_rel, 1)

# The docket: the same statement as a dim chip in the clear-all row, the
# fall's own "statement, not a control" idiom (round 36 item 6.1).
s7 = scene(html, 's7')
row = '<div class="clearall" id="clearall">\n'
assert row in s7
s7_new = s7.replace(row, row + '    <span class="fall-chip fc-dim stmt-chip">○ restored to 13:14 · live since 13:18</span>\n', 1)
html = html.replace(s7, s7_new, 1)

# Styles: the statement is a fact in the hourline's own voice, dimmed one
# step like the fall's window-cap chip; in the docket it is that chip.
html = html.replace(
    '  .hourline .gap { flex: 1; }',
    '  .hourline .gap { flex: 1; }\n'
    '  .hourline .stmt { color: var(--ink-3); }\n'
    '  .clearall { display: flex; gap: 10px; align-items: center; }\n'
    '  .clearall .stmt-chip { font-weight: 500; }', 1)

OUT.write_text(html)
print(OUT, len(html))
