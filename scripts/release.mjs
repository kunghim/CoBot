// Release metadata and the final upload allowlist. Never read runtime profiles.
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { appendFileSync, lstatSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const productVersion = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/;
export function validateInputs(version, msiVersion) {
  if (!productVersion.test(version ?? '')) throw new Error('Use a version such as 0.1.0-beta.2, without a v prefix or build metadata.');
  if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(msiVersion ?? '')) throw new Error('MSI version must have three numeric parts.');
  const parts = msiVersion.split('.').map(Number);
  if (!parts.every(Number.isSafeInteger) || parts[0] > 255 || parts[1] > 255 || parts[2] > 65535) throw new Error('MSI version exceeds Windows Installer limits.');
  return { version, msiVersion, tag: 'v' + version };
}

export function expectedAssets(version) {
  return ['windows-x64.msi', 'windows-x64.zip', 'macos-arm64.dmg']
    .flatMap((suffix) => {
      const name = 'CoBot-' + version + '-' + suffix;
      return [name, name + '.sha256'];
    }).sort();
}

export function verifyAssets(directory, version) {
  const expected = expectedAssets(version);
  const actual = readdirSync(directory).sort();
  if (JSON.stringify(actual) !== JSON.stringify(expected)) throw new Error('Release assets must contain exactly the Windows MSI/ZIP, ARM64 DMG and their checksums.');
  for (const name of actual) {
    const stat = lstatSync(join(directory, name));
    if (!stat.isFile() || stat.size === 0) throw new Error('Invalid release file: ' + name);
  }
  return expected.filter((name) => !name.endsWith('.sha256')).map((name) => {
    const path = join(directory, name);
    const sha256 = createHash('sha256').update(readFileSync(path)).digest('hex');
    const checksum = readFileSync(path + '.sha256', 'utf8').trim();
    if (checksum !== sha256 + '  ' + name) throw new Error('Checksum mismatch: ' + name);
    return { name, sha256, size: lstatSync(path).size };
  });
}

function gh(...args) {
  return execFileSync('gh', args, { encoding: 'utf8', maxBuffer: 10 * 1024 * 1024 }).trim();
}

function releases() {
  return JSON.parse(gh('api', '--paginate', '--slurp', 'repos/' + process.env.GITHUB_REPOSITORY + '/releases?per_page=100')).flat();
}

export function validateSource(ref, sha) {
  if (ref !== 'refs/heads/main') throw new Error('Release drafts must be built from main.');
  if (!/^[0-9a-f]{40}$/.test(sha ?? '')) throw new Error('Missing immutable source commit.');
}

export function validateUnusedVersion(tag, all, refs) {
  if (all.some((release) => release.tag_name === tag)) throw new Error('A release or draft already exists for ' + tag + '; do not overwrite it.');
  if (refs.some((ref) => ref.ref === 'refs/tags/' + tag)) throw new Error('Tag already exists: ' + tag);
}

function assertUnusedVersion(tag) {
  const all = releases();
  const refs = JSON.parse(gh('api', 'repos/' + process.env.GITHUB_REPOSITORY + '/git/matching-refs/tags/' + tag));
  validateUnusedVersion(tag, all, refs);
}

function prepare() {
  const { version, msiVersion, tag } = validateInputs(process.env.RELEASE_VERSION, process.env.MSI_VERSION);
  validateSource(process.env.GITHUB_REF, process.env.GITHUB_SHA);
  assertUnusedVersion(tag);
  const all = releases();
  const previous = all.filter((release) => !release.draft && release.assets.some((asset) => /^CoBot-.+-windows-x64\.msi$/.test(asset.name)))
    .sort((a, b) => b.published_at.localeCompare(a.published_at))[0];
  const previousTag = previous?.tag_name ?? '';
  if (previousTag && !productVersion.test(previousTag.replace(/^v/, ''))) throw new Error('Previous release has an unsupported tag.');
  const outputs = { version, msi_version: msiVersion, tag, previous_tag: previousTag };
  for (const [key, value] of Object.entries(outputs)) appendFileSync(process.env.GITHUB_OUTPUT, key + '=' + value + '\n');
  console.log('Preparing ' + tag + ' from ' + process.env.GITHUB_SHA + '; previous Windows release: ' + (previousTag || 'none'));
}

