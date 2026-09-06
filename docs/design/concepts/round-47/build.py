#!/usr/bin/env python3
"""Round 47: the flags tab gains campaign rows, a scored confidence number
and a full-width by-type strip (#988). Built on round 35's
verdicts-in-row.html verbatim -- round 34's table with the trio in the
row, as the owner approved it on 2026-09-01 -- and nothing already liked
is redrawn. Reads round-35/verdicts-in-row.html, writes
round-47/campaigns.html.

The one data change: a seventh open flag, ACTIVITY SPIKE on cam-porch at
11:10, scored 72. It is there to prove the campaign rule's other half --
the same source as the campaign, but two hours from it, so its own row
-- and to carry the issue's own confidence example. The chrome's flag
count moves from 6 to 7 with it.
"""
import pathlib
import re

root = pathlib.Path(__file__).resolve().parent.parent
s = (root / 'round-35/verdicts-in-row.html').read_text()
out = root / 'round-47/campaigns.html'


def sub1(old, new, count=1):
    global s
    assert s.count(old) >= 1, old[:90]
    s = s.replace(old, new, count)


sub1('<title>Round 35', '<title>Round 47')
sub1('title="6 open flags — 4 alarms, 2 advisories">⚑ 6', 'title="7 open flags — 4 alarms, 3 advisories">⚑ 7')
sub1('all six keep their place', 'all seven keep their place')

