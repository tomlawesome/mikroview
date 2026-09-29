/* AI — The swell. One intensity curve, I(t): up over 1.5s, held a beat,
   down over 1.3s. Everything the rain is — how many strokes, how fast,
   how long, how bright, how wide, how many inks — is a function of I.
   At the top of the curve the frame is full and light, and under that
   cover the place changes: door to wizard on the way in, wizard to fall
   on the way out. Additive blending is what makes the crowding glow. */
(() => {
  const reduced = WIZ.reduced;
  const UP = 1500, HOLD = 400, DOWN = 1300;
  const I = t => t < UP ? Math.pow(t / UP, 1.8) : t < UP + HOLD ? 1 : Math.max(0, 1 - FX.smooth((t - UP - HOLD) / DOWN));
  function swell(swap, done) {
    const { cx, W, H } = FX.open(); const ps = []; for (let i = 0; i < 70; i++) ps.push(FX.stroke(FX.rnd(0, W), FX.rnd(0, H)));
    let swapped = false;
    FX.loop((t, dt) => {
      const k = I(t); const rising = t < UP + HOLD;
      const N = 70 + Math.round(k * 2600), spd = 1 + k * 6.5, len = 13 + k * 80, wid = 2.5 + k * 1.8, al = 0.42 + k * 0.6;
      const wide = FX.clamp((k - 0.18) / 0.55, 0, 1);
      // on the way up the field fills everywhere at once; on the way down it thins everywhere at once (fewer spawn, all keep falling)
      const want = rising ? N : Math.round(k * 2600);
      while (ps.length < want) { const p = FX.stroke(FX.rnd(-40, W + 40), rising ? FX.rnd(-H * 0.4, H) : FX.rnd(-H * 0.3, -10)); if (Math.random() < wide) p.ink = FX.pick(FX.ALL); p.vx = FX.rnd(-20, 20); ps.push(p); }
      for (let i = ps.length - 1; i >= 0; i--) { const p = ps[i]; p.y += p.vy * spd * dt; p.x += p.vx * dt; if (p.y - len > H + 10) { if (ps.length <= want) { p.y = FX.rnd(-H * 0.3, -10); p.x = FX.rnd(-40, W + 40); } else ps.splice(i, 1); } }
      cx.globalCompositeOperation = 'source-over'; cx.clearRect(0, 0, W, H);
      // the ground darkens under the swell so the rain has something to bloom against, then the wash takes over
      // on the way down the ground lifts ahead of the rain, so the place shows through the thinning
      cx.globalAlpha = rising ? FX.clamp(k * 1.7, 0, 1) : FX.smooth((k - 0.25) / 0.55); cx.fillStyle = '#06080e'; cx.fillRect(0, 0, W, H);
      cx.globalCompositeOperation = 'lighter';
      for (const p of ps) FX.drawStroke(p, len * (0.7 + p.a * 0.6), wid, al * (0.55 + p.a * 0.7), k > 0.3);
      const wash = FX.smooth((k - 0.78) / 0.22);
      if (wash > 0) { cx.globalCompositeOperation = 'source-over'; cx.globalAlpha = wash * 0.96; cx.fillStyle = '#fff7e6'; cx.fillRect(0, 0, W, H); }
      if (!swapped && t >= UP + HOLD / 2) { swapped = true; swap(); }
      return !(t > UP + HOLD + DOWN + 200 && ps.length === 0) && t < UP + HOLD + DOWN + 1200;
    }, () => { FX.close(); done(); });
  }
  function wayIn() {
    document.body.classList.add('in');
    if (reduced) { WIZ.beat(300, () => WIZ.showWizard(false)); return; }
    swell(() => { document.body.classList.add('wiz'); WIZ.showWizard(false); WIZ.$('#page').classList.add('live'); }, () => { WIZ.phase = 'wiz'; const inp = document.querySelector('[data-field=name]'); if (inp) inp.focus(); });
  }
  function wayOut() {
    if (reduced) { WIZ.showFall(false); WIZ.beat(300, WIZ.fallLive); return; }
    swell(() => { WIZ.$('#rail').classList.add('away'); WIZ.showFall(false); WIZ.fallLive(); }, () => {});
  }
  WIZ.boot({ wayIn, wayOut });
})();
