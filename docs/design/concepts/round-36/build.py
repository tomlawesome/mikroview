#!/usr/bin/env python3
"""Round 36: the chrome sweep — every unmounted caption, note and control
from #691 item 6 that carries information or an ability, given a home in
round 30's own grammar. Built on round 30 verbatim.
Reads round-30/the-whole.html, writes round-36/the-whole.html."""
import pathlib

root = pathlib.Path.home() / 'projects/mikroview/docs/design/concepts'
src = (root / 'round-30/the-whole.html').read_text()
out = root / 'round-36/the-whole.html'
out.parent.mkdir(exist_ok=True)
s = src


def sub1(old, new, count=1):
    global s
    assert s.count(old) >= 1, old[:80]
    s = s.replace(old, new, count)


sub1('<title>Round 30', '<title>Round 36')

# =====================================================================
# 1. THE FALL — three statements the built fall makes and round 30 did
#    not draw (#691 item 6.1): an empty band says so, a band with more
#    carriers than it shows counts the rest, and a capped window admits
#    the cap. All three are #445 honesty statements, so they wear the
#    fall's existing statement inks: quiet in ink-3, never the dark red.
# =====================================================================

# 1a. the window cap: a third chip in the fall's head, dim, only when the
#     window holds more than the fall was given (34/s over 15 m does)
sub1('<span class="fall-chip fc-dim">○ 1 dark boundary — nothing logged</span>',
     '<span class="fall-chip fc-dim">○ 1 dark boundary — nothing logged</span>\n'
     '      <span class="fall-chip fc-dim">○ the most recent 5 000 events — this window holds more</span>')

# 1b. an empty band: wg1 → bridge1 is "quiet by choice" with a watch
#     holding, so it is empty here — round 30's single faint dash goes,
#     and the band says what empty means: logged, and nothing seen.
sub1('          <line class="carrier" x1="1020" y1="300" x2="1020" y2="330" stroke="var(--fall-accept)" stroke-width="1.4" stroke-dasharray="2 26" opacity="0.6"/>\n'
     '          <text x="1020" y="558" text-anchor="middle" class="port-l">:51820</text>\n',
     '          <text x="1020" y="330" text-anchor="middle" class="quiet-t">nothing in these 15 m</text>\n'
     '          <text x="1020" y="344" text-anchor="middle" class="quiet-t">logged — quiet, not dark</text>\n')

# 1c. the quieter count: lan → ether1 is the household and carries more
#     ports than the eight the band shows; the rest are counted beneath
#     the port labels, and the count is a way into the stream
sub1('          <text x="892" y="558" text-anchor="middle" class="port-l">:853</text>\n',
     '          <text x="892" y="558" text-anchor="middle" class="port-l">:853</text>\n'
     '          <a href="#s5"><text x="870" y="574" text-anchor="middle" class="quieter">+6 quieter ▸</text></a>\n')
# room beneath the floor for that line
sub1('<svg viewBox="0 0 1400 560" preserveAspectRatio="xMidYMid meet"\n           aria-label="Nine boundary bands',
     '<svg viewBox="0 0 1400 580" preserveAspectRatio="xMidYMid meet"\n           aria-label="Nine boundary bands')
sub1('  .dark-t { font: 9.5px var(--mono); fill: var(--fall-drop); }\n',
     '  .dark-t { font: 9.5px var(--mono); fill: var(--fall-drop); }\n'
     '  /* round 36: quiet is a fact, not a fault — the empty band states it in the dim ink, never the dark red */\n'
     '  .quiet-t { font: 9.5px var(--mono); fill: var(--ink-3); }\n'
     '  .quieter { font: 9.5px var(--mono); fill: var(--ink-3); cursor: pointer; }\n'
     '  .quieter:hover { fill: var(--accent); }\n')