# =====================================================================
# CSS -- one additive sheet, after round 35's last one
# =====================================================================
CSS = r'''
<style>
  /* ============================================================
     ROUND 47 — campaigns, the scored number, the by-type strip (#988).
     Additive to round 35; nothing above it is redrawn.
     ============================================================ */
  /* the strip and the table share one width, so the strip is exactly
     as wide as the table beneath it */
  .fwrap { width: 100%; max-width: 1520px; }

  /* --- flags by type: one cell per open type, across the whole width.
     The label carries identity (the type inks are round 30's family
     inks, a warm set that only the label tells apart); the bar is the
     count against the largest type's; click a cell to filter the table
     to that type, click again to clear. --- */
  .bytype { padding: 6px 18px 0; margin-bottom: 14px; }
  .bytype .btl { display: block; font: 600 9px var(--mono); letter-spacing: 0.14em; text-transform: uppercase; color: var(--ink-3); margin-bottom: 8px; }
  .bytype .btl b { color: var(--ink-2); font-weight: 600; }
  .btcells { display: flex; gap: 14px; }
  .btc { --ti: var(--ink-2); flex: 1 1 0; min-width: 0; background: transparent; border: 0; padding: 0; text-align: left; cursor: pointer; font: inherit; color: inherit; transition: opacity 0.18s; }
  .btc .btn { display: flex; justify-content: space-between; align-items: baseline; gap: 8px; font: 700 10.5px var(--mono); letter-spacing: 0.04em; text-transform: uppercase; color: var(--ti); white-space: nowrap; overflow: hidden; }
  .btc .btn i { font-style: normal; margin-right: 5px; }
  .btc .btn b { font: 600 12px var(--mono); color: var(--ink); font-variant-numeric: tabular-nums; letter-spacing: 0; }
  .btc .btbar { display: block; height: 3px; margin-top: 6px; border-radius: 2px; background: color-mix(in srgb, var(--ti) 16%, transparent); }
  .btc .btbar span { display: block; height: 100%; border-radius: 2px; background: var(--ti); transition: width 0.3s ease; }
  .btc:hover .btn { color: var(--ink); }
  .btc:hover .btn i, .btc.on .btn i { color: var(--ti); }
  .btc.on .btn { color: var(--ink); }
  .btc.on .btbar { background: color-mix(in srgb, var(--ti) 30%, transparent); box-shadow: 0 0 0 1px color-mix(in srgb, var(--ti) 40%, transparent); }
  .btcells.picked .btc:not(.on) { opacity: 0.38; }
  .btc.ft-unplanned { --ti: #ff5470; } .btc.ft-portscan { --ti: #ff9e64; } .btc.ft-outbound { --ti: #f072c8; }
  .btc.ft-repeat { --ti: #e0765a; } .btc.ft-surge { --ti: #e8b05a; } .btc.ft-talker { --ti: #b8c56a; }
  .btc:focus-visible { outline: 1px solid var(--accent); outline-offset: 4px; border-radius: 2px; }

  /* --- the scored number: beside the type, only where a detector
     scored it, and plainly visible (owner, 2026-09-06). No bands. --- */
  .fmark .conf { display: inline-block; font: 700 13px var(--mono); color: #ffffff; font-variant-numeric: tabular-nums; letter-spacing: 0; margin-left: 10px; padding: 0 7px; line-height: 18px; border-radius: 3px; background: rgba(233, 238, 251, 0.14); box-shadow: inset 0 0 0 1px rgba(233, 238, 251, 0.5); vertical-align: 1px; }
  tr.fdone .fmark .conf { color: inherit; box-shadow: none; background: transparent; padding: 0; }
  .story .scored { display: block; color: var(--ink-3); font-size: 11px; margin-top: 6px; }
  .story .scored b { color: var(--ink-2); font-weight: 600; }

  /* --- a campaign: one row for the flags one source raised inside one
     30-minute window. Its ink is the worst flag's; its FLAG cell names
     it; its EVIDENCE cell is one word per type inside, in that type's
     ink, then the span. It expands to its flags, indented beneath. --- */
  tr.camp .fmark .cn { font: 400 10.5px var(--mono); color: var(--ink-3); margin-left: 9px; letter-spacing: 0; text-transform: none; }
  tr.camp td.ev { white-space: nowrap; }
  .tchip { font-weight: 600; }
  .tchip.ft-unplanned { color: #ff5470; } .tchip.ft-portscan { color: #ff9e64; } .tchip.ft-outbound { color: #f072c8; }
  .tchip.ft-repeat { color: #e0765a; } .tchip.ft-surge { color: #e8b05a; } .tchip.ft-talker { color: #b8c56a; }
  .tchip + .tchip::before, .cspan::before { content: ' · '; color: var(--ink-3); font-weight: 400; }
  .cspan { color: var(--ink-3); }
  tr.camp .openc { transform: none; }
  tr.camp.open .openc { transform: rotate(90deg); }
  /* the members: the same flag rows, one step in, carried verbatim */
  tr.mem td:first-child { padding-left: 32px; }
  tr.mem td:first-child::after { content: ''; position: absolute; left: 16px; top: 50%; width: 8px; height: 1px; background: var(--hair-2); }
  /* the FLAG column is pinned at its resting width: opening the campaign never moves WHERE */
  .panel thead th:first-child { width: 208px; }
  /* the campaign mark: ⁂ is small in this face, so it gets its own size */
  tr.camp .fmark .cm { font-style: normal; font-size: 16px; line-height: 0; vertical-align: -2px; margin-right: 2px; }
  tr.mem td, tr.crule td { background: rgba(160, 185, 230, 0.025); }
  tr.mem.open td { background: rgba(160, 185, 230, 0.05); }
  tr.drawer.inc > td { background: rgba(160, 185, 230, 0.025); }
  /* why these are one campaign: one quiet line under the campaign row */
  tr.crule td { padding: 6px 18px 6px 32px; font: 10.5px var(--mono); color: var(--ink-3); border-bottom: 1px solid var(--hair); }
  tr.crule td b { color: var(--ink-2); font-weight: 600; }
  /* the ink line runs through the rule line, as it does through a drawer */
  tr.crule.ft-unplanned td { box-shadow: inset 3px 0 0 #ff5470; }
  tr.camp.open td { border-bottom-color: transparent; }
  tr.mem[hidden], tr.crule[hidden] { display: none; }
  /* a campaign called from its own row: the stamp reads for the set */
  tr.camp .vdone .by { color: var(--ink-3); }
  @media (prefers-reduced-motion: reduce) { .btc, .btc .btbar span { transition: none; } }
</style>
'''
sub1('</style>\n</head>\n<body>', '</style>\n' + CSS + '</head>\n<body>')

