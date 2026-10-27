import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, unlinkSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { bundleLicenses } from '../frontend/scripts/bundleLicenses.ts';
import { frontendBundle, frontendNoticeTexts } from './frontend-bundle.mjs';

const hash = (data) => createHash('sha256').update(data).digest('hex');
function fixture(t) {
  const parent = fileURLToPath(new URL('../.tmp/test-runs/', import.meta.url));
  mkdirSync(parent, { recursive: true });
  const root = mkdtempSync(join(parent, 'cobot-bundle-test-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const write = (path, value) => { mkdirSync(dirname(join(root, path)), { recursive: true }); writeFileSync(join(root, path), value); };
  write('frontend/main.js', 'console.log("fixture");');
  write('internal/webui/dist/main.js', 'console.log("bundled");');
  const lock = { packages: {} };
  for (const name of ['main-fixture', 'worker-fixture', 'fontkit']) {
    const owner = 'node_modules/' + name;
    lock.packages[owner] = { version: '1.0.0' };
    write('frontend/' + owner + '/package.json', JSON.stringify({ name, version: '1.0.0', main: 'index.js', type: 'module', license: 'MIT' }));
    write('frontend/' + owner + '/index.js', 'export const value = ' + JSON.stringify(name) + ';');
    if (name !== 'fontkit') write('frontend/' + owner + '/LICENSE', 'Fixture copyright and permission notice: ' + name);
  }
  write('frontend/package-lock.json', JSON.stringify(lock));
  const record = (path, base = root, resource) => ({ path, sha256: hash(readFileSync(join(base, path))), ...(resource === undefined ? {} : { resource }) });
  const owner = (name) => ({ path: 'node_modules/' + name, name, version: '1.0.0', files: [record('index.js', join(root, 'frontend/node_modules', name), false)] });
  const manifest = { version: 1, lockSha256: hash(readFileSync(join(root, 'frontend/package-lock.json'))), inputs: [record('main.js', join(root, 'frontend'))], files: [record('main.js', join(root, 'internal/webui/dist'))], packages: ['main-fixture', 'worker-fixture'].map(owner) };
  const receipt = join(root, 'internal/webui/dist/third-party-inputs.json');
  const save = () => writeFileSync(receipt, JSON.stringify(manifest));
  save();
  return { root, write, manifest, save, owner, record, receipt };
}

test('notices follow recorded packages; an installed Node-only package is excluded until used', (t) => {
  const f = fixture(t);
  let result = frontendNoticeTexts(f.root);
  assert.deepEqual(result.missing, []);
  assert.match(result.texts.join(''), /main-fixture/);
  assert.match(result.texts.join(''), /worker-fixture/);
  assert.doesNotMatch(result.texts.join(''), /fontkit/);
  f.manifest.packages.push(f.owner('fontkit')); f.save();
  result = frontendNoticeTexts(f.root);
  assert.deepEqual(result.missing, ['fontkit 1.0.0']);
  unlinkSync(join(f.root, 'frontend/node_modules/worker-fixture/LICENSE'));
  assert.deepEqual(frontendNoticeTexts(f.root).missing, ['worker-fixture 1.0.0', 'fontkit 1.0.0']);
});

test('missing, stale and changed build inputs or unrecorded outputs block collection', (t) => {
  for (const mutate of [
    (f) => unlinkSync(f.receipt),
    (f) => f.write('frontend/package-lock.json', '{}'),
    (f) => f.write('frontend/main.js', 'changed source'),
    (f) => f.write('frontend/node_modules/main-fixture/index.js', 'changed dependency'),
    (f) => f.write('internal/webui/dist/main.js', 'changed output'),
    (f) => f.write('internal/webui/dist/extra.js', 'unrecorded output'),
    (f) => { f.manifest.packages[0].path = '../outside'; f.save(); },
    (f) => { f.manifest.packages[0].version = '2.0.0'; f.save(); },
  ]) {
    const f = fixture(t); mutate(f);
    assert.throws(() => frontendBundle(f.root));
  }
});

test('copied resources retain their nested notices, and changed notice inputs fail', (t) => {
  const f = fixture(t), base = join(f.root, 'frontend/node_modules/main-fixture');
  f.write('frontend/node_modules/main-fixture/fonts/sample.ttf', 'fixture font');
  f.write('frontend/node_modules/main-fixture/fonts/LICENSE', 'Font-specific fixture notice');
  f.manifest.packages[0].files.push(f.record('fonts/sample.ttf', base, true), f.record('fonts/LICENSE', base, false)); f.save();
  assert.match(frontendNoticeTexts(f.root).texts.join(''), /Font-specific fixture notice/);
  unlinkSync(join(base, 'fonts/LICENSE'));
  assert.throws(() => frontendNoticeTexts(f.root));
});

test('packaging checks that each binary embeds the matching frontend provenance', (t) => {
  const f = fixture(t), binary = join(f.root, 'cobot');
  writeFileSync(binary, Buffer.concat([Buffer.from('synthetic executable\0'), readFileSync(f.receipt)]));
  assert.equal(frontendBundle(f.root, [binary]).length, 2);
  writeFileSync(binary, 'old executable');
  assert.throws(() => frontendBundle(f.root, [binary]), /does not embed/);
});

test('Vite records main code, worker-only packages and explicitly copied resources', async (t) => {
  const f = fixture(t);
  f.write('frontend/package.json', JSON.stringify({ type: 'module' }));
  f.write('frontend/index.html', '<script type="module" src="/main.js"></script>');
  f.write('frontend/main.js', 'import "./style.css";import {value} from "main-fixture";console.log(value);new Worker(new URL("./worker.js",import.meta.url),{type:"module"});');
  f.write('frontend/style.css', '@import "style-fixture";body{color:red}');
  f.write('frontend/node_modules/style-fixture/package.json', JSON.stringify({ name: 'style-fixture', version: '1.0.0', style: 'index.css' }));
  f.write('frontend/node_modules/style-fixture/index.css', '.fixture{color:blue}');
  f.write('frontend/node_modules/style-fixture/LICENSE', 'CSS-only fixture notice');
  f.write('frontend/worker.js', 'import {value} from "worker-fixture";self.postMessage(value);');
  f.write('frontend/node_modules/main-fixture/fonts/raw.bin', 'copied fixture resource');
  f.write('frontend/node_modules/main-fixture/fonts/LICENSE', 'Copied resource fixture notice');
  const viteRoot = fileURLToPath(new URL('../frontend/node_modules/vite/', import.meta.url));
  const vite = JSON.parse(readFileSync(join(viteRoot, 'package.json'), 'utf8'));
  f.write('frontend/node_modules/vite/package.json', JSON.stringify(vite));
  f.write('frontend/node_modules/vite/LICENSE.md', readFileSync(join(viteRoot, 'LICENSE.md')));
  const lock = JSON.parse(readFileSync(join(f.root, 'frontend/package-lock.json'), 'utf8'));
  lock.packages['node_modules/vite'] = { version: vite.version, dev: true };
  lock.packages['node_modules/worker-fixture'].dev = true;
  lock.packages['node_modules/style-fixture'] = { version: '1.0.0', dev: true };
  f.write('frontend/package-lock.json', JSON.stringify(lock));
  const licenses = bundleLicenses();
  const { build } = await import('../frontend/node_modules/vite/dist/node/index.js');
  await build({ root: join(f.root, 'frontend'), configFile: false, logLevel: 'silent', publicDir: false,
    plugins: [{ name: 'fixture-copy', generateBundle() { const path = join(f.root, 'frontend/node_modules/main-fixture/fonts/raw.bin'); licenses.recordResource(path); this.emitFile({ type: 'asset', fileName: 'resource.bin', source: readFileSync(path) }); } }, licenses.main],
    worker: { plugins: () => [licenses.worker()] },
    build: { outDir: '../internal/webui/dist', emptyOutDir: true },
  });
  const packages = frontendBundle(f.root);
  assert.deepEqual(packages.map((p) => p.name).sort(), ['main-fixture', 'style-fixture', 'vite', 'worker-fixture']);
  const files = JSON.parse(readFileSync(f.receipt, 'utf8')).files;
  assert.ok(files.some((file) => /worker-.*\.js$/.test(file.path)));
  assert.match(frontendNoticeTexts(f.root).texts.join(''), /Copied resource fixture notice/);
  assert.match(frontendNoticeTexts(f.root).texts.join(''), /Vite core license/);
  assert.match(frontendNoticeTexts(f.root).texts.join(''), /CSS-only fixture notice/);
});

test('a renderer sub-build can run in development without a production recorder', async (t) => {
  const f = fixture(t);
  f.write('frontend/index.html', '<script type="module" src="/main.js"></script>');
  const licenses = bundleLicenses();
  const { build } = await import('../frontend/node_modules/vite/dist/node/index.js');
  const result = await build({ root: join(f.root, 'frontend'), configFile: false, logLevel: 'silent', publicDir: false, plugins: [licenses.worker()], build: { write: false } });
  assert.ok(result.output.some((file) => file.type === 'chunk'));
});
