import { createHash } from 'node:crypto';
import { lstatSync, readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { dependencyLicenseText, licenseName, reviewedCanvasLicense, reviewedResvgLicense } from './dependency-policy.mjs';

const digest = (data) => createHash('sha256').update(data).digest('hex');
function safePath(value) {
  if (typeof value !== 'string' || !value || value.includes('\\') || value.includes('\0') || value.split('/').some((part) => !part || part === '.' || part === '..' || part.includes(':'))) throw new Error('Invalid frontend provenance path');
  return value;
}
function verifyFiles(root, files) {
  if (!Array.isArray(files) || !files.length) throw new Error('Missing frontend provenance inputs');
  const seen = new Set();
  for (const file of files) {
    const name = safePath(file.path);
    if (seen.has(name) || !/^[a-f0-9]{64}$/.test(file.sha256)) throw new Error('Invalid frontend provenance record: ' + name);
    seen.add(name);
    if (!lstatSync(join(root, name)).isFile() || digest(readFileSync(join(root, name))) !== file.sha256) throw new Error('Frontend build changed; rebuild before collecting notices: ' + name);
  }
  return seen;
}

export function frontendBundle(root, binaries = []) {
  const dist = join(root, 'internal/webui/dist');
  const bytes = readFileSync(join(dist, 'third-party-inputs.json'));
  const manifest = JSON.parse(bytes);
  if (manifest.version !== 1 || manifest.lockSha256 !== digest(readFileSync(join(root, 'frontend/package-lock.json')))) throw new Error('Frontend provenance is stale; rebuild before collecting notices');
  verifyFiles(join(root, 'frontend'), manifest.inputs);
  const files = verifyFiles(dist, manifest.files);
  const walk = (directory, prefix = '') => {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const name = prefix + entry.name;
      if (entry.isDirectory()) walk(join(directory, entry.name), name + '/');
      else if (name !== 'third-party-inputs.json' && (!entry.isFile() || !files.delete(name))) throw new Error('Unrecorded frontend output: ' + name);
    }
  };
  walk(dist);
  if (files.size) throw new Error('Missing frontend output');
  const lock = JSON.parse(readFileSync(join(root, 'frontend/package-lock.json'), 'utf8'));
  if (!Array.isArray(manifest.packages) || !manifest.packages.length) throw new Error('Missing frontend package provenance');
  const seen = new Set();
  const packages = manifest.packages.map((owner) => {
    if (typeof owner.name !== 'string' || !owner.name || typeof owner.version !== 'string' || !owner.version) throw new Error('Missing frontend package identity');
    const path = safePath(owner.path);
    if (!path.startsWith('node_modules/') || seen.has(path)) throw new Error('Invalid frontend package: ' + path);
    seen.add(path);
    const directory = join(root, 'frontend', path);
    const installed = JSON.parse(readFileSync(join(directory, 'package.json'), 'utf8'));
    if (owner.name !== installed.name || owner.version !== installed.version || lock.packages[path]?.version !== owner.version) throw new Error('Frontend package identity changed: ' + path);
    verifyFiles(directory, owner.files);
    if (owner.files.some((file) => typeof file.resource !== 'boolean')) throw new Error('Invalid frontend resource provenance: ' + path);
    // Copied font/CMap/WASM resources can have notices below the package root.
    const resources = new Set();
    for (const file of owner.files.filter((file) => file.resource)) {
      for (let parent = dirname(file.path); parent !== '.'; parent = dirname(parent)) resources.add(parent);
    }
    return { ...owner, directory, resources: [...resources].sort() };
  });
  for (const binary of binaries) {
    if (!readFileSync(binary).includes(bytes)) throw new Error('Binary does not embed this frontend build: ' + binary);
  }
  return packages;
}

export function frontendNoticeTexts(root, binaries = []) {
  const texts = [], missing = [];
  for (const owner of frontendBundle(root, binaries)) {
    const manifest = JSON.parse(readFileSync(join(owner.directory, 'package.json'), 'utf8'));
    let fallback;
    if (manifest.name.startsWith('@napi-rs/canvas-')) {
      const parent = JSON.parse(readFileSync(join(root, 'frontend/node_modules/@napi-rs/canvas/package.json'), 'utf8'));
      if (reviewedCanvasLicense(manifest, parent)) fallback = join(root, 'packaging/licenses/napi-rs-canvas-1.0.13');
    }
    if (manifest.name.startsWith('@resvg/resvg-js-')) {
      const parent = JSON.parse(readFileSync(join(root, 'frontend/node_modules/@resvg/resvg-js/package.json'), 'utf8'));
      if (reviewedResvgLicense(manifest, parent)) fallback = join(root, 'frontend/node_modules/@resvg/resvg-js');
    }
    const name = manifest.name + ' ' + manifest.version;
    const text = dependencyLicenseText(name, owner.directory, fallback);
    if (text) texts.push(text); else missing.push(name);
    for (const resource of owner.resources) {
      const directory = join(owner.directory, resource);
      if (readdirSync(directory, { withFileTypes: true }).some((file) => file.isFile() && licenseName.test(file.name))) texts.push(dependencyLicenseText(name + ' ' + resource, directory));
    }
  }
  return { texts, missing };
}
