import { build, type Plugin } from 'vite';
import { resolve } from 'node:path';
import { createHash } from 'node:crypto';

/** One self-contained renderer for both dev and packaged opaque frames. */
export function officeAssets(licensePlugin: () => Plugin): Plugin {
  let source: Promise<{ code: string; path: string }> | undefined;
  const compile = () => source ??= (async () => {
    const output = await build({ configFile: false, logLevel: 'error', publicDir: false, plugins: [licensePlugin()], build: { write: false, minify: true, lib: { entry: resolve('src/features/workspace/docxRenderer.ts'), name: 'CoBotDocx', formats: ['iife'] } } });
    const result = Array.isArray(output) ? output[0] : output;
    if (!('output' in result) || result.output[0].type !== 'chunk') throw new Error('DOCX renderer bundle missing');
    const code = result.output[0].code;
    return { code, path: 'assets/docx-renderer-' + createHash('sha256').update(code).digest('hex').slice(0, 12) + '.js' };
  })();
  return {
    name: 'cobot-office-assets',
    buildStart() { source = undefined; },
    resolveId(id) { if (id === 'virtual:cobot-docx-renderer') return '\0cobot-docx-renderer'; },
    async load(id) { if (id === '\0cobot-docx-renderer') return 'export default ' + JSON.stringify('/' + (await compile()).path); },
    async generateBundle() { const asset = await compile(); this.emitFile({ type: 'asset', fileName: asset.path, source: asset.code }); },
    configureServer(server) {
      server.middlewares.use(async (req, res, next) => {
        if (!req.url?.startsWith('/assets/docx-renderer-')) { next(); return; }
        try { const asset = await compile(); if (req.url.split('?')[0] !== '/' + asset.path) { next(); return; } res.setHeader('Content-Type', 'text/javascript; charset=utf-8'); res.end(asset.code); }
        catch (error) { next(error); }
      });
      server.watcher.on('change', (path) => { if (/docxRenderer|workbookArchive/.test(path)) source = undefined; });
    },
  };
}
