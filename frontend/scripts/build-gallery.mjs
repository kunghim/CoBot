#!/usr/bin/env node
// Builds the component gallery into one self-contained HTML file.
//
//   npm run gallery --prefix frontend [-- --out <file>]
//
// The page is rendered on the server first, so the file's own text holds
// every rule and description (readable without a browser), then the client
// bundle and the CSS are inlined so the demos respond when it is opened.
// Default output: docs/design/组件画廊.html at the repository root.
import { mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, extname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { build, createServer } from 'vite';

const frontend = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const repo = resolve(frontend, '..');
const outArg = process.argv.indexOf('--out');
const out = outArg > 0 ? resolve(process.argv[outArg + 1]) : join(repo, 'docs', 'design', '组件画廊.html');
const work = join(repo, '.tmp', 'gallery-build');
const generatedAt = new Date().toLocaleString('zh-CN', { hour12: false });

// 1. The client bundle: one JS file, one CSS file, everything inlined.
rmSync(work, { recursive: true, force: true });
await build({
  configFile: false,
  root: frontend,
  logLevel: 'warn',
  plugins: [react(), tailwindcss()],
  build: {
    outDir: work,
    emptyOutDir: true,
    assetsInlineLimit: Number.MAX_SAFE_INTEGER,
    cssCodeSplit: false,
    modulePreload: false,
    rollupOptions: { input: join(frontend, 'gallery.html'), output: { inlineDynamicImports: true } },
  },
});

// 2. The same page rendered to HTML on the server.
const quiet = console.error;
console.error = (...args) => {
  // Layout effects (dialog focus) do nothing on the server; that is fine.
  if (String(args[0]).includes('useLayoutEffect does nothing on the server')) return;
  quiet(...args);
};
const server = await createServer({
  configFile: false,
  root: frontend,
  logLevel: 'warn',
  appType: 'custom',
  // Only gallery modules are loaded; scanning the product entry would require
  // its format-specific virtual assets, which the standalone gallery omits.
  optimizeDeps: { noDiscovery: true },
  plugins: [react()],
  server: { middlewareMode: true, hmr: false, fs: { allow: [repo] } },
});
let markup;
try {
  const { renderToString } = await import('react-dom/server');
  const React = await import('react');
  const { default: Gallery } = await server.ssrLoadModule('/src/gallery/Gallery.tsx');
  markup = renderToString(React.createElement(Gallery, { generatedAt }));
  // Vite's SSR imports keep /src URLs, while the client bundle inlines them.
  // Inline the server-rendered images too: hydration keeps existing src attrs,
  // and the standalone gallery has no /src server to load pictures from.
  const imageTypes = { '.png': 'image/png', '.svg': 'image/svg+xml', '.jpg': 'image/jpeg', '.webp': 'image/webp' };
  markup = markup.replace(/(<img\b[^>]*\bsrc=")(\/src\/[^"?]+)(")/g, (match, before, url, after) => {
    const mime = imageTypes[extname(url)];
    if (!mime) return match;
    const data = readFileSync(resolve(frontend, '.' + url)).toString('base64');
    return `${before}data:${mime};base64,${data}${after}`;
  });
} finally {
  await server.close();
  console.error = quiet;
}

// 3. One file: prerendered markup, inlined CSS and script.
const assets = join(work, 'assets');
const files = readdirSync(assets);
const css = files.filter((name) => name.endsWith('.css')).map((name) => readFileSync(join(assets, name), 'utf8')).join('\n');
const js = files.filter((name) => name.endsWith('.js')).map((name) => readFileSync(join(assets, name), 'utf8')).join('\n');
let html = readFileSync(join(work, 'gallery.html'), 'utf8');
html = html.replace(/<link rel="stylesheet"[^>]*>/g, '').replace(/<script type="module"[^>]*><\/script>/g, '');
// Replacers are functions: a replacement string would read $& and $' in the code.
html = html.replace('<div id="root"><!--gallery--></div>', () => `<div id="root" data-generated-at="${generatedAt}">${markup}</div>`);
html = html.replace('</head>', () => `<style>${css}</style>\n</head>`);
// A script's text must not contain its own closing tag.
html = html.replace('</body>', () => `<script type="module">${js.replace(/<\/script/gi, '<\\/script')}</script>\n</body>`);
mkdirSync(dirname(out), { recursive: true });
writeFileSync(out, html);
console.log(`gallery written to ${out} (${Math.round(Buffer.byteLength(html) / 1024)} KB)`);
