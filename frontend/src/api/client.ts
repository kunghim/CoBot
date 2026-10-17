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

interface OkEnvelope<T> {
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

let bearer: string | null = null;
let baseUrl = '';
let transportReady: Promise<void> | null = null;
let bootstrapSecret: string | null = null;
let renewalTimer = 0;

export function bearerToken(): string | null { return bearer; }
export function clearBearer(): void { bearer = null; window.clearTimeout(renewalTimer); }
export function apiUrl(path: string): string { return baseUrl + path; }

function setEndpoint(endpoint: string): void {
  const url = new URL(endpoint);
  if (!['http:', 'https:'].includes(url.protocol) || !['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname) || url.username || url.password || url.pathname !== '/' || url.search || url.hash) {
    throw new Error('Invalid desktop loopback endpoint');
  }
  if (baseUrl && baseUrl !== url.origin) clearBearer();
  baseUrl = url.origin;
}

/** Every query waits for the native handoff before choosing its HTTP origin. */
function initializeTransport(): Promise<void> {
  if (!transportReady) transportReady = (async () => {
    const bridge = desktopBridge();
    if (bridge) { const handoff = await bridge.Bootstrap(); setEndpoint(handoff.endpoint); bootstrapSecret = handoff.secret; }
  })().catch((error: unknown) => { transportReady = null; throw error; });
  return transportReady;
}

export async function desktopSecret(): Promise<string | null> {
  await initializeTransport();
  if (bootstrapSecret) { const secret = bootstrapSecret; bootstrapSecret = null; return secret; }
  const bridge = desktopBridge();
  if (!bridge) return null;
  const handoff = await bridge.Bootstrap(); setEndpoint(handoff.endpoint); return handoff.secret;
}

const pendingWrites = new Map<string, string>();
function canonical(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).sort(([a], [b]) => a.localeCompare(b)).map(([key, child]) => [key, canonical(child)]));
  return value;
}

let csrfCookieName = 'cobot_csrf';
function csrfToken(): string {
  const entry = document.cookie.split(';').map((value) => value.trim()).find((value) => value.startsWith(csrfCookieName + '='));
  return entry ? decodeURIComponent(entry.slice(csrfCookieName.length + 1)) : '';
}

type UnauthorizedListener = () => void;
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
  await initializeTransport();
  const headers: Record<string, string> = {};
  const writing = !['GET', 'HEAD'].includes(method);
  const input = (body ?? {}) as Record<string, unknown>;
  const fingerprint = method + ' ' + path + ' ' + JSON.stringify(canonical(input));
  const requestId = String(input.requestId ?? pendingWrites.get(fingerprint) ?? newRequestId());
  if (writing) {
    if (pendingWrites.size >= 100 && !pendingWrites.has(fingerprint)) pendingWrites.delete(pendingWrites.keys().next().value!);
    pendingWrites.set(fingerprint, requestId);
  }
  const payload = !writing || method === 'DELETE' ? undefined : JSON.stringify({ ...input, requestId });
  if (payload !== undefined) headers['Content-Type'] = 'application/json';
  if (method === 'DELETE') {
    headers['Idempotency-Key'] = requestId;
    if (input.expectedRevision !== undefined) headers['If-Match'] = String(input.expectedRevision);
  }
  const token = bearerToken();
  if (token) headers.Authorization = 'Bearer ' + token;
  else headers['X-CSRF-Token'] = csrfToken();
  // Only durable business writes are safe to retry after a lost response.
  const retryable = !path.startsWith('/api/v1/auth/') && !path.startsWith('/api/v1/session/');
  let resp: Response;
  try { resp = await fetch(apiUrl(path), { method, headers, credentials: token ? 'omit' : 'include', body: payload }); }
  catch (error) {
    if (!retryable) throw error;
    resp = await fetch(apiUrl(path), { method, headers, credentials: token ? 'omit' : 'include', body: payload });
  }
  const text = await resp.text();
  let parsed: unknown;
  try { parsed = text ? JSON.parse(text) : undefined; } catch { parsed = undefined; }
  if (!resp.ok && resp.status < 500) pendingWrites.delete(fingerprint);
  if (!resp.ok) {
    const envelope = parsed as ErrorEnvelope | undefined;
    if (resp.status === 401) { clearBearer(); notifyUnauthorized(); }
    throw new ApiError(resp.status, envelope?.error?.code ?? 'internal', envelope?.error?.messageKey ?? 'error.internal', envelope?.error?.params ?? {});
  }
  const ok = parsed as OkEnvelope<T> | undefined;
  if (!ok || typeof ok !== 'object' || !('data' in ok)) throw new ApiError(500, 'internal', 'error.internal', {});
  pendingWrites.delete(fingerprint);
  return ok.data;
}

