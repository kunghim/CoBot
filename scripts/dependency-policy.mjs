import { createHash } from 'node:crypto';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

export const licenseName = /^(licen[cs]e|notice|copying|copyright)([._-].*)?$/i;

export function dependencyLicenseText(name, directory, fallback) {
  const files = readdirSync(directory, { withFileTypes: true }).filter((file) => file.isFile() && licenseName.test(file.name)).map((file) => file.name).sort();
  if (!files.length) {
    const manifest = join(directory, 'package.json'), readme = join(directory, 'README.md');
    const embedded = existsSync(manifest) && existsSync(readme) ? embeddedReadmeLicense(JSON.parse(readFileSync(manifest, 'utf8')), readFileSync(readme, 'utf8')) : undefined;
    if (embedded) return '\n' + '='.repeat(72) + '\n' + name + '\n\n--- README.md / License ---\n' + embedded;
    return fallback ? dependencyLicenseText(name, fallback) : undefined;
  }
  return '\n' + '='.repeat(72) + '\n' + name + '\n' + files.map((file) => '\n--- ' + file + ' ---\n' + readFileSync(join(directory, file), 'utf8')).join('\n');
}

export function reviewedCanvasLicense(manifest, parent) {
  return manifest.version === '1.0.13' && manifest.license === 'MIT' &&
    parent.name === '@napi-rs/canvas' && parent.version === manifest.version &&
    parent.optionalDependencies?.[manifest.name] === manifest.version;
}

// This published package carries its complete license only in README.md.
export function embeddedReadmeLicense(manifest, readme) {
  if (manifest.name !== 'isarray' || manifest.version !== '1.0.0' || manifest.license !== 'MIT') return undefined;
  const marker = '\n## License\n';
  const position = readme.indexOf(marker);
  if (position < 0) return undefined;
  const text = readme.slice(position + marker.length).trim();
  if (createHash('sha256').update(text).digest('hex') !== '3a7c107a9e6227f8e67ae8aab097925e6ba81f210193eef54ff38f7b0023fb39') return undefined;
  return text;
}

export function reviewedResvgLicense(manifest, parent) {
  return manifest.name.startsWith('@resvg/resvg-js-') && manifest.version === '2.6.2' && manifest.license === 'MPL-2.0' &&
    parent.name === '@resvg/resvg-js' && parent.version === manifest.version && parent.license === manifest.license &&
    parent.optionalDependencies?.[manifest.name] === manifest.version;
}
