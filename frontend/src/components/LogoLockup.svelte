<script lang="ts">
  import LogoMark from './LogoMark.svelte'
  let { size = 22 }: { size?: number } = $props()
</script>

<span class="lockup" style="--logo-size: {size}px">
  <LogoMark size={size} />
  <span class="wordmark">Mikro<span class="accent">View</span></span>
</span>

<style>
  .lockup {
    display: inline-flex;
    align-items: center;
    gap: calc(var(--logo-size) * 0.36);
  }

  .wordmark {
    font-weight: 700;
    font-size: calc(var(--logo-size) * 0.82);
    letter-spacing: -0.01em;
    color: var(--fg);
    white-space: nowrap;
  }

  .accent {
    color: var(--accent);
  }
</style>
