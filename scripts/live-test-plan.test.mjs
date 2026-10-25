import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, resolve } from 'node:path';
import { changeSummary, classify, coverageProblems, globToRegExp, loadCatalog, plan, touched } from './live-test-plan.mjs';

const config = {
  areas: [
    { id: 'relay', title: 'Relay', cases: 'cases/relay.md', paths: ['internal/app/relay*.go', 'frontend/src/features/tasks/**'] },
    { id: 'avatars', title: 'Avatars', cases: 'cases/avatars.md', paths: ['assets/avatars/**', 'internal/avatars/**'] },
    { id: 'gates', title: 'Gates', cases: 'cases/gates.md', paths: [] },
  ],
  shared: ['internal/domain/**', 'go.mod'],
  exempt: ['docs/**', '**/*_test.go', '**/*.md'],
};
const catalog = {
  config,
  cases: [
    { id: 'A1', tier: '冒烟', area: 'gates' },
    { id: 'D1-1', tier: '回归', area: 'relay' },
    { id: 'D1-2', tier: '回归', area: 'relay' },
    { id: 'AV-1', tier: '回归', area: 'avatars' },
    { id: 'AV-2', tier: '回归', area: 'avatars' },
  ],
};

test('globs: * stays in a directory, ** crosses them, a leading **/ may be empty', () => {
  assert.ok(globToRegExp('internal/app/relay*.go').test('internal/app/relay_context.go'));
  assert.ok(!globToRegExp('internal/app/*.go').test('internal/app/sub/x.go'));
  assert.ok(globToRegExp('frontend/src/features/tasks/**').test('frontend/src/features/tasks/a/b.tsx'));
  assert.ok(globToRegExp('**/*_test.go').test('x_test.go'));
  assert.ok(globToRegExp('**/*_test.go').test('internal/app/x_test.go'));
  assert.ok(!globToRegExp('a.b').test('aXb'));
});

test('files go to exempt first, then every matching area, then shared, else unmapped', () => {
  const result = classify(['internal/app/relay.go', 'internal/app/relay_test.go', 'docs/a.md', 'internal/domain/x.go', 'assets/avatars/a.json', 'weird/new.txt'], config);
  assert.deepEqual([...result.byArea.keys()].sort(), ['avatars', 'relay']);
  assert.deepEqual(result.exempt.sort(), ['docs/a.md', 'internal/app/relay_test.go']);
  assert.deepEqual(result.shared, ['internal/domain/x.go']);
  assert.deepEqual(result.unmapped, ['weird/new.txt']);
});

const entry = (result, commit, date = '2026-10-01') => ({ result, commit, date });

test('plan: smoke and never-tested cases run, changed areas run, the rest carries over', () => {
  const ledger = { cases: { A1: entry('pass', 'c1'), 'D1-1': entry('pass', 'c1'), 'D1-2': entry('pass', 'c1'), 'AV-1': entry('pass', 'c1') } };
  const result = plan(catalog, ledger, { changedSince: () => ['internal/app/relay.go'] });
  const reasons = Object.fromEntries(result.run.map(r => [r.id, r.reason]));
  assert.deepEqual(reasons, { A1: '冒烟', 'D1-1': '变更触发', 'D1-2': '变更触发', 'AV-2': '从未测' });
  assert.deepEqual(result.carry.map(c => c.id), ['AV-1']);
  assert.deepEqual(result.run.find(r => r.id === 'D1-1').files, ['internal/app/relay.go']);
});

test('plan: a failed or skipped case, an unknown commit, and --full all force a run', () => {
  const ledger = { cases: { A1: entry('pass', 'c1'), 'D1-1': entry('fail', 'c1'), 'D1-2': entry('pass', 'gone'), 'AV-1': entry('skip', 'c1'), 'AV-2': entry('partial', 'c1') } };
  const changedSince = commit => (commit === 'gone' ? undefined : []);
  const reasons = Object.fromEntries(plan(catalog, ledger, { changedSince }).run.map(r => [r.id, r.reason]));
  assert.deepEqual(reasons, { A1: '冒烟', 'D1-1': '上次失败', 'D1-2': '上次提交已不在历史里', 'AV-1': '上次未通过', 'AV-2': '上次部分覆盖' });
  assert.equal(plan(catalog, ledger, { changedSince, full: true }).run.length, catalog.cases.length);
});

test('plan: changed files nobody mapped are reported, shared code is listed', () => {
  const ledger = { cases: Object.fromEntries(catalog.cases.map(c => [c.id, entry('pass', 'c1')])) };
  const result = plan(catalog, ledger, { changedSince: () => ['brand/new.txt', 'go.mod'] });
  assert.deepEqual(result.unmapped, ['brand/new.txt']);
  assert.deepEqual(result.shared, ['go.mod']);
  assert.deepEqual(result.run.map(r => r.id), ['A1']);
});

