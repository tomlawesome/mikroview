#!/usr/bin/env python3
"""Round 45: the wizard's sixth step, back up the router (#394). Built on
round 44's backups.html verbatim; one scene is added after Settings and
nothing else moves. Reads round-44/backups.html, writes
round-45/wizard.html.

The built wizard has five ratified steps (setupsteps.ts STEP_TITLES).
This adds a sixth in the same idiom -- a lead sentence, the script the
router runs, copy, then the line that says what has arrived -- and one
more thing the other steps do not have: the caveat, in the amber the
heavy warning already uses, that the router never checks who it is
sending to.

States are section classes on #wiz: rest (waiting for the first push),
warrived (the first pair landed), wnokey (no key mounted, the step
cannot be done yet), wlost (the router is gone: the step re-prints
everything it needs from mikroview, and points at the backups kept).

Data story: mikroview at 10.0.40.5 (round 26's fictional address),
rb5009 the router, a fictional token. Today is 2 Sep.
"""
import pathlib

root = pathlib.Path(__file__).resolve().parent.parent
s = (root / 'round-44/backups.html').read_text()
out = root / 'round-45/wizard.html'

s = s.replace('<title>Round 44', '<title>Round 45', 1)


def sub(old, new, count=1):
    global s
    assert s.count(old) == count, (s.count(old), old[:60])
    s = s.replace(old, new)


TOKEN = 'mvt-8f3a2c…c21e'
SCRIPT = f'''/system script add name=mv-backup policy=read,write,test,sensitive source="
  /system backup save name=mv-backup dont-encrypt=yes
  /export file=mv-backup
  /tool fetch mode=sftp upload=yes address=10.0.40.5 port=47022 user=rb5009 password=\\"{TOKEN}\\" src-path=mv-backup.backup dst-path=rb5009.backup
  /tool fetch mode=sftp upload=yes address=10.0.40.5 port=47022 user=rb5009 password=\\"{TOKEN}\\" src-path=mv-backup.rsc dst-path=rb5009.rsc
  /file remove mv-backup.backup
  /file remove mv-backup.rsc
"'''
SCHED = '''/system scheduler add name=mv-backup interval=1d start-time=03:00:00 policy=read,write,test,sensitive on-event="/system script run mv-backup"
/system script run mv-backup'''

STEPS = [
    ('1', 'Trust the certificate', 'fetched 2 Sep 13:41 from 10.0.40.1'),
    ('2', 'Send logs', 'arrived 2 Sep 13:42 from 10.0.40.1'),
    ('3', 'Tag firewall rules', '14 rules tagged'),
    ('4', 'Push router state', 'every table has been pushed'),
    ('5', 'Name your router', 'rb5009'),
]


def steplist(current6):
    rows = ''
    for n, title, receipt in STEPS:
        rows += f'        <div class="wzrow done"><span class="wzn">{n}</span><span class="wzt"><b>{title}</b><i>{receipt}</i></span></div>\n'
    rows += ('        <div class="wzrow current"><span class="wzn">6</span><span class="wzt"><b>Back up the router</b>'
             '<i data-w="rest">nothing has arrived yet</i>'
             '<i data-w="warrived">arrived today 03:00 · rb5009.backup + .rsc</i>'
             '<i class="wzwarn" data-w="wnokey">needs a key mounted first</i>'
             '<i data-w="wlost">10 pairs kept · the newest today 03:00</i>'
             '</span></div>\n')
    rows += '        <div class="wzrow"><span class="wzn">✓</span><span class="wzt"><b>Where setup stands</b></span></div>\n'
    return rows


