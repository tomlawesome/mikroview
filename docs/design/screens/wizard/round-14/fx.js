/* A canvas over the page for AI and AJ: sized to the viewport at device
   resolution, inks read from the app's tokens (never a new colour), one
   requestAnimationFrame loop per journey. The DOM rain (fullfall.css) is
   the door's weather; the canvas takes over only for the journey's own
   frames, starting from strokes that look like the DOM's. */
window.FX = (() => {
  const css = getComputedStyle(document.documentElement);
  const tok = n => css.getPropertyValue(n).trim();
  const INKS = { accept: tok('--fall-accept'), drop: tok('--fall-drop'), nat: tok('--fall-nat'), lan: tok('--lane-lan'), srv: tok('--lane-srv'), iot: tok('--lane-iot'), guest: tok('--lane-guest'), ok: tok('--accept'), log: tok('--log'), now: tok('--now'), warn: tok('--drop'), alarm: tok('--alarm'), fg: tok('--fg') };
  const FALL = [INKS.accept, INKS.accept, INKS.accept, INKS.accept, INKS.drop, INKS.drop, INKS.nat];
  const ALL = [INKS.lan, INKS.srv, INKS.iot, INKS.guest, INKS.ok, INKS.log, INKS.now, INKS.warn, INKS.alarm, INKS.nat, INKS.accept, INKS.drop];
  let cv, cx, W, H, dpr;
  function open() {
    cv = document.getElementById('fx'); dpr = Math.min(2, devicePixelRatio || 1); W = innerWidth; H = innerHeight;
    cv.width = W * dpr; cv.height = H * dpr; cv.style.display = 'block'; cx = cv.getContext('2d'); cx.setTransform(dpr, 0, 0, dpr, 0, 0); cx.lineCap = 'round';
    return { cx, W, H };
  }
  function close() { cx.clearRect(0, 0, W, H); cv.style.display = 'none'; }
  const rnd = (a, b) => a + Math.random() * (b - a);
  const pick = arr => arr[(Math.random() * arr.length) | 0];
  /* a stroke like the DOM rain's: 2.5px wide, 13px long, one of the fall's inks */
  const stroke = (x, y, ink) => ({ x, y, vx: 0, vy: rnd(150, 230), len: 13, w: 2.5, a: rnd(0.3, 0.65), ink: ink || pick(FALL) });
  function drawStroke(p, len, w, a, glow) {
    const l = len || p.len; const nx = p.vx, ny = p.vy; const n = Math.hypot(nx, ny) || 1; const ux = nx / n, uy = ny / n;
    if (glow) { cx.globalAlpha = Math.min(1, a * 0.28); cx.lineWidth = (w || p.w) * 3.2; cx.strokeStyle = p.ink; cx.beginPath(); cx.moveTo(p.x, p.y); cx.lineTo(p.x - ux * l, p.y - uy * l); cx.stroke(); }
    cx.globalAlpha = Math.min(1, a); cx.lineWidth = w || p.w; cx.strokeStyle = p.ink; cx.beginPath(); cx.moveTo(p.x, p.y); cx.lineTo(p.x - ux * l, p.y - uy * l); cx.stroke();
  }
  /* run f(t, dt) each frame until it returns false */
  function loop(f, done) { let last = performance.now(); const t0 = last; const step = now => { const dt = Math.min(0.05, (now - last) / 1000); last = now; if (f(now - t0, dt) !== false) requestAnimationFrame(step); else done && done(); }; requestAnimationFrame(step); }
  const clamp = (v, a, b) => Math.max(a, Math.min(b, v));
  const smooth = p => { p = clamp(p, 0, 1); return p * p * (3 - 2 * p); };
  return { open, close, rnd, pick, stroke, drawStroke, loop, clamp, smooth, INKS, FALL, ALL, get cx() { return cx; }, get W() { return W; }, get H() { return H; } };
})();
