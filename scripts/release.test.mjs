import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdtempSync, readFileSync, rmSync, symlinkSync, unlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { reviewedCanvasLicense, embeddedReadmeLicense, reviewedResvgLicense } from './dependency-policy.mjs';
import { chooseVersions, createRelease, expectedAssets, nextMsiVersion, nextProductVersion, prepareRelease, releaseNotes, validateInputs, validateSource, validateUnusedVersion, verifyAssets } from './release.mjs';

const version = '0.1.0-beta.2';
function fixture(t, product = version) {
  const directory = mkdtempSync(join(tmpdir(), 'cobot-release-test-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  for (const name of expectedAssets(product).filter((name) => !name.endsWith('.sha256'))) {
    const content = Buffer.from('unit-test artifact: ' + name);
    writeFileSync(join(directory, name), content);
    const hash = createHash('sha256').update(content).digest('hex');
    writeFileSync(join(directory, name + '.sha256'), hash + '  ' + name + '\r\n');
  }
  return directory;
}

test('accepts a beta or stable version and rejects shell/path input and unsupported MSI values', () => {
  assert.deepEqual(validateInputs(version, '0.1.2'), { version, msiVersion: '0.1.2', tag: 'v' + version });
  assert.equal(validateInputs('1.0.0', '1.0.0').tag, 'v1.0.0');
  for (const value of ['v0.1.0', '0.1.0+meta', '../1.0.0', '1.0.0\nother=bad', '1.0.0\n', '1.0.0; echo bad', '01.0.0']) {
    assert.throws(() => validateInputs(value, '0.1.2'));
  }
  for (const value of ['1.2', '1.2.3.4', '256.0.0', '0.256.0', '0.1.65536', '0.01.2', '0.1.2\n', '-1.0.0']) {
    assert.throws(() => validateInputs(version, value));
  }
});

test('draft source must be an immutable main commit', () => {
  validateSource('refs/heads/main', 'a'.repeat(40));
  for (const ref of ['refs/heads/dev', 'refs/tags/v0.1.0', 'refs/heads/codex/release']) {
    assert.throws(() => validateSource(ref, 'a'.repeat(40)), /main/);
  }
  assert.throws(() => validateSource('refs/heads/main', 'main'), /commit/);
});

test('published releases, drafts and existing tags cannot be overwritten', () => {
  const tag = 'v' + version;
  for (const draft of [false, true]) {
    assert.throws(() => validateUnusedVersion(tag, [{ tag_name: tag, draft }], []), /already exists/);
  }
  assert.throws(() => validateUnusedVersion(tag, [], [{ ref: 'refs/tags/' + tag }]), /Tag already exists/);
  validateUnusedVersion(tag, [], [{ ref: 'refs/tags/' + tag + '0' }]);
});

test('complete supported assets produce checksum provenance', (t) => {
  const artifacts = verifyAssets(fixture(t), version);
  assert.equal(artifacts.length, 3);
  assert.ok(artifacts.every((artifact) => /^[a-f0-9]{64}$/.test(artifact.sha256) && artifact.size > 0));
  assert.ok(artifacts.some((artifact) => artifact.name.endsWith('-macos-arm64.dmg')));
  assert.ok(artifacts.every((artifact) => !artifact.name.includes('amd64')));
});

test('missing platform, extra Intel package or profile files block a release', (t) => {
  const directory = fixture(t);
  const name = expectedAssets(version)[0];
  unlinkSync(join(directory, name));
  assert.throws(() => verifyAssets(directory, version), /exactly/);
  writeFileSync(join(directory, name), 'restored');
  for (const extra of ['CoBot-' + version + '-macos-amd64.dmg', 'auth.json']) {
    writeFileSync(join(directory, extra), 'must not upload');
    assert.throws(() => verifyAssets(directory, version), /exactly/);
    unlinkSync(join(directory, extra));
  }
});

test('tampering or a checksum containing a different filename blocks a release', (t) => {
  const directory = fixture(t);
  const name = 'CoBot-' + version + '-macos-arm64.dmg';
  writeFileSync(join(directory, name), 'tampered');
  assert.throws(() => verifyAssets(directory, version), /Checksum mismatch/);
  const hash = createHash('sha256').update('tampered').digest('hex');
  writeFileSync(join(directory, name + '.sha256'), hash + '  ../../another-file\n');
  assert.throws(() => verifyAssets(directory, version), /Checksum mismatch/);
});

test('asset symlinks cannot expose files outside the staging directory', (t) => {
  const directory = fixture(t);
  const outside = fixture(t);
  const name = expectedAssets(version)[0];
  unlinkSync(join(directory, name));
  try {
    symlinkSync(join(outside, name), join(directory, name), 'file');
  } catch (error) {
    if (process.platform !== 'win32' || error.code !== 'EPERM') throw error;
    t.skip('Windows file symlinks require Developer Mode or the symbolic-link privilege');
    return;
  }
  assert.throws(() => verifyAssets(directory, version), /Invalid release file/);
});

test('asset directory links are rejected, including Windows junctions without elevation', (t) => {
  const directory = fixture(t);
  const outside = fixture(t);
  const name = expectedAssets(version)[0];
  unlinkSync(join(directory, name));
  symlinkSync(outside, join(directory, name), process.platform === 'win32' ? 'junction' : 'dir');
  assert.throws(() => verifyAssets(directory, version), /Invalid release file/);
});

test('notes distinguish automation from real-account and installation acceptance', () => {
  const notes = releaseNotes({ version, msiVersion: '0.1.2', sourceCommit: 'a'.repeat(40),
    repository: 'owner/repo', runUrl: 'https://github.com/owner/repo/actions/runs/1' });
  assert.match(notes, /Intel Macs are not included/);
  assert.match(notes, /Publication is automated/);
  assert.match(notes, /does not perform them/);
  assert.match(notes, /macOS 最低 0.146.0/);
  assert.match(notes, /\]\(https:\/\/support\.apple\.com\/zh-cn\/102445\)；/);
});

