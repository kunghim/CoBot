// Works out which live-test cases a release candidate needs, so a test session
// does not have to re-read the project to find out. Cases and the source paths
// that affect them live in docs/testing; results live in a private ledger.
//
//   node scripts/live-test-plan.mjs [--to <ref>] [--ledger <file>] [--full] [--json]
//   node scripts/live-test-plan.mjs mark --commit <ref> --record <file> ID=pass ID=fail:note ID=partial:what-is-not-covered ...
//   node scripts/live-test-plan.mjs touched [--from <ref>] [--to <ref>]   (during development)
//
// Exit status 2 means some changed files belong to no area: put them in an area
// (or in "shared"/"exempt") in docs/testing/areas.json before trusting the plan.
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const caseRow = /^\|\s*([A-Z]+[0-9]*(?:-[0-9]+)?)\s*\|\s*(冒烟|回归)\s*\|/;

export function globToRegExp(glob) {
  let source = '';
  for (let i = 0; i < glob.length; i++) {
    const c = glob[i];
    if (c === '*') {
      if (glob[i + 1] === '*') {
        if (glob[i + 2] === '/') { source += '(?:.*/)?'; i += 2; } else { source += '.*'; i += 1; }
      } else source += '[^/]*';
    } else if (c === '?') source += '[^/]';
    else source += c.replace(/[.+^${}()|[\]\\]/g, '\\$&');
  }
  return new RegExp(`^${source}$`);
}

const matcher = globs => { const patterns = globs.map(globToRegExp); return file => patterns.some(p => p.test(file)); };

export function loadCatalog(root) {
  const dir = join(root, 'docs/testing');
  const config = JSON.parse(readFileSync(join(dir, 'areas.json'), 'utf8'));
  const cases = [];
  for (const area of config.areas) {
    const path = join(dir, area.cases);
    if (!existsSync(path)) continue;
    for (const line of readFileSync(path, 'utf8').split('\n')) {
      const match = caseRow.exec(line);
      if (match) cases.push({ id: match[1], tier: match[2], area: area.id });
    }
  }
  return { config, cases };
}

// A file goes to exactly one bucket, in this order: exempt, areas (it may
// belong to several), shared, unmapped.
export function classify(files, config) {
  const exempt = matcher(config.exempt ?? []);
  const shared = matcher(config.shared ?? []);
  const areas = config.areas.map(a => ({ id: a.id, test: matcher(a.paths) }));
  const result = { byArea: new Map(), shared: [], exempt: [], unmapped: [] };
  for (const file of files) {
    if (exempt(file)) { result.exempt.push(file); continue; }
    const hits = areas.filter(a => a.test(file));
    if (hits.length) { for (const a of hits) (result.byArea.get(a.id) ?? result.byArea.set(a.id, []).get(a.id)).push(file); continue; }
    if (shared(file)) result.shared.push(file); else result.unmapped.push(file);
  }
  return result;
}

// changedSince(commit) -> the files changed between commit and the candidate,
// either a list or { changed, deleted }. Deleted files still trigger the area
// their path belongs to, but never count as unmapped: nothing is left to map.
// It returns undefined when the commit is unknown (rebased away), which counts
// as never tested.
export function plan(catalog, ledger, { changedSince, full = false }) {
  const entries = ledger?.cases ?? {};
  const cache = new Map();
  const changes = commit => {
    if (!cache.has(commit)) {
      const raw = changedSince(commit);
      if (!raw) cache.set(commit, undefined);
      else {
        const { changed, deleted = [] } = Array.isArray(raw) ? { changed: raw } : raw;
        const result = classify(changed, catalog.config);
        const gone = classify(deleted, catalog.config);
        for (const [id, files] of gone.byArea) result.byArea.set(id, [...(result.byArea.get(id) ?? []), ...files]);
        result.shared.push(...gone.shared);
        result.deletedUnmapped = gone.unmapped;
        cache.set(commit, result);
      }
    }
    return cache.get(commit);
  };
  const run = [];
  const carry = [];
  const unmapped = new Set();
  for (const item of catalog.cases) {
    const last = entries[item.id];
    let reason;
    let detail = [];
    if (full) reason = '全量';
    else if (!last) reason = '从未测';
    else if (last.result !== 'pass' && last.result !== 'waived') reason = `上次${{ fail: '失败', partial: '部分覆盖' }[last.result] ?? '未通过'}`;
    else if (item.tier === '冒烟') reason = '冒烟';
    else {
      const seen = changes(last.commit);
      if (!seen) reason = '上次提交已不在历史里';
      else {
        for (const file of seen.unmapped) unmapped.add(file);
        detail = seen.byArea.get(item.area) ?? [];
        if (detail.length) reason = '变更触发';
      }
    }
    if (reason) run.push({ ...item, reason, files: detail, ...(last?.note && last.result !== 'pass' ? { note: last.note } : {}) });
    else carry.push({ ...item, last });
  }
  // Files nobody mapped are reported even when every case happens to be due.
  for (const entry of Object.values(entries)) {
    const seen = entry.commit ? changes(entry.commit) : undefined;
    for (const file of seen?.unmapped ?? []) unmapped.add(file);
  }
  const sharedHits = new Set();
  for (const entry of Object.values(entries)) for (const file of changes(entry.commit)?.shared ?? []) sharedHits.add(file);
  return { run, carry, unmapped: [...unmapped].sort(), shared: [...sharedHits].sort() };
}

