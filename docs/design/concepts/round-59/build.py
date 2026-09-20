#!/usr/bin/env python3
"""Round 59: the ratified confidence rating (round 58, three-bands) and
#1232's notes box in one drawer, so the owner ratifies a single layout
and whichever session builds #1232 works from it. Reads
round-58/three-bands.html and writes combined.html.

Composition (Fable, 2026-09-13, proposed on #1231): the rating stays under
the sparkline in the right column -- it is the sparkline's number -- and
the note takes its own full-width band beneath both columns, above the
buttons. The box grows as you type and the drawer grows with it,
everything below moving down (owner ruling on #1232). "clear with a
note" leaves the drawer: #640 removed it and #1232 says the box is not
that -- you write first, then call it.

Two scenes: ACTIVITY SPIKE's drawer with the box empty (write-first), and
DROP SURGE's with what you wrote last time read back above the box, plus
a draft of a few lines in it, so the growth is on screen.
"""
import pathlib
import re

root = pathlib.Path(__file__).resolve().parent.parent
s = (root / 'round-58/three-bands.html').read_text()
here = root / 'round-59'

NOTE = (
    '<div class="note"><label class="nlab" for="{id}">note</label>'
    '<textarea id="{id}" rows="1" placeholder="why you\'re calling it what you\'re about to call it — optional">{draft}</textarea>'
    '<span class="nhint">kept with the verdict you choose next · editable later · goes if the verdict is undone</span></div>'
)

PRIOR = (
    '<div class="prior"><span class="plab">you wrote last time · checked 2 sept</span>'
    '<p>Same shape as the August one: broad, :445-heavy, forty-odd sources, nothing inside answered. '
    'Checked the upstream block list — every source already on it. Left it alone.</p></div>'
)

DRAFT = ('Third time this month. Still all :445, still nothing answered, but the source count is up '
         '(41 vs 28 last time) and two are from a /24 that wasn\'t in the August set.\nWorth a look at '
         'the upstream list again before I call it expected.')

CSS = r'''
<style>
  /* ============================================================
     ROUND 59 — the note band (#1232) joins the rating (#1231).
     Additive to round 58: nothing above the band moves.
     ============================================================ */
  .dwr-in .note { grid-column: 1 / -1; display: grid; gap: 6px; margin-top: 6px; padding-top: 12px; border-top: 1px solid var(--hair); }
  .dwr-in .note .nlab, .dwr-in .prior .plab { font: 600 9px var(--mono); letter-spacing: 0.14em; text-transform: uppercase; color: var(--ink-3); }
  .dwr-in .note textarea { display: block; width: 100%; box-sizing: border-box; min-height: 64px; resize: none; overflow: hidden; padding: 9px 12px; font: 12.5px/1.55 var(--sans); color: var(--ink); background: color-mix(in srgb, var(--ink) 4%, transparent); border: 1px solid var(--hair-2); border-radius: 6px; outline: none; }
  .dwr-in .note textarea::placeholder { color: var(--ink-3); }
  .dwr-in .note textarea:focus { border-color: var(--accent); }
  .dwr-in .note .nhint { font: 10.5px var(--mono); color: var(--ink-3); }
  .dwr-in .prior { grid-column: 1 / -1; display: grid; gap: 6px; margin-top: 6px; padding-top: 12px; border-top: 1px solid var(--hair); }
  .dwr-in .prior p { margin: 0; padding-left: 12px; border-left: 2px solid var(--hair-2); font: 12.5px/1.55 var(--sans); color: var(--ink-2); max-width: 72ch; }
  .dwr-in .prior + .note { margin-top: 0; padding-top: 0; border-top: 0; }
  tr.drawer.open .dwr { max-height: 900px; }
</style>
<script>
  // the box grows with what is typed; the drawer is max-height-gated
  // only for the open/close motion, so it grows with the box
  document.querySelectorAll('.note textarea').forEach(function (t) {
    var fit = function () { t.style.height = 'auto'; t.style.height = t.scrollHeight + 'px'; };
    t.addEventListener('input', fit); fit();
  });
</script>
'''

s = s.replace('<title>Round 58 · three-bands', '<title>Round 59 · combined')

# the two scored drawers get the band, in place of the retired "clear
# with a note" (the other flag drawers are not in this round's scenes)
acts = '<div class="dwr-acts"><button>open in metrics ▸</button><button class="quiet">clear with a note</button>'
n = s.count(acts)
assert n == 2, n
i = 0
def swap(m):
    global i
    i += 1
    return NOTE.format(id=f'note{i}', draft='') + '<div class="dwr-acts"><button>open in metrics ▸</button>'
s = re.sub(re.escape(acts), swap, s)

# DROP SURGE (d5): last time's note read back, and a draft in the box
d5 = re.search(r'<tr class="drawer ft-surge" id="d5">.*?</tr>', s, re.S)
blk = d5.group(0)
blk2 = re.sub(r'<div class="note"><label class="nlab" for="(note\d+)">note</label><textarea id="\1" rows="1" placeholder="([^"]*)"></textarea>',
              lambda m: PRIOR + f'<div class="note"><label class="nlab" for="{m.group(1)}">note</label><textarea id="{m.group(1)}" rows="1" placeholder="{m.group(2)}">{DRAFT}</textarea>',
              blk, count=1)
assert blk2 != blk
s = s.replace(blk, blk2)

s = s.replace('</body>', CSS + '</body>')
(here / 'combined.html').write_text(s)
