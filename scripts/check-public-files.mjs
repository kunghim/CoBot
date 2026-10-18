// Check the Git index, including force-added paths. Never inspect local data.
import { execFileSync, spawnSync } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const begin = '# BEGIN managed public file exceptions';
const end = '# END managed public file exceptions';
const aiInstruction = /(^|\/)(agents?|claude|gemini)\.md$/i;
const privateDirectory = /(^|\/)(\.claude|\.codex|\.agents|\.cursor|\.zcode|\.deepseek|\.kimi|credentials|traces)(\/|$)/i;

export function validatePolicy(policy) {
  if (!Array.isArray(policy.rootDotEntries) || !Array.isArray(policy.publicFiles)) throw new Error('Invalid public-files policy');
  for (const entry of policy.rootDotEntries) {
    if (typeof entry !== 'string' || !/^\.[a-zA-Z0-9_-][a-zA-Z0-9._-]*$/.test(entry) || entry.toLowerCase() === '.git') throw new Error('Invalid root dot entry');
  }
  for (const path of policy.publicFiles) {
    if (typeof path !== 'string' || !/^[\p{L}\p{N}_./ -]+$/u.test(path) || path.split('/').some(p => !p || p !== p.trim() || p === '.' || p === '..') || privateDirectory.test(path)) throw new Error('Invalid public file exception');
    if (!path.startsWith('docs/') && !aiInstruction.test(path)) throw new Error('Public exceptions must be documents or official AI instructions');
  }
}

export function managedIgnore(policy) {
  validatePolicy(policy);
  const lines = [begin, '# Root hidden entries are private unless explicitly listed in the policy.', '/.*'];
  for (const entry of policy.rootDotEntries) lines.push(`!/${entry}${entry === '.github' ? '/' : ''}`);
  const exceptions = new Set();
  for (const path of policy.publicFiles) {
    const parts = path.split('/');
    for (let i = 1; i < parts.length; i++) exceptions.add(`!/${parts.slice(0, i).join('/')}/`);
    exceptions.add(`!/${path}`);
  }
  lines.push(...exceptions, end);
  return lines.join('\n') + '\n';
}

export function privateReason(path, policy) {
  const first = path.split('/')[0];
  if (first.startsWith('.') && !policy.rootDotEntries.includes(first)) return 'root hidden entry is not allowed';
  if (privateDirectory.test(path)) return 'private data or assistant directory';
  if (/^experiments\//i.test(path)) return 'retired experiments';
  if ((/^docs\//i.test(path) || aiInstruction.test(path)) && !policy.publicFiles.includes(path)) return 'document or AI instruction has not been approved for publication';
  return undefined;
}

export function checkPublicFiles(root, { writeIgnore = false } = {}) {
  const policy = JSON.parse(readFileSync(resolve(root, 'scripts/public-files-policy.json'), 'utf8'));
  const expected = managedIgnore(policy);
  const ignorePath = resolve(root, '.gitignore');
  const ignore = readFileSync(ignorePath, 'utf8').replace(/\r\n/g, '\n');
  const start = ignore.indexOf(begin);
  const finish = ignore.indexOf(end);
  if (start < 0 || finish < start || ignore.indexOf(begin, start + begin.length) >= 0 || ignore.slice(finish + end.length).trim()) throw new Error('Managed public-file block must occur once at the end of .gitignore');
  if (ignore.slice(start) !== expected) {
    if (!writeIgnore) throw new Error('Public exceptions differ from policy; run node scripts/check-public-files.mjs --write-ignore');
    writeFileSync(ignorePath, ignore.slice(0, start) + expected);
  }
  const tracked = execFileSync('git', ['ls-files', '-z'], { cwd: root, encoding: 'utf8' }).split('\0').filter(Boolean);
  const rejected = new Map();
  for (const path of tracked) {
    const reason = privateReason(path, policy);
    if (reason) rejected.set(path, reason);
  }
  const ignored = spawnSync('git', ['check-ignore', '--no-index', '-z', '--stdin'], { cwd: root, input: tracked.length ? tracked.join('\0') + '\0' : '', encoding: 'utf8' });
  if (![0, 1].includes(ignored.status)) throw new Error(ignored.stderr || 'git check-ignore failed');
  for (const path of ignored.stdout.split('\0').filter(Boolean)) rejected.set(path, rejected.get(path) ?? 'tracked file is excluded by .gitignore');
  if (rejected.size) throw new Error([...rejected].map(([path, reason]) => `${JSON.stringify(path)}: ${reason}`).join('\n'));
  return tracked.length;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const count = checkPublicFiles(process.cwd(), { writeIgnore: process.argv.includes('--write-ignore') });
    console.log(`Public file policy passed (${count} tracked files).`);
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