# =====================================================================
# 2. THE TOPOGRAPHY — the degraded note (#691 item 6.4). When no
#    /ip address table has been pushed the zones are boundary-derived
#    and every CIDR slot is unknown. The fact lives where the router's
#    other pushed-table facts live (its own card), and each zone card's
#    address slot says honestly what it holds instead of a guess. A
#    state, not furniture: `#s3.degraded` shows it; the resting map is
#    round 30's, untouched.
# =====================================================================
sub1('          <rect class="isl" x="-128" y="-34" width="256" height="68" rx="12" stroke="rgba(157,184,232,0.4)"/>\n'
     '          <text x="-110" y="-6" class="n-name">rb5009</text>\n'
     '          <text x="-110" y="12" class="n-sub">RouterOS 7.20.1 · the waist · 41 rules</text>\n',
     '          <rect class="isl rt-isl" x="-128" y="-34" width="256" height="68" rx="12" stroke="rgba(157,184,232,0.4)"/>\n'
     '          <text x="-110" y="-6" class="n-name">rb5009</text>\n'
     '          <text x="-110" y="12" class="n-sub">RouterOS 7.20.1 · the waist · 41 rules</text>\n'
     '          <text x="-110" y="34" class="deg-t">no address table pushed — zones from boundaries</text>\n'
     '          <text x="-110" y="50" class="deg-t"><a href="#set"><tspan class="deg-go">Run setup… ▸</tspan></a> adds it</text>\n')
for cidr in ('10.0.10.0/24', '10.0.40.0/24', '10.0.20.0/24', '10.0.30.0/24'):
    sub1('<text x="22" y="26" class="n-cidr">%s</text>' % cidr,
         '<text x="22" y="26" class="n-cidr"><tspan class="cidr-v">%s</tspan><tspan class="cidr-deg">from boundaries</tspan></text>' % cidr)
# the WAN and WireGuard addresses come from the same table, so they are unknown too
sub1('<text x="-82" y="14" class="n-cidr">ether1 · 203.0.113.7</text>',
     '<text x="-82" y="14" class="n-cidr">ether1<tspan class="cidr-v"> · 203.0.113.7</tspan><tspan class="cidr-deg"> · no address pushed</tspan></text>')
sub1('<text x="-66" y="14" class="n-cidr">wg0 · 10.99.0.0/24</text>',
     '<text x="-66" y="14" class="n-cidr">wg0<tspan class="cidr-v"> · 10.99.0.0/24</tspan><tspan class="cidr-deg"> · no address pushed</tspan></text>')
sub1('  .n-cidr { font: 11px var(--mono); fill: var(--ink-3); }\n',
     '  .n-cidr { font: 11px var(--mono); fill: var(--ink-3); }\n'
     '  /* round 36: the degraded map. One statement on the router card; each zone\'s address slot\n'
     '     holds what it truly holds. #s3.degraded is the state; at rest none of this exists */\n'
     '  .deg-t, .cidr-deg { display: none; }\n'
     '  #s3.degraded .deg-t { display: initial; font: 10px var(--mono); fill: var(--ink-3); }\n'
     '  #s3.degraded .deg-go { fill: var(--accent); }\n'
     '  #s3.degraded .rt-isl { height: 100px; }\n'
     '  #s3.degraded .cidr-v { display: none; }\n'
     '  #s3.degraded .cidr-deg { display: initial; font-style: italic; }\n')

# =====================================================================
# 3. METRICS — the cross-section and the ledger (#691 items 6.9, 6.10).
#    The cross-section's job — read the minute under the cursor across
#    every series — is the hourline's job, so the hourline reads every
#    series and no aside is drawn. The ledger — the hour in totals,
#    magnitude not time — is the table view's foot: the register keeps
#    time, the table owns magnitude, one home for it.
# =====================================================================
sub1('    <span class="fact"><b>9</b> refused of <b>61</b> events</span><span class="sep">·</span>\n'
     '    <span class="fact"><b>2</b> flag episodes</span>\n',
     '    <span class="fact"><b>52</b> accepted</span><span class="sep">·</span>\n'
     '    <span class="fact ref"><b>9</b> refused</span><span class="sep">·</span>\n'
     '    <span class="fact"><b>6</b> natted</span><span class="sep">·</span>\n'
     '    <span class="fact"><b>2</b> flag episodes — unplanned · ring broken</span>\n')
