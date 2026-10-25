// Release metadata and the final upload allowlist. Never read runtime profiles.
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { appendFileSync, lstatSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const productVersion = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/;
export function validateInputs(version, msiVersion) {
  if (typeof version !== 'string' || version.trim() !== version || !productVersion.test(version)) throw new Error('Use a version such as 0.1.0-beta.2, without a v prefix or build metadata.');
  if (typeof msiVersion !== 'string' || msiVersion.trim() !== msiVersion || !/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(msiVersion)) throw new Error('MSI version must have three numeric parts.');
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
  return execFileSync('gh', args, { encoding: 'utf8', timeout: 60_000, maxBuffer: 10 * 1024 * 1024 }).trim();
}

function releases(repository, execute) {
  return JSON.parse(execute('api', '--paginate', '--slurp', 'repos/' + repository + '/releases?per_page=100')).flat();
}

export function validateSource(ref, sha) {
  if (ref !== 'refs/heads/main') throw new Error('Releases must be built from main.');
  if (!/^[0-9a-f]{40}$/.test(sha ?? '')) throw new Error('Missing immutable source commit.');
}

export function validateUnusedVersion(tag, all, refs) {
  if (all.some((release) => release.tag_name === tag)) throw new Error('A release or draft already exists for ' + tag + '; do not overwrite it.');
  if (refs.some((ref) => ref.ref === 'refs/tags/' + tag)) throw new Error('Tag already exists: ' + tag);
}

export function nextProductVersion(version) {
  const increase = (number) => {
    const value = Number(number);
    if (!Number.isSafeInteger(value) || value >= Number.MAX_SAFE_INTEGER) throw new Error('Version counter is exhausted.');
    return String(value + 1);
  };
  if (!productVersion.test(version)) throw new Error('Invalid previous product version.');
  const separator = version.indexOf('-');
  const core = separator === -1 ? version : version.slice(0, separator);
  const prerelease = separator === -1 ? undefined : version.slice(separator + 1);
  if (prerelease !== undefined) {
    const parts = prerelease.split('.');
    if (/^\d+$/.test(parts.at(-1))) parts[parts.length - 1] = increase(parts.at(-1));
    else parts.push('1');
    return core + '-' + parts.join('.');
  }
  const parts = core.split('.');
  parts[2] = increase(parts[2]);
  return parts.join('.');
}

function msiOrdinal(version) {
  validateInputs('0.0.0', version);
  const [major, minor, build] = version.split('.').map(Number);
  return (major * 256 + minor) * 65536 + build;
}

export function nextMsiVersion(version) {
  const next = msiOrdinal(version) + 1;
  if (next > 0xffffffff) throw new Error('MSI version counter is exhausted.');
  return [Math.floor(next / 16777216), Math.floor(next / 65536) % 256, next % 65536].join('.');
}

function previousRelease(all) {
  return all.filter((release) => !release.draft && release.assets.some((asset) => /^CoBot-.+-windows-x64\.msi$/.test(asset.name)))
    .sort((a, b) => b.published_at.localeCompare(a.published_at))[0];
}

function releaseManifest(release, repository, execute) {
  const asset = release.assets.find((asset) => asset.name === 'release-manifest.json');
  if (!asset || !Number.isSafeInteger(asset.id)) throw new Error('Previous release needs release-manifest.json before automatic versioning can be used.');
  const manifest = JSON.parse(execute('api', 'repos/' + repository + '/releases/assets/' + asset.id, '-H', 'Accept: application/octet-stream'));
  validateInputs(manifest.version, manifest.msiVersion);
  if (manifest.schemaVersion !== 1 || manifest.tag !== release.tag_name || manifest.tag !== 'v' + manifest.version || manifest.repository !== repository || !/^[a-f0-9]{40}$/.test(manifest.sourceCommit ?? '')) {
    throw new Error('Previous release manifest does not match its repository and tag.');
  }
  return manifest;
}

export function chooseVersions({ previous, all, refs, version, msiVersion }) {
  if (!version) {
    if (!previous) throw new Error('The first release needs an explicit product and MSI version.');
    if (all.some((release) => release.draft && productVersion.test(release.tag_name.replace(/^v/, '')))) {
      throw new Error('An unpublished version draft exists; inspect it before starting another automatic release.');
    }
    const occupied = new Set([...all.map((release) => release.tag_name), ...refs.map((ref) => ref.ref.replace('refs/tags/', ''))]);
    version = nextProductVersion(previous.version);
    while (occupied.has('v' + version)) version = nextProductVersion(version);
  }
  if (!msiVersion) {
    if (!previous) throw new Error('The first release needs an explicit MSI version.');
    msiVersion = nextMsiVersion(previous.msiVersion);
  }
  const chosen = validateInputs(version, msiVersion);
  if (previous && msiOrdinal(chosen.msiVersion) <= msiOrdinal(previous.msiVersion)) throw new Error('MSI version must increase beyond the previous published installer.');
  validateUnusedVersion(chosen.tag, all, refs);
  return chosen;
}

export function prepareRelease(environment = process.env, execute = gh) {
  const { GITHUB_REF: ref, GITHUB_SHA: sha, GITHUB_REPOSITORY: repository } = environment;
  validateSource(ref, sha);
  const main = JSON.parse(execute('api', 'repos/' + repository + '/git/ref/heads/main')).object.sha;
  if (main !== sha) return { should_release: false, reason: 'main advanced; the newer main run owns the release' };
  const all = releases(repository, execute);
  if (!environment.RELEASE_VERSION && all.some((release) => !release.draft && release.target_commitish === sha)) {
    return { should_release: false, reason: 'this source commit is already published' };
  }
  const previous = previousRelease(all);
  const previousTag = previous?.tag_name ?? '';
  const legacyManual = previous && !previous.assets.some((asset) => asset.name === 'release-manifest.json') && environment.RELEASE_VERSION && environment.MSI_VERSION;
  const manifest = previous && !legacyManual ? releaseManifest(previous, repository, execute) : null;
  if (!environment.RELEASE_VERSION && manifest?.sourceCommit === sha) {
    return { should_release: false, reason: 'this source commit is already published' };
  }
  const refs = JSON.parse(execute('api', '--paginate', '--slurp', 'repos/' + repository + '/git/matching-refs/tags/v')).flat();
  const chosen = chooseVersions({ previous: manifest, all, refs, version: environment.RELEASE_VERSION, msiVersion: environment.MSI_VERSION });
  return { should_release: true, version: chosen.version, msi_version: chosen.msiVersion, tag: chosen.tag, previous_tag: previousTag };
}

function prepare() {
  const outputs = prepareRelease();
  for (const [key, value] of Object.entries(outputs)) appendFileSync(process.env.GITHUB_OUTPUT, key + '=' + value + '\n');
  console.log(outputs.should_release ? 'Preparing ' + outputs.tag + ' from ' + process.env.GITHUB_SHA : 'Skipping release: ' + outputs.reason);
}

export function releaseNotes({ version, msiVersion, sourceCommit, repository, runUrl, changes = '', draftOnly = false }) {
  return [
    '# CoBot v' + version, '',
    'Source commit: https://github.com/' + repository + '/commit/' + sourceCommit,
    'Build and automated checks: ' + runUrl,
    'Windows Installer version: ' + msiVersion, '',
    ...(changes ? ['## Changes', '', changes, ''] : []),
    '## Downloads and requirements', '',
    '- Windows x64: choose the MSI installer or extract the complete ZIP; keep both helpers beside cobot-desktop.exe. Windows 10/11 and an existing WebView2 Runtime are required.',
    '- macOS Apple Silicon: open the ARM64 DMG, copy CoBot.app to Applications, eject the image, then open the installed app. Intel Macs are not included.',
    '- Install the official Codex CLI separately: Windows minimum 0.161.0, macOS minimum 0.146.0. CoBot uses a private Codex environment and requires its own sign-in.',
    '- Packages are unsigned; macOS uses an ad-hoc bundle signature without Developer ID signing or notarization. If you trust the package, follow Apple’s per-app first-launch instructions: https://support.apple.com/102445. Do not disable Gatekeeper globally.',
    '- Compare each download with its adjacent .sha256 file. GitHub Source code archives are not runnable packages.',
    '- Updates are manual. Close CoBot before upgrading; preserve the complete data root. No automatic updater is included.', '',
    '## Acceptance status', '',
    'Automated CI, package structure, architecture, version, signature integrity (macOS) and checksum checks passed for this build. Real Codex account/approval tests and interactive installation are separate acceptance work; this workflow does not perform them.',
    draftOnly
      ? 'This manual run created a draft only. Interactive installation and real-account behavior have not been accepted by this workflow.'
      : 'Publication is automated after these checks pass. Interactive installation and real-account behavior have not been accepted by this workflow.', '',
    '## 中文安装说明', '',
    '- Windows x64：任选 MSI 安装包或完整 ZIP；ZIP 解压后保留主程序旁的两个助手。系统需已有 WebView2 Runtime。',
    '- macOS Apple Silicon：打开 ARM64 DMG，将 CoBot.app 复制到「应用程序」，弹出映像后启动。暂不提供 Intel 包。',
    '- 另行安装官方 Codex CLI（Windows 最低 0.161.0，macOS 最低 0.146.0），在 CoBot 的私有环境中独立登录。',
    '- 程序未进行发行签名；macOS 也未公证。信任软件来源后，可参照 [Apple 的单个应用首次打开步骤](https://support.apple.com/zh-cn/102445)；不要全局关闭 Gatekeeper。',
    draftOnly
      ? '- 本次手动运行仅创建草稿；流水线不执行真实账号、Agent 审批或交互安装验收，不能据此认为这些场景已通过。'
      : '- 本版本在自动检查通过后发布；流水线不执行真实账号、Agent 审批或交互安装验收，不能据此认为这些场景已通过。',
    '- 更新前关闭程序并保留完整数据目录；目前没有自动更新。', '',
  ].join('\n');
}

export function verifyUploadedRelease(remote, metadata, files, { draft = true } = {}) {
  if (remote.draft !== draft || remote.tag_name !== metadata.tag || remote.target_commitish !== metadata.sourceCommit) throw new Error('Uploaded release does not match the selected source and state.');
  if (remote.prerelease !== metadata.version.includes('-')) throw new Error('GitHub did not confirm the expected release channel.');
  const expected = files.map((file) => file.name).sort();
  if (JSON.stringify(remote.assets.map((asset) => asset.name).sort()) !== JSON.stringify(expected)) throw new Error('Uploaded release assets are incomplete or unexpected.');
  for (const file of files) {
    const asset = remote.assets.find((asset) => asset.name === file.name);
    if (asset.state !== 'uploaded' || asset.size !== file.size || asset.digest !== 'sha256:' + file.sha256) throw new Error('Uploaded asset does not match its local checksum: ' + file.name);
  }
}

export function createRelease(directory, { environment = process.env, execute = gh, publish = false } = {}) {
  const { version, msiVersion, tag } = validateInputs(environment.RELEASE_VERSION, environment.MSI_VERSION);
  validateSource(environment.GITHUB_REF, environment.GITHUB_SHA);
  if (!directory) throw new Error('Supply the verified asset directory.');
  const artifacts = verifyAssets(directory, version);
  const repository = environment.GITHUB_REPOSITORY;
  const all = releases(repository, execute);
  const previous = previousRelease(all);
  if (environment.PREVIOUS_TAG !== undefined && environment.PREVIOUS_TAG !== (previous?.tag_name ?? '')) throw new Error('Published baseline changed during the build; rerun planning before publishing.');
  const previousManifest = previous?.assets.some((asset) => asset.name === 'release-manifest.json') ? releaseManifest(previous, repository, execute) : null;
  if (previousManifest && msiOrdinal(msiVersion) <= msiOrdinal(previousManifest.msiVersion)) throw new Error('MSI version must exceed the published baseline.');
  const refs = JSON.parse(execute('api', 'repos/' + repository + '/git/matching-refs/tags/' + tag));
  validateUnusedVersion(tag, all, refs);
  const noteArgs = ['api', '--method', 'POST', 'repos/' + repository + '/releases/generate-notes',
    '--raw-field', 'tag_name=' + tag, '--raw-field', 'target_commitish=' + environment.GITHUB_SHA];
  if (previous) noteArgs.push('--raw-field', 'previous_tag_name=' + previous.tag_name);
  const changes = JSON.parse(execute(...noteArgs)).body;
  const metadata = {
    schemaVersion: 1, version, msiVersion, tag, sourceCommit: environment.GITHUB_SHA,
    repository,
    runUrl: 'https://github.com/' + repository + '/actions/runs/' + environment.GITHUB_RUN_ID,
    artifacts,
  };
  const manifest = join(directory, 'release-manifest.json');
  writeFileSync(manifest, JSON.stringify(metadata, null, 2) + '\n');
  const notesPath = join(environment.RUNNER_TEMP, 'cobot-release-notes.md');
  writeFileSync(notesPath, releaseNotes({ ...metadata, changes, draftOnly: !publish }));
  const args = ['release', 'create', tag, '--draft', '--target', metadata.sourceCommit,
    '--title', 'CoBot ' + tag, '--notes-file', notesPath];
  if (version.includes('-')) args.push('--prerelease');
  args.push(...expectedAssets(version).map((name) => join(directory, name)), manifest);
  let url = execute(...args);
  const files = [...expectedAssets(version), 'release-manifest.json'].map((name) => {
    const data = readFileSync(join(directory, name));
    return { name, size: data.length, sha256: createHash('sha256').update(data).digest('hex') };
  });
  const remote = JSON.parse(execute('api', 'repos/' + repository + '/releases/tags/' + tag));
  verifyUploadedRelease(remote, metadata, files);
  if (publish) {
    execute('release', 'edit', tag, '--draft=false', '--latest=' + String(!version.includes('-')));
    const published = JSON.parse(execute('api', 'repos/' + repository + '/releases/tags/' + tag));
    verifyUploadedRelease(published, metadata, files, { draft: false });
    url = published.html_url ?? url;
  }
  appendFileSync(environment.GITHUB_STEP_SUMMARY, (publish ? 'Published release: ' : 'Created release draft: ') + url + '\n\nSource: ' + metadata.sourceCommit + '\n');
  console.log(url);
  return metadata;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    if (process.argv[2] === 'prepare') prepare();
    else if (process.argv[2] === 'draft') createRelease(process.argv[3]);
    else if (process.argv[2] === 'publish') createRelease(process.argv[3], { publish: process.env.RELEASE_DRAFT_ONLY !== 'true' });
    else throw new Error('Usage: node scripts/release.mjs prepare|draft|publish');
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
