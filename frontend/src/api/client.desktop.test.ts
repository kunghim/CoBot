import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

describe('desktop HTTP transport', () => {
  let client: typeof import('./client') | undefined;
  const endpoint = 'http://127.0.0.1:51234';
  const fetchMock = vi.fn<typeof fetch>();
  function installBridge(bootstrap: () => Promise<{secret: string; endpoint: string}>) {
    Object.defineProperty(window, 'go', { configurable: true, value: { desktop: { Bridge: { Bootstrap: bootstrap } } } });
  }
  const response = (data: unknown) => new Response(JSON.stringify({ data, requestId: 'desktop-test' }), { status: 200 });
  beforeEach(() => {
    vi.resetModules();
    fetchMock.mockReset();
    vi.stubGlobal('fetch', fetchMock);
  });
  afterEach(() => {
    client?.clearBearer();
    client = undefined;
    Reflect.deleteProperty(window, 'go');
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('waits for the native handoff before the first request chooses an origin', async () => {
    let resolve!: (value: {secret: string; endpoint: string}) => void;
    const handoff = new Promise<{secret: string; endpoint: string}>((done) => { resolve = done; });
    installBridge(() => handoff);
    fetchMock.mockResolvedValue(response({ authorized: false }));
    client = await import('./client');
    const pending = client.api.getStatus();
    await Promise.resolve();
    expect(fetchMock).not.toHaveBeenCalled();
    resolve({ secret: 'test-one-time-handoff', endpoint });
    await pending;
    expect(fetchMock.mock.calls[0][0]).toBe(endpoint + '/api/v1/status');
    expect(await client.desktopSecret()).toBe('test-one-time-handoff');
  });

  it.each(['https://outside.invalid', 'http://user:password@127.0.0.1:1234', endpoint + '/nested'])('rejects an invalid native endpoint: %s', async (invalid) => {
    installBridge(async () => ({ secret: 'test-secret', endpoint: invalid }));
    client = await import('./client');
    await expect(client.api.getStatus()).rejects.toThrow('Invalid desktop loopback endpoint');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('keeps bearer credentials in memory and renews against the native endpoint', async () => {
    vi.useFakeTimers();
    const bootstrap = vi.fn(async () => ({ secret: 'renewed-one-time-secret', endpoint }));
    bootstrap.mockResolvedValueOnce({ secret: 'initial-one-time-secret', endpoint });
    installBridge(bootstrap);
    const storageWrite = vi.spyOn(Storage.prototype, 'setItem');
    fetchMock.mockResolvedValueOnce(response({ bearerToken: 'fixture-bearer-1', bearerExpiresAt: new Date(Date.now() + 120_000).toISOString(), csrfCookieName: 'fixture_csrf' }));
    client = await import('./client');
    await client.api.exchangeBootstrap((await client.desktopSecret())!);
    fetchMock.mockResolvedValueOnce(response({ revision: 1 }));
    await client.api.getSettings();
    const firstRead = fetchMock.mock.calls[1];
    expect(firstRead[0]).toBe(endpoint + '/api/v1/settings');
    expect(firstRead[1]?.credentials).toBe('omit');
    expect(new Headers(firstRead[1]?.headers).get('Authorization')).toBe('Bearer fixture-bearer-1');
    fetchMock.mockResolvedValueOnce(response({ bearerToken: 'fixture-bearer-2', bearerExpiresAt: new Date(Date.now() + 300_000).toISOString(), csrfCookieName: 'fixture_csrf' }));
    await vi.advanceTimersByTimeAsync(60_001);
    expect(bootstrap).toHaveBeenCalledTimes(2);
    const renewal = fetchMock.mock.calls[2];
    expect(renewal[0]).toBe(endpoint + '/api/v1/session/bootstrap');
    expect(JSON.parse(String(renewal[1]?.body)).secret).toBe('renewed-one-time-secret');
    expect(client.bearerToken()).toBe('fixture-bearer-2');
    expect(storageWrite).not.toHaveBeenCalled();
  });
});