const sourceCommit = 'a'.repeat(40);
const previousManifest = {
  schemaVersion: 1, version: '0.1.0-beta.1', msiVersion: '0.1.1', tag: 'v0.1.0-beta.1',
  sourceCommit: 'b'.repeat(40), repository: 'owner/repo',
};
const previousRelease = {
  tag_name: previousManifest.tag, draft: false, published_at: '2026-01-01T00:00:00Z',
  target_commitish: previousManifest.sourceCommit,
  assets: [{ name: 'CoBot-0.1.0-beta.1-windows-x64.msi' }, { name: 'release-manifest.json', id: 1 }],
};

test('automatic versions stay in their prerelease channel or increment the stable patch', () => {
  assert.equal(nextProductVersion('0.1.0-beta.2'), '0.1.0-beta.3');
  assert.equal(nextProductVersion('1.0.0'), '1.0.1');
  assert.equal(nextProductVersion('1.2.0-rc-test.9'), '1.2.0-rc-test.10');
  assert.equal(nextProductVersion('1.2.0-beta'), '1.2.0-beta.1');
  assert.throws(() => nextProductVersion('1.0.9007199254740991'), /exhausted/);
  assert.equal(nextMsiVersion('0.1.2'), '0.1.3');
  assert.equal(nextMsiVersion('0.1.65535'), '0.2.0');
  assert.equal(nextMsiVersion('0.255.65535'), '1.0.0');
  assert.throws(() => nextMsiVersion('255.255.65535'), /exhausted/);
});

test('planning respects reserved tags, incomplete drafts, explicit stable promotion and MSI ordering', () => {
  const input = { previous: previousManifest, all: [previousRelease], refs: [] };
  assert.equal(chooseVersions({ ...input, refs: [{ ref: 'refs/tags/v0.1.0-beta.2' }] }).version, '0.1.0-beta.3');
  assert.throws(() => chooseVersions({ ...input, all: [...input.all, { tag_name: 'v0.1.0-beta.2', draft: true }] }), /unpublished version draft/);
  assert.deepEqual(chooseVersions({ ...input, version: '0.1.0' }), { version: '0.1.0', msiVersion: '0.1.2', tag: 'v0.1.0' });
  assert.throws(() => chooseVersions({ ...input, msiVersion: '0.1.1' }), /must increase/);
  assert.throws(() => chooseVersions({ previous: null, all: [], refs: [] }), /first release/);
  assert.equal(chooseVersions({ previous: null, all: [], refs: [], version: '0.1.0-beta.1', msiVersion: '0.1.1' }).tag, 'v0.1.0-beta.1');
});

function releaseEnvironment(directory) {
  return {
    GITHUB_REF: 'refs/heads/main', GITHUB_SHA: sourceCommit, GITHUB_REPOSITORY: 'owner/repo', GITHUB_RUN_ID: '123',
    RUNNER_TEMP: directory, GITHUB_STEP_SUMMARY: join(directory, 'summary.md'),
    RELEASE_VERSION: version, MSI_VERSION: '0.1.2', PREVIOUS_TAG: previousManifest.tag,
  };
}

