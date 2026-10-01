#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# The blocklist builder's real-RouterOS check (#1360, BUILD.md part 4).
# Run by hand and recorded, like docs/routeros-chr-exercise.md's run:
#
#   scripts/live-blocklist-chr.sh 7.24.4
#
# boots a CHR at that version (scripts/live-routeros.sh), renders the
# block from the same generator the builder's API calls
# (scripts/routeroscommands -step=blocklist: all seven lists on, each
# with its defaults, part 1's push with a placeholder address and token)
# and pastes it command by command, as an operator's terminal reads a
# pasted block. Then, asserting each step on the router's own console:
#
#   1. the lists loaded -- mv-bl-spamhaus >= 1,000 and mv-bl-et >= 100,
#      every count recorded -- the raw rules sit ahead of the drop list's
#      own rule, every scheduler and script is there, no fetched file is
#      left behind;
#   2. pasting the block again (without its run-now line) leaves the
#      count of scripts, schedulers and rules unchanged, and turns back
#      on a rule and a scheduler that were switched off in between;
#   3. one list's undo removes that list and nothing else;
#   4. the undo for everything leaves nothing named mv-bl, and leaves
#      the drop list's rule and the push script alone;
#   5. a loader fed a hostile file -- private space, over-wide prefixes,
#      a host name, junk -- loads only the public addresses in it. The
#      file is served from a throwaway nginx container on the Docker
#      network, never from a list's source.
#
# The transcript goes to docs/routeros-verification-logs/<version>-blocklist.log
# (or $2). The lists' own addresses appear in it: they are the lists'
# public data, not ours. Each run fetches every list once (step 1's
# run-now); Spamhaus asks for no automated fetch more than once an hour,
# so never loop this.
#
# Needs: Docker (the CHR and the probe's nginx run in containers), Go,
# and outbound HTTPS from the CHR to the seven sources. A source the
# host cannot reach shows up as a "the fetch failed" line in the log and
# a failed count assertion, never as a silent skip.
set -euo pipefail

VERSION="${1:?usage: scripts/live-blocklist-chr.sh <routeros version> [log]}"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG="${2:-$REPO/docs/routeros-verification-logs/$VERSION-blocklist.log}"

export CHR_VERSION="$VERSION"
export CHR_DIR="${CHR_DIR:-/tmp/mikroview-chr}"
export MVCHR_CONTAINER="${MVCHR_CONTAINER:-mikroview-chr-blocklist}"
export MVCHR_SERIAL_PORT="${MVCHR_SERIAL_PORT:-15921}"
export MVCHR_INPUT_PORT="${MVCHR_INPUT_PORT:-15922}"
export MVCHR_FORWARD_PORT="${MVCHR_FORWARD_PORT:-15923}"
PROBE_CONTAINER="${MVCHR_CONTAINER}-probe"

cleanup() {
  "$REPO/scripts/live-routeros.sh" down >/dev/null 2>&1 || true
  docker rm -f "$PROBE_CONTAINER" >/dev/null 2>&1 || true
  rm -rf "${PROBE_DIR:-}"
}
trap cleanup EXIT

cd "$REPO"
go build -o /dev/null ./scripts/routeroscommands
BLOCK="$(mktemp)"
UNDO_ET="$(mktemp)"
UNDO_ALL="$(mktemp)"
trap 'cleanup; rm -f "$BLOCK" "$UNDO_ET" "$UNDO_ALL"' EXIT
go run ./scripts/routeroscommands -step=blocklist -version="$VERSION" -nul >"$BLOCK"
go run ./scripts/routeroscommands -step=blocklist -undo=et -nul >"$UNDO_ET"
go run ./scripts/routeroscommands -step=blocklist -undo=all -nul >"$UNDO_ALL"

# The probe: a hostile list served on the Docker network.
PROBE_DIR="$(mktemp -d)"
cat >"$PROBE_DIR/hostile.txt" <<'EOF'
# a list gone bad: only the four public documentation addresses below may load
192.0.2.10
198.51.100.0/24
203.0.113.7/32
  8.8.4.4 trailing words after a blank
