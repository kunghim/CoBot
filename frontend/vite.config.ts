/// <reference types="vitest/config" />
import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { officeAssets } from './scripts/officeAssets';
import { pdfAssets } from './scripts/pdfAssets';
import { bundleLicenses } from './scripts/bundleLicenses';

// The dev server proxies /api to the Go core started by scripts/dev.*. The
// endpoint and the same-user runtime secret are injected through process
// environment only — never committed and never logged.
const apiEndpoint = process.env.COBOT_API_ENDPOINT ?? 'http://127.0.0.1:18080';
const adminSecret = process.env.COBOT_ADMIN_SECRET ?? '';

function devAuthorization(): Plugin {
  return {
    name: 'cobot-dev-authorization',
    apply: 'serve',
    configureServer(server) {
      if (!adminSecret) return;
      server.middlewares.use('/__cobot/dev-link', async (req, res) => {
        if (req.method !== 'POST') { res.statusCode = 405; res.end(); return; }
        // This route can mint page authority: restrict both Host and browser
        // Origin before forwarding the runtime secret, not just the API proxy.
        const host = req.headers.host ?? '';
        const origin = req.headers.origin;
        if (!/^(127\.0\.0\.1|localhost):\d+$/.test(host) || (origin && origin !== 'http://' + host)) {
          res.statusCode = 403; res.end(); return;
        }
        try {
          const upstream = await fetch(apiEndpoint + '/internal/v1/session/link', {
            method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Instance-Secret': adminSecret },
            body: JSON.stringify({ requestId: crypto.randomUUID() }),
          });
          res.statusCode = upstream.status;
          res.setHeader('Content-Type', 'application/json; charset=utf-8');
          res.end(await upstream.text());
        } catch {
          res.statusCode = 502;
          res.end(JSON.stringify({ error: { code: 'internal', messageKey: 'error.dev_link_failed' } }));
        }
      });
    },
  };
}

const licenses = bundleLicenses();
export default defineConfig({
  plugins: [react(), tailwindcss(), devAuthorization(), pdfAssets(licenses.recordResource), officeAssets(licenses.worker), licenses.main],
  worker: { plugins: () => [licenses.worker()] },
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
  },
});