export function releaseNotes({ version, msiVersion, sourceCommit, repository, runUrl }) {
  return [
    '# CoBot v' + version, '',
    'Source commit: https://github.com/' + repository + '/commit/' + sourceCommit,
    'Build and automated checks: ' + runUrl,
    'Windows Installer version: ' + msiVersion, '',
    '## Downloads and requirements', '',
    '- Windows x64: choose the MSI installer or extract the complete ZIP; keep both helpers beside cobot-desktop.exe. Windows 10/11 and an existing WebView2 Runtime are required.',
    '- macOS Apple Silicon: open the ARM64 DMG, copy CoBot.app to Applications, eject the image, then open the installed app. Intel Macs are not included.',
    '- Install the official Codex CLI separately: Windows minimum 0.161.0, macOS minimum 0.146.0. CoBot uses a private Codex environment and requires its own sign-in.',
    '- Packages are unsigned; macOS uses an ad-hoc bundle signature without Developer ID signing or notarization. If you trust the package, follow Apple’s per-app first-launch instructions: https://support.apple.com/102445. Do not disable Gatekeeper globally.',
    '- Compare each download with its adjacent .sha256 file. GitHub Source code archives are not runnable packages.',
    '- Updates are manual. Close CoBot before upgrading; preserve the complete data root. No automatic updater is included.', '',
    '## Acceptance status', '',
    'Automated CI, package structure, architecture, version, signature integrity (macOS) and checksum checks passed for this build. Real Codex account/approval tests and interactive installation are separate acceptance work; this workflow does not perform them.',
    'Before publishing this draft, record downloaded-package installation and first-launch results for both platforms, the real-agent checks appropriate to the changes, and any remaining limitations. Do not describe this draft as fully accepted.', '',
    '## 中文安装说明', '',
    '- Windows x64：任选 MSI 安装包或完整 ZIP；ZIP 解压后保留主程序旁的两个助手。系统需已有 WebView2 Runtime。',
    '- macOS Apple Silicon：打开 ARM64 DMG，将 CoBot.app 复制到「应用程序」，弹出映像后启动。暂不提供 Intel 包。',
    '- 另行安装官方 Codex CLI（Windows 最低 0.161.0，macOS 最低 0.146.0），在 CoBot 的私有环境中独立登录。',
    '- 程序未进行发行签名；macOS 也未公证。信任软件来源后，可参照 Apple 的单个应用首次打开步骤：https://support.apple.com/zh-cn/102445；不要全局关闭 Gatekeeper。',
    '- 本流水线不执行真实账号、Agent 审批或交互安装验收。公开发布前，请补充下载包安装、首次打开、必要的真实 Agent 测试结果及未测范围。',
    '- 更新前关闭程序并保留完整数据目录；目前没有自动更新。', '',
  ].join('\n');
}

function draft() {
  const { version, msiVersion, tag } = validateInputs(process.env.RELEASE_VERSION, process.env.MSI_VERSION);
  validateSource(process.env.GITHUB_REF, process.env.GITHUB_SHA);
  const directory = process.argv[3];
  if (!directory) throw new Error('Usage: node scripts/release.mjs draft <asset-directory>');
  const artifacts = verifyAssets(directory, version);
  assertUnusedVersion(tag);
  const metadata = {
    schemaVersion: 1, version, msiVersion, tag, sourceCommit: process.env.GITHUB_SHA,
    repository: process.env.GITHUB_REPOSITORY,
    runUrl: 'https://github.com/' + process.env.GITHUB_REPOSITORY + '/actions/runs/' + process.env.GITHUB_RUN_ID,
    artifacts,
  };
  const manifest = join(directory, 'release-manifest.json');
  writeFileSync(manifest, JSON.stringify(metadata, null, 2) + '\n');
  const notesPath = join(process.env.RUNNER_TEMP, 'cobot-release-notes.md');
  writeFileSync(notesPath, releaseNotes(metadata));
  const args = ['release', 'create', tag, '--draft', '--target', metadata.sourceCommit,
    '--title', 'CoBot ' + tag, '--notes-file', notesPath];
  if (version.includes('-')) args.push('--prerelease');
  args.push(...expectedAssets(version).map((name) => join(directory, name)), manifest);
  const url = gh(...args);
  appendFileSync(process.env.GITHUB_STEP_SUMMARY, 'Created release draft: ' + url + '\n\nSource: ' + metadata.sourceCommit + '\n');
  console.log(url);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    if (process.argv[2] === 'prepare') prepare();
    else if (process.argv[2] === 'draft') draft();
    else throw new Error('Usage: node scripts/release.mjs prepare|draft');
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
