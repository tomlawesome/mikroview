/* The track, shared by AE and AF: the round-8 "enrolment line" (four
   identical green boxes and a separate ruler) redrawn as one thing. Pure
   function of the engine state; CSS in inks.css. */
window.TRACK = (() => {
  const esc = s => MV.esc(s); const S = () => MV.story;
  const t = k => MV.ev(k) ? MV.ev(k).t : '';
  /* the stations this run has, in wire order */
  function stations(s) {
    const st = [];
    st.push({ id: 'copy', ink: 'token', lab: 'copied', st: s.copied ? MV.hms(S().t0) : 'not yet', state: s.copied ? 'done' : 'wait', at: 0 });
    st.push({ id: 'cert', ink: 'cert', lab: 'certificate', st: MV.has('cert') ? t('cert') : 'fetches /ca.crt', state: MV.has('cert') ? 'done' : s.copied ? 'wait' : 'later', at: S().at.cert, undo: 'cert' });
    if (s.refused) st.push({ id: 'enrol', ink: 'logs', lab: 'refused', st: `lines from ${S().other}`, state: 'alarm', at: S().at.refused });
    else st.push({ id: 'enrol', ink: 'logs', lab: 'logs', st: MV.has('enrol') ? `${t('enrol')} · ${s.lines.toLocaleString()} lines` : `enrol line from ${MV.from()}`, state: MV.has('enrol') ? 'done' : MV.has('cert') ? 'wait' : 'later', at: S().at.enrol, undo: 'logs' });
    if (s.push === false) st.push({ id: 'push', ink: 'push', lab: 'push', st: 'not now · address-only', state: 'skip', at: S().at.push });
    else st.push({ id: 'push', ink: 'push', lab: 'push', st: MV.has('push') ? `${t('push')} · v${s.version}` : 'end of the block', state: MV.has('push') ? 'done' : MV.has('enrol') ? 'wait' : 'later', at: S().at.push, undo: 'push' });
    if (s.backup === false) st.push({ id: 'backup', ink: 'backup', lab: 'backup', st: 'not now · none kept here', state: 'skip', at: S().at.backup });
    else st.push({ id: 'backup', ink: 'backup', lab: 'backup', st: MV.has('backup') ? `${t('backup')} · 03:00` : 'after the push', state: MV.has('backup') ? 'done' : (MV.has('push') || (s.push === false && MV.has('enrol'))) ? 'wait' : 'later', at: S().at.backup, undo: 'backup' });
    if (s.push !== false && (s.stage === 'tune' || s.stage === 'done' || s.tagged || s.tuneSkipped)) {
      if (s.tagged) st.push({ id: 'rules', ink: 'rules', lab: `${s.chosen.size} rules`, st: `lit since ${s.tunedAt}`, state: 'done', lanes: [...s.chosen].map(i => S().boundaries.find(b => b.sug === i)).filter(Boolean).map(b => b.lane), undo: 'tune' });
      else if (s.tuneSkipped) st.push({ id: 'rules', ink: 'rules', lab: 'rules', st: 'left dark', state: 'skip' });
      else st.push({ id: 'rules', ink: 'rules', lab: 'rules', st: s.tuneCopied ? 'waiting for the first new line' : 'touch a dark column', state: s.tuneCopied ? 'wait' : 'later' });
    }
    return st;
  }
  /* opts: compact (no stamps), undo (show undo links under done stations), open (which undo is open) */
  function render(s, opts = {}) {
    const st = stations(s); const n = st.length;
    const lastDone = st.reduce((a, x, i) => x.state === 'done' ? i : a, -1);
    const centre = i => ((i + 0.5) / n) * 100;
    // segments: from each done station to the next, in the next's ink if it is done, else the wire stays grey
    let segs = '';
    for (let i = 0; i < lastDone; i++) segs += `<div class="seg" style="left:${centre(i)}%;width:${centre(i + 1) - centre(i)}%;background:var(--ink-${st[i + 1].ink})"></div>`;
    // the NOW cursor: past the last arrival by the fraction of the expected gap that has elapsed
    let now = '';
    if (s.copied && lastDone >= 0 && lastDone < n - 1 && s.stage !== 'done') {
      const next = st.slice(lastDone + 1).find(x => x.state !== 'skip') || st[lastDone + 1]; const ni = st.indexOf(next);
      const from = st[lastDone].at || 0, to = next.at || from + 10; const frac = Math.max(0, Math.min(0.92, (s.clock - from) / Math.max(1, to - from)));
      now = `<div class="now" style="left:calc(${centre(lastDone) + (centre(ni) - centre(lastDone)) * frac}% - 1px)"></div>`;
    }
    const body = st.map(x => `<div class="stn ${x.state}" style="--ink:var(--ink-${x.ink})"><div class="dot"></div><div class="lab">${esc(x.lab)}</div>${opts.compact ? '' : `<div class="st">${esc(x.st)}</div>`}${x.lanes ? `<div class="sub">${x.lanes.map(l => `<i style="--lane:var(--lane-${l})"></i>`).join('')}</div>` : ''}${opts.undo && x.undo && x.state === 'done' ? `<button type="button" class="linkish u" data-act="undo" data-u="${x.undo}">${opts.open === x.undo ? 'hide' : 'undo'}</button>` : ''}</div>`).join('');
    return `<div class="track ${opts.compact ? 'compact' : ''}" aria-label="What has arrived"><div class="wire"></div>${segs}${now}${body}</div>`;
  }
  const undoText = u => (typeof MV.undoLines[u] === 'function' ? MV.undoLines[u]() : MV.undoLines[u]).join('\n');
  return { stations, render, undoText };
})();