192.168.88.0/24
10.20.30.40
172.0.0.0/8
172.16.5.5
100.64.1.1
127.0.0.1
169.254.10.10
0.0.0.0/0
8.0.0.0/7
224.0.0.1
evil.example
1.2.3.4/33
not an address
; a comment line
198.51.100.0/24
EOF
chmod 0755 "$PROBE_DIR"
chmod 0644 "$PROBE_DIR/hostile.txt"
docker rm -f "$PROBE_CONTAINER" >/dev/null 2>&1 || true
docker run -d --name "$PROBE_CONTAINER" -v "$PROBE_DIR:/usr/share/nginx/html:ro" nginx:alpine >/dev/null
PROBE_ADDR="$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$PROBE_CONTAINER")"

eval "$("$REPO/scripts/live-routeros.sh" up)"

mkdir -p "$(dirname "$LOG")"
PYTHONDONTWRITEBYTECODE=1 MVBL_MIRROR="${MVBL_MIRROR:-}" BLOCK="$BLOCK" UNDO_ET="$UNDO_ET" UNDO_ALL="$UNDO_ALL" LOG="$LOG" VERSION="$VERSION" \
  PROBE_URL="http://$PROBE_ADDR/hostile.txt" PROBE_FILE="$PROBE_DIR/hostile.txt" REPO="$REPO" python3 - <<'PY'
import importlib.util, os, re, sys, time, datetime

spec = importlib.util.spec_from_file_location('console', os.path.join(os.environ['REPO'], 'scripts/live-routeros-console.py'))
console = importlib.util.module_from_spec(spec)
spec.loader.exec_module(console)

def steps(path):
    return [s for s in open(path).read().split('\0') if s.strip()]

block, undo_et, undo_all = steps(os.environ['BLOCK']), steps(os.environ['UNDO_ET']), steps(os.environ['UNDO_ALL'])
# MVBL_MIRROR=<host> is for working on this script: every list URL points
# at http://<host>/<file> instead of its source, so a re-run fetches
# nothing from the lists' owners. A mirrored run is not a record.
mirror = os.environ.get('MVBL_MIRROR', '')
if mirror:
    block = [re.sub(r'https://[^"\\]*/([^/"\\]+\.(?:txt|json))', lambda m: f'http://{mirror}/{m.group(1)}', c).replace(' check-certificate=yes output=file', ' output=file') for c in block]
version = os.environ['VERSION']
log = open(os.environ['LOG'], 'w')
con = console.Console(port=int(os.environ['MVCHR_SERIAL_PORT']), timeout=2400)
con.login()
failures = []

def out(line=''):
    log.write(line + '\n')
    log.flush()
    print(line)

def shown(cmd):
    # A loader's add is ~10 KB on one line; the log keeps its head and
    # its size, which is enough to say which command it was. The full
    # text is what routeroscommands prints for this version.
    return cmd if len(cmd) <= 400 else cmd[:300] + f' … [{len(cmd)} bytes; the whole command is routeroscommands -step=blocklist -version={version}]'

def run(cmd, quiet=False):
    started = time.time()
    text = con.cmd(cmd)
    took = time.time() - started
    lines = text.split('\n')
    # Drop the console's echo of the command (it wraps at 200 columns).
    echo = re.sub(r'\s', '', cmd)
    body, seen = [], ''
    for l in lines:
        squeezed = re.sub(r'\s', '', l)
        if len(seen) < len(echo) and squeezed and echo.startswith(seen + squeezed):
            seen += squeezed
            continue
        if l.startswith('-- [written to'):
            continue
        body.append(l)
    reply = '\n'.join(body).rstrip()
    if not quiet:
        out('== ' + shown(cmd) + (f'   ({took:.0f}s)' if took >= 5 else ''))
        if reply:
            out(reply)
    return reply

