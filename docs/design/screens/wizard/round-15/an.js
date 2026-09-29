/* AN — The neon, sparks done properly. Round 14's AM ("I really like the
   rest") with the sparks redrawn: white-hot, cooling to the tube's own ink,
   fast thin streaks, fewer — and none at all with ?nosparks. Round 13's AL taken further: the box is a neon sign in
   the rain, and everything that happens to it is what happens to a tube.
     slide   you let go and slide down (the welcome goes up and away; the rain streaks)
     swell   the rain thickens and takes every ink MikroView owns — no white wash
     sign    the box moves to the centre, grows by half; as the swell comes on the sign
             strikes — every letter stutters alight, one after another — and from then
             on a letter now and then fails like a broken tube: a buzz, a stutter, a
             drop-out and a flash back, in whatever ink the rain is wearing. The border
             wobbles, glides through the rain's inks, and hums (the odd dip)
     line    a line grows from each side of the box: the tube extending, a bright head of
             current running out to the edges; when it gets there the whole line strikes
     catch   drops meeting the line are caught or slip by — porous at first, then total —
             and some catches throw a spark or two off the tube: white-hot, cooling to the
             tube's ink, gone in under half a second
     wipe    the line rises with all it holds, shedding sparks like a seam being welded;
             the box rides up and becomes the bar's wordmark; the line goes off the top
     groups  the wizard (or the fall) arrives in groups, each striking on like a tube
   Every colour is an app token read off :root (fx.js); the border, the line, the
   current, the beads and the sparks are the canvas, the letters are the DOM. */
