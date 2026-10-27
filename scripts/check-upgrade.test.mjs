import test from 'node:test';
import assert from 'node:assert/strict';
import { validateBaseline } from './check-upgrade.mjs';

test('upgrade baseline is an explicit version tag, never a branch or command option', () => {
  for (const tag of ['v0.1.0-beta.2', 'v1.2.3']) assert.equal(validateBaseline(tag), tag);
  for (const tag of ['', undefined, 'main', '--help', 'v1.2.3;echo', 'v1.2.3/../../main', 'v1.2.3\n']) {
    assert.throws(() => validateBaseline(tag), /published version tag/);
  }
});
