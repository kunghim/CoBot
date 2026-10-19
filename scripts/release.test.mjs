import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdtempSync, rmSync, symlinkSync, unlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { expectedAssets, releaseNotes, validateInputs, validateSource, validateUnusedVersion, verifyAssets } from './release.mjs';

const version = '0.1.0-beta.2';
function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), 'cobot-release-test-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  for (const name of expectedAssets(version).filter((name) => !name.endsWith('.sha256'))) {
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
  for (const value of ['v0.1.0', '0.1.0+meta', '../1.0.0', '1.0.0\nother=bad', '1.0.0; echo bad', '01.0.0']) {
    assert.throws(() => validateInputs(value, '0.1.2'));
  }
  for (const value of ['1.2', '1.2.3.4', '256.0.0', '0.256.0', '0.1.65536', '0.01.2', '-1.0.0']) {
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
  const name = expectedAssets(version)[0];
  unlinkSync(join(directory, name));
  symlinkSync(join(directory, expectedAssets(version)[1]), join(directory, name));
  assert.throws(() => verifyAssets(directory, version), /Invalid release file/);
});

test('notes distinguish automation from real-account and installation acceptance', () => {
  const notes = releaseNotes({ version, msiVersion: '0.1.2', sourceCommit: 'a'.repeat(40),
    repository: 'owner/repo', runUrl: 'https://github.com/owner/repo/actions/runs/1' });
  assert.match(notes, /Intel Macs are not included/);
  assert.match(notes, /Before publishing this draft/);
  assert.match(notes, /does not perform them/);
  assert.match(notes, /macOS 最低 0.146.0/);
});
