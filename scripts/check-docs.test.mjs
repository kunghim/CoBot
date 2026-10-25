import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, resolve } from 'node:path';
import { anchor, checkDocs, localeProblems } from './check-docs.mjs';

function fixture(t, files) {
  const root = mkdtempSync(resolve(realpathSync(tmpdir()), 'cobot-docs-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const base = {
    'AGENTS.md': '[rules](docs/rules/frontend.md)\n',
    'docs/README.md': '| [Frontend](rules/frontend.md) |\n',
    'docs/rules/frontend.md': '# 前端规则\n\n## 私人资料（`.local/`）\n',
    'frontend/src/i18n/locales/zh-CN.json': '{"a":{"b":"一"},"item":"项"}',
    'frontend/src/i18n/locales/en.json': '{"a":{"b":"one"},"item_one":"item","item_other":"items"}',
  };
  for (const [path, text] of Object.entries({ ...base, ...files })) {
    if (text === null) continue;
    mkdirSync(dirname(resolve(root, path)), { recursive: true });
    writeFileSync(resolve(root, path), text);
  }
  return root;
}

test('a consistent tree passes, including plural-only English keys', t => {
  assert.deepEqual(checkDocs(fixture(t, {})), []);
});

test('a README is allowed in any docs directory', t => {
  assert.deepEqual(checkDocs(fixture(t, { 'docs/README.md': '| [Frontend](rules/frontend.md) | [Index](sub/README.md) |\n', 'docs/sub/README.md': '# 子目录\n' })), []);
});

test('anchors follow GitHub heading rules', () => {
  assert.equal(anchor('私人资料（`.local/`）'), '私人资料local');
  assert.equal(anchor('Native lease hook'), 'native-lease-hook');
});

test('names, index coverage, links and anchors are reported', t => {
  const root = fixture(t, {
    'docs/rules/Frontend_Old.md': '# x\n',
    'docs/rules/frontend.md': '# 前端\n\n[gone](missing.md) [bad](frontend.md#nope) [ok](frontend.md#前端) `[code](ignored.md)` [web](https://example.com)\n',
  });
  const problems = checkDocs(root).join('\n');
  assert.match(problems, /Frontend_Old\.md: use lower-case/);
  assert.match(problems, /does not list docs\/rules\/Frontend_Old\.md/);
  assert.match(problems, /missing link target missing\.md/);
  assert.match(problems, /missing anchor frontend\.md#nope/);
  assert.doesNotMatch(problems, /ignored\.md|example\.com|#前端/);
});

test('locale keys must match in both languages', () => {
  assert.deepEqual(localeProblems({ a: '一', b: '二' }, { a: 'one', c: 'three' }), ['en.json is missing b', 'zh-CN.json is missing c']);
  assert.deepEqual(localeProblems({ n: '个' }, { n_one: 'one', n_other: 'many' }), []);
});
