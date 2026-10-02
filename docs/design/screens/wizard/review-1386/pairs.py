#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
#
# Lay the live frames (drive.mjs's shots/) beside round-15's at the same
# journey time, prototype on the left, build on the right -- one PNG per
# beat under pairs/, downscaled and palette-quantised so the committed
# evidence stays small. A third panel, where shots-before/ holds the
# same frame from the build as reviewed (09363519), shows what the fix
# changed. Run from this directory: python3 pairs.py
import os
from PIL import Image, ImageDraw

HERE = os.path.dirname(os.path.abspath(__file__))
ROUND = os.path.join(HERE, '..', 'round-15', 'shots')
LIVE = os.path.join(HERE, 'shots')
BEFORE = os.path.join(HERE, 'shots-before')
OUT = os.path.join(HERE, 'pairs')
os.makedirs(OUT, exist_ok=True)
W, H, PAD, CAP = 800, 500, 6, 24

IN = [400, 900, 1400, 1900, 2400, 2700, 3000, 3300, 3600, 3900, 4200, 4500, 4800, 5200, 5700, 6400]
OUT_T = [300, 700, 1100, 1500, 1900, 2200, 2500, 2800, 3100, 3400, 3700, 4100, 4600, 5300]
BEATS = [f'in-{t:04d}' for t in IN] + [f'out-{t:04d}' for t in OUT_T] + ['01-door', '22-done']


def panel(path):
    return Image.open(path).convert('RGB').resize((W, H), Image.LANCZOS)


def sheet(panels, name):
    out = Image.new('RGB', (W * len(panels) + PAD * (len(panels) - 1), H + CAP), (40, 40, 40))
    d = ImageDraw.Draw(out)
    for i, (label, im) in enumerate(panels):
        x = i * (W + PAD)
        out.paste(im, (x, CAP))
        d.text((x + 6, 6), label, fill=(230, 230, 230))
    out.convert('P', palette=Image.ADAPTIVE, colors=128).save(os.path.join(OUT, name), optimize=True)


for b in BEATS:
    proto = os.path.join(ROUND, f'an-neon-{b}.png')
    live = os.path.join(LIVE, f'live-{b}.png')
    before = os.path.join(BEFORE, f'live-{b}.png')
    if not (os.path.exists(proto) and os.path.exists(live)):
        continue
    panels = [(f'round-15 an-neon-{b} (ratified)', panel(proto))]
    if os.path.exists(before):
        panels.append((f'live-{b} as reviewed (09363519)', panel(before)))
    panels.append((f'live-{b} after the fixes', panel(live)))
    sheet(panels, f'{b}.png')

# the landing, the offer, the menu and the tour: the build alone
for n in ['10-wizard-landed', '23-fall', '24-offer', '25-menu', '26-tour']:
    p = os.path.join(LIVE, f'live-{n}.png')
    if os.path.exists(p):
        sheet([(f'live-{n}', panel(p))], f'{n}.png')

# reduced motion: every frame the DOM-shape timeline shot, in order
for way in ['in', 'out']:
    frames = sorted(f for f in os.listdir(LIVE) if f.startswith(f'reduced-{way}-'))
    if frames:
        sheet([(f[:-4], panel(os.path.join(LIVE, f))) for f in frames], f'reduced-{way}.png')
    frames = sorted(f for f in os.listdir(BEFORE) if f.startswith(f'reduced-{way}-')) if os.path.isdir(BEFORE) else []
    if frames:
        sheet([(f'{f[:-4]} as reviewed', panel(os.path.join(BEFORE, f))) for f in frames], f'reduced-{way}-before.png')
