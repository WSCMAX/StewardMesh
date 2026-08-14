import { defineConfig, loadEnv } from 'vite'
import react, { reactCompilerPreset } from '@vitejs/plugin-react'
import babel from '@rolldown/plugin-babel'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig(({ mode }) => {
  const development = loadEnv(mode, '.', '')
  const proxyTarget = development.STEWARDMESH_DEV_PROXY_TARGET || 'http://127.0.0.1:8080'

  return {
    plugins: [
      react(),
      babel({ presets: [reactCompilerPreset()] }),
      tailwindcss(),
    ],
    server: {
      port: 5173,
      // REQ-FOUNDATION-001 / SEC-MCP-001: keep every first-party protocol on
      // one browser-visible origin while allowing isolated local test ports.
      proxy: {
        '/api': proxyTarget,
        '/healthz': proxyTarget,
        '/.well-known': proxyTarget,
        '/oauth': proxyTarget,
        '/mcp': proxyTarget,
      },
    },
  }
})
