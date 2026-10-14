import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, api } from './client';

describe('ApiClient wire handling', () => {
  beforeEach(() => {
    document.cookie = 'cobot_csrf=token-123';
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('unwraps the {data, requestId} envelope', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(JSON.stringify({ data: { revision: 1, language: 'zh-CN' }, requestId: 'r1' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    const settings = await api.getSettings();
    expect(settings.language).toBe('zh-CN');
  });

  it('maps error envelopes onto ApiError', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            error: { code: 'revision_conflict', messageKey: 'error.settings.revision_conflict', params: { actual: 2 } },
            requestId: 'r2',
          }),
          { status: 409, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    );
    await expect(api.updateSettings({ language: 'en' }, 1)).rejects.toMatchObject({
      code: 'revision_conflict',
      messageKey: 'error.settings.revision_conflict',
      status: 409,
    } satisfies Partial<ApiError>);
  });

  it('sends a requestId and the CSRF header on writes', async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(JSON.stringify({ data: { revision: 2, language: 'en' }, requestId: 'r3' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
    );
    vi.stubGlobal('fetch', fetchMock);
    await api.updateSettings({ language: 'en' }, 2);
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/v1/settings');
    const headers = init.headers as Record<string, string>;
    expect(headers['X-CSRF-Token']).toBe('token-123');
    const body = JSON.parse(String(init.body));
    expect(typeof body.requestId).toBe('string');
    expect(body.expectedRevision).toBe(2);
    expect(body.patch).toEqual({ language: 'en' });
  });

  it('throws a generic internal error on malformed JSON success', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('not-json', { status: 200 })),
    );
    await expect(api.getSettings()).rejects.toBeInstanceOf(ApiError);
  });
});
