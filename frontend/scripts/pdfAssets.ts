import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { readFileSync, readdirSync } from 'node:fs';
import type { Plugin } from 'vite';

export function pdfAssets(recordResource: (path: string) => void): Plugin {
  const root = dirname(createRequire(import.meta.url).resolve('pdfjs-dist/package.json'));
  const version = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8')).version as string;
  const files = new Map<string, string>();
  for (const directory of ['cmaps', 'standard_fonts', 'iccs', 'wasm']) {
    for (const entry of readdirSync(join(root, directory), { withFileTypes: true })) {
      if (entry.isFile()) files.set(`assets/pdfjs/${version}/${directory}/${entry.name}`, join(root, directory, entry.name));
    }
  }
  return {
    name: 'cobot-pdf-assets',
    generateBundle() {
      for (const [fileName, path] of files) { recordResource(path); this.emitFile({ type: 'asset', fileName, source: readFileSync(path) }); }
    },
    configureServer(server) {
      server.middlewares.use((request, response, next) => {
        const path = files.get((request.url ?? '').split('?')[0].replace(/^\//, ''));
        if (!path) return next();
        response.setHeader('Content-Type', path.endsWith('.wasm') ? 'application/wasm' : path.endsWith('.js') ? 'text/javascript' : path.endsWith('.ttf') ? 'font/ttf' : 'application/octet-stream');
        response.setHeader('X-Content-Type-Options', 'nosniff');
        response.end(readFileSync(path));
      });
    },
  };
}
