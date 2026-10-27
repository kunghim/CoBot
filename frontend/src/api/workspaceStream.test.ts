import { afterEach, beforeEach, expect, it, vi } from 'vitest';
beforeEach(() => { vi.resetModules(); document.cookie = 'cobot_csrf=watch-csrf'; });
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });
const scope = { kind: 'chat' as const, id: 'chat-one' };
function response(chunks: string[], finish = true) {
  const cancel = vi.fn();
  const stream = new ReadableStream({ start(controller) { for (const chunk of chunks) controller.enqueue(new TextEncoder().encode(chunk)); if (finish) controller.close(); }, cancel });
  return { response: new Response(stream, { headers: { 'Content-Type': 'text/event-stream' } }), cancel };
}
it('authenticates the scoped subscription and handles split CRLF events without trusting payload state', async () => {
  const source = response(['event: rea', 'dy\r\ndata: {}\r', '\n\r\n: heartbeat\n\nevent: changed\ndata: {"path":"ignored"}\n\n']);
  const fetch = vi.fn().mockResolvedValue(source.response); vi.stubGlobal('fetch', fetch);
  const { streamWorkspace } = await import('./client'); const events = vi.fn();
  expect(await streamWorkspace(scope, ['', 'nested'], events, new AbortController().signal)).toBeNull();
  expect(events.mock.calls).toEqual([['ready'], ['changed']]);
  const [url, options] = fetch.mock.calls[0];
  expect(url).toBe('/api/v1/workspaces/chat/chat-one/watch');
  expect(options.headers['X-CSRF-Token']).toBe('watch-csrf');
  expect(options.credentials).toBe('include');
  expect(JSON.parse(options.body).paths).toEqual(['', 'nested']);
  expect(JSON.parse(options.body).requestId).toBeTruthy();
  expect(source.response.body!.locked).toBe(false);
});
it('releases a stream when native watching becomes unavailable', async () => {
  const source = response(['event: unavailable\ndata: {}\n\n'], false);
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(source.response));
  const { streamWorkspace } = await import('./client');
  expect(await streamWorkspace(scope, [''], vi.fn(), new AbortController().signal)).toBe('unavailable');
  expect(source.cancel).toHaveBeenCalledTimes(1);
  expect(source.response.body!.locked).toBe(false);
});
it('bounds malformed streams and releases their readers', async () => {
  const source = response(['x'.repeat(65537)], false);
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(source.response));
  const { streamWorkspace } = await import('./client');
  await expect(streamWorkspace(scope, [''], vi.fn(), new AbortController().signal)).rejects.toMatchObject({ messageKey: 'error.workspace.watch_unavailable' });
  expect(source.cancel).toHaveBeenCalledTimes(1);
});
it('preserves server capability errors and stops before sending a canceled subscription', async () => {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: { code: 'capability_missing', messageKey: 'error.workspace.watch_unavailable', params: {} } }), { status: 503 }));
  vi.stubGlobal('fetch', fetch); const { streamWorkspace } = await import('./client');
  await expect(streamWorkspace(scope, [''], vi.fn(), new AbortController().signal)).rejects.toMatchObject({ status: 503, messageKey: 'error.workspace.watch_unavailable' });
  const controller = new AbortController(); controller.abort();
  await expect(streamWorkspace(scope, [''], vi.fn(), controller.signal)).rejects.toMatchObject({ name: 'AbortError' });
  expect(fetch).toHaveBeenCalledTimes(1);
});
