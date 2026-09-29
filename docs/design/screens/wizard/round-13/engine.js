/* Round 10 engine (round 9's, plus D.next so a direction can own its
   own step order) — round 9 shared engine (round 8's, plus a `finish` action and flag)
   — round 8 shared engine (round 7's, plus address validation and the
   boundary model the fall draws from): the round-6 data story and simulated router,
   re-cut for the owner's direction of 2026-09-26 — "ask the user which
   features they want, one by one, then build the copy & paste block as
   one thing". Questions are asked in order; each answer adds or removes
   a section of one block; the block is shown only once every question
   is answered; then the router answers, one proof at a time. Each
   direction (Z, AA, AB) supplies render/tick and its own surface.
   Nothing here decides how anything looks. */
window.MV = (() => {
  const story = {
    mv: '192.168.13.15:8080', mvHost: '192.168.13.15', router: 'rb5009', addr: '192.168.13.1', other: '192.168.254.1',
    t0: 14 * 3600 + 2 * 60 + 50, tokenUntil: '14:17', token: 'demo0000demo0000demo',
    rules: 47, alreadyLog: ['in-ssh', 'est-rel', 'icmp'], leases: 31, peers: 3, interval: '20 min', reviewed: '7.24.2',
    suggestions: [
      { chain: 'forward', action: 'drop', comment: 'drop everything else', why: 'last rule on forward', prefix: 'fwd-drop' },
      { chain: 'input', action: 'drop', comment: 'drop from WAN', why: 'internet → router', prefix: 'wan-in' },
      { chain: 'forward', action: 'accept', comment: 'port-forward 443 → .40', why: 'inbound accept across the boundary', prefix: 'nat' },
      { chain: 'forward', action: 'drop', comment: 'guest → LAN', why: 'guest wifi → LAN', prefix: 'guest-lan' },
      { chain: 'input', action: 'drop', comment: 'wg0 → input', why: 'VPN peer → router', prefix: 'wg-in' },
    ],
    at: { cert: 8, enrol: 14, push: 31, backup: 32, refused: 12, tagged: 7 },
    /* The fall's boundaries for this router, as the push reveals them:
       one column per interface pair, in the fall's own words. `logs`
       names the prefixes already logging on it; `sug` the proposal index
       that would light it. lane: which lane ink it wears once watched. */
    boundaries: [
      { id: 'wan-in', name: 'ether1 · input', sub: 'internet → the router', logs: ['in-ssh'], sug: 1, lane: 'lan' },
      { id: 'fwd', name: 'bridge → ether1', sub: 'LAN to the internet', logs: ['est-rel'], sug: 0, lane: 'srv' },
      { id: 'nat', name: 'ether1 → bridge', sub: 'port-forward 443 → .40', logs: [], sug: 2, lane: 'iot' },
      { id: 'guest', name: 'bridge-guest → bridge', sub: 'guest wifi → LAN', logs: [], sug: 3, lane: 'guest' },
      { id: 'wg', name: 'wg0 · input', sub: 'VPN peers → the router', logs: [], sug: 4, lane: '5' },
      { id: 'icmp', name: 'any · icmp', sub: 'pings, both ways', logs: ['icmp'], sug: null, lane: 'lan' },
    ],
  };
  /* A router address is a dotted quad, nothing else: no port, no name,
     no CIDR (owner, 2026-09-26: "reject incorrect formatting"). */
  const addrProblem = v => {
    if (!v) return '';
    if (/[:/]/.test(v)) return v.includes(':') ? 'No port here — just the address. The port is MikroView’s side.' : 'No prefix length — the router’s own address, not its network.';
    if (/[^0-9.]/.test(v)) return 'A name will not do: the enrolment window binds to an address.';
    const p = v.split('.');
    if (p.length !== 4 || p.some(x => x === '' || Number(x) > 255)) return 'Four numbers, 0–255, separated by dots.';
    return '';
  };

  /* The questions, in order. kind: text | yesno | password. Each yes/no
     question owns one section of the block. */
  const questions = [
    { id: 'name', kind: 'text', title: 'Name this router', ask: 'What do you call this router?', hint: 'The name MikroView shows on the fall, the stream and Entities.', placeholder: 'rb5009', field: 'name' },
    { id: 'addr', kind: 'text', title: 'Its address', ask: 'Which address will it send from?', hint: 'The router\u2019s own address on the network MikroView sits on. The enrolment window opens for this address alone, so the log port never opens to anyone else.', placeholder: '192.168.13.1', field: 'addr' },
    { id: 'push', kind: 'yesno', title: 'Push router state', ask: 'Let the router push its rule table, address lists, leases and interfaces?', hint: 'Every 20 minutes the router runs a script that posts its own state to MikroView. With it the fall names its bands after your rules and Entities can count what it sees against what exists. Without it the fall still runs, address-only.', yes: 'Yes, every 20 minutes', no: 'Not now', gives: 'named bands on the fall · rules to tag · leases and interfaces on the map', without: 'the fall stays address-only' },
    { id: 'backup', kind: 'yesno', title: 'Back up the router', ask: 'Back the router up to MikroView every night?', hint: 'At 03:00 the router exports its configuration and posts it; MikroView keeps it encrypted under your key file. Nothing is fetched — the router sends.', yes: 'Yes, nightly at 03:00', no: 'Not now', gives: 'a nightly .backup and .rsc under your key', without: 'no backups kept here' },
    { id: 'pass', kind: 'password', title: 'Mint the token', ask: 'Your password, to mint this router\u2019s enrolment token', hint: 'Minting is what opens the log port for the address above, for 15 minutes, so it asks for your password at that moment. The token goes at the end of the block.', field: 'pass' },
  ];

  const fresh = () => ({
    stage: 'ask', q: 0, name: '', addr: '', pass: '', push: null, backup: null,
    useOther: false, copied: false, evidence: [], refused: false, version: null, standing: null,
    chosen: new Set([0, 1, 2, 3, 4]), tuneCopied: false, tagged: false, tuneSkipped: false, undoOpen: null,
    lines: 0, tail: [], clock: 0, tunedAt: null, showUndoAll: false, finished: false,
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
  const from = () => state.useOther ? story.other : (state.addr || story.addr);
  const logging = () => story.alreadyLog.concat(state.tagged ? [...state.chosen].map(i => story.suggestions[i].prefix) : []);
  const answered = q => { const s = state; return q.kind === 'yesno' ? s[q.id] !== null : q.kind === 'password' ? !!s.pass : q.id === 'addr' ? !!s.addr && !addrProblem(s.addr) : !!s[q.field]; };
  const allAnswered = () => questions.every(answered);
  const current = () => questions[state.q];

  /* The block, as sections. Directions render the assembled text or
     the sections themselves. */
  const sections = () => [
    { id: 'cert', title: 'Trust the certificate', on: true, lines: [
      `/tool fetch url="https://${story.mv}/ca.crt" check-certificate=no dst-path=mikroview-ca.crt`,
      `/certificate import file-name=mikroview-ca.crt passphrase=""`] },
    { id: 'logs', title: 'Send logs', on: true, lines: [
      `/system logging action add name=mikroview target=remote remote=${story.mvHost} remote-port=6514 remote-protocol=tcp remote-log-format=syslog src-address=${from()}`,
      `/system logging add topics=firewall action=mikroview`,
      `/system logging add topics=info,!firewall action=mikroview`] },
    { id: 'push', title: 'Push router state', on: state.push === true, lines: [
      `/system script add name=mv-push policy=read,test source=":local ruleRecs [:toarray \\"\\"]`,
      `:foreach i,v in=[/ip/firewall/filter print as-value] do={ :local rec {\\"ordinal\\"=\\$i; \\"comment\\"=(\\$v->\\"comment\\"); \\"chain\\"=(\\$v->\\"chain\\"); \\"action\\"=(\\$v->\\"action\\"); \\"logPrefix\\"=(\\$v->\\"log-prefix\\"); \\"log\\"=(\\$v->\\"log\\")}; :set ruleRecs (\\$ruleRecs, {\\$rec}) }`,
      `:local payload [:serialize to=json value={\\"kind\\"=\\"filter-rule\\"; \\"routerosVersion\\"=[/system/resource get version]; \\"wizardVersion\\"=3; \\"records\\"=\\$ruleRecs}]`,
      `/tool fetch url=\\"https://${story.mv}/api/ingest/routeros\\" http-method=post http-data=\\$payload http-header-field=(\\"Content-Type: application/json,Authorization: Bearer mvt-demo000000000000\\") check-certificate=yes output=none`,
      `\u2026 address-list, dhcp-lease, arp, ip-address, wireguard and logging pages follow the same shape \u2026"`,
      `/system scheduler add name=mv-push interval=20m policy=read,test on-event="/system script run mv-push"`,
      `/system script run mv-push`] },
    { id: 'backup', title: 'Back up nightly', on: state.backup === true, lines: [
      `/system script add name=mv-backup policy=read,test,sensitive,ftp source="/system backup save name=mv dont-encrypt=yes; /export file=mv; /tool fetch url=\\"https://${story.mv}/api/ingest/backup\\" http-method=post src-path=mv.backup http-header-field=(\\"Authorization: Bearer mvt-demo000000000000\\") check-certificate=yes output=none"`,
      `/system scheduler add name=mv-backup start-time=03:00:00 interval=1d policy=read,test,sensitive,ftp on-event="/system script run mv-backup"`] },
    { id: 'enrol', title: 'Enrol', on: true, lines: [`/log info "mikroview-enrol ${story.token}"`] },
  ];
  const pasteBlock = () => sections().filter(s => s.on).map(s => s.lines.join('\n')).join('\n');
  const pasteLines = () => pasteBlock().split('\n').length;
  const tagBlock = () => [...state.chosen].sort().map(i => { const s = story.suggestions[i]; return `/ip firewall filter set [find chain=${s.chain} action=${s.action} comment="${s.comment}"] log=yes log-prefix="${s.prefix}"`; }).join('\n');
  const undoLines = {
    cert: ['/certificate remove [find name=mikroview-ca.crt]'],
    logs: ['/system logging remove [find action=mikroview]', '/system logging action remove [find name=mikroview]'],
    push: ['/system scheduler remove [find name=mv-push]', '/system script remove [find name=mv-push]'],
    backup: ['/system scheduler remove [find name=mv-backup]', '/system script remove [find name=mv-backup]'],
    tune: () => [...state.chosen].sort().map(i => `/ip firewall filter set [find comment="${story.suggestions[i].comment}"] log=no log-prefix=""`),
  };
  const fixSrc = () => `/system logging action set [find name=mikroview] src-address=${state.addr || story.addr}`;

  /* ---- the simulated router ---- */
  const srcs = ['203.0.113.7', '198.51.100.23', '45.155.205.99', '185.220.101.4', '192.168.13.42', '192.168.13.77', '10.66.0.7', '91.240.118.12'];
  const ports = [22, 443, 3389, 23, 8080, 445, 5060, 1900, 53];
  const rnd = a => a[Math.floor(Math.random() * a.length)];
  const line = () => {
    const pre = rnd(logging());
    const proto = pre === 'icmp' ? 'ICMP' : rnd(['TCP (SYN)', 'TCP (ACK)', 'UDP']);
    const iface = pre === 'wg-in' ? 'wg0' : pre === 'guest-lan' ? 'bridge-guest' : rnd(['ether1', 'ether1', 'bridge']);
    const chain = ['in-ssh', 'wan-in', 'wg-in'].includes(pre) ? 'input' : 'forward';
    const dst = chain === 'input' ? '81.2.69.140' : rnd(['192.168.13.40', '192.168.13.12', '8.8.8.8', '142.250.187.206']);
    const src = rnd(srcs);
    const action = pre === 'nat' || pre === 'est-rel' ? 'accept' : 'drop';
    return { t: hms(now()), pre, chain, iface, proto, src, dst, port: rnd(ports), action, text: `${pre} ${chain}: in:${iface} out:${chain === 'input' ? '(unknown 0)' : rnd(['ether1', 'bridge'])}, proto ${proto}, ${src}:${40000 + Math.floor(Math.random() * 20000)}->${dst}:${rnd(ports)}, len ${40 + Math.floor(Math.random() * 1400)}` };
  };
  function startStream() {
    every(420, () => {
      const n = 1 + Math.floor(Math.random() * 3);
      for (let i = 0; i < n; i++) state.tail.push(line());
      if (state.tail.length > 40) state.tail.splice(0, state.tail.length - 40);
      state.lines += 3 + Math.floor(Math.random() * 7);
      D.tick && D.tick(state);
    });
  }
  function add(e) { e.t = hms(now()); state.evidence.push(e); render(); }
  const scenario = () => (document.getElementById('scenario') || { value: 'happy' }).value;
  function startRouter() {
    copyAt = Date.now(); baseAt = copyAt; clockBase = 0;
    every(250, () => { state.clock = clock(); D.tick && D.tick(state); });
    later(story.at.cert, () => add({ k: 'cert', h: `Certificate fetched by ${from()}` }));
    if (scenario() === 'wrongaddr' && !state.useOther) {
      later(story.at.refused, () => { state.refused = true; render(); });
      return;
    }
    schedule(story.at.enrol - story.at.cert, story.at.cert);
  }
  function schedule(enrolIn, base) {
    later(base + enrolIn, () => {
      add({ k: 'enrol', h: `Enrol line from ${from()}` }); startStream();
      if (state.push) later(story.at.push - story.at.enrol, () => {
        state.version = scenario() === 'ahead' ? '7.25.1' : '7.24.4'; state.standing = scenario() === 'ahead' ? 'ahead' : 'reviewed';
        add({ k: 'push', h: `First push from ${state.name}` });
        if (state.backup) later(1, () => add({ k: 'backup', h: 'Nightly backup scheduled' }));
      });
      else if (state.backup) later(3, () => add({ k: 'backup', h: 'Nightly backup scheduled' }));
    });
  }
  /* Which boundaries are watched right now: those with a prefix already
     logging, plus — once tagging has landed — those whose proposal was
     ticked. Before the push nothing is known, so nothing is drawn. */
  const watched = b => b.logs.length > 0 || (state.tagged && b.sug !== null && state.chosen.has(b.sug));
  const wouldWatch = b => b.logs.length > 0 || (b.sug !== null && state.chosen.has(b.sug));
  const arrivedAll = () => has('enrol') && (!state.push || has('push')) && (!state.backup || has('backup'));

  /* ---- actions ---- */
  function next() { if (D.next && D.next(state)) { render(); return; } if (state.q < questions.length - 1) { state.q++; } else if (allAnswered()) { state.stage = 'paste'; } render(); }
  function act(a, d = {}) {
    switch (a) {
      case 'yes': case 'no': { const q = current(); if (q.kind === 'yesno') { state[q.id] = a === 'yes'; next(); } break; }
      case 'next': { const q = current(); if (answered(q)) next(); break; }
      case 'back': if (state.stage === 'paste') { state.stage = 'ask'; state.q = questions.length - 1; } else if (state.q > 0) state.q--; render(); break;
      case 'goto': { const i = Number(d.q); if (state.stage === 'ask' || state.stage === 'paste') { state.stage = 'ask'; state.q = i; render(); } break; }
      case 'set': { const q = questions.find(x => x.id === d.q); if (q && q.kind === 'yesno' && (state.stage === 'ask' || state.stage === 'paste')) { state[q.id] = d.v === 'yes'; render(); } break; }
      case 'copy': if (state.copied) break; state.copied = true; render(); later(2, () => { state.stage = 'watch'; render(); startRouter(); }); break;
      case 'use-other': state.useOther = true; state.refused = false; render(); schedule(4, 0); break;
      case 'reroll': alert('Prototype: a new token would be minted (password asked again) and the block rewritten; the old token dies.'); break;
      case 'to-tune': if (arrivedAll()) { state.stage = state.push ? 'tune' : 'done'; render(); } break;
      case 'toggle': { const i = Number(d.i); state.chosen.has(i) ? state.chosen.delete(i) : state.chosen.add(i); render(); break; }
      case 'tune-copy': if (!state.chosen.size) break; state.tuneCopied = true; render(); later(story.at.tagged, () => { state.tagged = true; state.tunedAt = hms(now()); render(); }); break;
      case 'tune-skip': state.tuneSkipped = true; state.stage = 'done'; render(); break;
      case 'to-done': state.stage = 'done'; render(); break;
      case 'undo': state.undoOpen = state.undoOpen === d.u ? null : d.u; render(); break;
      case 'undo-all': state.showUndoAll = !state.showUndoAll; render(); break;
      case 'fall': alert('Prototype: opens the fall with ' + state.name + ' already flowing.'); break;
      case 'finish': state.finished = true; render(); break;
      case 'another': alert('Prototype: the same questions again for the next router.'); break;
      case 'reset': if (confirm('Prototype: forgets ' + (state.name || 'this router') + ' on MikroView — its token, its enrolment window and its record — and shows the lines that undo the router side. Start again?')) restart(); break;
      case 'leave': alert('Prototype: closes setup. Run setup… reopens it exactly here.'); break;
      case 'restart': restart(); break;
    }
  }
  function restart() { clearAll(); copyAt = 0; Object.assign(state, fresh()); render(); }
  function render() { D.render(state); D.tick && D.tick(state); }

  /* ---- wiring, shared ---- */
  document.addEventListener('click', e => { const b = e.target.closest('[data-act]'); if (b && !b.disabled) act(b.dataset.act, b.dataset); });
  document.addEventListener('input', e => {
    const f = e.target.dataset.field; if (!f) return;
    state[f] = f === 'pass' ? e.target.value : e.target.value.trim();
    D.field && D.field(f, state);
  });
  document.addEventListener('keydown', e => {
    if (e.target.matches('input') && e.key === 'Enter') { e.preventDefault(); act('next'); }
    if (D.key) D.key(e, state);
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
    if (location.search.includes('demo')) Object.assign(state, { name: story.router, addr: story.addr, pass: 'correct horse', push: true, backup: true, q: 4 });
    render();
  }
  const esc = s => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;');
  return { story, questions, state, boot, act, render, later, has, ev, from, logging, answered, allAnswered, current, sections, pasteBlock, pasteLines, tagBlock, undoLines, fixSrc, arrivedAll, addrProblem, watched, wouldWatch, hms, hm, now, esc };
})();
