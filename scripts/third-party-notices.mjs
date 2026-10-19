// Collect installed dependency license texts; never read user engine profiles.
// Run after npm ci, on the same platform used to build the desktop package.
import { execFileSync } from 'node:child_process';
import { readFileSync, readdirSync, writeFileSync, existsSync } from 'node:fs';
import { resolve, join } from 'node:path';

const destination = process.argv[2];
if (!destination) throw new Error('Usage: node scripts/third-party-notices.mjs <output.txt>');
const licenseName = /^(licen[cs]e|notice|copying|copyright)([._-].*)?$/i;
const texts = ['CoBot third-party notices\nGenerated from the dependencies installed for this build.\n'];
const missing = [];
function append(name, directory) {
  const files = readdirSync(directory, { withFileTypes: true })
    .filter((file) => file.isFile() && licenseName.test(file.name))
    .map((file) => file.name).sort();
  if (!files.length) { missing.push(name); return; }
  texts.push('\n' + '='.repeat(72) + '\n' + name + '\n');
  for (const file of files) texts.push('\n--- ' + file + ' ---\n' + readFileSync(join(directory, file), 'utf8'));
}
const goroot = execFileSync('go', ['env', 'GOROOT'], { encoding: 'utf8' }).trim();
const goVersion = execFileSync('go', ['env', 'GOVERSION'], { encoding: 'utf8' }).trim();
// Some local toolchain distributions omit the root LICENSE. This unmodified
// upstream copy is from golang/go go1.25.12, the verified build toolchain.
if (!existsSync(join(goroot, 'LICENSE')) && goVersion !== 'go1.25.12') {
  throw new Error('This toolchain omits LICENSE; review the matching Go license before packaging: ' + goVersion);
}
append('Go standard library and runtime ' + goVersion + ' (https://go.dev/LICENSE)',
  existsSync(join(goroot, 'LICENSE')) ? goroot : 'packaging/licenses/go');
if (process.platform === 'win32') {
  append('WiX UI templates and resources 6.0.2 (unmodified upstream source: https://github.com/wixtoolset/wix/tree/v6.0.2/src/ext/UI)',
    'packaging/licenses/wix');
}
const modules = execFileSync('go', ['list', '-deps', '-tags', 'desktop', '-f',
  '{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}',
  './cmd/cobot', './cmd/cobot-desktop', ...(process.platform === 'win32' ? ['./cmd/cobot-helper'] : [])], { encoding: 'utf8' })
  .split(/\r?\n/).filter(Boolean);
for (const line of [...new Set(modules)].sort()) {
  const [name, version, directory] = line.split('|');
  if (name === 'github.com/kunghim/CoBot') continue;
  append(name + ' ' + version, directory);
}
const lock = JSON.parse(readFileSync('frontend/package-lock.json', 'utf8'));
for (const [relative, entry] of Object.entries(lock.packages).sort()) {
  if (!relative || entry.dev) continue;
  const directory = resolve('frontend', relative);
  if (!existsSync(directory)) throw new Error('Missing installed production dependency: ' + relative);
  const manifest = JSON.parse(readFileSync(join(directory, 'package.json'), 'utf8'));
  append(manifest.name + ' ' + manifest.version, directory);
}
if (missing.length) throw new Error('License text needs review for:\n' + missing.join('\n'));
writeFileSync(destination, texts.join('\n'), 'utf8');
console.log('Dependency license texts written to ' + destination);
