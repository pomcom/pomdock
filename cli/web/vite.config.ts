import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// Built to web/dist and embedded into the pomdock binary (cli/webui.go).
//
// Dev loop: run `pomdock web --no-open`, open its printed URL once (the login
// cookie is per host, so it also covers :5173), then `npm run dev` and browse
// http://127.0.0.1:5173. The proxy rewrites Host and Origin so the server's
// local-only checks still pass.
const backend = process.env.POMDOCK_WEB_BACKEND ?? 'http://127.0.0.1:7332'

export default defineConfig({
  plugins: [svelte()],
  build: { outDir: 'dist', emptyOutDir: true, target: 'es2022' },
  server: {
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      '/api': {
        target: backend,
        changeOrigin: true,
        configure: (proxy) => proxy.on('proxyReq', (req) => {
          if (req.getHeader('origin')) req.setHeader('origin', backend)
        }),
      },
    },
  },
})