def value(expr):
    reply = run(':put ' + expr, quiet=True)
    got = [l for l in reply.split('\n') if l and not l.startswith('[') and not l.startswith(':put')]
    v = got[-1].strip() if got else ''
    out(f'== :put {expr}\n{v}')
    return v

def check(ok, what):
    out(('PASS  ' if ok else 'FAIL  ') + what)
    if not ok:
        failures.append(what)

def counts():
    c = {}
    c['scripts'] = int(value('[:len [/system script find where name~"^mv-bl-"]]'))
    c['schedulers'] = int(value('[:len [/system scheduler find where name~"^mv-bl-"]]'))
    c['raw4'] = int(value('[:len [/ip firewall raw find where comment~"^mikroview blocklist: "]]'))
    c['raw6'] = int(value('[:len [/ipv6 firewall raw find where comment~"^mikroview blocklist: "]]'))
    return c

out(f'# #1360 part 4: the blocklist builder\'s block on a real CHR {version}, {datetime.date.today()}.')
out('# Rendered by scripts/routeroscommands -step=blocklist (all seven lists on, catalogue')
out('# defaults, push re-set with a placeholder address and token) and pasted one console')
out('# command at a time by scripts/live-blocklist-chr.sh. The push itself fails at its')
out('# fetch (mikroview.invalid): that is the placeholder, not the block. Each list was')
out('# fetched once, from its source, by the router.')
if mirror:
    out(f'# MIRRORED RUN: every list was served from http://{mirror}/, not its source. Not a record.')
out()
run(':put [/system resource get version]')
# The drop list's own rule, as its setup card leaves it, so placement and
# undo have something of the operator's to keep clear of.
run('/ip firewall raw add chain=prerouting src-address-list=mikroview-drop action=drop comment="mikroview drop list"')
run(':put [/certificate settings print as-value]')

out('\n## 1. Paste the block, run every loader, read what the router holds\n')
for cmd in block:
    run(cmd)
lists = ['mv-bl-spamhaus', 'mv-bl-et', 'mv-bl-cins', 'mv-bl-blde', 'mv-bl-greensnow', 'mv-bl-dshield', 'mv-bl-bindef']
held = {n: int(value(f'[:len [/ip firewall address-list find where list={n}]]')) for n in lists}
held['mv-bl-spamhaus6'] = int(value('[:len [/ipv6 firewall address-list find where list=mv-bl-spamhaus6]]'))
run('/log print without-paging where message~"^mikroview blocklist"')
check(held['mv-bl-spamhaus'] >= 1000, f"mv-bl-spamhaus holds {held['mv-bl-spamhaus']} (>= 1,000)")
check(held['mv-bl-et'] >= 100, f"mv-bl-et holds {held['mv-bl-et']} (>= 100)")
for n in lists[2:] + ['mv-bl-spamhaus6']:
    check(held[n] > 0, f'{n} holds {held[n]}')
check(value('[:len [/ip firewall address-list find where list~"-next\\$"]]') == '0', 'no staging list left behind')
check(value('[:len [/file find where name~"^mv-bl-"]]') == '0', 'no fetched file left behind')
run('/ip firewall raw print without-paging')
run('/ipv6 firewall raw print without-paging')
order = value('[:len [/ip firewall raw find where comment~"^mikroview blocklist: "]]')
first_drop = value('[:pick [/ip firewall raw find] [:len [/ip firewall raw find where comment~"^mikroview blocklist: "]]]')
drop_rule = value('[/ip firewall raw find where comment="mikroview drop list"]')
check(first_drop == drop_rule and order == '8', f'the 8 blocklist rules sit ahead of the drop list\'s rule')
check(value('[/ipv6 firewall raw get ([find]->0) comment]') == 'mikroview blocklist: spamhaus (from)', 'the IPv6 rule went into an empty IPv6 raw table')
run('/system scheduler print detail without-paging where name~"^mv-bl-"')
before = counts()
check(before == {'scripts': 7, 'schedulers': 7, 'raw4': 8, 'raw6': 1}, f'7 scripts, 7 schedulers, 8 IPv4 rules and 1 IPv6 rule: {before}')
check(value('[:len [/system scheduler find where name~"^mv-bl-" and comment~"not supported"]]') == '0', 'no scheduler carries a RouterOS warning')