# =====================================================================
# THE STRIP, above the table -- and the wrap that gives the two one width
# =====================================================================
STRIP = '''<div class="fwrap">
    <div class="bytype" id="bytype" aria-label="Open flags by type — click a type to filter the table to it">
      <span class="btl">by type · <b id="btn-open">7</b> open</span>
      <div class="btcells" id="btcells">
        <button class="btc ft-unplanned" data-t="unplanned"><span class="btn"><span><i>✱</i>unplanned</span><b>1</b></span><span class="btbar"><span style="width:100%"></span></span></button>
        <button class="btc ft-portscan" data-t="port scan"><span class="btn"><span><i>✱</i>port scan</span><b>1</b></span><span class="btbar"><span style="width:100%"></span></span></button>
        <button class="btc ft-outbound" data-t="outbound"><span class="btn"><span><i>✱</i>outbound</span><b>1</b></span><span class="btbar"><span style="width:100%"></span></span></button>
        <button class="btc ft-repeat" data-t="repeated drops"><span class="btn"><span><i>✱</i>repeated drops</span><b>1</b></span><span class="btbar"><span style="width:100%"></span></span></button>
        <button class="btc ft-surge" data-t="drop surge"><span class="btn"><span><i>▲</i>drop surge</span><b>1</b></span><span class="btbar"><span style="width:100%"></span></span></button>
        <button class="btc ft-surge" data-t="activity spike"><span class="btn"><span><i>▲</i>activity spike</span><b>1</b></span><span class="btbar"><span style="width:100%"></span></span></button>
        <button class="btc ft-talker" data-t="new talker"><span class="btn"><span><i>▲</i>new talker</span><b>1</b></span><span class="btbar"><span style="width:100%"></span></span></button>
      </div>
    </div>
    <table>
      <thead><tr><th>flag</th><th>where</th><th>evidence</th><th>count</th><th>age</th><th class="vc">call it</th></tr></thead>'''
sub1('''  <div class="panel" id="p-flags" aria-label="Open flags, newest first">
    <table>
      <thead><tr><th>flag</th><th>where</th><th>evidence</th><th>count</th><th>age</th><th class="vc">call it</th></tr></thead>''',
     '  <div class="panel" id="p-flags" aria-label="Open flags, newest first">\n    ' + STRIP)
# close the wrap after the flags table
sub1('''    </table>
    <div class="caempty" id="caempty">''', '''    </table>
    </div>
    <div class="caempty" id="caempty">''')

# =====================================================================
# THE ROWS -- the campaign, its members, the two scored rows
# =====================================================================
TRIO = '<td class="vc"><span class="vrow"><button class="v expected" data-v="expected"><i>✓</i>expected</button><button class="v noise" data-v="noise"><i>~</i>noise</button><button class="v real" data-v="real"><i>✱</i>real</button></span><span class="openc">▸</span></td>'

# lift round 35's six flag rows (row + drawer) out of the tbody by id
body_start = s.index('<tbody>\n        <tr class="frow ft-unplanned open" data-d="d1">')
body_end = s.index('      </tbody>\n      <tbody id="excl">')
body = s[body_start:body_end]
rows = {}
for m in re.finditer(r'        <tr class="frow[^\n]*data-d="(d\d)"[^\n]*\n(?:.*\n)*?        </div></div></td></tr>\n', body):
    rows[m.group(1)] = m.group(0)
assert sorted(rows) == ['d1', 'd2', 'd3', 'd4', 'd5', 'd6'], sorted(rows)

