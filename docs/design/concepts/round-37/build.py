#!/usr/bin/env python3
"""Round 37: round 36's verdicts applied, and the surfaces with no home.
Built on round 36 verbatim.
Reads round-36/the-whole.html, writes round-37/the-whole.html."""
import pathlib
import re

root = pathlib.Path(__file__).resolve().parent.parent
src = (root / 'round-36/the-whole.html').read_text()
out = root / 'round-37/the-whole.html'
out.parent.mkdir(exist_ok=True)
s = src


def sub1(old, new, count=1):
    global s
    assert s.count(old) >= 1, old[:80]
    s = s.replace(old, new, count)


sub1('<title>Round 36', '<title>Round 37')

# =====================================================================
# 1. METRICS — the ledger sits above the minutes, not beneath them
#    (owner, 2026-09-02: "put them at the top not beneath"). Same block,
#    same inks; its rule now closes it off from the table below.
# =====================================================================
m = re.search(r'  <div class="ledger".*?\n  </div>\n', s, re.S)
assert m
ledger = m.group(0)
s = s.replace(ledger, '', 1)
sub1('id="mv-tab" aria-label="The table: the same hour and the same totals, refused in refused ink, the cursor\'s minute held amber across the view switch">\n  <table>\n',
     'id="mv-tab" aria-label="The table: the same hour and the same totals, refused in refused ink, the cursor\'s minute held amber across the view switch">\n'
     + ledger + '  <table>\n')
sub1("  /* round 36: the ledger is the table view's foot",
     "  /* round 37: the ledger is the table view's head (round 36 drew it as the foot; owner: at the top)")
sub1('padding-top: 18px; border-top: 1px solid var(--hair-2); }',
     'padding-bottom: 18px; border-bottom: 1px solid var(--hair-2); }')

# =====================================================================
# 2. THE FALL — the healthy-watch header reads WATCHED, in the accept
#    ink, no tick: the ink carries the verdict and the line's siblings
#    are plain statements. The story's one broken watcher (cam-porch
#    quiet hours, iot → wan, on the docket) is on the iot → ether1 band,
#    so that band says WATCH BROKEN in the alarm ink.
# =====================================================================
assert s.count('class="bh-watch">WATCH HOLDING ✓</text>') == 6
sub1('<text x="570" y="53" text-anchor="middle" class="bh-watch">WATCH HOLDING ✓</text>',
     '<text x="570" y="53" text-anchor="middle" class="bh-watch bad">WATCH BROKEN</text>')
s = s.replace('class="bh-watch">WATCH HOLDING ✓</text>', 'class="bh-watch">WATCHED</text>')

# =====================================================================
# 3. THE STREAM — the foot band goes (owner, 2026-09-02: "I don't want
#    that at all"). Its repeating pattern is a flag, its dark boundary
#    is on the fall and the map; the drops trend has no other home.
# =====================================================================
m = re.search(r'\n  /\* The three facts of the day.*?\.foot-legend \.k \{[^\n]*\n', s, re.S)
assert m
s = s.replace(m.group(0), '\n', 1)
m = re.search(r'  <div class="foot-legend">.*?\n  </div>\n', s, re.S)
assert m
s = s.replace(m.group(0), '', 1)
assert 'foot-legend' not in s

