// Checks what docs/workflows/docs.md promises and agents rely on: public
// document names, working relative links and anchors, a complete docs index,
// and identical message keys in both UI languages.
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const pluralSuffix = /_(zero|one|two|few|many|other)$/;

function markdownFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name);
    return entry.isDirectory() ? markdownFiles(path) : entry.name.endsWith('.md') ? [path] : [];
  });
}

// GitHub's heading anchors: lower case, punctuation dropped, spaces to hyphens.
export function anchor(heading) {
  return heading.trim().toLowerCase().replace(/[^\p{L}\p{N}\s_-]/gu, '').replace(/\s/g, '-');
}

export function linkProblems(file, text, root) {
  const problems = [];
  const body = text.replace(/```[\s\S]*?```/g, '').replace(/`[^`\n]*`/g, '');
  for (const [, target] of body.matchAll(/\]\(([^)\s]+)\)/g)) {
    if (/^[a-z][a-z0-9+.-]*:/i.test(target)) continue;
    const [path, hash] = target.split('#');
    const resolved = path ? resolve(dirname(file), decodeURI(path)) : file;
    const name = relative(root, file).split(sep).join('/');
    if (!existsSync(resolved)) { problems.push(`${name}: missing link target ${target}`); continue; }
    if (hash && statSync(resolved).isFile() && resolved.endsWith('.md')) {
      const headings = [...readFileSync(resolved, 'utf8').matchAll(/^#{1,6}\s+(.+)$/gm)].map(m => anchor(m[1]));
      if (!headings.includes(decodeURI(hash))) problems.push(`${name}: missing anchor ${target}`);
    }
  }
  return problems;
}

export function localeProblems(zh, en) {
  const flat = (value, prefix = '') => Object.entries(value).flatMap(([key, child]) =>
    child && typeof child === 'object' ? flat(child, `${prefix}${key}.`) : [`${prefix}${key}`]);
  const zhKeys = new Set(flat(zh));
  const enKeys = new Set(flat(en));
  const problems = [];
  for (const key of zhKeys) if (!enKeys.has(key) && !enKeys.has(`${key}_one`) && !enKeys.has(`${key}_other`)) problems.push(`en.json is missing ${key}`);
  for (const key of enKeys) if (!zhKeys.has(key) && !(pluralSuffix.test(key) && zhKeys.has(key.replace(pluralSuffix, '')))) problems.push(`zh-CN.json is missing ${key}`);
  return problems;
}

export function checkDocs(root) {
  const docs = join(root, 'docs');
  const files = markdownFiles(docs);
  const problems = [];
  for (const file of files) {
    const name = relative(docs, file).split(sep).join('/');
    if (!/^[a-z0-9]+(?:-[a-z0-9]+)*(?:\/[a-z0-9]+(?:-[a-z0-9]+)*)*\.md$/.test(name) && name !== 'README.md') problems.push(`docs/${name}: use lower-case English words joined by hyphens`);
  }
  const index = readFileSync(join(docs, 'README.md'), 'utf8');
  for (const file of files) {
    const name = relative(docs, file).split(sep).join('/');
    if (name !== 'README.md' && !index.includes(`](${name})`)) problems.push(`docs/README.md does not list docs/${name}`);
  }
  for (const file of [...files, join(root, 'AGENTS.md'), join(root, 'CONTRIBUTING.md')].filter(existsSync)) problems.push(...linkProblems(file, readFileSync(file, 'utf8'), root));
  const locales = join(root, 'frontend/src/i18n/locales');
  problems.push(...localeProblems(JSON.parse(readFileSync(join(locales, 'zh-CN.json'), 'utf8')), JSON.parse(readFileSync(join(locales, 'en.json'), 'utf8'))));
  return problems;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const problems = checkDocs(resolve(fileURLToPath(new URL('..', import.meta.url))));
  if (problems.length) {
    console.error(problems.join('\n'));
    process.exitCode = 1;
  } else console.log('Docs and locale keys passed.');
}