// During development: which areas and cases does a change touch? No ledger.
export function touched(catalog, { changed, deleted = [] }) {
  const seen = classify(changed, catalog.config);
  const gone = classify(deleted, catalog.config);
  const files = new Map(seen.byArea);
  for (const [id, list] of gone.byArea) files.set(id, [...(files.get(id) ?? []), ...list]);
  const areas = [...files].map(([id, list]) => ({ id, files: list, cases: catalog.cases.filter(c => c.area === id) }));
  return { areas, shared: [...seen.shared, ...gone.shared].sort(), unmapped: seen.unmapped.sort(), smoke: catalog.cases.filter(c => c.tier === '冒烟') };
}

export function coverageProblems(root, trackedFiles) {
  const dir = join(root, 'docs/testing');
  if (!existsSync(join(dir, 'areas.json'))) return [];
  const problems = [];
  const config = JSON.parse(readFileSync(join(dir, 'areas.json'), 'utf8'));
  const referenced = new Set(config.areas.map(a => a.cases));
  for (const area of config.areas) if (!existsSync(join(dir, area.cases))) problems.push(`docs/testing/areas.json: ${area.id} points at missing ${area.cases}`);
  for (const area of config.areas) {
    const path = join(dir, area.cases);
    if (!existsSync(path)) continue;
    const lines = readFileSync(path, 'utf8').split('\n');
    lines.forEach((line, index) => {
      if (caseRow.test(line) && !(index > 0 && lines[index - 1].startsWith('|'))) problems.push(`docs/testing/${area.cases}:${index + 1}: case row is outside its table (keep rows directly under the table header)`);
    });
  }
  const casesDir = join(dir, 'cases');
  if (existsSync(casesDir)) for (const name of readdirSync(casesDir)) if (name.endsWith('.md') && !referenced.has(`cases/${name}`)) problems.push(`docs/testing/cases/${name} is not used by any area`);
  const catalog = loadCatalog(root);
  const seen = new Set();
  for (const item of catalog.cases) {
    if (seen.has(item.id)) problems.push(`docs/testing: duplicate case id ${item.id}`);
    seen.add(item.id);
  }
  for (const area of config.areas) if (!catalog.cases.some(c => c.area === area.id)) problems.push(`docs/testing: area ${area.id} has no cases`);
  if (!catalog.cases.some(c => c.tier === '冒烟')) problems.push('docs/testing: there is no smoke case');
  const unmapped = classify(trackedFiles, config).unmapped;
  if (unmapped.length) problems.push(`docs/testing/areas.json: ${unmapped.length} tracked file(s) belong to no test area, e.g. ${unmapped.slice(0, 3).join(', ')}; add them to an area, "shared" or "exempt"`);
  return problems;
}

const git = (root, ...args) => execFileSync('git', args, { cwd: root, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 1 << 28 }).trim();
const tryGit = (root, ...args) => { try { return git(root, ...args); } catch { return undefined; } };

export function defaultLedger(root) {
  const common = git(root, 'rev-parse', '--path-format=absolute', '--git-common-dir');
  return join(dirname(common), '.local/validation/live-test-ledger.json');
}

function readLedger(path) {
  return existsSync(path) ? JSON.parse(readFileSync(path, 'utf8')) : { version: 1, cases: {} };
}

function parseArgs(argv) {
  const options = { positional: [] };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === '--full' || arg === '--json') options[arg.slice(2)] = true;
    else if (arg.startsWith('--')) {
      const value = argv[++i];
      if (value === undefined || value.startsWith('--')) throw new Error(`${arg} needs a value`);
      options[arg.slice(2)] = value;
    } else options.positional.push(arg);
  }
  return options;
}

