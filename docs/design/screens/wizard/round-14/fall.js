/* Mini-fall renderers shared by round 8's directions. Pure functions of
   the engine state returning markup; the CSS is fall.css. */
window.FALL = (() => {
  const S = () => MV.story; const esc = s => MV.esc(s);
  const laneVar = l => `var(--lane-${l})`;
  const peaks = { 'wan-in': [[':22 SSH', 'drop', 0.25], [':8291 Winbox', 'accept', 0.7]], fwd: [[':443 HTTPS', 'accept', 0.5]], nat: [[':443 HTTPS', 'accept', 0.55]], guest: [[':445 SMB', 'drop', 0.45]], wg: [[':51820 WG', 'accept', 0.5]], icmp: [['echo', 'accept', 0.5]] };
  const prefixCol = { 'in-ssh': 'wan-in', 'wan-in': 'wan-in', 'est-rel': 'fwd', 'fwd-drop': 'fwd', nat: 'nat', 'guest-lan': 'guest', 'wg-in': 'wg', icmp: 'icmp' };
  /* one column header: name, epithet, status label, lane underline */
  function header(b, st, opts = {}) {
    const cls = st === 'watched' ? 'ch-ok' : st === 'dark' ? 'ch-bad' : st === 'armed' ? 'ch-arm' : 'ch-dim';
    const text = st === 'watched' ? 'watched' : st === 'dark' ? 'dark — no log rule' : st === 'armed' ? 'tagged — waiting for a line' : st === 'proposed' ? 'dark — tick to watch' : opts.label || 'unknown';
    const lane = st === 'watched' || st === 'armed' ? laneVar(b.lane) : 'var(--fg-dim)';
    return `<div class="col ${st === 'dark' || st === 'proposed' ? 'dark' : ''} ${opts.pick ? 'pick' : ''} ${opts.chosen ? 'chosen' : ''}" style="--lane:${lane}" ${opts.attrs || ''}><div class="blab">${esc(b.name)}</div><div class="bsub">${esc(b.sub)}</div><div class="ch ${cls}">${text}</div><div class="bar"></div></div>`;
  }
  /* the strip of ticks above the columns */
  function strip(states) { return `<div class="ovstrip">${states.map(([b, st]) => `<span class="ovtick ${st === 'watched' ? 'on' : st === 'dark' ? 'dark' : ''}" style="background:${st === 'watched' ? laneVar(b.lane) : ''}"></span>`).join('')}</div>`; }
  const wave = (x0, w, h, amp) => { const cx = x0 + w / 2, s = w * 0.28; return `M${x0},${h} C${cx - s},${h} ${cx - s * 0.4},${h - h * amp} ${cx},${h - h * amp} C${cx + s * 0.4},${h - h * amp} ${cx + s},${h} ${x0 + w},${h}`; };
  /* the body: per column, peaks (watched) or hatch (dark); time rows; marks
     for the engine's tail lines; the NOW line. width/height in px. */
  function body(cols, s, W, H, opts = {}) {
    const n = cols.length, gap = 14, cw = (W - gap * (n - 1)) / n; const top = 64; const pxs = opts.pxPerSec || 2.2;
    const now = MV.now();
    let out = `<svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none" aria-hidden="true"><defs><pattern id="fall-hatch" width="8" height="8" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><line x1="0" y1="0" x2="0" y2="8" class="hatch-line"/></pattern></defs>`;
    cols.forEach(([b, st], i) => {
      const x = i * (cw + gap);
      if (st === 'watched' || st === 'armed') {
        const pk = (peaks[b.id] || []).slice(0, cw < 150 ? 1 : 3);
        pk.forEach(([l, ink, amp], k) => { const px = x + (k + 0.5) * (cw / pk.length); const pw = Math.min(cw * 0.6, 70); out += `<path class="spec ${ink}" d="${wave(px - pw / 2, pw, top - 6, amp)}"/><text class="spec-l" x="${px}" y="10" text-anchor="middle">${esc(l)}</text>`; });
      } else if (st === 'dark' || st === 'proposed') {
        out += `<rect x="${x}" y="${top}" width="${cw}" height="${H - top}" fill="url(#fall-hatch)" opacity="0.45"/><text class="dark-anno" x="${x + cw / 2}" y="${top + 26}" text-anchor="middle">blank because nothing is logged</text><text class="dark-anno q" x="${x + cw / 2}" y="${top + 40}" text-anchor="middle">— not because nothing is sent</text>`;
      } else if (st === 'unknown') {
        out += `<rect x="${x}" y="${top}" width="${cw}" height="${H - top}" fill="url(#fall-hatch)" opacity="0.25"/><text class="dark-anno q" x="${x + cw / 2}" y="${top + 26}" text-anchor="middle">${esc(opts.unknownText || 'nothing has arrived')}</text>`;
      }
    });
    // time rows
    for (let k = 0; k <= 6; k++) { const y = top + k * ((H - top) / 6); out += `<line class="gridline" x1="0" y1="${y}" x2="${W}" y2="${y}"/>`; }
    // marks
    if (s.tail && s.tail.length && opts.marks !== false) {
      const t0 = now; s.tail.forEach(l => { const secs = t0 - hmsToSec(l.t); const y = top + secs * pxs; if (y > H) return; const ci = opts.colOf ? opts.colOf(l) : cols.findIndex(([b]) => b.id === prefixCol[l.pre]); if (ci < 0) return; const [b, st] = cols[ci]; const x = ci * (cw + gap) + 6 + (hash(l.src) % Math.max(1, cw - 12)); const ink = l.action === 'drop' ? 'var(--fall-drop)' : st === 'unknown' ? 'var(--fall-other)' : 'var(--fall-accept)'; out += `<rect class="mark" x="${x}" y="${y}" width="2.4" height="7" rx="0.6" fill="${ink}"/>`; });
    }
    // NOW
    if (opts.now !== false) out += `<line class="nowline" x1="0" y1="${top}" x2="${W}" y2="${top}"/>${opts.nowCaption === false ? '' : `<text class="nowcap" x="${W - 4}" y="${top - 6}" text-anchor="end">NOW · ${MV.hms(now)}</text>`}`;
    out += '</svg>';
    return out;
  }
  const hmsToSec = t => { const [h, m, s] = t.split(':').map(Number); return h * 3600 + m * 60 + s; };
  const hash = str => { let h = 0; for (const c of String(str)) h = (h * 31 + c.charCodeAt(0)) >>> 0; return h; };
  /* the boundary states for the story at a given moment */
  function states(s, mode) {
    const B = S().boundaries;
    if (mode === 'preview') return B.map(b => [b, b.logs.length ? 'watched' : 'dark']);
    if (mode === 'pick') return B.map(b => [b, b.logs.length ? 'watched' : (b.sug !== null && s.chosen.has(b.sug)) ? 'armed' : 'proposed']);
    return B.map(b => [b, MV.watched(b) ? 'watched' : (s.tuneCopied && b.sug !== null && s.chosen.has(b.sug)) ? 'armed' : 'dark']);
  }
  return { header, strip, body, states, prefixCol, laneVar };
})();
