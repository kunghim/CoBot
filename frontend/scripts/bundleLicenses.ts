import { createHash } from 'node:crypto';
import { existsSync, readFileSync, readdirSync, realpathSync, writeFileSync } from 'node:fs';
import { dirname, isAbsolute, join, relative, resolve } from 'node:path';
import type { OutputBundle } from 'rollup';
import type { Plugin, ResolvedConfig } from 'vite';

const digest = (data: string | Uint8Array) => createHash('sha256').update(data).digest('hex');
const slash = (path: string) => path.replaceAll('\\', '/');

/** Records retained code and explicitly copied resources, including nested builds. */
export function bundleLicenses() {
  let config: ResolvedConfig;
  let frontendRoot: string;
  const inputs = new Map<string, string>();
  const noticeDirectories = new Set<string>();
  const generated = new Set<string>();
  const packages = new Map<string, { path: string; name: string; version: string; files: Map<string, { path: string; sha256: string; resource: boolean }> }>();
  const record = (id: string, resource = false) => {
    if (!config) return;
    const candidate = id.replace(/^\0/, '').split('?')[0];
    if (!isAbsolute(candidate) || !existsSync(candidate)) return;
    const path = realpathSync(candidate);
    const local = slash(relative(frontendRoot, path));
    if (local.startsWith('../')) throw new Error('Untracked frontend build input: ' + path);
    const sha256 = digest(readFileSync(path));
    if (!local.startsWith('node_modules/')) { inputs.set(local, sha256); return; }
    // A package can contain nested package.json files that only set "type".
    const start = local.lastIndexOf('node_modules/') + 'node_modules/'.length;
    const parts = local.slice(start).split('/');
    const key = local.slice(0, start) + parts.slice(0, parts[0].startsWith('@') ? 2 : 1).join('/');
    const directory = join(frontendRoot, key);
    const manifest = JSON.parse(readFileSync(join(directory, 'package.json'), 'utf8'));
    if (!packages.has(key)) packages.set(key, { path: key, name: manifest.name, version: manifest.version, files: new Map() });
    const owner = packages.get(key)!;
    const source = slash(relative(directory, path));
    owner.files.set(source, { path: source, sha256, resource: resource || owner.files.get(source)?.resource === true });
    const notices = (parent: string) => {
      if (noticeDirectories.has(parent)) return;
      noticeDirectories.add(parent);
      for (const file of readdirSync(parent, { withFileTypes: true })) {
        if (!file.isFile() || !/^(licen[cs]e|notice|copying|copyright)([._-].*)?$/i.test(file.name)) continue;
        const name = slash(relative(directory, join(parent, file.name)));
        if (!owner.files.has(name)) owner.files.set(name, { path: name, sha256: digest(readFileSync(join(parent, file.name))), resource: false });
      }
    };
    notices(directory);
    if (resource) for (let parent = dirname(path); parent !== directory; parent = dirname(parent)) notices(parent);
  };
  const recordBundle = (bundle: OutputBundle) => {
    if (!config) return;
    for (const output of Object.values(bundle)) {
      if (output.type !== 'chunk') continue;
      for (const [id, module] of Object.entries(output.modules)) {
        if (module.renderedLength <= 0) continue;
        if (['\0vite/modulepreload-polyfill.js', '\0vite/preload-helper.js', '\0commonjsHelpers.js'].includes(id)) {
          // Vite's LICENSE also preserves its bundled CommonJS plugin notices.
          generated.add(id.slice(1));
          const runtime = join(frontendRoot, 'node_modules/vite/package.json');
          if (!existsSync(runtime)) throw new Error('Injected runtime package is missing: vite');
          record(runtime);
        } else record(id);
      }
    }
  };
  const recordStyles = (paths: string[], bundle: OutputBundle) => {
    if (!config) return;
    if (!Object.values(bundle).some((output) => output.type === 'asset' && output.fileName.endsWith('.css'))) return;
    // CSS imports can be consumed by Vite's CSS pipeline without JS modules.
    for (const path of paths) if (/\.(?:css|scss|sass|less|styl)(?:\?|$)/i.test(path)) record(path);
  };
  const worker = (): Plugin => ({ name: 'cobot-worker-license-inputs', generateBundle(_, bundle) { recordBundle(bundle); recordStyles(this.getWatchFiles(), bundle); } });
  const main: Plugin = {
    name: 'cobot-bundle-license-inputs',
    apply: 'build',
    enforce: 'post',
    configResolved(value) { config = value; frontendRoot = realpathSync(config.root); },
    buildStart() {
      inputs.clear(); packages.clear(); noticeDirectories.clear(); generated.clear();
      for (const path of config.configFileDependencies) record(path);
    },
    generateBundle(_, bundle) { recordBundle(bundle); recordStyles(this.getWatchFiles(), bundle); },
    writeBundle: {
      order: 'post',
      handler() {
        const root = resolve(config.root, config.build.outDir);
        const files: { path: string; sha256: string }[] = [];
        const walk = (directory: string) => {
          for (const entry of readdirSync(directory, { withFileTypes: true })) {
            const path = join(directory, entry.name);
            if (entry.isDirectory()) walk(path);
            else if (entry.isFile()) {
              const name = slash(relative(root, path));
              if (name !== 'third-party-inputs.json') files.push({ path: name, sha256: digest(readFileSync(path)) });
            } else throw new Error('Unexpected frontend output: ' + path);
          }
        };
        walk(root);
        const manifest = {
          version: 1,
          lockSha256: digest(readFileSync(join(config.root, 'package-lock.json'))),
          inputs: [...inputs].sort().map(([path, sha256]) => ({ path, sha256 })),
          generatedRuntime: [...generated].sort(),
          packages: [...packages.values()].sort((a, b) => a.path.localeCompare(b.path)).map((owner) => ({ ...owner, files: [...owner.files.values()].sort((a, b) => a.path.localeCompare(b.path)) })),
          files: files.sort((a, b) => a.path.localeCompare(b.path)),
        };
        writeFileSync(join(root, 'third-party-inputs.json'), JSON.stringify(manifest, null, 2) + '\n');
      },
    },
  };
  return { main, worker, recordResource: (path: string) => record(path, true) };
}
