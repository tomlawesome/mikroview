<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The void's rain (#645 round 29, extracted and grown under #1214):
  // the fall's three inks falling across the whole dark ground, shared
  // by every pre-deck screen -- the door (AuthScreen) and the journey's
  // attach beat -- so the weather never stops between the first paint
  // and the live fall. Identity texture, not data: it never claims
  // traffic exists, so a virgin instance honestly shows the same sky.
  //
  // Forty strokes, transform-only, is the whole cost -- no particle
  // system, no JS after mount, nothing fetched. #1214 grew the field
  // from the round-29 scene's seventeen: on a real instance the owner
  // read the sparse door as "flat black" and "static", so the ratified
  // texture stays (accept-dominant, thin marks, dark ground) but at a
  // density that reads alive. Each stroke's --y is its resting place
  // under reduced motion: the rain hangs still instead of vanishing,
  // so the scene keeps its texture when its movement is declined.
  //
  // The mask lives HERE, on .fullfall itself, as a named variant per
  // consumer -- live-door.mjs and live-door-engines.mjs read the mask
  // off .fullfall's computed style, and the ratified rule (round 5,
  // fourth batch) is that the rain never crosses the centred elements.
  let {
    // Which centre is carved out of the layer: the door's form stack,
    // or the attach beat's taller command card.
    mask = 'door',
  }: {
    mask?: 'door' | 'attach'
  } = $props()
</script>

<!-- Per-stroke left/delay/duration/opacity live in this component's
     stylesheet as nth-child rules, NOT in style attributes: the app's
     CSP (default-src 'self') forbids inline style attributes, and
     Firefox enforces that on statically templated markup -- every
     stroke lost its position there and the whole fall collapsed into
     one block at the layer's origin (#645, owner report 2026-08-30).
     Chromium let the same markup through, so no Chromium-driven check
     can see this class of breakage. -->
<div class="fullfall" class:door={mask === 'door'} class:attach={mask === 'attach'} aria-hidden="true">
  <i></i>
  <i></i>
  <i class="r"></i>
  <i></i>
  <i></i>
  <i class="v"></i>
  <i></i>
  <i class="r"></i>
  <i></i>
  <i></i>
  <i></i>
  <i class="v"></i>
  <i class="r"></i>
  <i></i>
  <i></i>
  <i class="r"></i>
  <i></i>
  <i></i>
  <i></i>
  <i class="v"></i>
  <i></i>
  <i></i>
  <i class="r"></i>
  <i></i>
  <i></i>
  <i></i>
  <i class="r"></i>
  <i></i>
  <i class="v"></i>
  <i></i>
  <i></i>
  <i class="r"></i>
  <i></i>
  <i></i>
  <i class="r"></i>
  <i></i>
  <i class="v"></i>
  <i></i>
  <i class="r"></i>
  <i></i>
</div>