# the first drawer no longer opens at rest: the campaign is collapsed
rows['d1'] = rows['d1'].replace('<tr class="frow ft-unplanned open" data-d="d1">', '<tr class="frow ft-unplanned" data-d="d1">') \
                       .replace('<tr class="drawer open ft-unplanned" id="d1">', '<tr class="drawer ft-unplanned" id="d1">')


def member(r, c):
    """a campaign member: the same row, one step in, hidden until the campaign opens"""
    r = re.sub(r'<tr class="frow ([^"]*)" data-d="(d\d)">', r'<tr class="frow mem in-' + c + r' \1" data-d="\2" hidden>', r, count=1)
    r = re.sub(r'<tr class="drawer ([^"]*)" id="(d\d)">', r'<tr class="drawer inc in-' + c + r' \1" id="\2">', r, count=1)
    return r


campaign = ('        <tr class="frow camp ft-unplanned" data-c="c1" aria-label="Campaign: three flags from cam-porch inside one 30-minute window">'
            '<td class="fmark"><i class="cm">⁂</i> CAMPAIGN<span class="cn">3 flags</span></td>'
            '<td class="k"><a class="wl" href="#s3" onclick="descend()">cam-porch</a> · 10.0.20.14</td>'
            '<td class="ev"><span class="tchip ft-unplanned">unplanned</span><span class="tchip ft-outbound">outbound</span><span class="tchip ft-repeat">repeated drops</span><span class="cspan">13:28 → still arriving</span></td>'
            '<td class="num">26×</td><td class="t">24 m</td>' + TRIO + '</tr>\n'
            '        <tr class="crule in-c1 ft-unplanned" hidden><td colspan="6">one source, three flags, each inside 30 minutes of the last — <b>one campaign</b>. cam-porch\'s ACTIVITY SPIKE at 11:10 is two hours from these, so it keeps its own row.</td></tr>\n')

# the drop surge is scored: the number beside the type, and the line that says where it came from
d5 = rows['d5']
d5 = d5.replace('<td class="fmark">▲ DROP SURGE</td>', '<td class="fmark">▲ DROP SURGE<span class="conf" title="scored 71 of 100 by the detector">71</span></td>', 1)
d5 = d5.replace('Nothing inside answered anything.</div>',
                'Nothing inside answered anything.<span class="scored"><b>Scored 71.</b> How far this hour sits from the wan boundary\'s usual, and how much history backs that — 14 days here. The detector\'s number, not a verdict.</span></div>', 1)
d5 = d5.replace('baseline dashed · climbing since 13:20', 'baseline dashed · climbing since 13:20 · scored 71', 1)
assert d5.count('71') == 4, d5.count('71')

# the seventh flag: cam-porch's activity spike, two hours before its campaign
d7 = ('        <tr class="frow ft-surge" data-d="d7"><td class="fmark">▲ ACTIVITY SPIKE<span class="conf" title="scored 72 of 100 by the detector">72</span></td>'
      '<td class="k"><a class="wl" href="#s3" onclick="descend()">cam-porch</a> · 10.0.20.14</td><td>4.2× its usual hour, 11:10 → 11:50</td>'
      '<td class="num">—</td><td class="t">2 h 40 m</td>' + TRIO + '</tr>\n'
      '        <tr class="drawer ft-surge" id="d7"><td colspan="6"><div class="dwr"><div class="dwr-in">\n'
      '          <div class="story"><b>cam-porch was 4.2× busier than its usual late morning.</b> Between 11:10 and 11:50 the camera sent four times what it sends in that hour on an ordinary day, all of it accepted, most of it to its own cloud. It settled by noon; the campaign above began at 13:28, two hours later.'
      '<span class="scored"><b>Scored 72.</b> How far the hour sits from cam-porch\'s own usual, and how much history backs that — 14 days here. The detector\'s number, not a verdict.</span></div>\n'
      '          <div class="side"><span class="lab">the hour against its baseline</span>\n'
      '            <svg viewBox="0 0 260 34" preserveAspectRatio="none"><polyline points="0,27 30,26 60,18 90,9 120,7 150,9 180,14 210,22 240,26 260,26" fill="none" stroke="#e8b05a" stroke-width="1.6"/><polyline points="0,27 260,26" fill="none" stroke="rgba(160,185,230,0.35)" stroke-width="1" stroke-dasharray="3 4"/></svg>\n'
      '            <span class="span">baseline dashed · 11:10 → 11:50 · settled · scored 72</span></div>\n'
      '          <div class="lines">rate: 6/min → 25/min · top peer 203.0.113.20:443 · all accepted</div>\n'
      '          <div class="dwr-acts"><button>open in metrics ▸</button><button class="quiet">clear with a note</button><button class="quiet never">never again</button></div>\n'
      '        </div></div></td></tr>\n')

