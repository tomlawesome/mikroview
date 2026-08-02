<script lang="ts">
  let { size = 28 }: { size?: number } = $props()
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 64 64"
  xmlns="http://www.w3.org/2000/svg"
  role="img"
  aria-label="MikroView"
>
  <path
    d="M32 4 L55 13 L55 29 C55 46.5 45 57 32 61 C19 57 9 46.5 9 29 L9 13 Z"
    fill="#2f6fe0"
  />
  <path
    d="M32 4 L55 13 L55 29 C55 46.5 45 57 32 61 C19 57 9 46.5 9 29 L9 13 Z"
    fill="none"
    stroke="#1a4fc4"
    stroke-width="1"
  />
  <polyline
    points="15,34 22,34 26,20 32,47 36,25 40,39 44,34 49,34"
    fill="none"
    stroke="#ffffff"
    stroke-width="3.2"
    stroke-linecap="round"
    stroke-linejoin="round"
  />
  <circle cx="26" cy="20" r="2.6" fill="#3ecf7e" />
  <circle cx="32" cy="47" r="2.6" fill="#ef4444" />
</svg>
