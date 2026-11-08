// Holds the frontend to its import rules. The violations found must equal the
// committed list: a new one fails, and so does a fixed one still listed, so the
// list only shrinks. `--update` rewrites the list.
import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const listPath = join(root, 'import-violations.json');
const bin = join(root, 'node_modules', 'dependency-cruiser', 'bin', 'dependency-cruiser.mjs');
const output = execFileSync(process.execPath, [bin, 'src', '--config', 'dependency-cruiser.config.cjs', '--output-type', 'baseline'], { cwd: root, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 });

const key = ({ type, from, to, rule }) => `${rule.name}: ${from} -> ${to}${type === 'cycle' ? ' (cycle)' : ''}`;
const found = JSON.parse(output);
if (process.argv.includes('--update')) {
  writeFileSync(listPath, JSON.stringify(found, null, 2) + '\n');
  console.log(`Wrote ${found.length} known violations.`);
  process.exit(0);
}
const current = new Set(found.map(key));
const known = new Set(JSON.parse(readFileSync(listPath, 'utf8')).map(key));
const added = [...current].filter((item) => !known.has(item));
const fixed = [...known].filter((item) => !current.has(item));
for (const item of added) console.error(`new import violation: ${item}`);
for (const item of fixed) console.error(`no longer happens, run with --update: ${item}`);
if (added.length || fixed.length) {
  console.error('See docs/rules/architecture.md.');
  process.exit(1);
}
console.log(`Import rules passed (${known.size} known violations).`);
