/* AJ — The storm. Four beats on one clock:
   wind   0–1100ms   the rain tilts and gusts, more of it, longer, the edges close in
   drain  800–1900   every stroke bends toward the box and is drunk; the box charges
   break  1900       flash; the ring goes out; the page is revealed inside it;
                     the box jumps to the bar; embers ride the ring's edge
   settle 2900       the ring is past the corners; the embers are out; the page stands */
(() => {
  const $ = s => document.querySelector(s); const reduced = WIZ.reduced;
  const easeOut = p => 1 - Math.pow(1 - p, 3);
  function storm(opts) {
    const { cx, W, H } = FX.open(); const C = opts.centre(); const box = opts.box;
    const ps = []; for (let i = 0; i < 60; i++) ps.push(FX.stroke(FX.rnd(0, W), FX.rnd(0, H)));
    const embers = []; let charge = 0, broke = false, drank = 0; const flash = $('#flash');
    const ring = opts.ringTargets(); const rmax = Math.hypot(Math.max(C.x, W - C.x), Math.max(C.y, H - C.y)) * 1.05;
    const T_BREAK = opts.breakAt, T_END = T_BREAK + 1100;
    FX.loop((t, dt) => {
      cx.globalCompositeOperation = 'source-over'; cx.clearRect(0, 0, W, H);
      const wind = FX.smooth(t / 1100), gust = 0.55 + 0.45 * Math.sin(t / 170), drain = FX.smooth((t - opts.drainFrom) / (T_BREAK - opts.drainFrom - 100));
      if (t < T_BREAK) {
        const N = 60 + Math.round(wind * 520); const spawnAt = opts.spawn || (() => ({ x: FX.rnd(-W * 0.3, W), y: FX.rnd(-H * 0.3, -10) }));
        if (t < T_BREAK - 700) while (ps.length < N) { const s = spawnAt(); const p = FX.stroke(s.x, s.y); if (opts.inks) p.ink = FX.pick(opts.inks); ps.push(p); }
        for (let i = ps.length - 1; i >= 0; i--) {
          const p = ps[i]; const wx = wind * gust * 300 * (1 - drain);
          let ax = 0, ay = 0; if (drain > 0) { const dx = C.x - p.x, dy = C.y - p.y, d = Math.hypot(dx, dy) || 1; const g = drain * (1800 + 220000 / (d + 80)); ax = dx / d * g; ay = dy / d * g - 180 * drain; }
          p.vx += (wx - p.vx) * Math.min(1, dt * 2.2) + ax * dt; p.vy += ay * dt; if (drain > 0) { const damp = 1 - 1.6 * dt * drain; p.vx *= damp; p.vy *= damp; }
          const sp = Math.hypot(p.vx, p.vy); if (sp > 1500) { p.vx *= 1500 / sp; p.vy *= 1500 / sp; }
          p.x += p.vx * dt; p.y += p.vy * dt;
          const d = Math.hypot(C.x - p.x, C.y - p.y);
          if (d < 30 + drain * 30) { ps.splice(i, 1); drank++; continue; }
          if (p.y > H + 60 || p.x > W + 80 || p.x < -200) { if (t < T_BREAK - 700) { p.y = FX.rnd(-H * 0.3, -10); p.x = FX.rnd(-W * 0.3, W); p.vy = FX.rnd(150, 230); } else ps.splice(i, 1); }
        }
        charge = FX.clamp(drank / 380, 0, 1); box.style.setProperty('--charge', charge.toFixed(3));
        cx.globalCompositeOperation = 'lighter';
        for (const p of ps) { const sp = Math.hypot(p.vx, p.vy); FX.drawStroke(p, FX.clamp(13 + sp * 0.11 * (1 + wind), 13, 120), 2.5 + drain * 1.2, p.a * (0.9 + drain * 0.8), drain > 0.2); }
        // a faint pull-line map: the last 40px of each drop's path glows toward the box
      } else {
        if (!broke) { broke = true; document.body.classList.add('charged'); ps.length = 0; opts.onBreak(); flash.animate([{ opacity: 1 }, { opacity: 0.55, offset: 0.2 }, { opacity: 0 }], { duration: 420, easing: 'ease-out' });
          for (let i = 0; i < 300; i++) { const a = Math.random() * Math.PI * 2, sp = FX.rnd(700, 1500); embers.push({ x: C.x, y: C.y, vx: Math.cos(a) * sp, vy: Math.sin(a) * sp, ink: FX.pick(FX.ALL), a: FX.rnd(0.5, 1), w: FX.rnd(1.6, 3.2), born: t }); }
          const shake = [{ transform: 'translate(0,0)' }, { transform: 'translate(6px,-4px)' }, { transform: 'translate(-5px,3px)' }, { transform: 'translate(3px,2px)' }, { transform: 'translate(0,0)' }]; for (const el of ring) el.animate(shake, { duration: 340, easing: 'ease-out' }); }
        const p = FX.clamp((t - T_BREAK) / 1000, 0, 1); const r = rmax * easeOut(p);
        for (const el of ring) { el.style.setProperty('--r', r + 'px'); }
        // the ring's edge: amber to white, two strokes, additive
        cx.globalCompositeOperation = 'lighter';
        cx.globalAlpha = 0.9 * (1 - p * 0.6); cx.lineWidth = 18; cx.strokeStyle = FX.INKS.now; cx.beginPath(); cx.arc(C.x, C.y, r, 0, Math.PI * 2); cx.stroke();
        cx.globalAlpha = 1 - p * 0.5; cx.lineWidth = 3; cx.strokeStyle = '#fff'; cx.beginPath(); cx.arc(C.x, C.y, r, 0, Math.PI * 2); cx.stroke();
        for (let i = embers.length - 1; i >= 0; i--) { const e = embers[i]; const age = (t - e.born) / 1000; e.vy += 520 * dt; e.vx *= 1 - 0.9 * dt; e.vy *= 1 - 0.6 * dt; e.x += e.vx * dt; e.y += e.vy * dt; const al = e.a * FX.clamp(1 - age / 0.95, 0, 1); if (al <= 0) { embers.splice(i, 1); continue; } FX.drawStroke(e, FX.clamp(Math.hypot(e.vx, e.vy) * 0.03, 4, 34), e.w, al, true); }
      }
      return t < T_END;
    }, () => { FX.close(); for (const el of ring) { el.classList.remove('ringed'); el.style.removeProperty('--r'); } opts.done(); });
  }
  function wayIn() {
    document.body.classList.add('in');
    if (reduced) { WIZ.beat(300, () => WIZ.showWizard(false)); return; }
    const box = $('#wmbox'), page = $('#page'), target = $('#barwm'); target.style.opacity = '0';
    const a = box.getBoundingClientRect(); const C = { x: a.left + a.width / 2, y: a.top + a.height / 2 };
    page.style.setProperty('--cx', C.x + 'px'); page.style.setProperty('--cy', C.y + 'px'); page.classList.add('ringed'); page.setAttribute('aria-hidden', 'false');
    // the wizard is dressed behind the door before the break, so the ring reveals it standing
    WIZ.phase = 'wiz'; MV.render(); $('#rail').classList.remove('away'); $('#body').classList.remove('away'); $('#foot').classList.remove('away'); page.classList.add('live'); WIZ.phase = 'in';
    storm({
      centre: () => C, box, breakAt: 1900, drainFrom: 800, ringTargets: () => [page],
      onBreak() {
        // the box jumps to the bar and cools; the door's ground goes so the page shows inside the ring
        box.classList.add('riding'); box.style.left = a.left + 'px'; box.style.top = a.top + 'px'; $('#door').appendChild(box);
        const b = target.getBoundingClientRect(); const sx = b.width / box.querySelector('.wm').getBoundingClientRect().width;
        const dx = b.left - a.left - (a.width - a.width * sx) / 2 - 30 * sx, dy = b.top - a.top - (a.height - a.height * sx) / 2 - 12 * sx;
        requestAnimationFrame(() => { box.style.transform = `translate(${dx}px, ${dy}px) scale(${sx})`; box.style.setProperty('--charge', '0'); box.style.borderColor = 'transparent'; box.style.boxShadow = 'none'; box.style.background = 'transparent'; });
        $('#door').style.background = 'transparent'; $('.door .vignette').style.opacity = '0';
      },
      done() { box.remove(); target.style.opacity = ''; document.body.classList.add('wiz'); WIZ.phase = 'wiz'; MV.render(); const inp = $('[data-field=name]'); if (inp) inp.focus(); },
    });
  }
  function wayOut() {
    if (reduced) { WIZ.showFall(false); WIZ.beat(300, WIZ.fallLive); return; }
    const wm = $('#barwm'), ff = $('#fallframe'), sr = $('#siderail'); const a = wm.getBoundingClientRect(); const C = { x: a.left + a.width / 2, y: a.top + a.height / 2 };
    // a box round the bar's wordmark to charge, drawn where it sits
    const box = document.createElement('div'); box.className = 'wm-box riding'; box.style.left = (a.left - 12) + 'px'; box.style.top = (a.top - 6) + 'px'; box.style.padding = '6px 12px'; box.style.setProperty('--charge', '0'); box.innerHTML = '<span class="wm" style="font-size:15px">MIKRO<em>VIEW</em></span>'; document.body.appendChild(box); document.body.classList.add('in'); wm.style.opacity = '0';
    const strip = $('#strip').getBoundingClientRect();
    for (const el of [ff, sr]) { const r = el.getBoundingClientRect(); el.style.setProperty('--cx', (C.x - r.left) + 'px'); el.style.setProperty('--cy', (C.y - r.top) + 'px'); el.classList.add('ringed'); }
    document.body.classList.add('out'); WIZ.showFall(false); ff.style.opacity = '1';
    storm({
      centre: () => C, box, breakAt: 1300, drainFrom: 100, ringTargets: () => [ff, sr], inks: [FX.INKS.ok, FX.INKS.ok, FX.INKS.accept, FX.INKS.now],
      spawn: () => ({ x: FX.rnd(strip.left, strip.right), y: strip.top + FX.rnd(-2, 6) }),
      onBreak() { WIZ.fallLive(); $('#main').style.display = 'none'; $('#rail').classList.add('away'); box.style.setProperty('--charge', '0'); box.style.borderColor = 'transparent'; box.style.boxShadow = 'none'; box.style.background = 'transparent'; },
      done() { box.remove(); wm.style.opacity = '1'; document.body.classList.remove('in', 'charged'); },
    });
  }
  WIZ.boot({ wayIn, wayOut });
})();
