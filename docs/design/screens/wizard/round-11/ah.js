/* AH — The slide. One tween drives everything: y is how far you have
   fallen (px). The world (door stack + chute) moves at 1×, the page
   waits 3.2 screens below and comes up to meet you, the weather's planes
   loop at their own depths, and each stroke stretches with your speed. */
(() => {
  const $ = s => document.querySelector(s); const reduced = WIZ.reduced;
  const H = () => innerHeight;
  const cubic = p => p < 0.5 ? 4 * p * p * p : 1 - Math.pow(-2 * p + 2, 3) / 2;
  const planes = [...document.querySelectorAll('.weather .plane')];
  /* the chute's rows: NOW at the top, thirty seconds a row, dimmer with depth */
  (() => {
    const ch = $('#chute'); const now = new Date(); const hm = d => `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
    let out = `<div class="nowline"></div><div class="nowcap">NOW<br>${hm(now)}:${String(now.getSeconds()).padStart(2, '0')}</div>`;
    for (let k = 1; k <= 27; k++) { const y = k * 83; const d = new Date(now.getTime() - k * 30000); out += `<div class="row" style="top:${y}px;opacity:${(1 - k / 34).toFixed(2)}"></div>${k % 2 === 0 ? `<div class="t" style="top:${y}px;opacity:${(1 - k / 30).toFixed(2)}">${hm(d)}</div>` : ''}`; }
    out += `<div class="empty" style="top:${H() * 0.9}px"><b>nothing has arrived</b>no router is sending yet — that is the next screen</div>`;
    ch.innerHTML = out;
  })();
  /* run a slide: y from 0 to D over T ms; onFrame(y, v) paints; then done() */
  function slide(D, T, onFrame, done) {
    const t0 = performance.now(); let last = 0;
    const step = now => {
      const p = Math.min(1, (now - t0) / T); const y = D * cubic(p);
      const v = (y - last) / 16 * 60 / H() * 100; last = y; // vh per second, rough
      onFrame(y, v, p);
      if (p < 1) requestAnimationFrame(step); else done();
    };
    requestAnimationFrame(step);
  }
  const weather = (y, v, dim) => {
    const st = Math.min(9, 1 + Math.abs(v) / 28);
    planes.forEach(pl => { const k = Number(getComputedStyle(pl).getPropertyValue('--k')), z = Number(getComputedStyle(pl).getPropertyValue('--z')); const off = ((y * k) % H() + H()) % H(); pl.style.transform = `translateY(${-off}px) scale(${z})`; pl.style.setProperty('--stretch', (1 + (st - 1) * k).toFixed(2)); pl.style.opacity = dim ? String(0.9 - Math.min(0.6, (st - 1) * 0.07)) : ''; });
  };
  function wayIn() {
    document.body.classList.add('in');
    if (reduced) { WIZ.beat(300, () => { WIZ.showWizard(false); }); return; }
    // the amber box rides: reparent it to the viewport at its own spot, then send it to the bar's wordmark
    const box = $('#wmbox'), target = $('#barwm'); const a = box.getBoundingClientRect(); target.style.opacity = '0';
    box.classList.add('riding'); box.style.left = a.left + 'px'; box.style.top = a.top + 'px'; $('#door').appendChild(box);
    const D = H() * 3.2, T = 2600; const world = $('#world'), page = $('#page');
    page.style.transform = 'none'; const bf = target.getBoundingClientRect(); // where the wordmark sits once the page has landed
    page.style.transform = `translateY(${D}px)`; $('#page').setAttribute('aria-hidden', 'false');
    const sx = bf.width / box.querySelector('.wm').getBoundingClientRect().width;
    const dx = bf.left - a.left - (a.width - a.width * sx) / 2 - 30 * sx, dy = bf.top - a.top - (a.height - a.height * sx) / 2 - 12 * sx;
    WIZ.phase = 'in';
    WIZ.beat(350, () => { WIZ.$('#rail').classList.remove('away'); WIZ.$('#page').classList.add('live'); /* the page is dressed while still below */ WIZ.phase = 'wiz'; MV.render(); WIZ.$('#body').classList.remove('away'); WIZ.$('#foot').classList.remove('away'); WIZ.phase = 'in'; });
    slide(D, T, (y, v, p) => {
      world.style.transform = `translateY(${-y}px)`;
      page.style.transform = `translateY(${Math.max(0, D - y)}px)`;
      weather(y, v, true);
      const q = cubic(Math.min(1, Math.max(0, (p - 0.15) / 0.8)));
      box.style.transform = `translate(${dx * q}px, ${dy * q}px) scale(${1 - (1 - sx) * q})`;
      if (p > 0.7) { box.style.borderColor = 'transparent'; box.style.boxShadow = 'none'; }
    }, () => {
      // land: a short settle, then the page stands on its own and the weather clears
      page.animate([{ transform: 'translateY(0)' }, { transform: 'translateY(-7px)' }, { transform: 'translateY(0)' }], { duration: 260, easing: 'ease-out' });
      $('.weather').classList.add('gone'); box.remove(); target.style.opacity = '';
      WIZ.beat(280, () => { page.style.transform = ''; document.body.classList.add('wiz'); WIZ.phase = 'wiz'; MV.render(); const inp = $('[data-field=name]'); if (inp) inp.focus(); });
    });
  }
  function wayOut() {
    const s = MV.state; const main = $('#main'), ff = $('#fallframe');
    if (reduced) { WIZ.showFall(false); WIZ.beat(300, WIZ.fallLive); return; }
    document.body.classList.add('out'); WIZ.showFall(false); ff.style.opacity = '1'; $('#rail').classList.add('away');
    const D = H(), T = 1500; ff.style.transform = `translateY(${D}px)`;
    const w = $('.weather'); w.classList.remove('gone'); w.style.opacity = '1'; planes.forEach(pl => { pl.style.opacity = ''; });
    slide(D, T, (y, v) => { main.style.transform = `translateY(${-y}px)`; ff.style.transform = `translateY(${D - y}px)`; weather(y * 1.6, v * 1.6, true); }, () => {
      ff.animate([{ transform: 'translateY(0)' }, { transform: 'translateY(-6px)' }, { transform: 'translateY(0)' }], { duration: 240, easing: 'ease-out' });
      w.classList.add('gone'); WIZ.beat(250, () => { ff.style.transform = ''; WIZ.fallLive(); });
    });
  }
  WIZ.boot({ wayIn, wayOut });
})();