BODY = f'''
      <div class="wzbody">
        <h2 data-w="rest warrived wnokey">6 · Back up the router</h2>
        <h2 data-w="wlost">6 · Back up the router — <span class="wzwarn">rb5009 is gone</span></h2>

        <p class="lead" data-w="rest warrived">Every night the router saves itself twice — the binary backup that restores it whole, and the plain export you can read — and drops both into mikroview. Nothing is sent back, and nothing is left on the router. The token below is minted for this one router and is already in the script.</p>
        <p class="lead" data-w="wnokey">Mikroview keeps a backup only under a key it does not hold in the data directory, and none is mounted. Mount one and this step prints the script; until then the drop box is closed and a push would be refused.</p>
        <p class="lead" data-w="wlost">The router that pushed these is not answering. Everything a replacement needs from this side is here, in the order it needs it: trust the certificate, send logs, then run the backup script again so the new router keeps pushing. Its backups are under Settings.</p>

        <div class="wzcaveat" data-w="rest warrived wlost"><b>Only on a network you trust.</b> RouterOS never checks who it is sending a backup to — anyone on the path between the router and 10.0.40.5 could read the pair, and the token with it. On a LAN you control that is fine; across the internet it is not, and mikroview cannot tell the difference from here.</div>

        <div data-w="rest warrived wlost">
        <p class="note" data-w="rest warrived">The token is shown once. Anyone who can read the script on the router can read it, so it is scoped to that one router and to this drop box.</p>
        <p class="note" data-w="wlost">The old router's token still opens the drop box, so a replacement can use this script as it stands; <a class="olink">mint a new one</a> retires the old.</p>
        <pre class="wzpre">{SCRIPT}</pre>
        <a class="olink wzcopy">copy script</a>
        <p class="note">Then have it run nightly, and once now to test it:</p>
        <pre class="wzpre">{SCHED}</pre>
        <a class="olink wzcopy">copy</a>
        </div>

        <div data-w="wnokey">
        <pre class="wzpre dim">   — the script prints here once a key is mounted —</pre>
        <p class="note"><a class="olink">how to mount one</a> · the same key keeps the event history and the state store.</p>
        </div>

        <div class="wzobs">
          <span data-w="rest"><span class="wzdot waiting"></span>waiting for the first push — the script above runs once at the end; give it a minute</span>
          <span data-w="warrived"><span class="wzdot ok"></span>arrived today 03:00 · rb5009.backup 412 KiB + rb5009.rsc 38 KiB · kept under the key · <a class="olink">see it in Settings</a></span>
          <span data-w="wnokey"><span class="wzdot"></span>nothing to wait for yet</span>
          <span data-w="wlost"><span class="wzdot ok"></span>10 pairs kept for rb5009, the newest today 03:00 · <a class="olink">download the newest .backup</a> to restore the replacement, then run the script above</span>
        </div>

        <div class="wzacts">
          <a class="olink" data-w="rest warrived wlost">back</a>
          <span class="wzbtn" data-w="rest">go on — it can arrive later</span>
          <span class="wzbtn" data-w="warrived">next</span>
          <span class="wzbtn dim" data-w="wnokey">next — skip for now</span>
          <span class="wzbtn" data-w="wlost">done — the replacement is pushing</span>
        </div>
      </div>
'''

SCENE = f'''
<!-- ============ ROUND 45 : THE WIZARD, STEP 6 — BACK UP THE ROUTER ============ -->
<section class="scene op" id="wiz" aria-label="Setup wizard: step 6, back up the router">
  <div class="scenebar">
    <span class="wordmark" title="home — whichever card you keep first">MIKRO<em>VIEW</em></span>
    <span class="scstatus"><span><span class="dot"></span>LIVE · 34/s</span><button class="who">tom (admin)</button></span>
  </div>
  <div class="opwrap"><div class="opanel wzpanel">
    <div class="wzlay">
      <div class="wzsteps">
        <h3>setup</h3>
{steplist(True)}      </div>
{BODY}
    </div>
  </div></div>
</section>
'''

# after the settings scene
SET_END = '    </div>\n    </div>\n    </div>\n  </div></div>\n</section>\n'
i = s.index('<section class="scene op" id="set"')
j = s.index(SET_END, i) + len(SET_END)
s = s[:j] + SCENE + s[j:]

