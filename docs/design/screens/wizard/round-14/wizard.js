/* Round 11's wizard: round 10's AG steps, bodies, bar, strip and fall,
   carried forward verbatim. What is new is the seam at the bottom: a
   direction supplies wayIn() and wayOut(), and calls showWizard(),
   showFall() and fallLive() at its own beats. */
const $ = s => document.querySelector(s); const esc = MV.esc; const S = MV.story; const Q = MV.questions;
const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
const t = k => MV.ev(k) ? MV.ev(k).t : '';
/* ---- steps: five, gated ---- */
const STEPS = [
  { id: 'router', title: 'The router' },
  { id: 'pass', title: 'Mint the token' },
  { id: 'paste', title: 'Paste once' },
  { id: 'tune', title: 'Tag firewall rules' },
  { id: 'stand', title: 'Where setup stands' },
];
const routerDone = s => !!s.name && !!s.addr && !MV.addrProblem(s.addr) && s.push !== null && s.backup !== null;
const stageOf = s => s.stage === 'ask' ? (s.q === 4 ? 'pass' : 'router') : s.stage === 'watch' ? 'paste' : s.stage === 'done' ? 'stand' : s.stage;
/* which steps are reached: everything up to the furthest the run has got */
const reachedIdx = s => s.stage === 'done' ? 4 : s.stage === 'tune' ? 3 : (s.stage === 'paste' || s.stage === 'watch') ? 2 : s.q === 4 ? 1 : 0;
function rowState(s, id) {
  const a = { cls: '', receipt: '' };
  switch (id) {
    case 'router': if (MV.has('enrol')) { a.cls = 'done'; a.receipt = `${s.name} · enrolled ${MV.from()} · ${t('enrol')}`; } else if (routerDone(s)) { a.cls = 'chosen'; a.receipt = `${s.name} · ${s.addr} · push ${s.push ? 'yes' : 'not now'} · backup ${s.backup ? 'yes' : 'not now'}`; } else a.receipt = 'name, address, push, backup'; break;
    case 'pass': if (s.pass) { a.cls = MV.has('cert') ? 'done' : 'chosen'; a.receipt = `token good until ${S.tokenUntil}`; } else a.receipt = 'your password, once'; break;
    case 'paste': if (MV.arrivedAll()) { a.cls = 'done'; a.receipt = `everything arrived · ${s.lines.toLocaleString()} lines`; } else if (MV.has('cert')) { a.cls = 'chosen'; a.receipt = 'certificate fetched · waiting'; } else if (s.copied) a.receipt = 'copied · waiting for the certificate'; else a.receipt = 'one block, into the terminal'; break;
    case 'tune': if (s.push === false) a.receipt = 'needs the push'; else if (s.tagged) { a.cls = 'done'; a.receipt = `${s.chosen.size} rules tagged · ${s.tunedAt}`; } else if (s.tuneSkipped) { a.cls = 'chosen'; a.receipt = 'left dark'; } else a.receipt = 'proposed from the push'; break;
    case 'stand': a.cls = s.stage === 'done' ? 'done' : ''; break;
  }
  return a;
}
function renderSteps(s) {
  const cur = stageOf(s); const reached = reachedIdx(s);
  $('#steps').innerHTML = STEPS.map((d, i) => { const r = rowState(s, d.id); const locked = i > reached; const can = !locked && i < reached && s.stage !== 'watch' && s.stage !== 'tune' && s.stage !== 'done'; return `<li><button type="button" class="step-row ${r.cls} ${locked ? 'locked' : ''} ${cur === d.id ? 'current' : ''}" ${can ? `data-act="goto-step" data-i="${i}"` : 'disabled'} aria-current="${cur === d.id ? 'step' : 'false'}" ${locked ? 'aria-disabled="true" title="After the step before it"' : ''}><span class="step-n">${d.id === 'stand' ? '✓' : i + 1}</span><span class="step-text"><span class="step-title">${d.title}</span>${r.receipt ? `<span class="step-receipt">${esc(r.receipt)}</span>` : ''}</span></button></li>`; }).join('');
}
/* ---- bodies ---- */
function routerBody(s) {
  const prob = MV.addrProblem(s.addr);
  const seg = (id, yes, no) => `<div class="seg" role="radiogroup"><button type="button" class="${s[id] === true ? 'on' : ''}" data-act="set" data-q="${id}" data-v="yes" role="radio" aria-checked="${s[id] === true}">${yes}</button><button type="button" class="${s[id] === false ? 'on no' : ''}" data-act="set" data-q="${id}" data-v="no" role="radio" aria-checked="${s[id] === false}">${no}</button></div>`;
  return `<h3>Your first router.</h3><p class="hint">Four things, then one paste. Nothing touches the router until you paste, and MikroView never connects to it — the router sends.</p>
  <div class="form">
    <label class="k" for="f-name">Name<small>What MikroView calls it on the fall, the stream and Entities.</small></label><div class="v"><input id="f-name" type="text" data-field="name" value="${esc(s.name)}" placeholder="rb5009" autocomplete="off"></div>
    <label class="k" for="f-addr">Its address<small>Its own address on the network MikroView sits on. The enrolment window opens for this address alone.</small></label><div class="v"><input id="f-addr" type="text" data-field="addr" value="${esc(s.addr)}" placeholder="192.168.13.1" autocomplete="off" class="${prob ? 'bad' : ''}" aria-invalid="${prob ? 'true' : 'false'}"><p class="problem" data-problem aria-live="polite">${esc(prob)}</p></div>
    <label class="k">Push router state<small>Every 20 minutes the router posts its rule table, address lists, leases and interfaces.</small></label><div class="v">${seg('push', 'Yes, every 20 minutes', 'Not now')}<div class="gives"><b>Yes</b> — named bands on the fall, rules to tag, leases and interfaces on the map. <i>Not now</i> — the fall stays address-only.</div></div>
    <label class="k">Back up nightly<small>At 03:00 the router exports its configuration and posts it, kept encrypted under your key file.</small></label><div class="v">${seg('backup', 'Yes, nightly at 03:00', 'Not now')}<div class="gives"><b>Yes</b> — a nightly .backup and .rsc under your key. <i>Not now</i> — no backups kept here.</div></div>
  </div>`;
}
function passBody(s) {
  return `<h3>Your password, to mint ${esc(s.name)}’s token.</h3><p class="hint">Minting opens the log port for ${esc(s.addr)}, for 15 minutes, so it asks for your password at that moment. The token goes at the end of the block.</p><div class="form" style="grid-template-columns:auto"><div class="v"><input type="password" data-field="pass" value="${esc(s.pass)}" placeholder="password" aria-label="Your password" style="width:320px"></div></div>`;
}
function blockHtml() { return MV.sections().filter(x => x.on).map((x, i) => `<span class="sec"># ${i + 1} · ${x.title}</span>\n${x.lines.map(esc).join('\n')}`).join('\n'); }
function pasteBody(s) {
  const secs = MV.sections().filter(x => x.on);
  return `<h3>One paste.</h3><p class="hint">${secs.length} parts, in order — ${secs.map(x => x.title.toLowerCase()).join(', ')} — and the enrol line last. Into the router’s <b>terminal</b> (WinBox ▸ New Terminal, or ssh), not a script.</p>
  <pre class="script" aria-label="The block to paste">${blockHtml()}</pre>
  <div class="copyrow"><button type="button" class="primary" data-act="copy" ${s.copied ? 'disabled' : ''}>${s.copied ? 'Copied' : 'Copy'}</button><span class="token-life">Token good until ${S.tokenUntil} (15 minutes) · <button type="button" class="linkish" data-act="reroll">Reroll</button></span></div>
  <div class="obs waiting">${s.copied ? 'Copied. Waiting for the router — the certificate fetch comes first.' : 'Nothing has arrived yet. Copy, paste, and this line will say what the router did.'}</div>`;
}
function watchBody(s) {
  const refused = s.refused ? `<div class="warnbox" style="max-width:720px"><span><b>Lines from ${S.other} arrived without the enrol line and were refused.</b> If that is this router, its logging action is sending from another address — usually an older <span class="mono">remote</span> action, or a <span class="mono">src-address</span> left from a previous setup. Fix it on the router, then paste the last line of the block again:</span><pre>${esc(MV.fixSrc())}\n${esc(MV.sections().at(-1).lines[0])}</pre><span>Or, if ${S.other} is the address it should send from: <button type="button" class="linkish" data-act="use-other">enrol at ${S.other} instead</button>.</span></div>` : '';
  const ahead = MV.has('push') && s.standing === 'ahead' ? `<div class="cautionbox" style="max-width:720px"><b>RouterOS ${s.version} is newer than these commands were reviewed on (${S.reviewed}).</b> They ran and the push arrived. If anything reads wrong, this is the first suspect.</div>` : '';
  const stream = MV.has('enrol') ? `<div class="stream" aria-label="Lines arriving">${s.tail.slice(-6).map(l => `<div class="l"><span class="dim">${l.t}</span> <b>${l.pre}</b> ${esc(l.text.slice(l.pre.length))}</div>`).join('')}</div>` : '';
  const last = TRACK.stations(s).filter(x => x.state === 'done').at(-1);
  const latest = last && last.id !== 'copy' ? `<div class="obs arrived">${esc(MV.ev(last.undo === 'logs' ? 'enrol' : last.id) ? MV.ev(last.undo === 'logs' ? 'enrol' : last.id).h : last.lab)}<small>${esc(last.st.split(' · ')[0])}</small></div>` : `<div class="obs waiting">Copied. Waiting for the router — the certificate fetch comes first.</div>`;
  const lead = MV.arrivedAll() ? `<h3>${esc(s.name)} is sending.</h3><p class="hint">Everything you chose has arrived; each station stands on evidence.</p>` : `<h3>The router’s turn.</h3><p class="hint">The block is on the router. Each part answers in its own time — every station that lights is something that arrived.</p>`;
  return `${lead}${TRACK.render(s)}${latest}${refused}${ahead}${stream}`;
}
function tuneBody(s) {
  const sug = S.suggestions;
  const obs = s.tagged ? `<div class="obs counting">${s.chosen.size} rules logging · first new line ${s.tunedAt}<small>${s.lines.toLocaleString()} lines</small></div>` : s.tuneCopied ? `<div class="obs waiting">Waiting for the first line carrying a new prefix.</div>` : `<div class="obs quiet">Nothing to wait for until you paste — the rule table is already here.</div>`;
  const rows = sug.map((r, i) => { const on = s.chosen.has(i); return `<label class="${s.tagged && on ? 'lit' : ''}"><input type="checkbox" data-act="toggle" data-i="${i}" ${on ? 'checked' : ''} ${s.tuneCopied ? 'disabled' : ''}><span class="mono">${r.chain}</span><span class="act ${r.action}">${r.action}</span><span>${esc(r.comment)}</span><span class="why">${esc(r.why)}</span><span class="pre">${r.prefix}</span></label>`; }).join('');
  return `<h3>${sug.length} rules log nothing.</h3><p class="hint">The push read ${S.rules} rules; ${S.alreadyLog.length} already log. These ${sug.length} sit on boundaries nobody watches — a rule, but no line. Tick the ones to tag; this is the only second paste.</p>
  <div class="rules"><label class="h"><span></span><span>chain</span><span>action</span><span>rule</span><span>why it matters</span><span>prefix</span></label>${rows}</div>
  <pre class="script" aria-label="Tagging block" style="max-width:760px">${esc(MV.tagBlock()) || '<span class="sec"># nothing ticked</span>'}</pre>
  <div class="copyrow"><button type="button" class="primary" data-act="tune-copy" ${s.tuneCopied || !s.chosen.size ? 'disabled' : ''}>${s.tuneCopied ? 'Copied' : `Copy — ${s.chosen.size} rule${s.chosen.size === 1 ? '' : 's'}`}</button><span class="note">Safe to paste again; it sets, never adds.</span></div>${obs}`;
}
function doneBody(s) {
  const rows = [
    { done: true, t: 'Certificate trusted', r: `fetched by ${MV.from()} · ${t('cert')}`, u: 'cert' },
    { done: true, t: 'Logs flowing', r: `enrolled at ${MV.from()} · ${t('enrol')} · ${s.lines.toLocaleString()} lines`, u: 'logs' },
    s.push ? { done: true, t: 'Router state pushed', r: `every 20 minutes · first ${t('push')} · RouterOS ${s.version}`, u: 'push' } : { done: false, t: 'Router state', r: 'not now · the fall stays address-only' },
    s.backup ? { done: true, t: 'Nightly backup', r: `03:00 · scheduled ${t('backup')}`, u: 'backup' } : { done: false, t: 'Backup', r: 'not now · no backups kept here' },
    s.push ? (s.tagged ? { done: true, t: 'Rules tagged', r: `${s.chosen.size} rules log · since ${s.tunedAt}`, u: 'tune' } : { done: false, t: 'Rules', r: 'left dark · 5 boundaries log nothing' }) : null,
  ].filter(Boolean);
  const n = rows.filter(r => r.done).length, k = rows.length - n;
  const undoAll = s.showUndoAll ? `<pre style="max-width:720px">${esc([...MV.undoLines.cert, ...MV.undoLines.logs, ...(s.push ? MV.undoLines.push : []), ...(s.backup ? MV.undoLines.backup : []), ...(s.tagged ? MV.undoLines.tune() : [])].join('\n'))}</pre><p class="note">Then <button type="button" class="linkish" data-act="reset">forget ${esc(s.name)} on MikroView</button> — its token, its enrolment and its record.</p>` : '';
  return `<h3>${esc(s.name)} is sending.</h3><p class="hint">${n} ${n === 1 ? 'thing stands' : 'things stand'} on evidence${k ? `; ${k} ${k === 1 ? 'was' : 'were'} set aside` : ''}. Finish takes you to the fall, with ${esc(s.name)} already flowing.</p>${TRACK.render(s, { compact: true })}
  <div class="ledger">${rows.map(r => `<div class="row ${r.done ? '' : 'skip'}"><span class="step-n ${r.done ? 'done' : 'skip'}">${r.done ? '✓' : '–'}</span><span>${r.t}<div class="r ${r.done ? 'done' : 'skip'}">${esc(r.r)}</div></span>${r.u ? `<button type="button" class="linkish u" data-act="undo" data-u="${r.u}">${s.undoOpen === r.u ? 'Hide' : 'Undo'}</button>` : '<span></span>'}${s.undoOpen === r.u && r.u ? `<div class="undo"><pre>${esc(TRACK.undoText(r.u))}</pre><p class="note">Paste on the router. MikroView notices when the lines stop and this row goes back to waiting.</p></div>` : ''}</div>`).join('')}</div>
  <p class="note">Admin ▸ Run setup… reopens this ledger any time. To start again from nothing, <button type="button" class="linkish" data-act="undo-all">${s.showUndoAll ? 'hide the undo lines' : 'undo everything on the router first'}</button>.</p>${undoAll}`;
}
function renderFoot(s) {
  let l = '', r = '', hint = ''; const st = stageOf(s);
  if (st === 'router') { hint = routerDone(s) ? '' : 'All four, then Next'; r = `<button type="button" class="primary" data-act="router-next" ${routerDone(s) ? '' : 'disabled'}>Next</button>`; }
  else if (st === 'pass') { l = `<button type="button" data-act="goto-step" data-i="0">Back</button>`; r = `<button type="button" class="primary" data-act="next" ${s.pass ? '' : 'disabled'}>Mint the token</button>`; }
  else if (st === 'paste' && s.stage === 'paste') { l = `<button type="button" data-act="goto-step" data-i="1">Back</button>`; hint = 'Next checks what has arrived'; r = `<button type="button" class="primary" disabled>Next</button>`; }
  else if (st === 'paste') { hint = MV.arrivedAll() ? '' : 'Next checks what has arrived'; r = `<button type="button" class="primary" data-act="to-tune" ${MV.arrivedAll() ? '' : 'disabled'}>Next</button>`; }
  else if (st === 'tune') { r = `<button type="button" data-act="tune-skip">Skip this step</button><button type="button" class="primary" data-act="to-done" ${s.tagged ? '' : 'disabled'}>Next</button>`; hint = s.tagged ? '' : 'Next checks what has arrived'; }
  else if (st === 'stand') { l = `<button type="button" data-act="another">Add another router</button>`; r = `<button type="button" class="primary" data-act="finish">Finish</button>`; }
  $('#foot').innerHTML = `${l}<span class="fhint">${hint}</span>${r}`;
}
/* the bar: wordmark, chips, strip */
function renderBar(s) {
  const chips = [];
  if (s.name) chips.push(`<span class="att dec"><i></i>${esc(s.name)}</span>`);
  if (s.refused) chips.push(`<span class="att alarm"><i></i>refused · ${S.other}</span>`);
  if (MV.has('cert')) chips.push(`<span class="att ok"><i></i>cert · ${t('cert')}</span>`);
  if (MV.has('enrol')) chips.push(`<span class="att ok"><i></i>logs · ${t('enrol')}</span>`);
  if (MV.has('push')) { chips.push(`<span class="att ok"><i></i>push · 20 min</span>`); const dark = S.boundaries.filter(b => !MV.watched(b)).length; chips.push(dark ? `<span class="att dark"><i></i>${dark} dark</span>` : `<span class="att ok"><i></i>every boundary watched</span>`); if (s.standing === 'ahead') chips.push(`<span class="att now"><i></i>RouterOS ${s.version} — ahead of review</span>`); }
  if (MV.has('backup')) chips.push(`<span class="att ok"><i></i>backup · 03:00</span>`);
  if (s.tagged) chips.push(`<span class="att ok"><i></i>${s.chosen.size} rules</span>`);
  $('#chips').innerHTML = chips.join('');
  $('#barright').innerHTML = MV.has('enrol') ? `<span class="att ok"><i></i>live · ${(4 + (s.tagged ? 3 : 0))}/s</span><span>${s.lines.toLocaleString()} lines</span>` : `<span class="att"><i></i>no router yet</span>`;
  // the strip: one tick until the push names boundaries; then one per boundary
  const cols = MV.has('push') ? FALL.states(s) : [[{ lane: 'lan' }, MV.has('enrol') ? 'watched' : 'off']];
  $('#strip').innerHTML = FALL.strip(cols);
}
function render(s) {
  if (phase === 'door' || phase === 'in') return;
  renderSteps(s); renderBar(s);
  const b = $('#body'); const focused = document.activeElement && document.activeElement.dataset ? document.activeElement.dataset.field : null;
  const st = stageOf(s);
  b.innerHTML = st === 'router' ? routerBody(s) : st === 'pass' ? passBody(s) : s.stage === 'paste' ? pasteBody(s) : st === 'paste' ? watchBody(s) : st === 'tune' ? tuneBody(s) : doneBody(s);
  renderFoot(s);
  const inp = b.querySelector('input[data-field]'); if (inp && (!focused || focused === inp.dataset.field)) { inp.focus(); const v = inp.value; inp.value = ''; inp.value = v; }
  if (phase === 'fall' || phase === 'out') renderFall(s);
}
function field(f, s) { if (f === 'addr') { const p = MV.addrProblem(s.addr); const el = $('[data-problem]'); if (el) el.textContent = p; const inp = $('[data-field=addr]'); if (inp) { inp.classList.toggle('bad', !!p); inp.setAttribute('aria-invalid', p ? 'true' : 'false'); } } const nb = $('[data-act=router-next]'); if (nb) nb.disabled = !routerDone(s); const mb = $('[data-act=next]'); if (mb) mb.disabled = !s.pass; }
/* our own step order, hooked into the engine's next() */
function next(s) {
  if (s.stage !== 'ask') return false;
  if (s.q < 4) { if (routerDone(s)) { s.q = 4; } return true; }
  if (s.pass) { s.stage = 'paste'; } return true;
}
let lastTick = 0;
function tick(s) { if (s.stage !== 'watch' && s.stage !== 'tune' && phase !== 'fall') return; const now = Date.now(); if (now - lastTick < 400) return; lastTick = now; const r = document.querySelector('.body .track'); if (r && s.stage === 'watch') r.outerHTML = TRACK.render(s); const st = document.querySelector('.stream'); if (st) st.innerHTML = s.tail.slice(-6).map(l => `<div class="l"><span class="dim">${l.t}</span> <b>${l.pre}</b> ${esc(l.text.slice(l.pre.length))}</div>`).join(''); const c = document.querySelector('.obs.counting small'); if (c) c.textContent = s.lines.toLocaleString() + ' lines'; const br = $('#barright'); if (br && MV.has('enrol')) br.innerHTML = `<span class="att ok"><i></i>live · ${(4 + (s.tagged ? 3 : 0))}/s</span><span>${s.lines.toLocaleString()} lines</span>`; if (phase === 'fall') paintFall(s); }
/* ---- the fall, after the way out ---- */
function fallCols(s) { return MV.has('push') ? FALL.states(s) : [[{ id: 'all', name: (s.name || 'your router') + ' · all traffic', sub: 'address-only until the push names boundaries', lane: 'lan', logs: [], sug: null }, 'addr']]; }
function renderFall(s, pre) {
  const cols = fallCols(s);
  $('#cols').style.gridTemplateColumns = `repeat(${cols.length}, 1fr)`;
  $('#cols').innerHTML = cols.map(([b, st], i) => FALL.header(b, st, st === 'addr' ? { label: 'address-only — no rule table yet' } : {}).replace('class="col ', `class="col ${pre ? 'pre' : ''} `).replace('<div class="col', `<div style="transition-delay:${i * 70}ms" class="col`)).join('');
  paintFall(s);
}
function paintFall(s) {
  const fb = $('#fb'); const W = fb.clientWidth || 1200, H = fb.clientHeight || 700; const cols = fallCols(s);
  fb.innerHTML = FALL.body(cols, s, W, H, { nowCaption: false, colOf: MV.has('push') ? null : () => 0, now: true });
  const ax = $('#axis'); const top = 74, pxs = 2.2; let lab = `<span class="now-t" style="top:${top - 30}px;line-height:1.2">NOW<br>${MV.hms(MV.now())}</span>`; for (let k = 1; k <= 6; k++) { const secs = k * 30; lab += `<span style="top:${top + secs * pxs - 6}px">${MV.hm(MV.now() - secs)}</span>`; } ax.innerHTML = lab;
}
/* ---- the journeys are the direction's; this is the seam ---- */
let phase = 'door'; let J = { wayIn() {}, wayOut() {} };
const beat = (ms, f) => setTimeout(f, reduced ? Math.min(ms, 300) : ms);
/* the wizard stands (page live, steps rendered); `animate` lets the rail and body arrive by their own transitions */
function showWizard(animate = true) {
  $('#page').setAttribute('aria-hidden', 'false'); phase = 'wiz'; render(MV.state);
  const go = () => { $('#rail').classList.remove('away'); $('#body').classList.remove('away'); $('#foot').classList.remove('away'); $('#page').classList.add('live'); const inp = $('[data-field=name]'); if (inp) inp.focus(); };
  if (animate) requestAnimationFrame(() => requestAnimationFrame(go)); else go();
  document.body.classList.add('wiz');
}
/* the fall stands in the page (bar and strip stay; the body is gone) */
function showFall(pre = false) { const s = MV.state; document.body.classList.add('out'); renderFall(s, pre); $('#fallframe').classList.add('rising'); }
function fallLive() { document.body.classList.add('fall'); phase = 'fall'; paintFall(MV.state); }
document.addEventListener('click', e => {
  const b = e.target.closest('[data-act]'); if (!b || b.disabled) return;
  const a = b.dataset.act; const s = MV.state;
  if (a === 'enter') { if (phase === 'door') { phase = 'in'; J.wayIn(); } }
  else if (a === 'router-next') { if (routerDone(s)) { s.q = 4; MV.render(); } }
  else if (a === 'goto-step') { const i = Number(b.dataset.i); if (s.stage === 'ask' || s.stage === 'paste') { s.stage = 'ask'; s.q = i === 0 ? 0 : 4; if (i === 2) s.stage = 'paste'; MV.render(); } }
  else if (a === 'finish') { if (phase === 'wiz') { phase = 'out'; J.wayOut(); } }
});
document.addEventListener('change', e => { if (e.target.matches('input[type=checkbox][data-act=toggle]')) { e.target.checked = MV.state.chosen.has(Number(e.target.dataset.i)); } });
document.addEventListener('keydown', e => { if (phase === 'door' && e.key === 'Enter') { e.preventDefault(); phase = 'in'; J.wayIn(); } });
window.addEventListener('resize', () => { if (phase === 'fall') paintFall(MV.state); });
window.WIZ = {
  boot(journey) {
    J = journey; MV.boot({ render, field, tick, next });
    if (location.search.includes('skipdoor')) { document.body.classList.add('in', 'wiz'); $('#door').remove(); showWizard(false); }
  },
  showWizard, showFall, fallLive, beat, reduced, get phase() { return phase; }, set phase(p) { phase = p; }, $: $,
};
