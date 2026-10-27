import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '../../api/client';
import { currentFiles, resourceFiles } from './resourceFiles';
import type { WorkspaceFile } from '../../api/types';
const scope = { kind: 'team' as const, id: 'team' };
const file = (path: string): WorkspaceFile => ({ path, name: path, kind: 'text', size: 1, version: '1', modifiedAt: '' });
afterEach(() => vi.restoreAllMocks());
describe('resource file confirmation', () => {
  it('omits missing and changed files and canonicalizes duplicate aliases', async () => {
    vi.spyOn(api, 'workspaceFile').mockImplementation(async (_scope, target) => {
      if (target === 'missing') throw new Error('missing');
      return file(target === '/repo/a.md' ? 'a.md' : target);
    });
    vi.spyOn(api, 'workspaceChangeDiff').mockImplementation(async (_scope, _run, path) => {
      if (path === 'historical.md') return {} as never;
      throw new Error('not recorded');
    });
    const result = await resourceFiles(scope, ['missing', 'changed.md', 'a.md', '/repo/a.md', 'historical.md'], 0, [], ['changed.md'], 'run', new AbortController().signal);
    expect(result.files.map((entry) => entry.file.path)).toEqual(['a.md']);
    expect(result.next).toBe(-1);
  });
  it('limits each page to twenty confirmed resources', async () => {
    vi.spyOn(api, 'workspaceFile').mockImplementation(async (_scope, target) => file(target));
    const targets = Array.from({ length: 25 }, (_, index) => `${index}.md`);
    const first = await resourceFiles(scope, targets, 0, [], [], undefined, new AbortController().signal);
    expect(first.files).toHaveLength(20);
    expect(first.next).toBe(20);
    const second = await resourceFiles(scope, targets, first.next, first.files.map((item) => item.file.path), [], undefined, new AbortController().signal);
    expect(second.files).toHaveLength(5);
    expect(second.next).toBe(-1);
  });
  it('propagates cancellation instead of interpreting it as missing files', async () => {
    const controller = new AbortController(); controller.abort();
    await expect(currentFiles(scope, ['a.md'], controller.signal)).rejects.toMatchObject({ name: 'AbortError' });
  });
});
