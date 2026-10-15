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

let csrfCookieName = 'cobot_csrf';
function csrfToken(): string {
  const entry = document.cookie.split(';').map((value) => value.trim()).find((value) => value.startsWith(csrfCookieName + '='));
  return entry ? decodeURIComponent(entry.slice(csrfCookieName.length + 1)) : '';
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
  logout: () => request<{ loggedOut: boolean }>('POST', '/api/v1/session/logout', {}),

  // Teams, roles and members (M2).
  listTeams: () => request<import('./types').TeamListResult>('GET', '/api/v1/teams'),
  createTeam: (body: { name: string; directory: string; initialRoleIds: string[]; adoptMarker?: boolean }) =>
    request<import('./types').CreateTeamResult>('POST', '/api/v1/teams', body),
  getTeam: (teamId: string) => request<import('./types').TeamWithBots>('GET', `/api/v1/teams/${teamId}`),
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
  sendMessage: (botId: string, text: string) =>
    request<import('./types').SendMessageResult>('POST', `/api/v1/bots/${botId}/messages`, { text }),
  cancelMessage: (messageId: string) =>
    request<import('./types').Message>('POST', `/api/v1/messages/${messageId}/cancel`, {}),
  interruptRun: (runId: string) =>
    request<import('./types').InterruptRunResult>('POST', `/api/v1/runs/${runId}/interrupt`, {}),

  // Approvals (M2).
  listApprovals: (params: { teamId: string; state?: 'pending' | 'history'; requesterBotId?: string; limit?: number }) => {
    const query = new URLSearchParams({ teamId: params.teamId });
    if (params.state) query.set('state', params.state);
    if (params.requesterBotId) query.set('requesterBotId', params.requesterBotId);
    if (params.limit !== undefined) query.set('limit', String(params.limit));
    return request<import('./types').ApprovalListResult>('GET', `/api/v1/approvals?${query.toString()}`);
  },
  decideApproval: (approvalId: string, body: { optionId: string; comment?: string; expectedRevision: number }) =>
    request<import('./types').Approval>('POST', `/api/v1/approvals/${approvalId}/decisions`, body),

  // Engine account and models (M2).
  getEngine: () => request<import('./types').EngineStatus>('GET', '/api/v1/engine'),
  engineLogin: () => request<import('./types').EngineLogin>('POST', '/api/v1/auth/login', {}),
  engineLoginCancel: () => request<{ loginCancelled: boolean }>('POST', '/api/v1/auth/login/cancel', {}),
  engineLogout: () => request<{ loggedOut: boolean }>('POST', '/api/v1/auth/logout', {}),
  listModels: () => request<import('./types').ModelListResult>('GET', '/api/v1/models'),

  // Tasks, transfers, questions and collaboration records (M3).
  listTasks: (params: { teamId: string; ownerId?: string; limit?: number }) => {
    const query = new URLSearchParams({ teamId: params.teamId });
    if (params.ownerId) query.set('ownerId', params.ownerId);
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
  acceptTransfer: (transferId: string) =>
    request<import('./types').Task>('POST', `/api/v1/transfers/${transferId}/accept`, {}),
  cancelTransfer: (transferId: string) =>
    request<import('./types').Transfer>('POST', `/api/v1/transfers/${transferId}/cancel`, {}),
  listQuestions: (params: { teamId: string; status?: string; limit?: number }) => {
    const query = new URLSearchParams({ teamId: params.teamId });
    if (params.status) query.set('status', params.status);
    if (params.limit !== undefined) query.set('limit', String(params.limit));
    return request<import('./types').QuestionListResult>('GET', `/api/v1/questions?${query.toString()}`);
  },
  answerQuestion: (questionId: string, body: { answerText: string; expectedRevision: number }) =>
    request<import('./types').Question>('POST', `/api/v1/questions/${questionId}/answers`, body),
  cancelQuestion: (questionId: string, body: { expectedRevision: number }) =>
    request<import('./types').Question>('POST', `/api/v1/questions/${questionId}/cancel`, body),
  listCollaboration: (botId: string, limit?: number) => {
    const query = limit !== undefined ? `?limit=${limit}` : '';
    return request<import('./types').CollaborationListResult>('GET', `/api/v1/bots/${botId}/collaboration${query}`);
  },

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
    request<import('./types').Team>('DELETE', `/api/v1/teams/${teamId}${expectedRevision !== undefined ? `?expectedRevision=${expectedRevision}` : ''}`),
  deleteBot: (botId: string, expectedRevision?: number) =>
    request<import('./types').Bot>('DELETE', `/api/v1/bots/${botId}${expectedRevision !== undefined ? `?expectedRevision=${expectedRevision}` : ''}`),
  rebindDirectory: (teamId: string, body: { directory: string; adoptMarker?: boolean; expectedRevision?: number }) =>
    request<import('./types').Team>('POST', `/api/v1/teams/${teamId}/directory/rebind`, body),
  retryTeam: (teamId: string) =>
    request<import('./types').Team>('POST', `/api/v1/teams/${teamId}/retry`, {}),

  // Activity, memories, directories, authorizations (M5).
  listActivity: (teamId: string, limit?: number) =>
    request<{ items: import('./types').ActivityItem[]; nextCursor: string }>('GET', `/api/v1/teams/${teamId}/activity${limit !== undefined ? `?limit=${limit}` : ''}`),
  listMemories: (botId: string, limit?: number) =>
    request<import('./types').MemoryListResult>('GET', `/api/v1/bots/${botId}/memories${limit !== undefined ? `?limit=${limit}` : ''}`),
  deleteMemory: (botId: string, memoryId: string) =>
    request<{ deleted: boolean }>('DELETE', `/api/v1/bots/${botId}/memories/${memoryId}`),
  listDirectories: (path?: string) =>
    request<import('./types').DirectoryListResult>('GET', `/api/v1/directories${path ? `?path=${encodeURIComponent(path)}` : ''}`),
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