out('\n## 2. Switch a rule and a scheduler off, paste the block again (not its run-now line)\n')
run('/ip firewall raw disable [find comment="mikroview blocklist: et (from)"]')
run('/system scheduler disable [find name=mv-bl-et]')
for cmd in block[:-1]:
    run(cmd)
after = counts()
check(after == before, f'pasting twice changed nothing: {after}')
check(value('[/ip firewall raw get [find comment="mikroview blocklist: et (from)"] disabled]') == 'false', 'the re-paste turned the rule back on')
check(value('[/system scheduler get [find name=mv-bl-et] disabled]') == 'false', 'the re-paste turned the scheduler back on')

out('\n## 3. One list\'s undo\n')
for cmd in undo_et:
    run(cmd)
check(value('[:len [/ip firewall address-list find where list~"^mv-bl-et"]]') == '0', 'mv-bl-et is gone')
check(value('[:len [/ip firewall raw find where comment~"^mikroview blocklist: et "]]') == '0', "et's rules are gone")
check(value('[:len [/system script find where name=mv-bl-et]]') == '0' and value('[:len [/system scheduler find where name=mv-bl-et]]') == '0', "et's script and scheduler are gone")
check(int(value('[:len [/ip firewall address-list find where list=mv-bl-spamhaus]]')) == held['mv-bl-spamhaus'], 'mv-bl-spamhaus is untouched')

out('\n## 4. Undo everything\n')
for cmd in undo_all:
    run(cmd)
check(counts() == {'scripts': 0, 'schedulers': 0, 'raw4': 0, 'raw6': 0}, 'no mv-bl script, scheduler or rule is left')
check(value('[:len [/ip firewall address-list find where list~"^mv-bl-"]]') == '0' and value('[:len [/ipv6 firewall address-list find where list~"^mv-bl-"]]') == '0', 'no mv-bl list is left')
check(value('[:len [/file find where name~"^mv-bl-"]]') == '0', 'no mv-bl file is left')
check(value('[:len [/ip firewall raw find where comment="mikroview drop list"]]') == '1', "the drop list's rule is untouched")
check(value('[:len [/system script find where name=mv-push]]') == '1' and value('[:len [/system scheduler find where name=mv-push]]') == '1', 'the push script and its scheduler are untouched')

out('\n## 5. A loader fed a hostile file\n')
et_loader = next(c for c in block if c.startswith(':if ([:len [/system script find name=mv-bl-et]]'))
probe = et_loader.replace('https://rules.emergingthreats.net/blockrules/compromised-ips.txt', os.environ['PROBE_URL'])
probe = probe.replace(' check-certificate=yes output=file', ' output=file')
out('# The ET loader as generated, its URL pointed at ' + os.environ['PROBE_URL'] + ', served by a throwaway nginx container:')
for l in open(os.environ['PROBE_FILE']).read().rstrip('\n').split('\n'):
    out('#   ' + l)
run(probe)
run('/system script run mv-bl-et')
run('/ip firewall address-list print without-paging where list=mv-bl-et')
run('/log print without-paging where message~"^mikroview blocklist: et"')
got = sorted(l.split()[-1] for l in run(':foreach i in=[/ip firewall address-list find where list=mv-bl-et] do={ :put [/ip firewall address-list get $i address] }', quiet=True).split('\n') if re.match(r'^\d', l.strip()))
check(got == sorted(['192.0.2.10', '198.51.100.0/24', '203.0.113.7', '8.8.4.4']), f'only the public addresses loaded: {got}')
for cmd in undo_all:
    run(cmd, quiet=True)

out()
out('RESULT: ' + ('PASS' if not failures else 'FAIL -- ' + '; '.join(failures)))
sys.exit(1 if failures else 0)
PY