new_body = ('<tbody>\n' + campaign + member(rows['d1'], 'c1') + '\n' + member(rows['d3'], 'c1') + '\n' + member(rows['d4'], 'c1') + '\n'
            + rows['d2'] + '\n' + d5 + '\n' + rows['d6'] + '\n' + d7)
s = s[:body_start] + new_body + s[body_end:]

# the port scan's story gains nothing; the repeated-drops story already
# calls UNPLANNED its sibling, which the campaign now shows in place.

# =====================================================================
# JS -- the campaign opens and is called; the strip counts and filters;
#       sort and filter treat a campaign and its flags as one group
# =====================================================================
# 1. groups(): a campaign's rule line, members and their drawers travel with it
sub1('''      for (var i = 0; i < rows.length; i++) {
        if (rows[i].classList.contains('drawer')) continue;
        var g = [rows[i]];
        if (rows[i + 1] && rows[i + 1].classList.contains('drawer')) g.push(rows[i + 1]);
        out.push(g);
      }''', '''      for (var i = 0; i < rows.length; i++) {
        if (rows[i].classList.contains('drawer') || rows[i].classList.contains('mem') || rows[i].classList.contains('crule')) continue;
        var g = [rows[i]];
        for (var j = i + 1; j < rows.length && (rows[j].classList.contains('drawer') || rows[j].classList.contains('mem') || rows[j].classList.contains('crule')); j++) g.push(rows[j]);
        out.push(g);
      }''')
# 2. filter: a campaign shows when it matches, or when any flag inside it does — then opened to just those
sub1('''        groups().forEach(function (g) {
          var cells = g[0].cells;
          var show = terms.every(function (t, j) {
            return !t || (cells[j] && cells[j].textContent.toLowerCase().indexOf(t) !== -1);
          });
          g.forEach(function (r) { r.style.display = show ? '' : 'none'; });
        });''', '''        function hit(row) {
          var cells = row.cells;
          return terms.every(function (t, j) { return !t || (cells[j] && cells[j].textContent.toLowerCase().indexOf(t) !== -1); });
        }
        var any = terms.some(function (t) { return t; });
        groups().forEach(function (g) {
          if (g[0].classList.contains('camp')) {
            var mems = g.filter(function (r) { return r.classList.contains('mem'); });
            var own = hit(g[0]), hits = mems.filter(hit);
            var show = own || hits.length > 0;
            g[0].style.display = show ? '' : 'none';
            if (any && !own) {
              // only the flags that match, and the campaign opened to show them
              g[0].classList.add('open');
              g.forEach(function (r) { if (r === g[0]) return; r.style.display = 'none'; });
              hits.forEach(function (r) { r.hidden = false; r.style.display = ''; var d = document.getElementById(r.dataset.d); d.style.display = ''; });
            } else {
              g.forEach(function (r) { if (r === g[0]) return; r.style.display = show ? '' : 'none'; });
              if (any) return;
              if (window.setCampaign) window.setCampaign(g[0], g[0].dataset.wasOpen === '1');
            }
            return;
          }
          var show2 = hit(g[0]);
          g.forEach(function (r) { r.style.display = show2 ? '' : 'none'; });
        });''')