sub1('  .hourline .fact b { color: var(--ink-2); font-weight: 600; }\n',
     '  .hourline .fact b { color: var(--ink-2); font-weight: 600; }\n'
     '  .hourline .fact.ref b { color: var(--fall-drop); }\n')

LEDGER = '''  <div class="ledger" aria-label="The ledger: the same hour in totals — magnitude, not time">
    <div class="lcol"><h4>top rules</h4>
      <div class="lrow"><span class="l">nat-wan</span><span class="tr"><span class="b" style="width:100%"></span></span><span class="c">3 018</span></div>
      <div class="lrow"><span class="l">lan-to-srv</span><span class="tr"><span class="b" style="width:74%"></span></span><span class="c">2 240</span></div>
      <div class="lrow"><span class="l">wan-in-drop</span><span class="tr"><span class="b ref" style="width:58%"></span></span><span class="c">1 754</span></div>
      <div class="lrow"><span class="l">iot-egress-ntp</span><span class="tr"><span class="b" style="width:21%"></span></span><span class="c">631</span></div>
      <div class="lrow"><span class="l">iot-to-lan-drop</span><span class="tr"><span class="b ref" style="width:5%"></span></span><span class="c">140</span></div>
    </div>
    <div class="lcol"><h4>top talkers</h4>
      <div class="lrow"><span class="l">tom-desktop</span><span class="tr"><span class="b" style="width:100%"></span></span><span class="c">9 812</span></div>
      <div class="lrow"><span class="l">phone-tom</span><span class="tr"><span class="b" style="width:67%"></span></span><span class="c">6 590</span></div>
      <div class="lrow"><span class="l">pihole</span><span class="tr"><span class="b" style="width:54%"></span></span><span class="c">5 302</span></div>
      <div class="lrow"><span class="l">laptop-anna</span><span class="tr"><span class="b" style="width:41%"></span></span><span class="c">4 011</span></div>
      <div class="lrow"><span class="l">cam-porch</span><span class="tr"><span class="b ref" style="width:2%"></span></span><span class="c">140</span></div>
    </div>
    <div class="lcol"><h4>by device</h4>
      <div class="lrow"><span class="l">rb5009</span><span class="tr"><span class="b" style="width:100%"></span></span><span class="c">48 882</span></div>
      <div class="lrow dim"><span class="l">one router — every event is its</span></div>
    </div>
    <div class="lcol"><h4>by protocol</h4>
      <div class="split"><span style="flex:31" class="tcp"></span><span style="flex:17" class="udp"></span><span style="flex:1" class="icmp"></span></div>
      <div class="lrow"><span class="l"><i class="tcp"></i>tcp</span><span class="c">31 240</span></div>
      <div class="lrow"><span class="l"><i class="udp"></i>udp</span><span class="c">17 011</span></div>
      <div class="lrow"><span class="l"><i class="icmp"></i>icmp</span><span class="c">631</span></div>
    </div>
    <div class="lcol"><h4>the hour by action</h4>
      <div class="split"><span style="flex:45120" class="acc"></span><span style="flex:3762" class="refd"></span><span style="flex:3018" class="natd"></span></div>
      <div class="lrow"><span class="l"><i class="acc"></i>accepted</span><span class="c">45 120</span></div>
      <div class="lrow"><span class="l"><i class="refd"></i>refused</span><span class="c">3 762</span></div>
      <div class="lrow"><span class="l"><i class="natd"></i>natted</span><span class="c">3 018</span></div>
    </div>
    <div class="lcol"><h4>episodes by flag type</h4>
      <div class="lrow"><span class="l">unplanned</span><span class="tr"><span class="b alarm" style="width:100%"></span></span><span class="c">2</span></div>
      <div class="lrow"><span class="l">ring broken</span><span class="tr"><span class="b alarm" style="width:50%"></span></span><span class="c">1</span></div>
    </div>
  </div>
'''
sub1('    <tfoot><tr><th scope="row">hour total</th><td>45 120</td><td class="ref">3 762</td><td class="nat">3 018</td><td>3</td><td class="t">:443</td><td class="t">tom-desktop</td></tr></tfoot>\n'
     '  </table>\n</div>\n',
     '    <tfoot><tr><th scope="row">hour total</th><td>45 120</td><td class="ref">3 762</td><td class="nat">3 018</td><td>3</td><td class="t">:443</td><td class="t">tom-desktop</td></tr></tfoot>\n'
     '  </table>\n' + LEDGER + '</div>\n')
