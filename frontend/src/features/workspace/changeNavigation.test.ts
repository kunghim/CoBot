import { afterEach, expect, it, vi } from 'vitest';
import { api } from '../../api/client';
import { listenForChanges, openWorkspaceChanges } from './changeNavigation';
const cleanups: Array<() => void> = [];
afterEach(() => { cleanups.splice(0).forEach((cleanup) => cleanup()); vi.restoreAllMocks(); });
it('rejects a malformed history subscription instead of invoking it', async () => {
  vi.spyOn(api, 'workspaceChangeFiles').mockResolvedValue({ run: { id: 'run', botId: 'bot' } } as never);
  const scope = { kind: 'chat' as const, id: 'invalid-listener' };
  cleanups.push(listenForChanges(scope, {} as never));
  await expect(openWorkspaceChanges(scope, 'run')).rejects.toMatchObject({ messageKey: 'error.workspace.unavailable' });
});
it('routes validated history to its owning workspace', async () => {
  vi.spyOn(api, 'workspaceChangeDiff').mockResolvedValue({ run: { id: 'run', botId: 'bot' } } as never);
  const team = vi.fn(); const chat = vi.fn();
  cleanups.push(listenForChanges({ kind: 'team', id: 'same' }, team), listenForChanges({ kind: 'chat', id: 'same' }, chat));
  await openWorkspaceChanges({ kind: 'team', id: 'same' }, 'run', 'a.md');
  expect(team).toHaveBeenCalledWith({ runId: 'run', botId: 'bot', path: 'a.md' });
  expect(chat).not.toHaveBeenCalled();
});
it('does not navigate when historical ownership is rejected', async () => {
  vi.spyOn(api, 'workspaceChangeFiles').mockRejectedValue(new Error('foreign run'));
  const listener = vi.fn(); cleanups.push(listenForChanges({ kind: 'chat', id: 'chat' }, listener));
  await expect(openWorkspaceChanges({ kind: 'chat', id: 'chat' }, 'foreign')).rejects.toThrow('foreign run');
  expect(listener).not.toHaveBeenCalled();
});
it('keeps the latest request when responses arrive out of order', async () => {
  let resolveFirst!: (value: never) => void;
  vi.spyOn(api, 'workspaceChangeFiles').mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve; })).mockResolvedValue({ run: { id: 'second', botId: 'bot' } } as never);
  const listener = vi.fn(); const scope = { kind: 'team' as const, id: 'latest' };
  cleanups.push(listenForChanges(scope, listener));
  const first = openWorkspaceChanges(scope, 'first');
  await openWorkspaceChanges(scope, 'second');
  resolveFirst({ run: { id: 'first', botId: 'bot' } } as never); await first;
  expect(listener).toHaveBeenCalledTimes(1);
  expect(listener).toHaveBeenCalledWith({ runId: 'second', botId: 'bot', path: undefined });
});
