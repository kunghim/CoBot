import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '../../api/client';
import { listenForFiles, openWorkspaceFile } from './fileNavigation';
import type { WorkspaceFile } from '../../api/types';

const file: WorkspaceFile = { path: 'src/app.ts', name: 'app.ts', kind: 'text', size: 20, modifiedAt: '2026-10-26T00:00:00Z', version: '1', line: 4 };
const cleanups: (() => void)[] = [];
afterEach(() => { cleanups.splice(0).forEach((cleanup) => cleanup()); vi.restoreAllMocks(); });

describe('file navigation', () => {
  it('rejects a malformed navigation subscription instead of invoking it', async () => {
    vi.spyOn(api, 'workspaceFile').mockResolvedValue(file);
    const scope = { kind: 'chat' as const, id: 'invalid-listener' };
    cleanups.push(listenForFiles(scope, {} as never));
    await expect(openWorkspaceFile(scope, 'src/app.ts')).rejects.toMatchObject({ messageKey: 'error.workspace.unavailable' });
  });
  it('uses server-normalized paths and keeps chat and team listeners separate', async () => {
    vi.spyOn(api, 'workspaceFile').mockResolvedValue(file);
    const team = vi.fn(); const chat = vi.fn();
    cleanups.push(listenForFiles({ kind: 'team', id: 'same' }, team), listenForFiles({ kind: 'chat', id: 'same' }, chat));
    await openWorkspaceFile({ kind: 'chat', id: 'same' }, '/project/src/app.ts:4');
    expect(chat).toHaveBeenCalledWith(file);
    expect(team).not.toHaveBeenCalled();
    expect(api.workspaceFile).toHaveBeenCalledWith({ kind: 'chat', id: 'same' }, '/project/src/app.ts:4');
  });
  it('does not open a rejected path or deliver a late response to another workspace', async () => {
    const listener = vi.fn();
    const cleanup = listenForFiles({ kind: 'team', id: 'old' }, listener); cleanups.push(cleanup);
    const request = vi.spyOn(api, 'workspaceFile').mockRejectedValue(new Error('outside'));
    await expect(openWorkspaceFile({ kind: 'team', id: 'old' }, '../private')).rejects.toThrow('outside');
    expect(listener).not.toHaveBeenCalled();
    request.mockResolvedValue(file); cleanup();
    cleanups.push(listenForFiles({ kind: 'team', id: 'new' }, listener));
    await expect(openWorkspaceFile({ kind: 'team', id: 'old' }, 'src/app.ts')).rejects.toMatchObject({ messageKey: 'error.workspace.unavailable' });
    expect(listener).not.toHaveBeenCalled();
  });
});