sub1('  .mtable { position: absolute; inset: 150px 24px 40px; display: flex; justify-content: center; align-items: flex-start; overflow: hidden; }\n',
     '  .mtable { position: absolute; inset: 150px 24px 40px; display: flex; flex-direction: column; align-items: center; gap: 26px; overflow: hidden; }\n'
     '  /* round 36: the ledger is the table view\'s foot — the same hour in totals, magnitude not time.\n'
     '     Bars, no boxes; each column a ranked answer, the split columns a whole cut into its parts */\n'
     '  .ledger { display: grid; grid-template-columns: repeat(6, 1fr); gap: 28px; width: 92%; max-width: 1480px; padding-top: 18px; border-top: 1px solid var(--hair-2); }\n'
     '  .ledger h4 { margin: 0 0 8px; font: 600 9.5px var(--mono); letter-spacing: 0.12em; text-transform: uppercase; color: var(--ink-3); }\n'
     '  .lrow { display: flex; align-items: center; gap: 8px; font: 11px var(--mono); color: var(--ink-2); padding: 2px 0; }\n'
     '  .lrow .l { flex: 0 0 auto; min-width: 0; max-width: 55%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }\n'
     '  .lrow .tr { flex: 1; height: 5px; background: var(--hair); border-radius: 3px; overflow: hidden; }\n'
     '  .lrow .b { display: block; height: 100%; background: var(--accent); opacity: 0.7; }\n'
     '  .lrow .b.ref { background: var(--fall-drop); } .lrow .b.alarm { background: var(--alarm); }\n'
     '  .lrow .c { margin-left: auto; color: var(--ink); font-weight: 600; font-variant-numeric: tabular-nums; }\n'
     '  .lrow.dim { color: var(--ink-3); font-size: 10px; } .lrow.dim .l { max-width: none; }\n'
     '  .lrow i { display: inline-block; width: 7px; height: 7px; border-radius: 2px; margin-right: 6px; vertical-align: 0; }\n'
     '  .ledger .split { display: flex; height: 6px; border-radius: 3px; overflow: hidden; margin: 2px 0 8px; gap: 1px; }\n'
     '  .ledger .tcp { background: var(--accent); } .ledger .udp { background: var(--lan); } .ledger .icmp { background: var(--ink-3); }\n'
     '  .ledger .acc { background: var(--fall-accept); } .ledger .refd { background: var(--fall-drop); } .ledger .natd { background: var(--nat); }\n')