# =====================================================================
# 4. ENTITIES — rules and ports are names too (#691 item 6). Not tabs:
#    the metrics page's three view names, one underlined, each with its
#    count. The hosts table is untouched; rules and ports get the same
#    table, and the hint under it says where each kind of name comes
#    from. A rule with no comment on the router shows its number; a
#    port nobody has named shows a dash — both are things to click.
# =====================================================================
EVIEWS = '''    <div class="eviews" id="eviews" role="group" aria-label="Which names: hosts, rules or ports"><span class="on" data-v="hosts">hosts <em>23</em></span><span data-v="rules">rules <em>41</em></span><span data-v="ports">ports <em>12</em></span></div>
'''
RULES = '''    <table class="etable ev" id="et-rules" hidden>
      <thead><tr><th>name</th><th>chain</th><th>action</th><th>last fired</th><th>marks</th></tr></thead>
      <tbody>
        <tr><td class="k">nat-wan</td><td>srcnat</td><td><span class="badge b-nat">NAT</span></td><td>now</td><td></td></tr>
        <tr><td class="k">lan-to-srv</td><td>forward</td><td><span class="badge b-accept">ACCEPT</span></td><td>now</td><td></td></tr>
        <tr><td class="k">wan-in-drop</td><td>input</td><td><span class="badge b-drop">DROP</span></td><td>2 s</td><td></td></tr>
        <tr class="warn"><td class="k">iot-to-lan-drop</td><td>forward</td><td><span class="badge b-drop">DROP</span></td><td>now</td><td><span style="color:var(--alarm)">✱ new carrier · ×14</span></td></tr>
        <tr><td class="k">iot-egress-ntp</td><td>forward</td><td><span class="badge b-accept">ACCEPT</span></td><td>4 m</td><td></td></tr>
        <tr><td class="k">srv-to-lan-mgmt</td><td>forward</td><td><span class="badge b-accept">ACCEPT</span></td><td>2 m</td><td></td></tr>
        <tr><td class="k">wg1-to-lan</td><td>forward</td><td><span class="badge b-accept">ACCEPT</span></td><td>19 m</td><td><span style="color:#a78bfa">◉ watched</span></td></tr>
        <tr><td class="k dim">#17 — no comment on the router</td><td>forward</td><td><span class="badge b-drop">DROP</span></td><td>12 s</td><td></td></tr>
        <tr><td class="k">guest-to-wan</td><td>forward</td><td><span class="badge b-accept">ACCEPT</span></td><td class="dim">never — no log rule</td><td><span style="color:var(--alarm)">dark</span></td></tr>
      </tbody>
    </table>
    <table class="etable ev" id="et-ports" hidden>
      <thead><tr><th>name</th><th>port</th><th>last seen</th><th>marks</th></tr></thead>
      <tbody>
        <tr><td class="k">DNS</td><td>53 · udp</td><td>now</td><td></td></tr>
        <tr><td class="k">HTTPS</td><td>443 · tcp</td><td>now</td><td></td></tr>
        <tr class="warn"><td class="k">SMB</td><td>445 · tcp</td><td>now</td><td><span style="color:var(--alarm)">✱ new carrier · iot → bridge1</span></td></tr>
        <tr><td class="k">SSH</td><td>22 · tcp</td><td>now</td><td></td></tr>
        <tr><td class="k">NTP</td><td>123 · udp</td><td>4 m</td><td></td></tr>
        <tr><td class="k">Winbox</td><td>8291 · tcp</td><td>2 s</td><td></td></tr>
        <tr><td class="k">DoT</td><td>853 · tcp</td><td>1 m</td><td></td></tr>
        <tr><td class="k">unifi</td><td>8443 · tcp</td><td>2 m</td><td></td></tr>
        <tr><td class="k dim">— · unnamed</td><td>5001 · tcp</td><td>now</td><td></td></tr>
      </tbody>
    </table>
    <p class="oghint ev-hint" data-for="rules" hidden>a rule's name is its comment on the router until you give it one here — the router's rule is never touched</p>
    <p class="oghint ev-hint" data-for="ports" hidden>a port's name is yours to give — 445 reads SMB because you said so; a dash is a port nobody has named yet</p>
'''
HOSTHINT = "    <p class=\"oghint\">a name is yours to give — click one to rename it; the router's own names arrive with its pushes</p>\n"
HOSTHINT2 = HOSTHINT.replace('class="oghint"', 'class="oghint ev-hint" data-for="hosts"')
sub1('    <table class="etable">\n      <thead><tr><th>name</th><th>lane</th>',
     EVIEWS + '    <table class="etable ev" id="et-hosts">\n      <thead><tr><th>name</th><th>lane</th>')
sub1(HOSTHINT, RULES + HOSTHINT2)

# =====================================================================
# 5. THE READ-ONLY VIEWER — declared once, in words, in the one place
#    every screen already says who you are: the account chip. Drawn
#    as a state on the entities screen (#ent.viewer): the chip reads
#    "anna (viewer) · read-only" and the rename hint says who gives
#    names instead. No lock icons; the affordances just go quiet.
# =====================================================================
ent_a = s.index('<section class="scene op" id="ent"')
ent_b = s.index('<section class="scene op" id="set"')
ent = s[ent_a:ent_b]
assert ent.count('<button class="who">tom (admin)</button>') == 1
ent = ent.replace('<button class="who">tom (admin)</button>',
                  '<button class="who"><span class="adm">tom (admin)</span><span class="vw">anna (viewer) · read-only</span></button>')