(() => {
  const $ = s => document.querySelector(s); const reduced = WIZ.reduced;
  const H = () => innerHeight;
  const cubic = p => p < 0.5 ? 4 * p * p * p : 1 - Math.pow(-2 * p + 2, 3) / 2;
  const easeOut = p => 1 - Math.pow(1 - p, 3);
  const cl = FX.clamp, sm = FX.smooth;
  /* colours: tokens are hex; the border glides between inks rather than snapping */
  const rgb = h => { h = h.replace('#', ''); if (h.length === 3) h = h.split('').map(c => c + c).join(''); return [0, 2, 4].map(i => parseInt(h.slice(i, i + 2), 16)); };
  const mix = (a, b, q) => `rgb(${a.map((v, i) => Math.round(v + (b[i] - v) * q)).join(',')})`;
  const AMBER = rgb(FX.INKS.now);
  /* the riding box: a copy of the wordmark in a fixed element; `from` is the rect it starts on */
  function makeBox(from) {
    const el = document.createElement('div'); el.className = 'ride'; el.setAttribute('aria-hidden', 'true');
    el.innerHTML = '<span class="wm">' + 'MIKRO'.split('').map(c => `<span class="l">${c}</span>`).join('') + '<em>' + 'VIEW'.split('').map(c => `<span class="l">${c}</span>`).join('') + '</em></span>';
    document.body.appendChild(el);
    const w = el.offsetWidth, h = el.offsetHeight, wmW = el.querySelector('.wm').offsetWidth;
    const s0 = from.wm.width / wmW; const c0 = { x: from.wm.left + from.wm.width / 2, y: from.wm.top + from.wm.height / 2 };
    const letters = [...el.querySelectorAll('.l')];
    const put = (cx, cy, s) => { el.style.transform = `translate(${cx - w / 2}px, ${cy - h / 2}px) scale(${s})`; return { cx, cy, hw: w * s / 2, hh: h * s / 2 }; };
    return { el, w, h, wmW, s0, c0, letters, put };
  }
  /* ---- the tube: how a letter fails ---- */
  const lit = (L, ink) => { L.style.color = ink; L.style.textShadow = `0 0 5px ${ink}, 0 0 14px ${ink}`; };
  const dark = L => { L.style.color = 'rgba(233, 238, 251, 0.12)'; L.style.textShadow = 'none'; };
  const plain = L => { L.style.color = ''; L.style.textShadow = ''; };
  /* a pattern is a list of [ms, state]; state is 'lit', 'dark' or 'plain' */
  const patterns = {
    buzz: () => { const out = []; const n = 5 + ((Math.random() * 6) | 0); for (let i = 0; i < n; i++) out.push([FX.rnd(18, 42), i % 2 ? 'dark' : 'lit']); out.push([0, 'plain']); return out; },
    stutter: () => [[50, 'lit'], [35, 'dark'], [70, 'lit'], [30, 'dark'], [FX.rnd(200, 320), 'lit'], [0, 'plain']],
    dropout: () => [[FX.rnd(140, 320), 'dark'], [45, 'lit'], [25, 'dark'], [FX.rnd(120, 220), 'lit'], [0, 'plain']],
    hold: () => [[FX.rnd(160, 300), 'lit'], [0, 'plain']],
  };
  const PICK = ['buzz', 'stutter', 'dropout', 'hold', 'buzz', 'stutter'];
  function schedule(L, ink, kind, t, delay = 0) { let at = t + delay; L._seq = patterns[kind]().map(([ms, st]) => { const step = { at, st, ink }; at += ms; return step; }); L._cur = null; }
  function runLetters(letters, t) {
    for (const L of letters) {
      if (!L._seq) continue;
      let step = null; while (L._seq.length && L._seq[0].at <= t) step = L._seq.shift();
      if (step && step !== L._cur) { L._cur = step; step.st === 'lit' ? lit(L, step.ink) : step.st === 'dark' ? dark(L) : plain(L); }
      if (!L._seq.length && L._cur && L._cur.st === 'plain') { L._seq = null; L._cur = null; }
    }
  }
  /* the journey. o: { from (wm rect), to (bar wm rect), slide(y), D, scale, swap(), groups [[ms, fn]], landed(), done() } */
  function journey(o) {
    const k_ = o.scale || 1, T = ms => ms * k_;
    const SLIDE_END = T(2600), SWELL_ON = T(1100), LINE_ON = T(2500), LINE_FULL = T(3100), WIPE_ON = T(3700), WIPE_END = T(4600), RAIN_OFF = T(4500), BOX_GO = T(4050), BOX_END = T(4800);
    const box = makeBox(o.from); let rect = box.put(box.c0.x, box.c0.y, box.s0);
    const { cx, W: CW, H: CH } = FX.open();
    const target = o.to; const sT = target.width / box.wmW; const tC = { x: target.left + target.width / 2, y: target.top + target.height / 2 };
    const ps = []; for (let i = 0; i < 70; i++) ps.push(FX.stroke(FX.rnd(0, CW), FX.rnd(0, CH)));
    const pool = [], sparks = []; let lineY = CH / 2, lineL = 0, lineR = 0, lineOn = false, lineStruck = 0, ground = 0, swapped = false, lastY = 0, signLit = false;
    let inkA = AMBER, inkB = AMBER, inkT = 0, flickerAt = 0, dipUntil = 0, dipNext = 0;
    const wobble = (u, t, amp) => Math.sin(u * 6.3 + t * 0.0021) * amp + Math.sin(u * 13.1 - t * 0.0033) * amp * 0.5 + Math.sin(u * 2.2 + t * 0.0012) * amp * 0.7;
    const NOSPARKS = location.search.includes('nosparks');
    const spark = (x, y, ink, n, up = 1) => { if (NOSPARKS) return; for (let i = 0; i < n; i++) sparks.push({ x, y, vx: FX.rnd(-160, 160), vy: FX.rnd(-420, -140) * up, ink, born: 0, life: FX.rnd(200, 460), px: x, py: y }); };
    FX.loop((t, dt) => {
      /* --- the curves --- */
      const sp = cl(t / SLIDE_END, 0, 1); const y = o.D * cubic(sp); const v = (y - lastY) / Math.max(dt, 0.001) / CH; lastY = y; o.slide(y);
      const k = t < SWELL_ON ? 0 : t < SLIDE_END ? Math.pow((t - SWELL_ON) / (SLIDE_END - SWELL_ON), 1.6) : t < WIPE_ON - T(300) ? 1 : cl(1 - (t - WIPE_ON + T(300)) / (RAIN_OFF - WIPE_ON + T(300)), 0, 1);
      const grow = t < LINE_ON ? 0 : easeOut(cl((t - LINE_ON) / (LINE_FULL - LINE_ON), 0, 1));
      const wipe = t < WIPE_ON ? 0 : cubic(cl((t - WIPE_ON) / (WIPE_END - WIPE_ON), 0, 1));
      const catchRate = cl((t - LINE_ON) / (WIPE_ON - LINE_ON), 0, 1);
      const go = t < BOX_GO ? 0 : cubic(cl((t - BOX_GO) / (BOX_END - BOX_GO), 0, 1));
      /* --- the box --- */
      const bq = easeOut(cl(t / T(800), 0, 1));
      const bcx = box.c0.x + (CW / 2 - box.c0.x) * bq, s1 = box.s0 + (1.5 - box.s0) * bq;
      const bcyBase = box.c0.y + (CH / 2 - box.c0.y) * bq; lineY = CH / 2 + (24 - 40 - CH / 2) * wipe;
      const hhNow = box.h * (s1 + (sT - s1) * go) / 2; const bcy = wipe > 0 ? Math.max(lineY, 10 + hhNow) : bcyBase;
      rect = box.put(bcx + (tC.x - bcx) * go, bcy + (tC.y - bcy) * go, s1 + (sT - s1) * go);
      /* the tube's ink: amber until the swell, then gliding through the rain's colours */
      if (k > 0.05 && t >= inkT) { inkA = inkB; inkB = rgb(FX.pick(k > 0.3 ? FX.ALL : FX.FALL)); inkT = t + FX.rnd(380, 720); }
      const iq = inkT ? 1 - cl((inkT - t) / 550, 0, 1) : 0; const ink = k > 0.05 || go > 0 ? mix(inkA, inkB, sm(iq)) : mix(AMBER, AMBER, 0);
      /* --- the rain --- */
      const rising = t < WIPE_ON - T(300); const streak = cl(v * 0.9, 0, 5);
      const N = 70 + Math.round(k * 2300), spd = 1 + k * 5.5 + streak * 0.8, len = 13 + k * 62 + streak * 14, wid = 2.5 + k * 1.4, al = 0.42 + k * 0.4;
      const wide = cl((k - 0.15) / 0.5, 0, 1);
      const want = rising ? N : Math.round(k * 2300);
      while (ps.length < want) { const p = FX.stroke(FX.rnd(-40, CW + 40), rising && k < 0.9 ? FX.rnd(-CH * 0.4, CH * 0.6) : FX.rnd(-CH * 0.3, -10)); if (Math.random() < wide) p.ink = FX.pick(FX.ALL); p.vx = FX.rnd(-14, 14); ps.push(p); }
      if (grow > 0) { const hw = rect.hw + 4; lineL = (rect.cx - hw) * (1 - grow) - 20 * grow; lineR = (rect.cx + hw) + (CW + 20 - rect.cx - hw) * grow; if (!lineOn) { lineOn = true; for (const p of ps) if (p.y > lineY) p.below = true; } if (grow >= 1 && !lineStruck) { lineStruck = t; spark(lineL + 30, lineY, ink, 9); spark(lineR - 30, lineY, ink, 9); } }
      for (let i = ps.length - 1; i >= 0; i--) {
        const p = ps[i]; p.y += p.vy * spd * dt; p.x += p.vx * dt;
        if (lineOn && !p.below && p.y >= lineY && p.x >= lineL && p.x <= lineR) {
          if (p.tried === undefined) { p.tried = true; if (Math.random() < 0.08 + 0.92 * Math.pow(catchRate, 1.4) || wipe > 0) { pool.push({ x: p.x, ink: p.ink, born: t, w: p.w }); if (sparks.length < 300 && Math.random() < 0.38) spark(p.x, lineY, ink, 1 + ((Math.random() * 1.6) | 0)); ps.splice(i, 1); continue; } else p.below = true; }
        }
        if (p.y - len > CH + 10) { if (ps.length <= want) { p.y = FX.rnd(-CH * 0.3, -10); p.x = FX.rnd(-40, CW + 40); p.below = false; p.tried = undefined; } else ps.splice(i, 1); }
      }
      for (let i = sparks.length - 1; i >= 0; i--) { const s = sparks[i]; s.born += dt * 1000; if (s.born > s.life) { sparks.splice(i, 1); continue; } s.px = s.x; s.py = s.y; s.vy += 1500 * dt; s.x += s.vx * dt; s.y += s.vy * dt; }
      /* --- paint --- */
      cx.globalCompositeOperation = 'source-over'; cx.clearRect(0, 0, CW, CH);
      ground = rising ? Math.max(ground, cl(k * 1.7, 0, 1)) : ground;
      if (ground > 0) {
        cx.fillStyle = '#06080e';
        if (!lineOn) { cx.globalAlpha = ground; cx.fillRect(0, 0, CW, CH); }
        else { cx.globalAlpha = ground; cx.fillRect(0, 0, CW, Math.max(0, lineY)); const below = ground * (1 - sm((t - LINE_FULL) / T(900))); if (below > 0.01) { cx.globalAlpha = below; cx.fillRect(0, Math.max(0, lineY), CW, CH); } }
      }
      cx.globalCompositeOperation = 'lighter';
      for (const p of ps) FX.drawStroke(p, len * (0.7 + p.a * 0.6), wid, al * (0.55 + p.a * 0.7), k > 0.35 && k < 0.85);
      /* the hum: now and then the tube dips for a few frames */
      if (t > dipNext) { dipUntil = t + FX.rnd(25, 70); dipNext = dipUntil + FX.rnd(250, 900) * (k > 0.1 ? 1 : 3); }
      const hum = (t < dipUntil ? 0.45 : 1) * (0.96 + Math.random() * 0.04);
      const amp = (0.6 + k * 3.2) * (1 - go);
      cx.globalCompositeOperation = 'source-over'; cx.strokeStyle = ink; cx.lineJoin = 'round'; cx.lineCap = 'round';
      const drawBox = (a, lw, inf) => {
        cx.globalAlpha = a; cx.lineWidth = lw; cx.beginPath(); const L = rect.cx - rect.hw - inf, R = rect.cx + rect.hw + inf, Tp = rect.cy - rect.hh - inf, B = rect.cy + rect.hh + inf; const n = 14;
        for (let i = 0; i <= n; i++) { const u = i / n; const px = L + (R - L) * u; const py = Tp + wobble(u, t, amp); i ? cx.lineTo(px, py) : cx.moveTo(px, py); }
        for (let i = 1; i <= n; i++) { const u = i / n; cx.lineTo(R + wobble(u + 1, t, amp), Tp + (B - Tp) * u); }
        for (let i = 1; i <= n; i++) { const u = i / n; cx.lineTo(R - (R - L) * u, B + wobble(u + 2, t, amp)); }
        for (let i = 1; i <= n; i++) { const u = i / n; cx.lineTo(L + wobble(u + 3, t, amp), B - (B - Tp) * u); }
        cx.closePath(); cx.stroke();
      };
      const boxA = (1 - sm((go - 0.55) / 0.45)) * hum;
      if (boxA > 0) { drawBox(boxA * 0.35, 6 + k * 6, 4); drawBox(boxA, 1.5 + k * 0.8, 4); }
      if (lineOn && lineY > -30) {
        const wet = cl(pool.length / 120, 0, 1); let lineA = 1 - sm((t - WIPE_END) / T(300));
        /* the strike: when the current reaches both edges the whole tube flickers on */
        if (lineStruck) { const u = (t - lineStruck) / T(340); if (u < 1) lineA *= u < 0.15 ? 1 : u < 0.25 ? 0.2 : u < 0.4 ? 1 : u < 0.5 ? 0.4 : 1; }
        lineA *= hum;
        const seg = (x0, x1, lw, a) => { if (x1 <= x0) return; cx.globalAlpha = a * lineA; cx.lineWidth = lw; cx.beginPath(); for (let x = x0; x <= x1; x += 24) { const py = lineY + wobble(x / CW * 3, t, amp * 0.5); x === x0 ? cx.moveTo(x, py) : cx.lineTo(x, py); } cx.lineTo(x1, lineY + wobble(x1 / CW * 3, t, amp * 0.5)); cx.stroke(); };
        const whole = wipe > 0 && bcy > lineY + rect.hh; const gapL = whole ? lineR : rect.cx - rect.hw - 4, gapR = whole ? lineR : rect.cx + rect.hw + 4;
        cx.strokeStyle = ink;
        seg(lineL, gapL, 5 + wet * 8, 0.18 + wet * 0.25); seg(gapR, lineR, 5 + wet * 8, 0.18 + wet * 0.25);
        seg(lineL, gapL, 1.6, 0.95); seg(gapR, lineR, 1.6, 0.95);
        /* the current: while the tube is still extending, a bright head runs out at each end */
        if (grow > 0 && grow < 1) { cx.globalCompositeOperation = 'lighter'; for (const hx of [lineL, lineR]) { const g = cx.createRadialGradient(hx, lineY, 0, hx, lineY, 22); g.addColorStop(0, '#fff7e6'); g.addColorStop(0.35, ink); g.addColorStop(1, 'rgba(0,0,0,0)'); cx.fillStyle = g; cx.globalAlpha = 0.95; cx.beginPath(); cx.arc(hx, lineY, 22, 0, 6.29); cx.fill(); } cx.globalCompositeOperation = 'source-over'; }
        /* what the line has caught: each drop a bead on it, bright when it lands, then part of the wet */
        cx.globalCompositeOperation = 'lighter';
        for (let i = pool.length - 1; i >= 0; i--) { const d = pool[i]; const age = (t - d.born) / T(1100); if (age > 1 && pool.length > 900) { pool.splice(i, 1); continue; } const a = age < 1 ? 0.95 - 0.55 * age : 0.4; cx.globalAlpha = a * lineA; cx.fillStyle = d.ink; const r = 1.6 + Math.min(1, age) * 1.6; cx.beginPath(); cx.ellipse(d.x, lineY - 1 + wobble(d.x / CW * 3, t, amp * 0.5), r * 1.6, r, 0, 0, 6.29); cx.fill(); }
      }
      /* the sparks: a streak as long as its speed, white-hot at birth, the tube's ink by mid-life, gone by the end */
      if (sparks.length) { cx.globalCompositeOperation = 'lighter'; cx.lineCap = 'round'; for (const s of sparks) { const u = s.born / s.life; const a = (1 - u) * (1 - u); const tail = 0.03; cx.globalAlpha = Math.min(1, a * 1.1); cx.lineWidth = 1.5 - u * 0.7; cx.strokeStyle = u < 0.3 ? '#fff7e6' : s.ink; cx.beginPath(); cx.moveTo(s.x - s.vx * tail, s.y - s.vy * tail); cx.lineTo(s.x, s.y); cx.stroke(); } }
      /* --- the letters --- */
      if (!signLit && k > 0.08) { signLit = true; box.letters.forEach((L, i) => schedule(L, FX.INKS.now, 'stutter', t, i * 55 + FX.rnd(0, 40))); } // the sign strikes, letter by letter
      if (signLit && k > 0.1 && go < 0.6 && t >= flickerAt) {
        flickerAt = t + FX.rnd(110, 300); const i = (Math.random() * box.letters.length) | 0; const L = box.letters[i];
        if (!L._seq) { const inkL = FX.pick(k > 0.3 ? FX.ALL : FX.FALL); const kind = FX.pick(PICK); schedule(L, inkL, kind, t); const N2 = box.letters[i + 1]; if (N2 && !N2._seq && Math.random() < 0.3) schedule(N2, inkL, kind, t, FX.rnd(10, 40)); }
      }
      runLetters(box.letters, t);
      if (go >= 0.6) box.letters.forEach(L => { if (L._seq) { L._seq = null; L._cur = null; plain(L); } });
      if (!swapped && k >= 0.85 && t > SLIDE_END - T(400)) { swapped = true; o.swap(); }
      if (t >= BOX_END && !box.el.classList.contains('landed')) { box.el.classList.add('landed'); o.landed(); setTimeout(() => box.el.remove(), 320); }
      return t < T(6500) || (ps.length > 0 && t < WIPE_END + T(2500)) || (sparks.length > 0 && t < WIPE_END + T(2500));
    }, () => { FX.close(); o.done(); });
    o.groups.forEach(([ms, fn]) => setTimeout(fn, T(ms)));
  }
  /* each element strikes on like a tube, one after another */
  const strike = (els, gap) => els.forEach((el, i) => { el.style.animationDelay = `${i * gap}ms`; el.classList.add('strike'); setTimeout(() => { el.style.animationDelay = ''; }, i * gap + 1000); });
  function wayIn() {
    document.body.classList.add('in', 'ak', 'am');
    if (reduced) { WIZ.beat(300, () => { WIZ.showWizard(false); document.body.classList.remove('ak', 'am'); }); return; }
    const src = $('#wmbox'); const from = { wm: src.querySelector('.wm').getBoundingClientRect() }; src.classList.add('gone');
    const world = $('#world'); const bar = $('#barwm'); bar.style.opacity = '0'; const to = bar.getBoundingClientRect();
    WIZ.phase = 'in';
    journey({
      from, to, D: H() * 3.2, scale: 1,
      slide: y => { world.style.transform = `translateY(${-y}px)`; },
      swap: () => { document.body.classList.add('wiz'); $('#page').setAttribute('aria-hidden', 'false'); WIZ.phase = 'wiz'; MV.render(); WIZ.phase = 'in'; $('#body').classList.remove('away'); },
      groups: [
        [4350, () => document.body.classList.add('ak-bar')],
        [4450, () => document.body.classList.add('ak-strip')],
        [4550, () => { strike([...document.querySelectorAll('#steps li')], 90); $('#rail').classList.remove('away'); }],
        [4800, () => strike([...$('#body').children], 120)],
        [5350, () => { $('#foot').classList.remove('away'); strike([$('#foot')], 0); }],
      ],
      landed: () => { bar.style.opacity = ''; $('#page').classList.add('live'); },
      done: () => { document.body.classList.remove('ak', 'am'); document.querySelectorAll('.strike').forEach(el => el.classList.remove('strike')); WIZ.phase = 'wiz'; MV.render(); const inp = $('[data-field=name]'); if (inp) inp.focus(); },
    });
  }
  function wayOut() {
    const main = $('#main');
    if (reduced) { WIZ.showFall(false); WIZ.beat(300, WIZ.fallLive); return; }
    document.body.classList.add('ak', 'am'); const bar = $('#barwm'); const from = { wm: bar.getBoundingClientRect() }; const to = from.wm; bar.style.opacity = '0';
    $('#rail').classList.add('away');
    journey({
      from, to, D: H(), scale: 0.78,
      slide: y => { main.style.transform = `translateY(${-y}px)`; },
      swap: () => { WIZ.showFall(true); document.body.classList.add('ak-strip'); },
      groups: [
        [4350, () => document.body.classList.add('ak-bar')],
        [4450, () => { const cols = [...document.querySelectorAll('#cols .col')]; strike(cols, 90); cols.forEach(c => c.classList.remove('pre')); }],
        [4800, () => document.body.classList.add('ak-fb')],
        [5300, () => { WIZ.fallLive(); document.body.classList.remove('ak', 'am'); document.querySelectorAll('.strike').forEach(el => el.classList.remove('strike')); }],
      ],
      landed: () => { bar.style.opacity = ''; },
      done: () => { document.body.classList.remove('ak', 'am'); if (WIZ.phase !== 'fall') WIZ.fallLive(); },
    });
  }
  WIZ.boot({ wayIn, wayOut });
})();