# =====================================================================
# 4. THE STREAM — the hand (#691 item 6.13, and #749's cause). The
#    whisper commands the stream and its seek is what stops the lines
#    following, so the verbs sit on the whisper's own line, right of its
#    facts: follow · pause · group in the spans' segmented idiom, and
#    wipe as a quiet pill. The stat line keeps to facts — the rate, the
#    drop share, the top talker, and how far back the server's ring
#    reaches (item 6.13's "buffer %"); `autoscroll on` leaves the prose
#    and becomes the `following` verb. Column resize (item 6.8): no
#    handle at rest; the column boundary reveals itself under the hand.
# =====================================================================
sub1('      <span class="wstat" id="wstat"><b class="k">34/s</b> now · <b class="r">drops 26%</b> · top talker <b class="k">cam-porch</b> · autoscroll <b class="k" id="wauto">on</b></span>\n'
     '    </div>\n',
     '      <span class="wstat" id="wstat"><b class="k">34/s</b> now · <b class="r">drops 26%</b> · top talker <b class="k">cam-porch</b> · ring holds <b class="k">41 m</b></span>\n'
     '      <span class="spans hand" role="group" aria-label="The stream\'s hand">'
     '<span class="on" id="hfollow" role="button" tabindex="0" title="Follow the newest line as it arrives — off while you are reading back">following</span>'
     '<span id="hpause" role="button" tabindex="0" title="Hold the lines where they are; what arrives waits, counted">pause</span>'
     '<span id="hgroup" role="button" tabindex="0" title="Fold repeats of the same line into one, with a count">group</span></span>\n'
     '      <button class="wfence" id="hwipe" title="Wipe the lines held on this screen — the server keeps its own">wipe</button>\n'
     '    </div>\n')
sub1('  .wfence.on { color: var(--now); border-color: var(--now); }\n',
     '  .wfence.on { color: var(--now); border-color: var(--now); }\n'
     '  /* round 36: the hand — the stream\'s three toggles in the spans\' own idiom, and wipe as a quiet pill */\n'
     '  .whisper .hand span { cursor: pointer; }\n'
     '  .whisper .hand span:hover { color: var(--ink); }\n'
     '  .whisper .hand .on { color: var(--accent); border-color: rgba(167, 139, 250, 0.45); }\n'
     '  .whisper .hand #hpause.on { color: var(--now); border-color: rgba(232, 176, 90, 0.5); }\n'
     '  /* held is a state worth noticing: the way back wears the now ink until it is taken */\n'
     '  .whisper .hand #hfollow:not(.on) { color: var(--now); border: 1px solid rgba(232, 176, 90, 0.5); }\n'
     '  table.stream tr.gcount td:last-child::after { content: " ×" attr(data-n); color: var(--ink-3); margin-left: 8px; }\n'
     '  table.stream tr.wiped td { text-align: center; color: var(--ink-3); padding: 28px 10px; font-family: var(--mono); font-size: 11px; }\n'
     '  /* column resize: nothing drawn at rest; the boundary shows itself under the hand and the cursor says what it does */\n'
     '  table.stream th { position: relative; }\n'
     '  table.stream th::after { content: ""; position: absolute; right: -3px; top: 5px; bottom: 5px; width: 6px; cursor: col-resize; border-right: 1px solid transparent; transition: border-color 0.15s; }\n'
     '  table.stream th:hover::after, table.stream th.grip::after { border-right-color: var(--hair-2); }\n')

# the whisper's seek turns following off in the mockup's own prose: the
# word now lives on the pill, so the stat says where you went and the pill
# says the stream is held
sub1("        stat.innerHTML = '<b class=\"k\">34/s</b> now · <b class=\"r\">drops 26%</b> · top talker <b class=\"k\">cam-porch</b> · autoscroll <b class=\"k\">on</b>'; }",
     "        stat.innerHTML = '<b class=\"k\">34/s</b> now · <b class=\"r\">drops 26%</b> · top talker <b class=\"k\">cam-porch</b> · ring holds <b class=\"k\">41 m</b>'; setFollow(true); }")
sub1("          stat.innerHTML = '<b class=\"k\">fenced ' + hm(T0 + a * (T1 - T0)) + '–' + hm(T0 + b * (T1 - T0)) + '</b> · <b class=\"r\">drops 31%</b> in the fence · autoscroll <b class=\"k\">off</b>';",
     "          stat.innerHTML = '<b class=\"k\">fenced ' + hm(T0 + a * (T1 - T0)) + '–' + hm(T0 + b * (T1 - T0)) + '</b> · <b class=\"r\">drops 31%</b> in the fence'; setFollow(false);")
