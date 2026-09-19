import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { VitePWA } from 'vite-plugin-pwa'

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    // injectManifest (not generateSW) because the mobile self-service pages
    // need custom runtime-caching logic (src/sw.ts), not just precached
    // static assets.
    VitePWA({
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'sw.ts',
      injectRegister: false, // registered manually in main.tsx, only for the /m/* mobile pages
      manifest: {
        name: 'MDM 租借自助服務',
        short_name: '租借自助',
        description: '借用人自助每日回報／歸還，離線時先存裝置上，連線後自動同步',
        lang: 'zh-TW',
        start_url: '/m/rentals',
        scope: '/',
        display: 'standalone',
        background_color: '#f6f7f9',
        theme_color: '#4f46e5',
        icons: [
          { src: '/pwa-192.png', sizes: '192x192', type: 'image/png' },
          { src: '/pwa-512.png', sizes: '512x512', type: 'image/png' },
        ],
      },
      injectManifest: {
        // NOTE: the mobile self-service pages (src/pages/mobile/*) are their
        // own small lazy-loaded chunks, but they still depend on the same
        // main entry bundle as the desktop admin app (React, the router,
        // shared stores/lib) — there's no code-split boundary between them
        // yet, since App.tsx still imports every desktop page eagerly. So
        // for the PWA to actually boot offline right now, the whole build
        // output has to be precached, not just the Mobile* chunks; trimming
        // this down would need lazy-loading the desktop pages too (they
        // aren't today), which is a larger follow-up, not done here.
        globPatterns: ['**/*.{js,css,html}'],
        // The main entry chunk (React + router + the whole desktop admin
        // app, ~5-6MB) is included above Workbox's normal 2MB precache
        // limit — see the injectManifest comment above for why it can't be
        // trimmed out yet.
        maximumFileSizeToCacheInBytes: 10 * 1024 * 1024,
      },
      // Off by default, the service worker only runs in a production build
      // (vite-plugin-pwa's normal behavior) — which meant offline testing
      // against `vite dev` silently did nothing. type:'module' runs sw.ts
      // directly as an ES module in dev (no injectManifest bundling step),
      // so the precache/runtime-caching logic can be exercised locally too.
      devOptions: { enabled: true, type: 'module' },
    }),
  ],
  server: {
    allowedHosts: [
      'mdm.isha.net'
    ],
    proxy: {
      '/mdm.v1': {
        target: 'http://localhost:8080',
        // SSE / streaming 需要長連線，不要超時
        timeout: 0,
      },
      '/webhook': 'http://localhost:8080',
      '/api': 'http://localhost:8080',
    },
  },
})
