import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { lstatSync, mkdtempSync, mkdirSync, readFileSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { basename, dirname, resolve } from 'node:path';
import { checkPublicFiles, managedIgnore, privateReason } from './check-public-files.mjs';
import { checkPrRoute } from './check-pr-route.mjs';

const baseline = { rootDotEntries: ['.github', '.gitignore'], publicFiles: [] };
const ignore = readFileSync(new URL('../.gitignore', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
function fixture(t, policy = baseline) {
  const temporaryParent = realpathSync(tmpdir());
  const root = mkdtempSync(resolve(temporaryParent, 'cobot-public-policy-'));
  t.after(() => {
    if (dirname(root) !== temporaryParent || !basename(root).startsWith('cobot-public-policy-') || lstatSync(root).isSymbolicLink()) throw new Error('Unexpected fixture cleanup path');
    rmSync(root, { recursive: true, force: true });
  });
  execFileSync('git', ['init', '-q', root]);
  const put = (path, data = 'synthetic policy fixture\n') => {
    mkdirSync(dirname(resolve(root, path)), { recursive: true });
    writeFileSync(resolve(root, path), data);
    execFileSync('git', ['add', '-f', '--', path], { cwd: root });
  };
  put('.gitignore', ignore.slice(0, ignore.indexOf('# BEGIN managed public file exceptions')) + managedIgnore(policy));
  put('scripts/public-files-policy.json', JSON.stringify(policy));
  return { root, put };
}

test('real Git index accepts product files and contributor settings', t => {
  const { root, put } = fixture(t);
  for (const path of ['.github/workflows/ci.yml', 'internal/roles/defaults/leader.json', 'internal/wiretrace/viewer.html', 'api/openapi.yaml', 'frontend/package-lock.json']) put(path);
  assert.equal(checkPublicFiles(root), 7);
});

test('force-added private files are rejected; untracked local documents are untouched', t => {
  const { root, put } = fixture(t);
  mkdirSync(resolve(root, 'docs'));
  writeFileSync(resolve(root, 'docs/local.md'), 'synthetic untracked document');
  assert.equal(checkPublicFiles(root), 2);
  for (const path of ['docs/notes.md', 'AGENTS.md', '.future/config.json', 'frontend/.kimi/settings.json', '.github/copilot-instructions.md', 'tests/traces/run.json', 'runtime.sqlite', 'experiments/run.mjs']) put(path);
  assert.throws(() => checkPublicFiles(root), error => ['docs/notes.md', 'AGENTS.md', '.future/config.json', 'frontend/.kimi/settings.json', '.github/copilot-instructions.md', 'tests/traces/run.json', 'runtime.sqlite', 'experiments/run.mjs'].every(path => error.message.includes(path)));
  assert.equal(readFileSync(resolve(root, 'docs/local.md'), 'utf8'), 'synthetic untracked document');
});

test('approved nested bilingual docs and official instructions do not expose neighbors', t => {
  const policy = { ...baseline, publicFiles: ['docs/versions/v1/README.md', 'docs/介绍.md', 'AGENTS.md', 'CLAUDE.md'] };
  const { root, put } = fixture(t, policy);
  for (const path of policy.publicFiles) put(path);
  assert.equal(checkPublicFiles(root), 6);
  put('docs/versions/v1/private.md');
  put('frontend/AGENTS.md');
  assert.throws(() => checkPublicFiles(root), /has not been approved/);
});

test('ignore exceptions must match the policy and remain the final block', t => {
  const { root } = fixture(t);
  writeFileSync(resolve(root, '.gitignore'), readFileSync(resolve(root, '.gitignore'), 'utf8') + '!/docs/**\n');
  assert.throws(() => checkPublicFiles(root), /at the end/);
});

test('policy rejects traversal and private-directory publication exceptions', () => {
  for (const path of ['docs/../auth.json', 'docs/**', 'docs/.claude/config.json', 'docs/traces/run.json', 'runtime/auth.json']) assert.throws(() => managedIgnore({ ...baseline, publicFiles: [path] }));
  assert.throws(() => managedIgnore({ rootDotEntries: ['.git'], publicFiles: [] }));
  assert.ok(privateReason('.unknown/file', baseline));
});

function pr(base, head, repo = 'kunghim/CoBot', login = 'contributor') {
  return { pull_request: { base: { ref: base, repo: { full_name: 'kunghim/CoBot' } }, head: { ref: head, repo: repo ? { full_name: repo } : null }, user: { login } } };
}
test('topic/fork PRs target dev; only dev promotions and Dependabot updates target main', () => {
  assert.doesNotThrow(() => checkPrRoute(pr('dev', 'feature', 'contributor/CoBot')));
  assert.doesNotThrow(() => checkPrRoute(pr('main', 'dev')));
  assert.doesNotThrow(() => checkPrRoute(pr('main', 'dependabot/go_modules/example', 'kunghim/CoBot', 'dependabot[bot]')));
  for (const event of [pr('main', 'feature'), pr('main', 'dependabot/go_modules/example'), pr('main', 'dependabot/go_modules/example', 'contributor/CoBot', 'dependabot[bot]'), pr('main', 'dev', 'contributor/CoBot'), pr('main', 'dev', null), pr('release', 'dev'), {}]) assert.throws(() => checkPrRoute(event));
});