sub1("        stat.innerHTML = '<b class=\"k\">' + hm(t) + '</b> · 41/s then · <b class=\"r\">drops 22%</b> · scrolled to it · autoscroll <b class=\"k\">off</b>';",
     "        stat.innerHTML = '<b class=\"k\">' + hm(t) + '</b> · 41/s then · <b class=\"r\">drops 22%</b> · scrolled to it'; setFollow(false);")
sub1('''    function applyWindow(a, b) {
      rows().forEach(function (tr) {
        var m = rowMin(tr); if (m === null) return;
        tr.classList.toggle('outside', m < a || m > b);
      });
    }
''', '''    function applyWindow(a, b) {
      rows().forEach(function (tr) {
        var m = rowMin(tr); if (m === null) return;
        tr.classList.toggle('outside', m < a || m > b);
      });
    }
    // ---- round 36: the hand ----
    var REST = '<b class="k">34/s</b> now · <b class="r">drops 26%</b> · top talker <b class="k">cam-porch</b> · ring holds <b class="k">41 m</b>';
    var follow = document.getElementById('hfollow'), pause = document.getElementById('hpause'),
      group = document.getElementById('hgroup'), wipe = document.getElementById('hwipe');
    // following is a two-way switch: the whisper turns it off, the pill turns it back on (#749)
    function setFollow(on) { follow.classList.toggle('on', on); follow.textContent = on ? 'following' : 'follow'; }
    follow.addEventListener('click', function () {
      if (follow.classList.contains('on')) { setFollow(false); return; }
      setFollow(true); cursor.setAttribute('visibility', 'hidden'); applyWindow(T0, T1);
      if (!fenceOn) stat.innerHTML = REST;
    });
    pause.addEventListener('click', function () {
      var on = !pause.classList.contains('on'); pause.classList.toggle('on', on);
      pause.textContent = on ? 'paused' : 'pause';
      stat.innerHTML = on ? '<b class="k">held at 14:02:11</b> · <b class="k">212</b> arrived since, waiting · ' + REST.replace(/^.*?· /, '') : REST;
    });
    // group folds repeats of the same line — same action, ends and port — into the first, with a count
    group.addEventListener('click', function () {
      var on = !group.classList.contains('on'); group.classList.toggle('on', on);
      var seen = {};
      document.querySelectorAll('table.stream tbody tr').forEach(function (tr) {
        if (!tr.cells || tr.cells.length < 9) return;
        var key = [1, 2, 4, 7].map(function (i) { return tr.cells[i].textContent.trim(); }).join('|');
        if (!on) { tr.hidden = false; tr.classList.remove('gcount'); tr.querySelector('td:last-child').removeAttribute('data-n'); return; }
        if (seen[key]) { tr.hidden = true; seen[key].n += 1; seen[key].tr.querySelector('td:last-child').setAttribute('data-n', seen[key].n); seen[key].tr.classList.add('gcount'); }
        else seen[key] = { tr: tr, n: 1 };
      });
    });
    wipe.addEventListener('click', function () {
      var tb = document.querySelector('table.stream tbody');
      tb.innerHTML = '<tr class="wiped"><td colspan="9">nothing since 14:02:11 — wiped here, by you · the server\\'s ring still holds every line</td></tr>';
      stat.innerHTML = '<b class="k">wiped 14:02:11</b> · ' + REST;
    });
''')

# the quiet statement is a statement: plated like the dark one
sub1("var SEL = '#s3 .chip-t, #s3 .alarm-t, #s2 .flag-t, #s2 .dark-t, #s2 .now-t';",
     "var SEL = '#s3 .chip-t, #s3 .alarm-t, #s2 .flag-t, #s2 .dark-t, #s2 .quiet-t, #s2 .now-t';")
out.write_text(s)
print(out, len(s.split('\n')))