test('plan: deleted files trigger their area but are never reported as unmapped', () => {
  const ledger = { cases: Object.fromEntries(catalog.cases.map(c => [c.id, entry('pass', 'c1')])) };
  const result = plan(catalog, ledger, { changedSince: () => ({ changed: [], deleted: ['internal/app/relay_old.go', 'internal/app/tasks.go'] }) });
  assert.deepEqual(result.unmapped, []);
  assert.deepEqual(result.run.map(r => r.id).sort(), ['A1', 'D1-1', 'D1-2']);
});
test('touched: lists the areas a change reaches with all their cases, and flags shared and unmapped files', () => {
  const result = touched(catalog, { changed: ['internal/app/relay.go', 'go.mod', 'brand/new.txt'], deleted: ['assets/avatars/old.json'] });
  assert.deepEqual(result.areas.map(a => [a.id, a.cases.map(c => c.id)]), [['relay', ['D1-1', 'D1-2']], ['avatars', ['AV-1', 'AV-2']]]);
  assert.deepEqual(result.shared, ['go.mod']);
  assert.deepEqual(result.unmapped, ['brand/new.txt']);
  assert.deepEqual(result.smoke.map(c => c.id), ['A1']);
  assert.deepEqual(touched(catalog, { changed: ['docs/a.md'] }).areas, []);
});
test('change summary: lists real PRs only, keeps every commit subject, counts dependency bumps apart', () => {
  const summary = changeSummary(
    ['Merge pull request #79 from kunghim/fix/desktop-session', 'Merge pull request #73 from kunghim/main', "Merge branch 'dev' into x", 'Merge pull request #79 from kunghim/fix/desktop-session'],
    ['Add projectless chats', 'Bump vitest from 1 to 2', 'Add a model catalog'],
  );
  assert.deepEqual(summary, { prs: ['#79 fix/desktop-session'], commits: ['Add projectless chats', 'Add a model catalog'], bumps: 1 });
});
function tree(t, files) {
  const root = mkdtempSync(resolve(realpathSync(tmpdir()), 'cobot-live-test-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  for (const [path, text] of Object.entries(files)) {
    mkdirSync(dirname(resolve(root, path)), { recursive: true });
    writeFileSync(resolve(root, path), text);
  }
  return root;
}

const table = rows => `| ID | 层 | 步骤 | 期望 |\n| --- | --- | --- | --- |\n${rows.join('\n')}\n`;
const small = {
  'docs/testing/areas.json': JSON.stringify({ areas: [{ id: 'one', title: 'One', cases: 'cases/one.md', paths: ['src/**'] }], shared: [], exempt: ['docs/**'] }),
  'docs/testing/cases/one.md': table(['| X1 | 冒烟 | a | b |', '| X2-1 | 回归 | a | b |']),
};

test('the catalog reads IDs and tiers from the case tables', t => {
  const loaded = loadCatalog(tree(t, small));
  assert.deepEqual(loaded.cases, [{ id: 'X1', tier: '冒烟', area: 'one' }, { id: 'X2-1', tier: '回归', area: 'one' }]);
});

test('coverage problems: a clean tree passes; unmapped files, duplicate IDs, orphan case files and empty areas are reported', t => {
  assert.deepEqual(coverageProblems(tree(t, small), ['src/a.go', 'docs/x.md']), []);
  const bad = tree(t, {
    ...small,
    'docs/testing/cases/one.md': table(['| X1 | 冒烟 | a | b |', '| X1 | 回归 | a | b |']),
    'docs/testing/cases/orphan.md': table(['| Z1 | 回归 | a | b |']),
  });
  const problems = coverageProblems(bad, ['src/a.go', 'lib/new.go']).join('\n');
  assert.match(problems, /duplicate case id X1/);
  assert.match(problems, /cases\/orphan\.md is not used by any area/);
  assert.match(problems, /1 tracked file\(s\) belong to no test area, e\.g\. lib\/new\.go/);
  const loose = tree(t, { ...small, 'docs/testing/cases/one.md': table(['| X1 | 冒烟 | a | b |']) + '\n一段说明。\n\n| X2 | 回归 | a | b |\n' });
  assert.match(coverageProblems(loose, ['src/a.go']).join('\n'), /one\.md:\d+: case row is outside its table/);
  const empty = tree(t, { ...small, 'docs/testing/cases/one.md': '# nothing here\n' });
  assert.match(coverageProblems(empty, ['src/a.go']).join('\n'), /area one has no cases|there is no smoke case/);
  assert.deepEqual(coverageProblems(tree(t, {}), ['x']), []);
});

test('plan: a waived case is carried over, not run, until its area changes', () => {
  const ledger = { cases: { A1: entry('pass', 'c1'), 'D1-1': entry('waived', 'c1'), 'D1-2': entry('pass', 'c1'), 'AV-1': entry('pass', 'c1'), 'AV-2': entry('pass', 'c1') } };
  const quiet = plan(catalog, ledger, { changedSince: () => [] });
  assert.deepEqual(quiet.run.map(r => r.id), ['A1']);
  assert.ok(quiet.carry.some(c => c.id === 'D1-1'));
  const touchedArea = plan(catalog, ledger, { changedSince: () => ['internal/app/relay.go'] });
  assert.ok(touchedArea.run.some(r => r.id === 'D1-1' && r.reason === '变更触发'));
});