CSS = '''
  /* round 45 -- the wizard step */
  .wzlay { display: grid; grid-template-columns: 300px 1fr; gap: 28px; align-items: start; }
  .wzsteps h3 { font: 600 9.5px var(--mono); letter-spacing: 0.16em; text-transform: uppercase; color: var(--ink-3); margin: 0 0 8px; }
  .wzrow { display: flex; gap: 12px; padding: 9px 10px; border-radius: 8px; font: 12.5px var(--sans); color: var(--ink-2); }
  .wzrow.current { background: var(--glass); border: 1px solid var(--hair-2); }
  .wzrow .wzn { font: 600 11px var(--mono); color: var(--ink-3); width: 14px; padding-top: 1px; }
  .wzrow.done .wzn { color: var(--ok); }
  .wzrow.current .wzn { color: var(--accent); }
  .wzt { display: flex; flex-direction: column; gap: 2px; }
  .wzt b { font-weight: 500; color: var(--ink); }
  .wzt i { font: 11px var(--sans); font-style: normal; color: var(--ink-3); }
  .wzt i.wzwarn, .wzwarn { color: var(--now); }
  .wzbody h2 { font: 500 17px var(--sans); color: var(--ink); margin: 2px 0 12px; }
  .wzbody .lead { font: 13px/1.55 var(--sans); color: var(--ink-2); margin: 0 0 12px; max-width: 72ch; }
  .wzbody .note { font: 11.5px/1.5 var(--sans); color: var(--ink-3); margin: 8px 0 6px; max-width: 72ch; }
  .wzcaveat { border-left: 2px solid var(--now); padding: 6px 12px; margin: 0 0 14px; font: 12px/1.5 var(--sans); color: var(--ink-2); max-width: 72ch; }
  .wzcaveat b { color: var(--now); font-weight: 600; }
  .wzpre { font: 11px/1.5 var(--mono); color: var(--ink); background: var(--raised); border: 1px solid var(--hair); border-radius: 8px; padding: 10px 12px; margin: 0; white-space: pre; overflow-x: auto; }
  .wzpre.dim { color: var(--ink-3); }
  .wzcopy { display: inline-block; font: 11px var(--sans); margin: 4px 0 0; }
  .wzobs { margin: 16px 0 0; padding: 10px 0 0; border-top: 1px solid var(--hair); font: 12px var(--sans); color: var(--ink-2); }
  .wzdot { display: inline-block; width: 7px; height: 7px; border-radius: 50%; background: var(--ink-3); margin-right: 8px; vertical-align: middle; }
  .wzdot.ok { background: var(--ok); }
  .wzdot.waiting { background: var(--accent); animation: brpulse 1.6s ease-in-out infinite; }
  .wzacts { display: flex; justify-content: space-between; align-items: center; margin-top: 18px; font: 12px var(--sans); }
  .wzbtn { border: 1px solid var(--hair-2); border-radius: 6px; padding: 5px 12px; color: var(--ink); background: var(--glass); }
  .wzbtn.dim { color: var(--ink-3); }
  #wiz [data-w] { display: none; }
  #wiz:not(.warrived):not(.wnokey):not(.wlost) [data-w~="rest"] { display: revert; }
  #wiz.warrived [data-w~="warrived"] { display: revert; }
  #wiz.wnokey [data-w~="wnokey"] { display: revert; }
  #wiz.wlost [data-w~="wlost"] { display: revert; }
'''
sub('  .oin { font: 600 11px var(--mono);', CSS + '  .oin { font: 600 11px var(--mono);')

# the URL applies w-states to #wiz: wizard.html?w=wnokey#wiz
sub('document.getElementById("set").classList.add(c);</script>',
    'document.getElementById("set").classList.add(c);'
    'for (const c of (new URLSearchParams(location.search).get("w") || "").split(",")) if (c) document.getElementById("wiz").classList.add(c);</script>')

out.write_text(s)
print(out, len(s))
