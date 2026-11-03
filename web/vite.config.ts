import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'
import type { Plugin, ProxyOptions } from 'vite'
import { defineConfig } from 'vitest/config'

// The server of tmp/server.yaml; LACIS_SERVER overrides it.
const server = process.env.LACIS_SERVER ?? 'http://localhost:8443'

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

// serverConfig gives the dev front the configuration the server writes in
// its index.html (ways to log in, MCP…), instead of the defaults of
// web/index.html; they stay when the server does not answer.
function serverConfig(): Plugin {
  const tag = /(<script id="lacis-config" type="application\/json">)[^<]*(<\/script>)/
  return {
    name: 'lacis-server-config',
    apply: 'serve',
    async transformIndexHtml(html) {
      try {
        const page = await (await fetch(server + '/')).text()
        const config = tag.exec(page)?.[0]
        return config ? html.replace(tag, config) : html
      } catch {
        return html
      }
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss(), serverConfig()],
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
      '/oauth': proxy,
      '/mcp': proxy,
      '/.well-known': proxy,
    },
  },
  test: { environment: 'jsdom', globals: true, setupFiles: ['./src/test/setup.ts'], include: ['src/**/*.test.{ts,tsx}'] },
})