ent = ent.replace(HOSTHINT2,
                  HOSTHINT2 + "    <p class=\"oghint vw-hint\">names are an admin's to give — you are reading, and everything here is yours to read</p>\n")

# =====================================================================
# 6. THE UNREGISTERED ROUTER — the device status strip's facts are the
#    router cards already; its one fact with no home is "pushing, but
#    not registered". Drawn as a state (#ent.unreg): the dashed
#    "a third router?" slot becomes that router's card, in the now ink
#    — attention, not alarm — until it is registered.
# =====================================================================
assert ent.count('      <div class="fcard add">\n') == 1
ent = ent.replace('      <div class="fcard add">\n',
                  '      <div class="fcard unreg">\n'
                  '        <div class="fhead"><b>10.0.50.1</b><span class="fstate warn">● PUSHING · UNREGISTERED</span></div>\n'
                  '        <div class="frow">RouterOS 7.19.4 · pushing since 13:40 · 3 events/s now</div>\n'
                  '        <div class="frow dim">its lines are kept; it has no name and no zones until it is registered</div>\n'
                  '        <div class="frow"><a class="olink">register it ▸</a></div>\n'
                  '      </div>\n'
                  '      <div class="fcard add">\n')
s = s[:ent_a] + ent + s[ent_b:]

sub1('  .fstate.quiet { color: var(--ink-3); }\n',
     '  .fstate.quiet { color: var(--ink-3); }\n'
     '  .fstate.warn { color: var(--now); }\n'
     '  /* round 37: the entities\' three kinds of name — the metrics page\'s view idiom */\n'
     '  .eviews { display: flex; gap: 16px; font: 500 11px var(--sans); color: var(--ink-3); margin: 14px 0 8px; }\n'
     '  .eviews span { cursor: pointer; } .eviews em { font-style: normal; color: var(--ink-3); font: 10px var(--mono); margin-left: 3px; }\n'
     '  .eviews .on { color: var(--ink); border-bottom: 1px solid var(--accent); padding-bottom: 2px; }\n'
     '  .etable td.k.dim { color: var(--ink-3); } .etable td.dim { color: var(--ink-3); }\n'
     '  .etable .badge { font-size: 9px; }\n'
     '  /* the viewer: the chip says it, once; the rename hint gives way */\n'
     '  .who .vw, .vw-hint, #ent.viewer .who .adm, #ent.viewer .ev-hint { display: none; }\n'
     '  #ent.viewer .who .vw { display: inline; } #ent.viewer .vw-hint { display: block; }\n'
     '  #ent.viewer .etable td.k { cursor: default; } #ent.viewer .etable td.k:hover { color: var(--ink); }\n'
     '  /* the unregistered router: a state of the third slot */\n'
     '  .fcard.unreg { display: none; border-color: rgba(232, 176, 90, 0.45); }\n'
     '  #ent.unreg .fcard.unreg { display: block; } #ent.unreg .fcard.add { display: none; }\n')

# =====================================================================
# 7. THE STREAM — saved filters and the CSV. A saved filter is the box's
#    business: "saved ▾" sits at the box's right end and opens a short
#    list under it (the who-menu's dress); with a filter set, the list
#    ends in "save this filter as…". The CSV is a verb on the lines held
#    here, so it sits with wipe, and says what it gives: csv ↓.
# =====================================================================
sub1('        <span class="fbtype" id="fbtype">type a term, or click a value in a row</span>\n      </div>\n',
     '        <span class="fbtype" id="fbtype">type a term, or click a value in a row</span>\n'
     '        <span class="fsaved" id="fsaved" role="button" tabindex="0" aria-haspopup="menu" aria-expanded="false" title="Saved filters">saved ▾</span>\n'
     '        <div class="fpmenu" id="fpmenu" role="menu" aria-label="Saved filters">\n'
     '          <div class="mg" role="none">\n'
     '            <button role="menuitem"><span>cam-porch → nas</span><span class="fpx" title="forget this one">×</span></button>\n'
     '            <button role="menuitem"><span>wan drops</span><span class="fpx" title="forget this one">×</span></button>\n'
     '            <button role="menuitem"><span>dns, all of it</span><span class="fpx" title="forget this one">×</span></button>\n'
     '          </div>\n'
     '          <div class="mg" role="none"><button role="menuitem" class="fpsave">save this filter as…</button></div>\n'
     '        </div>\n'
     '      </div>\n')