# 3. the flag count in the chrome counts flags, never the campaign row that holds them
sub1("function fcount() { var n = flags.querySelectorAll('tr.frow:not(.fdone)').length; fmk.textContent = '⚑ ' + n; }",
     "function fcount() { var n = flags.querySelectorAll('tr.frow:not(.camp):not(.fdone)').length; fmk.textContent = '⚑ ' + n; if (window.strip) window.strip(); }")
# 4. the row-click guard: a campaign row has no drawer of its own; it opens its flags
sub1('''  document.querySelectorAll('tr.frow[data-d]').forEach(function (r) {
    r.addEventListener('click', function (e) {
      if (e.target.closest('a, button')) return; // where-links navigate and pills act; neither toggles
      var d = document.getElementById(r.dataset.d);''', '''  // ---- round 47: a campaign row opens to its flags ----
  function setCampaign(c, open) {
    c.classList.toggle('open', open);
    c.dataset.wasOpen = open ? '1' : '0';
    document.querySelectorAll('tr.in-' + c.dataset.c).forEach(function (r) {
      if (r.classList.contains('drawer')) { if (!open) { r.classList.remove('open'); var m = r.previousElementSibling; if (m) m.classList.remove('open'); } return; }
      r.hidden = !open;
    });
  }
  window.setCampaign = setCampaign;
  document.querySelectorAll('tr.frow.camp').forEach(function (c) {
    c.addEventListener('click', function (e) {
      if (e.target.closest('a, button')) return;
      setCampaign(c, !c.classList.contains('open'));
    });
  });
  document.querySelectorAll('tr.frow[data-d]').forEach(function (r) {
    r.addEventListener('click', function (e) {
      if (e.target.closest('a, button')) return; // where-links navigate and pills act; neither toggles
      var d = document.getElementById(r.dataset.d);''')

