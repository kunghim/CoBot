/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// The dev server proxies /api to the Go core started by scripts/dev.*. The
// endpoint and the same-user runtime secret are injected through process
// environment only — never committed and never logged.
const apiEndpoint = process.env.COBOT_API_ENDPOINT ?? 'http://127.0.0.1:18080';
const adminSecret = process.env.COBOT_ADMIN_SECRET ?? '';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
  },
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
  },
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': {
        target: apiEndpoint,
        changeOrigin: false,
      },
    },
    configureServer(server) {
      // Dev-only convenience: mint a one-time bootstrap secret on demand so
      // the page works whenever the developer opens it, while the product
      // 60s/one-use rule stays intact. Enabled only when the dev script
      // handed the runtime secret to this process.
      if (!adminSecret) return;
      server.middlewares.use('/__cobot/dev-link', async (req, res) => {
        if (req.method !== 'POST') {
          res.statusCode = 405;
          res.end();
          return;
        }
        try {
          const upstream = await fetch(apiEndpoint + '/internal/v1/session/link', {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
              'X-Instance-Secret': adminSecret,
            },
            body: JSON.stringify({ requestId: 'dev-' + Date.now() }),
          });
          const body = await upstream.text();
          res.statusCode = upstream.status;
          res.setHeader('Content-Type', 'application/json; charset=utf-8');
          res.end(body);
        } catch (err) {
          res.statusCode = 502;
          res.end(JSON.stringify({ error: { code: 'internal', messageKey: 'error.dev_link_failed' } }));
        }
      });
    },
  },
});