// No test invokes gh or creates a real release. The adapter emulates GitHub's
// uploaded file metadata so the complete publish ordering can be exercised.
function githubFixture(directory, options = {}) {
  const commands = [];
  const product = options.version ?? version;
  let remote;
  const execute = (...args) => {
    commands.push(args);
    if (args[0] === 'api') {
      const endpoint = args.find((arg) => arg.startsWith('repos/'));
      if (endpoint.endsWith('/git/ref/heads/main')) return JSON.stringify({ object: { sha: options.main ?? sourceCommit } });
      if (endpoint.includes('/releases?')) return JSON.stringify([options.all ?? [previousRelease]]);
      if (endpoint.endsWith('/releases/assets/1')) return JSON.stringify(options.manifest ?? previousManifest);
      if (endpoint.includes('/git/matching-refs/')) return JSON.stringify([]);
      if (endpoint.endsWith('/releases/generate-notes')) return JSON.stringify({ body: 'Changes from merged pull requests.' });
      if (endpoint.endsWith('/releases/tags/v' + product)) return JSON.stringify(remote);
    }
    if (args[0] === 'release' && args[1] === 'create') {
      if (options.uploadFailure) throw new Error('synthetic interrupted upload');
      const assets = [...expectedAssets(product), 'release-manifest.json'].map((name) => {
        const data = readFileSync(join(directory, name));
        return { name, state: 'uploaded', size: data.length, digest: 'sha256:' + createHash('sha256').update(data).digest('hex') };
      });
      if (options.missingAsset) assets.pop();
      if (options.wrongDigest) assets[0].digest = 'sha256:' + '0'.repeat(64);
      remote = { tag_name: 'v' + product, target_commitish: options.wrongSource ? 'c'.repeat(40) : sourceCommit,
        draft: true, prerelease: options.wrongChannel ? !product.includes('-') : product.includes('-'), assets };
      return 'https://github.com/owner/repo/releases/tag/v' + product;
    }
    if (args[0] === 'release' && args[1] === 'edit') {
      remote.draft = false;
      return '';
    }
    throw new Error('Unexpected GitHub operation: ' + args.join(' '));
  };
  return { commands, execute };
}

test('an already published commit or obsolete main run does not allocate another version', () => {
  const environment = { GITHUB_REF: 'refs/heads/main', GITHUB_SHA: sourceCommit, GITHUB_REPOSITORY: 'owner/repo' };
  const published = githubFixture('', { all: [{ ...previousRelease, target_commitish: sourceCommit }] });
  assert.equal(prepareRelease(environment, published.execute).should_release, false);
  assert.equal(published.commands.length, 2);
  const stale = githubFixture('', { main: 'c'.repeat(40) });
  assert.equal(prepareRelease(environment, stale.execute).should_release, false);
  assert.equal(stale.commands.length, 1);
  const normal = githubFixture('');
  assert.deepEqual(prepareRelease(environment, normal.execute), {
    should_release: true, version, msi_version: '0.1.2', tag: 'v' + version, previous_tag: previousManifest.tag,
  });
  assert.throws(() => prepareRelease(environment, githubFixture('', { manifest: { ...previousManifest, repository: 'another/repo' } }).execute), /does not match/);
  const branchTarget = githubFixture('', { all: [{ ...previousRelease, target_commitish: 'main' }], manifest: { ...previousManifest, sourceCommit } });
  assert.equal(prepareRelease(environment, branchTarget.execute).should_release, false);
});

test('publication verifies every uploaded digest before making the release visible', (t) => {
  const directory = fixture(t);
  const github = githubFixture(directory);
  createRelease(directory, { environment: releaseEnvironment(directory), execute: github.execute, publish: true });
  const create = github.commands.find((args) => args[0] === 'release' && args[1] === 'create');
  const editIndex = github.commands.findIndex((args) => args[0] === 'release' && args[1] === 'edit');
  assert.ok(create.includes('--draft'));
  assert.ok(create.includes('--prerelease'));
  assert.ok(create.includes(sourceCommit));
  assert.ok(github.commands[editIndex - 1].at(-1).endsWith('/releases/tags/v' + version));
  assert.ok(github.commands[editIndex].includes('--latest=false'));
  assert.match(readFileSync(join(directory, 'summary.md'), 'utf8'), /Published release/);
  assert.match(readFileSync(join(directory, 'cobot-release-notes.md'), 'utf8'), /Changes from merged pull requests/);
});