# 5. the campaign's own trio calls every flag inside it; the strip counts and filters
STRIPJS = r'''
  // ---- round 47: the campaign's trio, and the by-type strip ----
  (function () {
    var flags = document.querySelector('#p-flags tbody');
    var TRIO = '<span class="vrow"><button class="v expected" data-v="expected"><i>✓</i>expected</button><button class="v noise" data-v="noise"><i>~</i>noise</button><button class="v real" data-v="real"><i>✱</i>real</button></span><span class="openc">▸</span>';
    function members(c) { return Array.prototype.filter.call(flags.querySelectorAll('tr.mem.in-' + c.dataset.c), function (r) { return true; }); }
    function restore(c) { c.classList.remove('fdone', 'struck', 'isreal', 'expected', 'noise', 'real'); c.cells[5].innerHTML = TRIO; }
    flags.querySelectorAll('tr.frow.camp').forEach(function (c) {
      c.addEventListener('click', function (e) {
        var b = e.target.closest('button.v'); if (!b) return;
        e.stopPropagation();
        var v = b.dataset.v, mems = members(c);
        // every flag inside takes the same call, through its own chip, so each dims or stamps exactly as it would alone
        mems.forEach(function (m) { var mb = m.querySelector('button.v[data-v="' + v + '"]'); if (mb) mb.click(); });
        c.classList.remove('struck', 'expected', 'noise', 'real'); void c.offsetWidth;
        c.classList.add('struck', v);
        if (v === 'real') c.classList.add('isreal'); else c.classList.add('fdone');
        var cell = c.cells[5];
        cell.innerHTML = '<span class="vdone"><span class="stamp ' + v + '">' + v + '</span><span class="by">all ' + mems.length + '</span><a>undo</a></span><span class="openc">▸</span>';
        cell.querySelector('a').addEventListener('click', function (e) {
          e.stopPropagation();
          mems.forEach(function (m) { var u = m.querySelector('.vdone a'); if (u) u.click(); });
          restore(c);
        });
        if (v !== 'real') setCampaign(c, false);
      });
    });
    // a flag inside undone on its own: the campaign's stamp no longer reads for the set
    flags.addEventListener('click', function (e) {
      var u = e.target.closest('tr.mem .vdone a'); if (!u) return;
      var m = u.closest('tr.mem'), c = flags.querySelector('tr.camp[data-c="' + m.className.match(/in-(c\d)/)[1] + '"]');
      if (c && c.querySelector('.vdone')) setTimeout(function () { restore(c); }, 0);
    });

    // the strip: what the table holds, by type — recounted whenever a flag is called or put back
    var ORDER = ['unplanned', 'port scan', 'outbound', 'repeated drops', 'drop surge', 'activity spike', 'new talker'];
    var MARK = { 'unplanned': '✱', 'port scan': '✱', 'outbound': '✱', 'repeated drops': '✱', 'drop surge': '▲', 'activity spike': '▲', 'new talker': '▲' };
    var FT = { 'unplanned': 'ft-unplanned', 'port scan': 'ft-portscan', 'outbound': 'ft-outbound', 'repeated drops': 'ft-repeat', 'drop surge': 'ft-surge', 'activity spike': 'ft-surge', 'new talker': 'ft-talker' };
    var cells = document.getElementById('btcells'), picked = null;
    var finput = document.querySelector('#p-flags thead tr.filters input');
    function typeOf(r) { return r.querySelector('.fmark').firstChild.textContent.trim().replace(/^[✱▲] /, '').toLowerCase(); }
    function strip() {
      var counts = {}, total = 0;
      flags.querySelectorAll('tr.frow:not(.camp):not(.fdone)').forEach(function (r) { var t = typeOf(r); counts[t] = (counts[t] || 0) + 1; total++; });
      var types = ORDER.filter(function (t) { return counts[t]; });
      var max = Math.max.apply(null, types.map(function (t) { return counts[t]; }).concat([1]));
      types.sort(function (a, b) { return counts[b] - counts[a] || ORDER.indexOf(a) - ORDER.indexOf(b); });
      document.getElementById('btn-open').textContent = total;
      cells.innerHTML = '';
      types.forEach(function (t) {
        var b = document.createElement('button');
        b.className = 'btc ' + FT[t] + (picked === t ? ' on' : ''); b.dataset.t = t;
        b.innerHTML = '<span class="btn"><span><i>' + MARK[t] + '</i></span><b></b></span><span class="btbar"><span></span></span>';
        b.querySelector('.btn > span').appendChild(document.createTextNode(t));
        b.querySelector('.btn b').textContent = counts[t];
        b.querySelector('.btbar span').style.width = Math.round(100 * counts[t] / max) + '%';
        cells.appendChild(b);
      });
      if (picked && !counts[picked]) pick(null);
      cells.classList.toggle('picked', !!picked);
      document.getElementById('bytype').style.display = total ? '' : 'none';
    }
    function pick(t) {
      picked = (picked === t) ? null : t;
      cells.querySelectorAll('.btc').forEach(function (b) { b.classList.toggle('on', b.dataset.t === picked); });
      cells.classList.toggle('picked', !!picked);
      finput.value = picked || '';
      finput.dispatchEvent(new Event('input'));
    }
    cells.addEventListener('click', function (e) { var b = e.target.closest('.btc'); if (b) pick(b.dataset.t); });
    // typing in the FLAG filter unpicks a cell that no longer matches what is typed
    finput.addEventListener('input', function () { if (picked && finput.value.toLowerCase() !== picked) { picked = null; cells.querySelectorAll('.btc').forEach(function (b) { b.classList.remove('on'); }); cells.classList.remove('picked'); } });
    window.strip = strip;
    strip();
  })();
</script>
</body>'''
sub1('</script>\n</body>', STRIPJS)

out.write_text(s)
print(out, len(s))
