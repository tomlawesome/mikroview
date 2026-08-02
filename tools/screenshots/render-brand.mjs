// One-off dev tool: renders brand/*.svg into PNG exports (favicons, GitHub
// social preview) via headless Chromium, so every raster asset traces back
// to the same vector source instead of being redrawn by hand.
import { chromium } from 'playwright'
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'

const MARK_SVG = readFileSync(new URL('../../brand/logo-mark.svg', import.meta.url), 'utf8')
const LOCKUP_DARK_SVG = readFileSync(new URL('../../brand/logo-lockup-dark.svg', import.meta.url), 'utf8')

const outDir = new URL('../../brand/', import.meta.url)
mkdirSync(outDir, { recursive: true })

const browser = await chromium.launch()
const page = await browser.newPage()

function markAtSize(size) {
  return MARK_SVG.replace('width="64" height="64"', `width="${size}" height="${size}"`)
}

async function renderSvgToPng(svg, size, path, background = null) {
  await page.setViewportSize({ width: size, height: size })
  await page.setContent(`
    <html><body style="margin:0;width:${size}px;height:${size}px;${background ? `background:${background};` : ''}display:flex;align-items:center;justify-content:center;">
      ${svg}
    </body></html>
  `)
  const el = page.locator('svg').first()
  await el.evaluate((node, s) => {
    node.setAttribute('width', s)
    node.setAttribute('height', s)
  }, size)
  await page.screenshot({ path, omitBackground: !background })
}

// Favicons (transparent)
for (const size of [16, 32, 48, 64]) {
  await renderSvgToPng(MARK_SVG, size, new URL(`favicon-${size}.png`, outDir).pathname)
}
// Apple touch icon (opaque, iOS doesn't composite transparency well)
await renderSvgToPng(MARK_SVG, 180, new URL('apple-touch-icon.png', outDir).pathname, '#0b0e14')

// Square mark PNG for avatars etc, transparent
await renderSvgToPng(MARK_SVG, 512, new URL('logo-mark-512.png', outDir).pathname)

// GitHub social preview: 1280x640
await page.setViewportSize({ width: 1280, height: 640 })
await page.setContent(`
  <html><body style="margin:0;width:1280px;height:640px;background:#0b0e14;position:relative;overflow:hidden;font-family:-apple-system,Segoe UI,system-ui,Roboto,sans-serif;">
    <div style="position:absolute;inset:0;background:
      radial-gradient(1100px 500px at 88% 8%, rgba(45,111,224,0.20), transparent 60%),
      radial-gradient(700px 380px at 8% 100%, rgba(62,207,126,0.08), transparent 60%);">
    </div>
    <div style="position:absolute;inset:0;background-image:linear-gradient(rgba(255,255,255,0.035) 1px, transparent 1px),linear-gradient(90deg, rgba(255,255,255,0.035) 1px, transparent 1px);background-size:44px 44px;mask-image:linear-gradient(to bottom, black, transparent 75%);"></div>
    <div style="position:absolute;right:-170px;top:50%;transform:translateY(-50%);opacity:0.08;line-height:0;">${markAtSize(760)}</div>
    <div style="position:relative;height:100%;display:flex;flex-direction:column;justify-content:center;padding:0 88px;">
      <div style="transform:scale(1.9);transform-origin:left center;margin-bottom:46px;">${LOCKUP_DARK_SVG}</div>
      <div style="font-size:29px;color:#9aa3b2;max-width:820px;line-height:1.5;font-weight:400;">
        A real-time RouterOS firewall live view &mdash; see every connection<br/>accepted, dropped, or rejected, and by which rule.
      </div>
      <div style="display:flex;gap:14px;margin-top:40px;">
        <span style="font-family:ui-monospace,monospace;font-size:15px;color:#3ecf7e;background:rgba(62,207,126,0.12);border:1px solid rgba(62,207,126,0.3);padding:7px 14px;border-radius:6px;">ACCEPT</span>
        <span style="font-family:ui-monospace,monospace;font-size:15px;color:#f5a623;background:rgba(245,166,35,0.12);border:1px solid rgba(245,166,35,0.3);padding:7px 14px;border-radius:6px;">DROP</span>
        <span style="font-family:ui-monospace,monospace;font-size:15px;color:#ef4444;background:rgba(239,68,68,0.12);border:1px solid rgba(239,68,68,0.3);padding:7px 14px;border-radius:6px;">REJECT</span>
      </div>
    </div>
  </body></html>
`)
await page.screenshot({ path: new URL('social-preview.png', outDir).pathname })

await browser.close()
console.log('brand PNGs rendered to', outDir.pathname)
