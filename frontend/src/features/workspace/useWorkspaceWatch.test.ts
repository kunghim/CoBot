import { act, createElement } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, expect, it, vi } from 'vitest';
import * as client from '../../api/client';
import type { WorkspaceScope } from '../../api/types';
import { useWorkspaceWatch } from './useWorkspaceWatch';
afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers(); });
function Harness({ scope, paths, retry = 0, refresh }: { scope: WorkspaceScope; paths: string[]; retry?: number; refresh: (reset: boolean) => void }) {
  const unavailable = useWorkspaceWatch(scope, paths, retry, refresh);
  return createElement('span', null, unavailable ? 'unavailable' : 'available');
}
function mount() { const element = document.createElement('div'); document.body.append(element); const root = createRoot(element); return { root, element, close() { act(() => root.unmount()); element.remove(); } }; }
it('reuses equivalent subscriptions and aborts the old scope and unmounted view', async () => {
  const subscriptions: Array<{ signal: AbortSignal; notify: (event: 'ready' | 'changed') => void }> = [];
  const stream = vi.spyOn(client, 'streamWorkspace').mockImplementation((_scope, _paths, notify, signal) => new Promise((resolve) => { subscriptions.push({ signal, notify }); signal.addEventListener('abort', () => resolve(null)); }));
  const view = mount(); const refresh = vi.fn(); const scope = { kind: 'team' as const, id: 'a' };
  try {
    await act(async () => view.root.render(createElement(Harness, { scope, paths: ['docs', '', 'docs'], refresh })));
    act(() => { subscriptions[0].notify('ready'); subscriptions[0].notify('changed'); });
    expect(refresh.mock.calls).toEqual([[false], [true]]);
    await act(async () => view.root.render(createElement(Harness, { scope, paths: ['', 'docs'], refresh })));
    expect(stream).toHaveBeenCalledTimes(1);
    await act(async () => view.root.render(createElement(Harness, { scope: { kind: 'chat', id: 'a' }, paths: [''], refresh })));
    expect(subscriptions[0].signal.aborted).toBe(true);
    expect(stream).toHaveBeenCalledTimes(2);
  } finally { view.close(); }
  expect(subscriptions[1].signal.aborted).toBe(true);
});
it('reconciles directory pages after reconnecting the same subscription', async () => {
  vi.useFakeTimers(); const refresh = vi.fn();
  const stream = vi.spyOn(client, 'streamWorkspace').mockImplementationOnce(async (_scope, _paths, notify) => { notify('ready'); return null; }).mockImplementation((_scope, _paths, notify, signal) => { notify('ready'); return new Promise((resolve) => signal.addEventListener('abort', () => resolve(null))); });
  const view = mount();
  try {
    await act(async () => view.root.render(createElement(Harness, { scope: { kind: 'chat', id: 'a' }, paths: [''], refresh })));
    await act(async () => vi.advanceTimersByTimeAsync(1000));
    expect(stream).toHaveBeenCalledTimes(2);
    expect(refresh.mock.calls).toEqual([[false], [true]]);
  } finally { view.close(); }
});
it.each([422, 503])('keeps capability failures (%s) quiet until explicit retry and bounds directory subscriptions', async (status) => {
  vi.useFakeTimers(); const refresh = vi.fn();
  const stream = vi.spyOn(client, 'streamWorkspace').mockRejectedValue(new client.ApiError(status, 'capability_missing', 'error.workspace.watch_unavailable', {}));
  const view = mount(); const scope = { kind: 'chat' as const, id: 'a' };
  try {
    await act(async () => view.root.render(createElement(Harness, { scope, paths: [''], refresh })));
    expect(view.element.textContent).toBe('unavailable');
    await act(async () => vi.advanceTimersByTimeAsync(30000));
    expect(stream).toHaveBeenCalledTimes(1);
    await act(async () => view.root.render(createElement(Harness, { scope, paths: [''], retry: 1, refresh })));
    expect(stream).toHaveBeenCalledTimes(2);
    await act(async () => view.root.render(createElement(Harness, { scope, paths: Array.from({ length: 65 }, (_, index) => 'dir-' + index), refresh })));
    expect(view.element.textContent).toBe('unavailable');
    expect(stream).toHaveBeenCalledTimes(2);
  } finally { view.close(); }
});