test('incomplete uploads, wrong hashes and changed baselines cannot publish', (t) => {
  for (const options of [{ missingAsset: true }, { wrongDigest: true }, { wrongSource: true }, { wrongChannel: true }, { uploadFailure: true }, { all: [] }]) {
    const directory = fixture(t);
    const github = githubFixture(directory, options);
    assert.throws(() => createRelease(directory, { environment: releaseEnvironment(directory), execute: github.execute, publish: true }));
    assert.ok(!github.commands.some((args) => args[0] === 'release' && args[1] === 'edit'));
  }
});

test('manual draft-only builds verify uploads but never publish', (t) => {
  const directory = fixture(t);
  const github = githubFixture(directory);
  createRelease(directory, { environment: releaseEnvironment(directory), execute: github.execute, publish: false });
  assert.ok(!github.commands.some((args) => args[0] === 'release' && args[1] === 'edit'));
  assert.match(readFileSync(join(directory, 'summary.md'), 'utf8'), /Created release draft/);
  assert.match(readFileSync(join(directory, 'cobot-release-notes.md'), 'utf8'), /manual run created a draft only/);
});

test('explicit stable promotion publishes as latest without the prerelease flag', (t) => {
  const directory = fixture(t, '0.1.0');
  const github = githubFixture(directory, { version: '0.1.0' });
  createRelease(directory, { environment: { ...releaseEnvironment(directory), RELEASE_VERSION: '0.1.0' }, execute: github.execute, publish: true });
  const create = github.commands.find((args) => args[0] === 'release' && args[1] === 'create');
  assert.ok(!create.includes('--prerelease'));
  assert.ok(github.commands.some((args) => args[0] === 'release' && args[1] === 'edit' && args.includes('--latest=true')));
});

test('reviewed canvas license cannot be reused for another version or unrelated package', () => {
  const manifest = { name: '@napi-rs/canvas-darwin-arm64', version: '1.0.13', license: 'MIT' };
  const parent = { name: '@napi-rs/canvas', version: '1.0.13', optionalDependencies: { [manifest.name]: '1.0.13' } };
  assert.equal(reviewedCanvasLicense(manifest, parent), true);
  assert.equal(reviewedCanvasLicense({ ...manifest, version: '1.0.14' }, parent), false);
  assert.equal(reviewedCanvasLicense({ ...manifest, name: 'unrelated' }, parent), false);
  assert.equal(reviewedCanvasLicense({ ...manifest, license: 'unknown' }, parent), false);
});


test('embedded README licenses are limited to reviewed package versions and complete text', () => {
  const manifest = { name: 'isarray', version: '1.0.0', license: 'MIT' };
  const text = '\n## License\n' + "(MIT)\n\nCopyright (c) 2013 Julian Gruber &lt;julian@juliangruber.com&gt;\n\nPermission is hereby granted, free of charge, to any person obtaining a copy of\nthis software and associated documentation files (the \"Software\"), to deal in\nthe Software without restriction, including without limitation the rights to\nuse, copy, modify, merge, publish, distribute, sublicense, and/or sell copies\nof the Software, and to permit persons to whom the Software is furnished to do\nso, subject to the following conditions:\n\nThe above copyright notice and this permission notice shall be included in all\ncopies or substantial portions of the Software.\n\nTHE SOFTWARE IS PROVIDED \"AS IS\", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR\nIMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,\nFITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE\nAUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER\nLIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,\nOUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE\nSOFTWARE.";
  assert.match(embeddedReadmeLicense(manifest, text), /Copyright/);
  assert.equal(embeddedReadmeLicense({ ...manifest, version: '2.0.0' }, text), undefined);
  assert.equal(embeddedReadmeLicense({ ...manifest, name: 'other' }, text), undefined);
  assert.equal(embeddedReadmeLicense(manifest, text.replace('SOFTWARE.', '')), undefined);
});


test('resvg native license reuse requires matching parent version and published binding map', () => {
  const manifest = { name: '@resvg/resvg-js-darwin-arm64', version: '2.6.2', license: 'MPL-2.0' };
  const parent = { name: '@resvg/resvg-js', version: '2.6.2', license: 'MPL-2.0', optionalDependencies: { [manifest.name]: '2.6.2' } };
  assert.equal(reviewedResvgLicense(manifest, parent), true);
  assert.equal(reviewedResvgLicense({ ...manifest, version: '2.6.3' }, parent), false);
  assert.equal(reviewedResvgLicense({ ...manifest, license: 'MIT' }, parent), false);
  assert.equal(reviewedResvgLicense(manifest, { ...parent, optionalDependencies: {} }), false);
});
