/* Round 6 shared engine: one data story, one simulated router, one state
   machine. Each direction (W, X, Y) supplies render/paint and its own
   surface. Nothing here decides how anything looks. */
window.MV = (() => {
  const story = {
    mv: '192.168.13.15:8080', mvHost: '192.168.13.15', router: 'rb5009', addr: '192.168.13.1', other: '192.168.254.1',
    t0: 14 * 3600 + 2 * 60 + 50, tokenUntil: '14:17',
    rules: 47, alreadyLog: ['in-ssh', 'est-rel', 'icmp'], lists: 12, leases: 31, peers: 3, interval: '20 min',
    ifaces: [
      { n: 'ether1', d: 'WAN', a: '81.2.69.140', wan: true },
      { n: 'bridge', d: 'LAN', a: '192.168.13.1/24', leases: 31 },
      { n: 'wg0', d: 'WireGuard', a: '10.66.0.1/24', peers: 3 },
    ],
    suggestions: [
      { chain: 'forward', action: 'drop', comment: 'drop everything else', why: 'last rule on forward', prefix: 'fwd-drop' },
      { chain: 'input', action: 'drop', comment: 'drop from WAN', why: 'internet → router', prefix: 'wan-in' },
      { chain: 'forward', action: 'accept', comment: 'port-forward 443 → .40', why: 'inbound accept across the boundary', prefix: 'nat' },
      { chain: 'forward', action: 'drop', comment: 'guest → LAN', why: 'guest wifi → LAN', prefix: 'guest-lan' },
      { chain: 'input', action: 'drop', comment: 'wg0 → input', why: 'VPN peer → router', prefix: 'wg-in' },
    ],
    // story-second offsets from Copy
    at: { cert: 8, enrol: 14, push: 31, backup: 32, refused: 12, tagged: 7 },
  };
  const fresh = () => ({
    stage: 'tell', name: '', addr: '', pass: '', backup: true, useOther: false, open: false, copied: false,
    evidence: [], checks: false, refused: false, version: null, standing: null, showWarning: false,
    chosen: new Set([0, 1, 2, 3, 4]), tuneCopied: false, tagged: false, tuneSkipped: false, undoOpen: null,
    lines: 0, tail: [], rate: [], clock: 0, tunedAt: null,
  });
  const state = fresh();
  let D = null, timers = [], tickers = [], copyAt = 0, clockBase = 0, baseAt = 0;

  const speed = () => Number((document.getElementById('speed') || { value: 4 }).value);
  const later = (storySec, f) => { timers.push(setTimeout(f, storySec * 1000 / speed())); };
  const every = (ms, f) => { const id = setInterval(f, ms); tickers.push(id); return id; };
  const clearAll = () => { timers.forEach(clearTimeout); tickers.forEach(clearInterval); timers = []; tickers = []; };
  const hms = s => { s = Math.round(s); const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), x = s % 60; return `${h}:${String(m).padStart(2, '0')}:${String(x).padStart(2, '0')}`; };
  const hm = s => hms(s).slice(0, 5);
  const now = () => story.t0 + clock();
  const clock = () => copyAt ? clockBase + (Date.now() - baseAt) * speed() / 1000 : 0;
  const rebase = () => { if (!copyAt) return; clockBase = clock(); baseAt = Date.now(); };
  const has = k => state.evidence.some(e => e.k === k);
  const ev = k => state.evidence.find(e => e.k === k);
  const from = () => state.useOther ? story.other : state.addr;
  const logging = () => story.alreadyLog.concat(state.tagged ? [...state.chosen].map(i => story.suggestions[i].prefix) : []);

  const pasteBlock = () => {
    const b = state.backup ? '\n:if ([:len [/system scheduler find name=mv-backup]] = 0) do={ /system scheduler add name=mv-backup start-time=03:00:00 interval=1d policy=read,test,sensitive,ftp on-event="/system script run mv-backup" }' : '';
    return `/tool fetch url="https://${story.mv}/ca.crt" check-certificate=no dst-path=mikroview-ca.crt
/certificate import file-name=mikroview-ca.crt passphrase=""
/system logging action add name=mikroview target=remote remote=${story.mvHost} remote-port=6514 remote-protocol=tcp remote-log-format=syslog src-address=${from() || story.addr}
/system logging add topics=firewall action=mikroview
/system logging add topics=info,!firewall action=mikroview
:if ([:len [/system script find name=mv-push]] = 0) do={ /system script add name=mv-push policy=read,test source=":local ruleRecs [:toarray \\"\\"]
:foreach i,v in=[/ip/firewall/filter print as-value] do={ :local rec {\\"ordinal\\"=\\$i; \\"comment\\"=(\\$v->\\"comment\\"); \\"chain\\"=(\\$v->\\"chain\\"); \\"action\\"=(\\$v->\\"action\\"); \\"logPrefix\\"=(\\$v->\\"log-prefix\\"); \\"log\\"=(\\$v->\\"log\\")}; :set ruleRecs (\\$ruleRecs, {\\$rec}) }
:local payload [:serialize to=json value={\\"kind\\"=\\"filter-rule\\"; \\"page\\"=1; \\"pages\\"=1; \\"routerosVersion\\"=[/system/resource get version]; \\"wizardVersion\\"=3; \\"records\\"=\\$ruleRecs}]
/tool fetch url=\\"https://${story.mv}/api/ingest/routeros\\" http-method=post http-data=\\$payload http-header-field=(\\"Content-Type: application/json,Authorization: Bearer mvt-8f19c2e0b6d4a1c8\\") check-certificate=yes output=none
… address-list, dhcp-lease, arp, ip-address, wireguard and logging pages follow the same shape …
" } else={ /system script set [find name=mv-push] policy=read,test source="…" }
:if ([:len [/system scheduler find name=mv-push]] = 0) do={ /system scheduler add name=mv-push interval=20m policy=read,test on-event="/system script run mv-push" }${b}
/system script run mv-push
/log info "mikroview-enrol k7q2m9x4p1w8e3r6t0y5"`;
  };
  const pasteLines = () => pasteBlock().split('\n').length;
  const tagBlock = () => [...state.chosen].sort().map(i => { const s = story.suggestions[i]; return `/ip firewall filter set [find chain=${s.chain} action=${s.action} comment="${s.comment}"] log=yes log-prefix="${s.prefix}"`; }).join('\n');
  const undoLines = {
    connect: ['/certificate remove [find name=mikroview-ca.crt]', '/system logging remove [find action=mikroview]', '/system logging action remove [find name=mikroview]'],
    push: ['/system scheduler remove [find name=mv-push]', '/system script remove [find name=mv-push]'],
    backup: ['/system scheduler remove [find name=mv-backup]', '/system script remove [find name=mv-backup]'],
    tune: () => [...state.chosen].sort().map(i => `/ip firewall filter set [find comment="${story.suggestions[i].comment}"] log=no log-prefix=""`),
  };

  /* ---- the simulated router ---- */
  const srcs = ['203.0.113.7', '198.51.100.23', '45.155.205.99', '185.220.101.4', '192.168.13.42', '192.168.13.77', '10.66.0.7', '91.240.118.12'];
  const ports = [22, 443, 3389, 23, 8080, 445, 5060, 1900, 53];
  const rnd = a => a[Math.floor(Math.random() * a.length)];
  const line = () => {
    const pre = rnd(logging().concat(logging().length ? ['est-rel', 'icmp'] : []));
    const proto = pre === 'icmp' ? 'ICMP' : rnd(['TCP (SYN)', 'TCP (ACK)', 'UDP']);
    const iface = pre === 'wg-in' ? 'wg0' : pre === 'guest-lan' ? 'bridge-guest' : rnd(['ether1', 'ether1', 'bridge']);
    const chain = ['in-ssh', 'wan-in', 'wg-in'].includes(pre) ? 'input' : 'forward';
    const dst = chain === 'input' ? '81.2.69.140' : rnd(['192.168.13.40', '192.168.13.12', '8.8.8.8', '142.250.187.206']);
    return { t: hms(now()), pre, text: `firewall,info ${pre} ${chain}: in:${iface} out:${chain === 'input' ? '(unknown 0)' : rnd(['ether1', 'bridge'])}, proto ${proto}, ${rnd(srcs)}:${40000 + Math.floor(Math.random() * 20000)}->${dst}:${rnd(ports)}, len ${40 + Math.floor(Math.random() * 1400)}` };
  };
  function startStream() {
    every(420, () => {
      const n = 1 + Math.floor(Math.random() * 3);
      for (let i = 0; i < n; i++) state.tail.push(line());
      if (state.tail.length > 60) state.tail.splice(0, state.tail.length - 60);
      state.lines += 3 + Math.floor(Math.random() * 7);
      D.tick && D.tick(state);
    });
    every(1000, () => { state.rate.push({ t: now(), v: 4 + Math.random() * 6 + (state.tagged ? 3 : 0) }); if (state.rate.length > 400) state.rate.shift(); });
  }
  function add(e) { e.t = hms(now()); state.evidence.push(e); render(); }
  const scenario = () => (document.getElementById('scenario') || { value: 'happy' }).value;
  function startRouter() {
    copyAt = Date.now(); baseAt = copyAt; clockBase = 0;
    every(250, () => { state.clock = clock(); D.tick && D.tick(state); });
    const sc = scenario();
    later(story.at.cert, () => add({ k: 'cert', h: `Certificate fetched by ${from()}` }));
    if (sc === 'wrongaddr' && !state.useOther) {
      later(story.at.refused, () => { state.refused = true; state.checks = true; render(); });
      return;
    }
    schedule(story.at.enrol - story.at.cert, story.at.cert);
  }
  function schedule(enrolIn, base) {
    later(base + enrolIn, () => {
      add({ k: 'enrol', h: `Enrol line from ${from()}` }); startStream();
      later(story.at.push - story.at.enrol, () => {
        state.version = scenario() === 'ahead' ? '7.25.1' : '7.24.4'; state.standing = scenario() === 'ahead' ? 'ahead' : 'reviewed';
        add({ k: 'push', h: `First push from ${state.name}` });
        if (state.backup) later(1, () => add({ k: 'backup', h: 'Nightly backup scheduled' }));
      });
    });
  }

  /* ---- actions ---- */
  function act(a, d = {}) {
    switch (a) {
      case 'backup': state.backup = !state.backup; render(); break;
      case 'go-paste': if (state.name && state.addr && state.pass) { state.stage = 'paste'; render(); } break;
      case 'fold': state.open = !state.open; render(); break;
      case 'copy': if (state.copied) break; state.copied = true; render(); later(2, () => { state.stage = 'watch'; render(); startRouter(); }); break;
      case 'back-tell': state.stage = 'tell'; state.copied = false; render(); break;
      case 'checks': state.checks = !state.checks; render(); break;
      case 'use-other': state.useOther = true; state.refused = false; render(); schedule(4, 0); break;
      case 'reroll': alert('Prototype: a new token would be minted and the paste rewritten; the old one dies.'); break;
      case 'warn': state.showWarning = !state.showWarning; render(); break;
      case 'to-tune': state.stage = 'tune'; render(); break;
      case 'toggle': { const i = Number(d.i); state.chosen.has(i) ? state.chosen.delete(i) : state.chosen.add(i); render(); break; }
      case 'tune-copy': if (!state.chosen.size) break; state.tuneCopied = true; render(); later(story.at.tagged, () => { state.tagged = true; state.tunedAt = hms(now()); render(); }); break;
      case 'tune-skip': state.tuneSkipped = true; state.stage = 'done'; render(); break;
      case 'to-done': state.stage = 'done'; render(); break;
      case 'undo': state.undoOpen = state.undoOpen === d.u ? null : d.u; render(); break;
      case 'change': state.stage = 'tell'; render(); break;
      case 'fall': alert('Prototype: opens the fall with ' + state.name + ' already flowing.'); break;
      case 'another': alert('Prototype: the same form again, MikroView\'s address already known.'); break;
      case 'reset': if (confirm('Prototype: forgets ' + state.name + ' on MikroView and shows the undo lines for the router. Restart?')) restart(); break;
      case 'leave': alert('Prototype: closes setup. The router\'s page reopens it.'); break;
      case 'restart': restart(); break;
    }
  }
  function restart() { clearAll(); copyAt = 0; Object.assign(state, fresh()); render(); }
  function render() { D.render(state); D.paint && D.paint(state); D.tick && D.tick(state); const go = document.querySelector('[data-act=go-paste]'); if (go) go.disabled = !(state.name && state.addr && state.pass); }

  /* ---- wiring, shared ---- */
  document.addEventListener('click', e => { const b = e.target.closest('[data-act]'); if (b && !b.disabled) act(b.dataset.act, b.dataset); });
  document.addEventListener('input', e => {
    const f = e.target.dataset.field; if (!f) return;
    state[f] = f === 'pass' ? e.target.value : e.target.value.trim();
    const go = document.querySelector('[data-act=go-paste]'); if (go) go.disabled = !(state.name && state.addr && state.pass);
    D.paint && D.paint(state); D.field && D.field(f, state);
  });
  document.addEventListener('change', e => {
    if (e.target.id === 'scenario') restart();
    if (e.target.id === 'speed') rebase();
  });
  function protoBar() {
    const b = document.createElement('div'); b.className = 'proto-bar'; b.setAttribute('role', 'group'); b.setAttribute('aria-label', 'Prototype controls');
    b.innerHTML = `<b>Prototype</b><label>scenario <select id="scenario"><option value="happy">happy path</option><option value="wrongaddr">router sends from another address</option><option value="ahead">RouterOS ahead of review</option></select></label><label>speed <select id="speed"><option value="1">real time</option><option value="4" selected>4×</option><option value="12">12×</option></select></label><button type="button" data-act="restart">Restart</button>`;
    document.body.appendChild(b);
  }
  function boot(direction) {
    D = direction; protoBar();
    if (location.search.includes('demo')) Object.assign(state, { name: story.router, addr: story.addr, pass: 'correct horse' });
    render();
  }
  const esc = s => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;');
  return { story, state, boot, act, render, later, has, ev, from, logging, pasteBlock, pasteLines, tagBlock, undoLines, hms, hm, now, esc };
})();