// What changed between the oldest tested commit and the candidate: real PRs
// (not branch syncs) and every non-merge commit subject, because a PR title
// often hides what it carried in.
export function changeSummary(mergeSubjects, commitSubjects) {
  const prs = [];
  for (const subject of mergeSubjects) {
    const match = /^Merge pull request #(\d+) from [^/\s]+\/(\S+)/.exec(subject);
    if (match && !/^(main|dev)$/.test(match[2]) && !prs.includes(`#${match[1]} ${match[2]}`)) prs.push(`#${match[1]} ${match[2]}`);
  }
  const bumps = commitSubjects.filter(s => /^Bump /.test(s)).length;
  return { prs, commits: commitSubjects.filter(s => !/^Bump /.test(s)), bumps };
}

const reasonOrder = ['冒烟', '变更触发', '全量', '上次失败', '上次部分覆盖', '上次未通过', '上次提交已不在历史里', '从未测'];

function report(result, { to, ledgerPath, ledgerExists, summary }) {
  const lines = [];
  const count = reason => result.run.filter(r => r.reason === reason).length;
  lines.push(`候选提交 ${to.slice(0, 7)}；账本 ${ledgerExists ? ledgerPath : '不存在（所有用例按从未测处理）'}`);
  lines.push(`要跑 ${result.run.length} 条（冒烟 ${count('冒烟')}、变更触发 ${count('变更触发')}、从未测 ${count('从未测')}、上次未通过 ${result.run.filter(r => r.reason.startsWith('上次')).length}${count('全量') ? `、全量 ${count('全量')}` : ''}），沿用 ${result.carry.length} 条`);
  lines.push('', '== 要跑（按功能区）==');
  const byArea = new Map();
  for (const item of result.run) (byArea.get(item.area) ?? byArea.set(item.area, []).get(item.area)).push(item);
  for (const [area, items] of byArea) {
    const parts = [];
    for (const reason of [...new Set([...reasonOrder, ...items.map(i => i.reason)])]) {
      const group = items.filter(i => i.reason === reason);
      if (!group.length) continue;
      const files = reason === '变更触发' ? [...new Set(group.flatMap(i => i.files))] : [];
      parts.push(`${reason}：${group.map(i => (i.note && reason.startsWith('上次') ? `${i.id}（${i.note}）` : i.id)).join('、')}${files.length ? `（${files.slice(0, 2).join(', ')}${files.length > 2 ? ` 等 ${files.length} 个文件` : ''}）` : ''}`);
    }
    lines.push(`[${area}] ${parts.join('｜')}`);
  }
  lines.push('', '== 沿用上次结果 ==');
  const groups = new Map();
  for (const c of result.carry) {
    const key = `${c.last.result} @ ${c.last.commit.slice(0, 7)}，${c.last.date}`;
    (groups.get(key) ?? groups.set(key, []).get(key)).push(c.id);
  }
  if (!groups.size) lines.push('（无）');
  for (const [key, ids] of groups) lines.push(`${key}（${ids.length} 条）：${ids.join('、')}`);
  if (result.shared.length) lines.push('', '== 共享代码有改动（靠冒烟覆盖）==', result.shared.slice(0, 12).join('\n') + (result.shared.length > 12 ? `\n… 共 ${result.shared.length} 个` : ''));
  if (summary && (summary.prs.length || summary.commits.length)) {
    lines.push('', '== 这段时间的变化 ==');
    if (summary.prs.length) lines.push(`PR：${summary.prs.join('；')}`);
    if (summary.commits.length) lines.push(`提交 ${summary.commits.length} 条${summary.bumps ? `（另有 ${summary.bumps} 条依赖升级）` : ''}：`, ...summary.commits.slice(0, 40).map(s => `  ${s}`), ...(summary.commits.length > 40 ? [`  … 共 ${summary.commits.length} 条`] : []));
  }
  if (result.unmapped.length) lines.push('', `== 需要人工归类的文件（${result.unmapped.length}）：不属于任何测试区，先在 docs/testing/areas.json 里归类 ==`, ...result.unmapped);
  return lines.join('\n');
}
function main(argv) {
  const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
  const [command, ...rest] = argv[0] && !argv[0].startsWith('-') ? argv : ['plan', ...argv];
  const options = parseArgs(rest);
  const ledgerPath = options.ledger ?? defaultLedger(root);
  if (command === 'mark') {
    const commit = git(root, 'rev-parse', options.commit ?? 'HEAD');
    const catalog = loadCatalog(root);
    const known = new Set(catalog.cases.map(c => c.id));
    const ledger = readLedger(ledgerPath);
    const date = new Date().toISOString().slice(0, 10);
    for (const pair of options.positional) {
      const match = /^([A-Z]+[0-9]*(?:-[0-9]+)?)=(pass|fail|skip|partial|waived)(?::(.*))?$/.exec(pair);
      if (!match || !known.has(match[1])) throw new Error(`cannot mark "${pair}": use ID=pass|fail|skip|partial|waived[:note] (partial and waived need a note) with an ID from docs/testing`);
      if ((match[2] === 'partial' || match[2] === 'waived') && !match[3]) throw new Error(`cannot mark "${pair}": ${match[2]} needs a note (what is not covered / who decided)`);
      ledger.cases[match[1]] = { result: match[2], commit, date, ...(options.record ? { record: options.record } : {}), ...(match[3] ? { note: match[3] } : {}) };
    }
    mkdirSync(dirname(ledgerPath), { recursive: true });
    writeFileSync(ledgerPath, JSON.stringify(ledger, null, 2) + '\n');
    console.log(`ledger ${ledgerPath}: ${options.positional.length} case(s) recorded at ${commit.slice(0, 7)}`);
    return 0;
  }
  if (command === 'touched') {
    const to = options.to ? git(root, 'rev-parse', options.to) : undefined;
    const base = git(root, 'merge-base', options.from ?? 'origin/dev', to ?? 'HEAD');
    const changed = [];
    const deleted = [];
    for (const line of git(root, 'diff', '--name-status', '--no-renames', base, ...(to ? [to] : [])).split('\n').filter(Boolean)) {
      const [status, ...rest] = line.split('\t');
      (status === 'D' ? deleted : changed).push(rest.join('\t'));
    }
    if (!to) changed.push(...git(root, 'ls-files', '--others', '--exclude-standard').split('\n').filter(Boolean));
    const result = touched(loadCatalog(root), { changed, deleted });
    if (options.json) { console.log(JSON.stringify(result, null, 2)); return result.unmapped.length ? 2 : 0; }
    const lines = [`相对 ${base.slice(0, 7)} 改了 ${changed.length + deleted.length} 个文件`];
    for (const area of result.areas) {
      lines.push('', `[${area.id}] ${area.files.length} 个文件，如 ${area.files.slice(0, 3).join(', ')}`, `  对应用例：${area.cases.map(c => `${c.id}（${c.tier}）`).join('、')}`);
    }
    if (!result.areas.length) lines.push('', '没有改到任何功能区。');
    if (result.shared.length) lines.push('', `共享代码有改动（${result.shared.length} 个），至少跑冒烟用例：${result.smoke.map(c => c.id).join('、')}`);
    if (result.unmapped.length) lines.push('', `未归类的文件（${result.unmapped.length}），先在 docs/testing/areas.json 里归类：`, ...result.unmapped);
    console.log(lines.join('\n'));
    return result.unmapped.length ? 2 : 0;
  }
  if (command !== 'plan') throw new Error('Usage: live-test-plan.mjs [plan] [--to ref] [--ledger file] [--full] [--json] | touched [--from ref] [--to ref] | mark --commit ref --record file ID=pass ...');
  const to = git(root, 'rev-parse', options.to ?? 'origin/dev');
  const catalog = loadCatalog(root);
  const ledger = readLedger(ledgerPath);
  const changedSince = commit => {
    if (!commit || tryGit(root, 'cat-file', '-e', `${commit}^{commit}`) === undefined) return undefined;
    const changed = [];
    const deleted = [];
    for (const line of git(root, 'diff', '--name-status', '--no-renames', commit, to).split('\n').filter(Boolean)) {
      const [status, ...rest] = line.split('\t');
      (status === 'D' ? deleted : changed).push(rest.join('\t'));
    }
    return { changed, deleted };
  };
  const result = plan(catalog, ledger, { changedSince, full: Boolean(options.full) });
  const oldest = Object.values(ledger.cases).map(c => c.commit).filter(c => c && tryGit(root, 'cat-file', '-e', `${c}^{commit}`) !== undefined)
    .sort((a, b) => Number(tryGit(root, 'log', '-1', '--format=%ct', a)) - Number(tryGit(root, 'log', '-1', '--format=%ct', b)))[0];
  const subjects = args => (oldest ? git(root, 'log', ...args, `${oldest}..${to}`).split('\n').filter(Boolean) : []);
  const summary = changeSummary(subjects(['--merges', '--format=%s']), subjects(['--no-merges', '--format=%s']));
  if (options.json) console.log(JSON.stringify({ to, run: result.run, carry: result.carry, unmapped: result.unmapped, shared: result.shared, changes: summary }, null, 2));
  else console.log(report(result, { to, ledgerPath, ledgerExists: existsSync(ledgerPath), summary }));
  return result.unmapped.length ? 2 : 0;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { process.exitCode = main(process.argv.slice(2)); } catch (error) { console.error(error.message); process.exitCode = 1; }
}
