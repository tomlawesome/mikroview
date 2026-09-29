# Generates the direction page from round 11's skeleton, so the wizard's
# markup is round 10's AG exactly and only the door and the journey differ.
import re
sv = open('/home/codex/projects/mikroview/frontend/src/components/Fullfall.svelte').read()
frag = '<div class="fullfall door" id="fullfall" aria-hidden="true">' + ''.join(re.findall(r'<i[^>]*></i>', sv)) + '</div>'
page = """<div class="page" id="page" aria-hidden="true">
  <div class="bar"><span class="wm" id="barwm">MIKRO<em>VIEW</em></span><div class="chips" id="chips"></div><div class="right" id="barright"></div></div>
  <div class="striprow" id="strip"></div>
  <nav class="rail away" id="rail" aria-label="Setup steps"><ol id="steps"></ol></nav>
  <div class="main" id="main"><div class="body away" id="body"></div><div class="foot away" id="foot"></div></div>
  <div class="fallframe" id="fallframe"><div class="axis" id="axis"></div><div class="fall"><div class="cols" id="cols"></div><div class="fallbody" id="fb"></div></div></div>
  <nav class="siderail" id="siderail" aria-label="Sections"><div class="stcards"><div class="stcard on"><span class="nm">The fall</span></div><div class="stcard"><span class="nm">Topography</span></div><div class="stcard"><span class="nm">Metrics</span></div><div class="stcard"><span class="nm">Stream</span></div><div class="stcard"><span class="nm">The docket</span></div><div class="stcard"><span class="nm">Entities</span></div><div class="stcard"><span class="nm">Settings</span></div><div class="stcard"><span class="nm">Log every rule</span></div></div></nav>
</div>"""
stack = """<div class="stack">
    <div class="wm-box" id="wmbox"><span class="wm">MIKRO<em>VIEW</em></span></div>
    <h1>Welcome</h1>
    <p class="subtitle">First sign-in. Setting up your first router comes next.</p>
    <label class="field"><span>account</span><input type="text" id="acct" value="tom" autocomplete="off"></label>
    <label class="field"><span>password</span><input type="password" id="pw" value="correct horse"></label>
    <button type="button" class="submit-btn" id="enter" data-act="enter">Enter</button>
  </div>"""
def html(letter, slug, title, extra_body, door_inner):
    return """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>MikroView setup — %s · %s</title>
<link rel="stylesheet" href="shell.css">
<link rel="stylesheet" href="fall.css">
<link rel="stylesheet" href="inks.css">
<link rel="stylesheet" href="wizard.css">
<link rel="stylesheet" href="fullfall.css">
<link rel="stylesheet" href="door.css">
<link rel="stylesheet" href="%s.css">
</head>
<body>
%s
%s<div class="door" id="door">
  %s
</div>
<script src="engine.js"></script>
<script src="fall.js"></script>
<script src="track.js"></script>
<script src="wizard.js"></script>
<script src="fx.js"></script>
<script src="%s.js"></script>
</body>
</html>
""" % (letter, title, slug, page, extra_body, door_inner, slug)
open('an-neon.html', 'w').write(html('AN', 'an', 'The neon, sparks done properly', '<canvas id="fx" aria-hidden="true"></canvas>\n',
    '%s\n  <div class="clear" aria-hidden="true"></div>\n  <div class="world" id="world">\n  %s\n  </div>' % (frag, stack)))
print('page written')
