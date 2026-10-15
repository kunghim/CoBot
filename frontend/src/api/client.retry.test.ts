import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

describe('ApiClient retry identity', () => {
  let api: typeof import('./client').api;
  const fetchMock = vi.fn<typeof fetch>();

  function success(): Response {
    return new Response(JSON.stringify({ data: { accepted: true }, requestId: 'response-id' }), { status: 200 });
  }

  function observed(index: number) {
    const [url, init] = fetchMock.mock.calls[index];
    expect(init).toBeDefined();
    return { url, ...init, headers: new Headers(init?.headers) };
  }

  function messageId(index: number): string {
    const body = JSON.parse(String(observed(index).body)) as { requestId: string };
    expect(body.requestId).toEqual(expect.any(String));
    expect(body.requestId.length).toBeGreaterThan(0);
    return body.requestId;
  }

  beforeEach(async () => {
    vi.resetModules();
    fetchMock.mockReset();
    vi.stubGlobal('fetch', fetchMock);
    ({ api } = await import('./client'));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('automatically retries a lost response with the same request and identity', async () => {
    fetchMock.mockRejectedValueOnce(new TypeError('network lost')).mockResolvedValueOnce(success());

    await api.sendMessage('bot-a', 'Please review this change');

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(observed(0).url).toBe('/api/v1/bots/bot-a/messages');
    expect(observed(0).method).toBe('POST');
    expect(JSON.parse(String(observed(0).body))).toEqual({ text: 'Please review this change', requestId: messageId(0) });
    expect(observed(1)).toEqual(observed(0));
    expect(messageId(1)).toBe(messageId(0));
  });

  it('keeps the identity when resubmitting after both network attempts fail', async () => {
    fetchMock.mockRejectedValueOnce(new TypeError('first loss')).mockRejectedValueOnce(new TypeError('second loss'));
    await expect(api.sendMessage('bot-a', 'Review')).rejects.toThrow('second loss');
    expect(fetchMock).toHaveBeenCalledTimes(2);

    fetchMock.mockResolvedValueOnce(success());
    await api.sendMessage('bot-a', 'Review');

    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(observed(1)).toEqual(observed(0));
    expect(observed(2)).toEqual(observed(0));
    expect(messageId(2)).toBe(messageId(0));
  });

  it.each([500, 503])('keeps the identity when resubmitting after HTTP %s', async (status) => {
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ error: { code: 'internal', messageKey: 'error.internal' } }), { status }));
    await expect(api.sendMessage('bot-a', 'Review')).rejects.toMatchObject({ status });
    expect(fetchMock).toHaveBeenCalledTimes(1);

    fetchMock.mockResolvedValueOnce(success());
    await api.sendMessage('bot-a', 'Review');

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(observed(1)).toEqual(observed(0));
    expect(messageId(1)).toBe(messageId(0));
  });

  it('starts a new identity for the same message after confirmed success', async () => {
    fetchMock.mockResolvedValueOnce(success()).mockResolvedValueOnce(success());
    await api.sendMessage('bot-a', 'Review');
    await api.sendMessage('bot-a', 'Review');

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(messageId(1)).not.toBe(messageId(0));
    expect(observed(1).url).toBe(observed(0).url);
    expect(JSON.parse(String(observed(1).body)).text).toBe('Review');
  });

  it('separates pending identities for different targets and message bodies', async () => {
    fetchMock.mockImplementation(async () => new Response('{}', { status: 503 }));
    await expect(api.sendMessage('bot-a', 'Review')).rejects.toMatchObject({ status: 503 });
    await expect(api.sendMessage('bot-b', 'Review')).rejects.toMatchObject({ status: 503 });
    await expect(api.sendMessage('bot-a', 'Different review')).rejects.toMatchObject({ status: 503 });
    await expect(api.sendMessage('bot-a', 'Review')).rejects.toMatchObject({ status: 503 });

    expect(fetchMock).toHaveBeenCalledTimes(4);
    expect(observed(1).url).toBe('/api/v1/bots/bot-b/messages');
    expect(JSON.parse(String(observed(2).body)).text).toBe('Different review');
    expect(new Set([messageId(0), messageId(1), messageId(2)]).size).toBe(3);
    expect(messageId(3)).toBe(messageId(0));
  });

  it('sends DELETE identity and revision headers unchanged on automatic retry', async () => {
    fetchMock.mockRejectedValueOnce(new TypeError('network lost')).mockResolvedValueOnce(success());
    await api.deleteTeam('team-a', 7);

    expect(fetchMock).toHaveBeenCalledTimes(2);
    const first = observed(0);
    expect(first.url).toBe('/api/v1/teams/team-a');
    expect(first.method).toBe('DELETE');
    expect(first.body).toBeUndefined();
    expect(first.headers.get('Idempotency-Key')).toEqual(expect.any(String));
    expect(first.headers.get('Idempotency-Key')?.length).toBeGreaterThan(0);
    expect(first.headers.get('If-Match')).toBe('7');
    expect(observed(1)).toEqual(first);
  });
});