sub1('  .fbox .fbtype { color: var(--ink-3); }\n',
     '  .fbox .fbtype { color: var(--ink-3); }\n'
     '  .fbox { position: relative; }\n'
     '  .fbox .fsaved { margin-left: auto; color: var(--ink-3); cursor: pointer; white-space: nowrap; font-size: 10.5px; }\n'
     '  .fbox .fsaved:hover, .fbox .fsaved.on { color: var(--ink); }\n'
     '  .fpmenu { position: absolute; top: calc(100% + 6px); right: 0; min-width: 220px; z-index: 40;\n'
     '    background: rgba(10, 14, 24, 0.94); backdrop-filter: blur(10px); border: 1px solid var(--hair-2); border-radius: 12px; padding: 2px 0;\n'
     '    box-shadow: 0 18px 44px rgba(0, 0, 0, 0.5); display: none; }\n'
     '  .fpmenu.open { display: block; }\n'
     '  .fpmenu .mg { padding: 6px 0; } .fpmenu .mg + .mg { border-top: 1px solid var(--hair); }\n'
     '  .fpmenu .mg > button { display: flex; width: 100%; justify-content: space-between; align-items: baseline; gap: 18px;\n'
     '    background: none; border: 0; text-align: left; cursor: pointer; font: 12px var(--mono); color: var(--ink-2); padding: 6px 14px; }\n'
     '  .fpmenu .mg > button:hover { color: var(--ink); background: rgba(160, 185, 230, 0.06); }\n'
     '  .fpmenu .fpx { color: var(--ink-3); } .fpmenu .fpx:hover { color: var(--alarm); }\n'
     '  .fpmenu .fpsave { color: var(--accent); font-family: var(--sans); font-size: 12px; }\n')
sub1('      <button class="wfence" id="hwipe" title="Wipe the lines held on this screen — the server keeps its own">wipe</button>\n',
     '      <button class="wfence" id="hwipe" title="Wipe the lines held on this screen — the server keeps its own">wipe</button>\n'
     '      <button class="wfence" id="hcsv" title="The lines held on this screen, as a CSV file — 212 rows, every column">csv ↓</button>\n')

# =====================================================================
# 8. UPTIME — mikroview's own, beside its version in the account menu's
#    foot: the one line that already says what this is and which one.
# =====================================================================
sub1('About &amp; licence<span class="ver">0.9 · AGPL-3.0</span>',
     'About &amp; licence<span class="ver">0.9 · AGPL-3.0 · up 12 d 4 h</span>')
sub1('  .whomenu .theme {',
     '  .whomenu .ver { margin-left: 18px; font: 10.5px var(--mono); color: var(--ink-3); white-space: nowrap; }\n'
     '  .whomenu .theme {')

# ---- behaviour: the entities views, the saved-filters menu. Their
#      scenes come after the metrics script, so this runs at the end ----
JS = (
     "  document.querySelectorAll('#eviews span').forEach(function (s) {\n"
     "    s.addEventListener('click', function () {\n"
     "      var v = s.dataset.v;\n"
     "      document.querySelectorAll('#eviews span').forEach(function (x) { x.classList.toggle('on', x === s); });\n"
     "      document.querySelectorAll('#ent .etable.ev').forEach(function (t) { t.hidden = (t.id !== 'et-' + v); });\n"
     "      document.querySelectorAll('#ent .ev-hint').forEach(function (h) { h.hidden = (h.dataset.for !== v); });\n"
     "    });\n"
     "  });\n"
     "  (function () {\n"
     "    var b = document.getElementById('fsaved'), m = document.getElementById('fpmenu');\n"
     "    b.addEventListener('click', function (e) { e.stopPropagation(); var on = !m.classList.contains('open'); m.classList.toggle('open', on); b.classList.toggle('on', on); b.setAttribute('aria-expanded', on); });\n"
     "    document.addEventListener('click', function () { m.classList.remove('open'); b.classList.remove('on'); b.setAttribute('aria-expanded', 'false'); });\n"
     "  })();\n"
)
sub1('</script>\n</body>', '</script>\n<script>\n' + JS + '</script>\n</body>')

out.write_text(s)
print(out, len(s.splitlines()), 'lines')
