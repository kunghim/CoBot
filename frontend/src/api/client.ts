// ApiClient is the single fetch wrapper for the loopback API. It owns the
// wire envelope, requestId idempotency keys and CSRF handling; features never
// call fetch directly.

export class ApiError extends Error {
  readonly code: string;
  readonly messageKey: string;
  readonly params: Record<string, unknown>;
  readonly status: number;

  constructor(status: number, code: string, messageKey: string, params: Record<string, unknown>) {
    super(`${code}: ${messageKey}`);
    this.status = status;
    this.code = code;
    this.messageKey = messageKey;
    this.params = params;
  }
}

export interface OkEnvelope<T> {
  data: T;
  requestId: string;
}

interface ErrorEnvelope {
  error: { code: string; messageKey: string; params?: Record<string, unknown> };
  requestId: string;
}

function newRequestId(): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID();
  }
  return 'req-' + Date.now() + '-' + Math.random().toString(16).slice(2);
}

const BEARER_KEY = 'cobot_bearer';

/** Desktop-only bearer storage; the browser flow keeps cookies. */
export function bearerToken(): string | null {
  try {
    return sessionStorage.getItem(BEARER_KEY);
  } catch {
    return null;
  }
}

export function clearBearer(): void {
  try {
    sessionStorage.removeItem(BEARER_KEY);
  } catch {
    /* storage unavailable: nothing to clear */
  }
}

function csrfToken(): string {
  const match = document.cookie.match(/(?:^|;\s*)cobot_csrf=([^;]+)/);
  return match ? decodeURIComponent(match[1]) : '';
}

export type UnauthorizedListener = () => void;
const unauthorizedListeners = new Set<UnauthorizedListener>();

/** Subscribes to session-loss notifications (SSE 401, expired cookie...). */
export function onUnauthorized(listener: UnauthorizedListener): () => void {
  unauthorizedListeners.add(listener);
  return () => unauthorizedListeners.delete(listener);
}

function notifyUnauthorized(): void {
  for (const listener of unauthorizedListeners) listener();
}

/** Reports a session loss discovered outside request() (for example an SSE
 * fetch that returned 401 in desktop bearer mode). */
export function reportUnauthorized(): void {
  notifyUnauthorized();
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  const payload = body === undefined ? undefined : JSON.stringify({ requestId: newRequestId(), ...(body as object) });
  if (payload !== undefined) headers['Content-Type'] = 'application/json';
  const bearer = bearerToken();
  if (bearer) {
    // Desktop shell: the page runs on the Wails asset origin and
    // authenticates with the in-memory bearer, never with cookies.
    headers['Authorization'] = 'Bearer ' + bearer;
  } else {
    headers['X-CSRF-Token'] = csrfToken();
  }

  const resp = await fetch(path, { method, headers, credentials: 'include', body: payload });
  const text = await resp.text();
  let parsed: unknown = undefined;
  if (text) {
    try {
      parsed = JSON.parse(text);
    } catch {
      parsed = undefined;
    }
  }
  if (!resp.ok) {
    const envelope = parsed as ErrorEnvelope | undefined;
    if (resp.status === 401) {
      clearBearer();
      notifyUnauthorized();
    }
    throw new ApiError(
      resp.status,
      envelope?.error?.code ?? 'internal',
      envelope?.error?.messageKey ?? 'error.internal',
      envelope?.error?.params ?? {},
    );
  }
  const ok = parsed as OkEnvelope<T> | undefined;
  if (!ok || typeof ok !== 'object' || !('data' in ok)) {
    // A 200 without the {data,requestId} envelope is a contract violation,
    // not a client bug; surface it as an internal error.
    throw new ApiError(500, 'internal', 'error.internal', {});
  }
  return ok.data;
}

export const api = {
  getStatus: () => request<import('./types').Status>('GET', '/api/v1/status'),
  getSettings: () => request<import('./types').Settings>('GET', '/api/v1/settings'),
  updateSettings: (patch: import('./types').SettingsPatch, expectedRevision?: number) =>
    request<import('./types').Settings>('PATCH', '/api/v1/settings', {
      patch,
      ...(expectedRevision !== undefined ? { expectedRevision } : {}),
    }),
  getSnapshot: () => request<import('./types').Snapshot>('GET', '/api/v1/snapshot'),
  exchangeBootstrap: async (secret: string) => {
    const result = await request<{ sessionExpiresAt: string; userId: string; bearerToken?: string; bearerExpiresAt?: string }>(
      'POST',
      '/api/v1/session/bootstrap',
      { secret },
    );
    // Only the desktop shell keeps the bearer; browser sessions stay cookies.
    if (result.bearerToken && desktopBridge()) {
      try {
        sessionStorage.setItem(BEARER_KEY, result.bearerToken);
      } catch {
        /* storage unavailable */
      }
    }
    return result;
  },
  logout: () => request<{ loggedOut: boolean }>('POST', '/api/v1/auth/logout', {}),
};

import { desktopBridge } from '../platform/native-bridge';

/** Dev-only: ask the Vite dev server to mint a bootstrap secret via the
 * same-user runtime credential handed to it by scripts/dev.*. */
export async function devLinkSecret(): Promise<string | null> {
  if (!import.meta.env.DEV) return null;
  try {
    const resp = await fetch('/__cobot/dev-link', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ requestId: newRequestId() }),
    });
    if (!resp.ok) return null;
    const body = (await resp.json()) as OkEnvelope<{ bootstrapSecret: string }>;
    return body.data?.bootstrapSecret ?? null;
  } catch {
    return null;
  }
}
