import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'
import type { ProxyOptions } from 'vite'
import { defineConfig } from 'vitest/config'

// The server of tmp/server.yaml; LACIS_SERVER overrides it.
const server = process.env.LACIS_SERVER ?? 'https://localhost:8443'

// The dev server is plain HTTP: the session cookie the server marks Secure
// would be refused by the browsers that do not trust http://localhost.
const proxy: ProxyOptions = {
  target: server,
  secure: false,
  changeOrigin: true,
  configure: (p) => {
    p.on('proxyRes', (res) => {
      const cookies = res.headers['set-cookie']
      if (cookies) res.headers['set-cookie'] = cookies.map((c) => c.replace(/;\s*Secure/gi, ''))
    })
  },
}

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  // Relative assets: lacis-server serves the front under any base path,
  // which the <base> of index.html gives at runtime.
  base: './',
  build: { outDir: 'dist', emptyOutDir: true, chunkSizeWarningLimit: 2000 },
  server: {
    proxy: {
      '/graphql': proxy,
      '/auth': proxy,
      '/icons': proxy,
    },
  },
  test: { environment: 'jsdom', globals: true, setupFiles: ['./src/test/setup.ts'], include: ['src/**/*.test.{ts,tsx}'] },
})