function pageQuery(params: Record<string, string | number | undefined>): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) if (value !== undefined && value !== '') query.set(key, String(value));
  return query.size ? '?' + query.toString() : '';
}

export const api = {
  getStatus: async () => {
    const status = await request<import('./types').Status>('GET', '/api/v1/status');
    if (status.csrfCookieName) csrfCookieName = status.csrfCookieName;
    return status;
  },
  getSettings: () => request<import('./types').Settings>('GET', '/api/v1/settings'),
  updateSettings: (patch: import('./types').SettingsPatch, expectedRevision?: number) =>
    request<import('./types').Settings>('PATCH', '/api/v1/settings', {
      patch,
      ...(expectedRevision !== undefined ? { expectedRevision } : {}),
    }),
  getSnapshot: () => request<import('./types').Snapshot>('GET', '/api/v1/snapshot'),
  exchangeBootstrap: async (secret: string) => {
    const result = await request<{ sessionExpiresAt: string; userId: string; bearerToken?: string; bearerExpiresAt?: string; csrfCookieName: string }>(
      'POST',
      '/api/v1/session/bootstrap',
      { secret },
    );
    csrfCookieName = result.csrfCookieName;
    if (result.bearerToken && desktopBridge()) {
      bearer = result.bearerToken;
      window.clearTimeout(renewalTimer);
      const delay = Math.max(1000, Date.parse(result.bearerExpiresAt ?? '') - Date.now() - 60_000);
      renewalTimer = window.setTimeout(() => {
        void desktopSecret().then((secret) => secret ? api.exchangeBootstrap(secret) : null).catch(() => { clearBearer(); notifyUnauthorized(); });
      }, Number.isFinite(delay) ? delay : 25 * 60_000);
    }
    return result;
  },

  // Teams, roles and members (M2).
  listTeams: (cursor?: string) => request<import('./types').TeamListResult>('GET', '/api/v1/teams' + pageQuery({ cursor })),
  createTeam: (body: { name: string; directory: string; initialRoleIds: string[] }) =>
    request<import('./types').CreateTeamResult>('POST', '/api/v1/teams', body),
  renameTeam: (teamId: string, name: string, expectedRevision?: number) =>
    request<import('./types').Team>('PATCH', `/api/v1/teams/${teamId}`, { name, expectedRevision }),
  setTeamPermissionMode: (teamId: string, permissionMode: import('./types').PermissionMode, expectedRevision?: number) =>
    request<import('./types').Team>('PATCH', `/api/v1/teams/${teamId}`, { permissionMode, expectedRevision }),
  getBotStats: (botId: string) => request<import('./types').SessionStats>('GET', `/api/v1/bots/${botId}/stats`),
  setTeamReasoningEffort: (teamId: string, reasoningEffort: string, expectedRevision?: number) =>
    request<import('./types').Team>('PATCH', `/api/v1/teams/${teamId}`, { reasoningEffort, expectedRevision }),
  addBot: (teamId: string, roleId: string) =>
    request<import('./types').Bot>('POST', `/api/v1/teams/${teamId}/bots`, { roleId }),
  listRoles: () => request<import('./types').RoleListResult>('GET', '/api/v1/roles'),

  // Conversations and runs (M2).
  listMessages: (botId: string, params?: { before?: string; limit?: number }) => {
    const query = new URLSearchParams();
    if (params?.before) query.set('before', params.before);
    if (params?.limit !== undefined) query.set('limit', String(params.limit));
    const suffix = query.toString();
    return request<import('./types').ConversationResult>(
      'GET',
      `/api/v1/bots/${botId}/messages${suffix ? `?${suffix}` : ''}`,
    );
  },
  /** What passed between two members, oldest first within a page; the
   * cursor walks back to older pages. */
  listExchanges: (params: { teamId: string; botId: string; peerBotId: string; cursor?: string }) => {
    const query = new URLSearchParams({ teamId: params.teamId, peerBotId: params.peerBotId });
    if (params.cursor) query.set('cursor', params.cursor);
    return request<{ items: import('./types').Message[]; nextCursor?: string }>('GET', `/api/v1/bots/${params.botId}/collaboration?${query.toString()}`);
  },
  sendMessage: (botId: string, text: string) =>
    request<import('./types').SendMessageResult>('POST', `/api/v1/bots/${botId}/messages`, { text }),
  cancelMessage: (messageId: string) =>
    request<import('./types').Message>('POST', `/api/v1/messages/${messageId}/cancel`, {}),
  interruptRun: (runId: string) =>
    request<import('./types').InterruptRunResult>('POST', `/api/v1/runs/${runId}/interrupt`, {}),

  // Approvals (M2).
  listApprovals: (params: { teamId: string; state?: 'pending' | 'history'; requesterBotId?: string; viewBotId?: string; cursor?: string; limit?: number }) => {
    const query = new URLSearchParams({ teamId: params.teamId });
    if (params.state) query.set('state', params.state);
    if (params.requesterBotId) query.set('requesterBotId', params.requesterBotId);
    if (params.viewBotId) query.set('viewBotId', params.viewBotId);
    if (params.cursor) query.set('cursor', params.cursor);
    if (params.limit !== undefined) query.set('limit', String(params.limit));
    return request<import('./types').ApprovalListResult>('GET', `/api/v1/approvals?${query.toString()}`);
  },
  decideApproval: (approvalId: string, body: { optionId: string; comment?: string; expectedRevision: number }) =>
    request<import('./types').Approval>('POST', `/api/v1/approvals/${approvalId}/decisions`, body),

  // Engine account and models (M2).
  getEngine: () => request<import('./types').EngineStatus>('GET', '/api/v1/engine'),
  checkEngine: () => request<import('./types').EngineStatus>('POST', '/api/v1/engine/check', {}),
  engineLogin: () => request<import('./types').EngineLogin>('POST', '/api/v1/auth/login', {}),
  engineLoginCancel: () => request<{ loginCancelled: boolean }>('POST', '/api/v1/auth/login/cancel', {}),
  engineLogout: () => request<{ loggedOut: boolean }>('POST', '/api/v1/auth/logout', {}),
  listModels: () => request<import('./types').ModelListResult>('GET', '/api/v1/models'),

  // Tasks, transfers and questions (M3).
  listTasks: (params: { teamId: string; ownerId?: string; viewBotId?: string; cursor?: string; limit?: number }) => {
    const query = new URLSearchParams({ teamId: params.teamId });
    if (params.ownerId) query.set('ownerId', params.ownerId);
    if (params.viewBotId) query.set('viewBotId', params.viewBotId);
    if (params.cursor) query.set('cursor', params.cursor);
    if (params.limit !== undefined) query.set('limit', String(params.limit));
    return request<import('./types').TaskListResult>('GET', `/api/v1/teams/${params.teamId}/tasks?${query.toString()}`);
  },
  createTask: (
    teamId: string,
    body: { title: string; description?: string; acceptanceCriteria?: string; ownerId: string; parentId?: string; dependencyIds?: string[] },
  ) => request<import('./types').Task>('POST', `/api/v1/teams/${teamId}/tasks`, body),
  getTask: (taskId: string) => request<import('./types').TaskDetailResult>('GET', `/api/v1/tasks/${taskId}`),
  patchTask: (
    taskId: string,
    body: { status?: string; blockedReason?: string; ownerId?: string; expectedRevision: number },
  ) => request<import('./types').Task>('PATCH', `/api/v1/tasks/${taskId}`, body),
  submitEvidence: (
    taskId: string,
    body: { summary: string; artifactRefs?: string[]; candidateRef?: string; testResult?: string; reviewResult?: string },
  ) => request<import('./types').Evidence>('POST', `/api/v1/tasks/${taskId}/evidence`, body),
  confirmTask: (taskId: string, body: { evidenceIds?: string[]; expectedRevision: number }) =>
    request<import('./types').Task>('POST', `/api/v1/tasks/${taskId}/confirm`, body),
  offerTransfer: (taskId: string, body: { toBotId: string; note?: string }) =>
    request<import('./types').Transfer>('POST', `/api/v1/tasks/${taskId}/transfers`, body),
  acceptTransfer: (transferId: string, expectedRevision: number) =>
    request<import('./types').Task>('POST', `/api/v1/transfers/${transferId}/accept`, { expectedRevision }),
  cancelTransfer: (transferId: string) =>
    request<import('./types').Transfer>('POST', `/api/v1/transfers/${transferId}/cancel`, {}),
  listQuestions: (params: { teamId: string; status?: string; viewBotId?: string; cursor?: string; limit?: number }) => {
    const query = new URLSearchParams({ teamId: params.teamId });
    if (params.status) query.set('status', params.status);
    if (params.viewBotId) query.set('viewBotId', params.viewBotId);
    if (params.cursor) query.set('cursor', params.cursor);
    if (params.limit !== undefined) query.set('limit', String(params.limit));
    return request<import('./types').QuestionListResult>('GET', `/api/v1/questions?${query.toString()}`);
  },
  answerQuestion: (questionId: string, body: { answerText: string; expectedRevision: number }) =>
    request<import('./types').Question>('POST', `/api/v1/questions/${questionId}/answers`, body),
  cancelQuestion: (questionId: string, body: { expectedRevision: number }) =>
    request<import('./types').Question>('POST', `/api/v1/questions/${questionId}/cancel`, body),

  // Team lifecycle (M4 backend, M5 surface).
  archiveTeam: (teamId: string, expectedRevision?: number) =>
    request<import('./types').Team>('POST', `/api/v1/teams/${teamId}/archive`, {
      ...(expectedRevision !== undefined ? { expectedRevision } : {}),
    }),
  restoreTeam: (teamId: string, expectedRevision?: number) =>
    request<import('./types').Team>('POST', `/api/v1/teams/${teamId}/restore`, {
      ...(expectedRevision !== undefined ? { expectedRevision } : {}),
    }),
  deleteTeam: (teamId: string, expectedRevision?: number) =>
    request<import('./types').Team>('DELETE', `/api/v1/teams/${teamId}`, { expectedRevision }),
  deleteBot: (botId: string, expectedRevision?: number) =>
    request<import('./types').Bot>('DELETE', `/api/v1/bots/${botId}`, { expectedRevision }),

  // Activity, memories, directories, authorizations (M5).
  listActivity: (teamId: string, viewBotId?: string, cursor?: string) => request<{ items: import('./types').ActivityItem[]; nextCursor: string }>('GET', `/api/v1/teams/${teamId}/activity` + pageQuery({ viewBotId, cursor })),
  listMemories: (botId: string, cursor?: string) => request<import('./types').MemoryListResult>('GET', `/api/v1/bots/${botId}/memories` + pageQuery({ cursor })),
  deleteMemory: (botId: string, memoryId: string) =>
    request<{ deleted: boolean }>('DELETE', `/api/v1/bots/${botId}/memories/${memoryId}`),
  pickDirectory: () => request<{ path: string; cancelled: boolean }>('POST', '/api/v1/directories/pick', {}),
  warmTeam: (teamId: string) => request<{ warming: boolean }>('POST', `/api/v1/teams/${teamId}/warm`, {}),
  openTeamPath: (teamId: string, path: string, reveal = false) =>
    request<{ path: string }>('POST', `/api/v1/teams/${teamId}/open-path`, { path, reveal }),
  checkDirectory: (path: string) =>
    request<import('./types').DirectoryCheckResult>('POST', '/api/v1/directories/check', { path }),
  listAuthorizations: () =>
    request<{ items: import('./types').AuthorizationBinding[] }>('GET', '/api/v1/authorizations'),
  revokeAuthorization: (botId: string) =>
    request<{ revoked: boolean }>('POST', `/api/v1/authorizations/${botId}/revoke`, {}),
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
