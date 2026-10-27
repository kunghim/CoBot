// Collect shipped dependency license texts; never read user engine profiles.
// Run after the frontend build, on the platform used to package the desktop app.
import { execFileSync } from 'node:child_process';
import { writeFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';
import { dependencyLicenseText } from './dependency-policy.mjs';
import { frontendNoticeTexts } from './frontend-bundle.mjs';

const destination = process.argv[2];
if (!destination) throw new Error('Usage: node scripts/third-party-notices.mjs <output.txt> [embedded-binary ...]');
const texts = ['CoBot third-party notices\nGenerated from the Go dependency graph and recorded frontend build inputs.\n'];
const frontend = frontendNoticeTexts(process.cwd(), process.argv.slice(3));
const missing = [...frontend.missing];
function append(name, directory, fallback) {
  const text = dependencyLicenseText(name, directory, fallback);
  if (text) texts.push(text); else missing.push(name);
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
texts.push(...frontend.texts);
if (missing.length) throw new Error('License text needs review for:\n' + missing.join('\n'));
writeFileSync(destination, texts.join('\n'), 'utf8');
console.log('Dependency license texts written to ' + destination);