<style>
  .fullfall {
    position: absolute;
    inset: 0;
    overflow: hidden;
    pointer-events: none;
  }

  /* The centre is carved out entirely -- the rain never crosses the
     screen's own elements, whatever their combined height turns out to
     be (round 5 fourth batch). One ellipse per consumer: the door's
     form stack, and the attach beat's taller command card. */
  .fullfall.door {
    -webkit-mask: radial-gradient(ellipse 460px 380px at 50% 52%, transparent 62%, black 78%);
    mask: radial-gradient(ellipse 460px 380px at 50% 52%, transparent 62%, black 78%);
  }

  .fullfall.attach {
    -webkit-mask: radial-gradient(ellipse 520px 460px at 50% 50%, transparent 62%, black 78%);
    mask: radial-gradient(ellipse 520px 460px at 50% 50%, transparent 62%, black 78%);
  }

  .fullfall i {
    position: absolute;
    top: -20px;
    width: 2.5px;
    height: 13px;
    border-radius: 2px;
    background: var(--fall-accept);
    animation: fall 5.5s linear infinite;
  }

  .fullfall i.r {
    background: var(--fall-drop);
  }

  .fullfall i.v {
    background: var(--fall-nat);
  }

  /* Three duration tiers desynchronise the field: at forty strokes a
     single shared period reads as one repeating curtain. */
  .fullfall i:nth-child(1) { left: 2%; animation-delay: 0.2s; --y: 8%; opacity: 0.5; }
  .fullfall i:nth-child(2) { left: 4%; animation-delay: 3.1s; animation-duration: 6.4s; --y: 64%; opacity: 0.3; }
  .fullfall i:nth-child(3) { left: 7%; animation-delay: 1.6s; animation-duration: 4.6s; --y: 31%; opacity: 0.55; }
  .fullfall i:nth-child(4) { left: 9%; animation-delay: 4.4s; --y: 78%; opacity: 0.4; }
  .fullfall i:nth-child(5) { left: 12%; animation-delay: 2.2s; animation-duration: 6.4s; --y: 15%; opacity: 0.65; }
  .fullfall i:nth-child(6) { left: 14%; animation-delay: 5s; animation-duration: 4.6s; --y: 52%; opacity: 0.45; }
  .fullfall i:nth-child(7) { left: 17%; animation-delay: 0.9s; --y: 88%; opacity: 0.3; }
  .fullfall i:nth-child(8) { left: 19%; animation-delay: 3.7s; animation-duration: 6.4s; --y: 24%; opacity: 0.4; }
  .fullfall i:nth-child(9) { left: 22%; animation-delay: 1.2s; animation-duration: 4.6s; --y: 70%; opacity: 0.6; }
  .fullfall i:nth-child(10) { left: 24%; animation-delay: 4.8s; --y: 41%; opacity: 0.35; }
  .fullfall i:nth-child(11) { left: 27%; animation-delay: 2.7s; animation-duration: 6.4s; --y: 95%; opacity: 0.5; }
  .fullfall i:nth-child(12) { left: 29%; animation-delay: 0.5s; animation-duration: 4.6s; --y: 58%; opacity: 0.4; }
  .fullfall i:nth-child(13) { left: 32%; animation-delay: 3.4s; --y: 12%; opacity: 0.6; }
  .fullfall i:nth-child(14) { left: 34%; animation-delay: 1.9s; animation-duration: 6.4s; --y: 83%; opacity: 0.35; }
  .fullfall i:nth-child(15) { left: 37%; animation-delay: 5.3s; animation-duration: 4.6s; --y: 36%; opacity: 0.55; }
  .fullfall i:nth-child(16) { left: 39%; animation-delay: 2.4s; --y: 67%; opacity: 0.35; }
  .fullfall i:nth-child(17) { left: 42%; animation-delay: 4.1s; animation-duration: 6.4s; --y: 47%; opacity: 0.5; }
  .fullfall i:nth-child(18) { left: 44%; animation-delay: 0.7s; animation-duration: 4.6s; --y: 20%; opacity: 0.45; }
  .fullfall i:nth-child(19) { left: 47%; animation-delay: 3.9s; --y: 74%; opacity: 0.3; }
  .fullfall i:nth-child(20) { left: 49%; animation-delay: 1.4s; animation-duration: 6.4s; --y: 55%; opacity: 0.6; }
  .fullfall i:nth-child(21) { left: 52%; animation-delay: 4.6s; animation-duration: 4.6s; --y: 29%; opacity: 0.4; }
  .fullfall i:nth-child(22) { left: 54%; animation-delay: 2s; --y: 90%; opacity: 0.5; }
  .fullfall i:nth-child(23) { left: 57%; animation-delay: 5.6s; animation-duration: 6.4s; --y: 10%; opacity: 0.35; }
  .fullfall i:nth-child(24) { left: 59%; animation-delay: 0.3s; animation-duration: 4.6s; --y: 62%; opacity: 0.55; }
  .fullfall i:nth-child(25) { left: 62%; animation-delay: 3.3s; --y: 44%; opacity: 0.3; }
  .fullfall i:nth-child(26) { left: 64%; animation-delay: 1.1s; animation-duration: 6.4s; --y: 81%; opacity: 0.45; }
  .fullfall i:nth-child(27) { left: 67%; animation-delay: 4.3s; animation-duration: 4.6s; --y: 18%; opacity: 0.6; }
  .fullfall i:nth-child(28) { left: 69%; animation-delay: 2.6s; --y: 72%; opacity: 0.35; }
  .fullfall i:nth-child(29) { left: 72%; animation-delay: 5.8s; animation-duration: 6.4s; --y: 33%; opacity: 0.5; }
  .fullfall i:nth-child(30) { left: 74%; animation-delay: 0.8s; animation-duration: 4.6s; --y: 60%; opacity: 0.4; }
  .fullfall i:nth-child(31) { left: 77%; animation-delay: 3.6s; --y: 26%; opacity: 0.55; }
  .fullfall i:nth-child(32) { left: 79%; animation-delay: 1.7s; animation-duration: 6.4s; --y: 86%; opacity: 0.3; }
  .fullfall i:nth-child(33) { left: 82%; animation-delay: 4.9s; animation-duration: 4.6s; --y: 50%; opacity: 0.45; }
  .fullfall i:nth-child(34) { left: 84%; animation-delay: 2.9s; --y: 6%; opacity: 0.6; }
  .fullfall i:nth-child(35) { left: 87%; animation-delay: 0.4s; animation-duration: 6.4s; --y: 76%; opacity: 0.35; }
  .fullfall i:nth-child(36) { left: 89%; animation-delay: 3s; animation-duration: 4.6s; --y: 39%; opacity: 0.5; }
  .fullfall i:nth-child(37) { left: 92%; animation-delay: 5.1s; --y: 92%; opacity: 0.3; }
  .fullfall i:nth-child(38) { left: 94%; animation-delay: 1.5s; animation-duration: 6.4s; --y: 22%; opacity: 0.55; }
  .fullfall i:nth-child(39) { left: 97%; animation-delay: 4s; animation-duration: 4.6s; --y: 66%; opacity: 0.4; }
  .fullfall i:nth-child(40) { left: 99%; animation-delay: 2.5s; --y: 46%; opacity: 0.5; }

  @keyframes fall {
    to {
      transform: translateY(106vh);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    /* Still rain, not no rain: reduced motion declines the falling, not
       the scene. Each stroke stops at its own --y instead of animating
       from above the frame (where the animation's absence would
       otherwise strand every stroke off-screen at top: -20px). */
    .fullfall i {
      animation: none;
      top: var(--y, 50%);
    }
  }
</style>
