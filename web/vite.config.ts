// Vite dev server proxies /api to the Go server (SYNKRA_MAIL_HTTP_ADDR,
// default 127.0.0.1:8080) so the SPA and the API share an origin in dev —
// no CORS anywhere. In production both sit behind the same reverse proxy.
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: process.env.SYNKRA_MAIL_HTTP_ADDR || 'http://127.0.0.1:8080',
        changeOrigin: false,
      },
    },
  },
  build: { target: 'es2022', sourcemap: true },
})
