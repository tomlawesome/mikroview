#!/usr/bin/env python3
"""Round 38: round 37's verdict applied — the entities screen loses its
descriptor lines. Built on round 37 verbatim.
Reads round-37/the-whole.html, writes round-38/the-whole.html."""
import pathlib
import re

root = pathlib.Path(__file__).resolve().parent.parent
s = (root / 'round-37/the-whole.html').read_text()
out = root / 'round-38/the-whole.html'
out.parent.mkdir(exist_ok=True)

s = s.replace('<title>Round 37', '<title>Round 38', 1)

# =====================================================================
# ENTITIES — no descriptor lines under the tables (owner, 2026-09-02:
# "Remove all these little descriptor lines on the entities views, I
# don't want them"). The three view hints and the viewer's hint go;
# the viewer is still declared on the account chip, once. Where a name
# comes from is documentation (round 5; round 30 §5).
# =====================================================================
n = 0
for pat in (r'    <p class="oghint ev-hint"[^\n]*\n', r'    <p class="oghint vw-hint"[^\n]*\n'):
    s, k = re.subn(pat, '', s)
    n += k
assert n == 4, n
assert 'ev-hint' not in s.split('<section class="scene op" id="ent"')[1].split('</section>')[0]
# the JS that toggled the hints with the views has nothing to toggle
s = s.replace("      document.querySelectorAll('#ent .ev-hint').forEach(function (h) { h.hidden = (h.dataset.for !== v); });\n", '', 1)
assert "#ent .ev-hint" not in s

out.write_text(s)
print(out, len(s.splitlines()), 'lines')
