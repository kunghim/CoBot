// Exercise the released store's migrations, then start the candidate twice
// against that synthetic profile. No installed application or login is used.
import { execFileSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { copyFileSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
export function validateBaseline(tag) {
  if (typeof tag !== 'string' || tag.trim() !== tag || !/^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(tag)) {
    throw new Error('Supply a published version tag such as v0.1.0-beta.2.');
  }
  return tag;
}

export function checkUpgrade(tag) {
  validateBaseline(tag);
  const git = (...args) => execFileSync('git', args, { cwd: root, encoding: 'utf8' }).trim();
  const baseline = git('rev-parse', '--verify', `refs/tags/${tag}^{commit}`);
  // Never generate a fixture from a release newer than this candidate.
  git('merge-base', '--is-ancestor', baseline, 'HEAD');
  const run = join(root, '.tmp', 'test-runs', 'upgrade-' + randomUUID());
  const source = join(run, 'previous-source');
  const profile = join(run, 'profile');
  mkdirSync(source, { recursive: true });
  const archive = join(run, 'previous.tar');
  execFileSync('git', ['archive', '--format=tar', '--output', archive, baseline], { cwd: root });
  execFileSync('tar', ['-xf', archive, '-C', source]);
  const fixtureDir = join(source, 'cmd', 'cobot-upgrade-fixture');
  mkdirSync(fixtureDir, { recursive: true });
  copyFileSync(join(root, 'tests', 'upgrade', 'seed', 'main.go'), join(fixtureDir, 'main.go'));
  const seed = join(run, process.platform === 'win32' ? 'seed.exe' : 'seed');
  const result = { baselineTag: tag, baselineSha: baseline, candidateSha: git('rev-parse', 'HEAD'), passed: false };
  try {
    execFileSync('go', ['build', '-o', seed, './cmd/cobot-upgrade-fixture'], { cwd: source, stdio: 'inherit' });
    execFileSync(seed, ['--data-root', profile], { stdio: 'inherit' });
    result.baselineSchema = JSON.parse(readFileSync(join(profile, 'upgrade-fixture.json'), 'utf8')).schemaVersion;
    execFileSync('go', ['test', './tests/integration', '-run', '^TestPreviousReleaseProfileUpgrades$', '-count=1', '-v'], {
      cwd: root,
      env: { ...process.env, COBOT_UPGRADE_PROFILE: profile },
      stdio: 'inherit',
    });
    result.passed = true;
  } finally {
    const reportDir = join(root, '.tmp', 'upgrade-report');
    mkdirSync(reportDir, { recursive: true });
    writeFileSync(join(reportDir, 'result.json'), JSON.stringify(result, null, 2) + '\n');
  }
  console.log(`Upgrade verified: ${tag} -> candidate, including a second startup and a persisted settings edit.`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  checkUpgrade(process.argv[2] ?? process.env.UPGRADE_BASE_TAG);
}
