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
  listChats: (cursor = '', filter: { pinned?: string; archived?: boolean; sort?: string; search?: string; section?: string } = {}) => request<import('./types').ChatList>('GET', '/api/v1/chats' + pageQuery({ cursor, ...filter, archived: filter.archived ? 'true' : undefined })),
  searchConversations: (q: string) => request<import('./types').SearchResults>('GET', '/api/v1/search' + pageQuery({ q })),
  chatNavigation: () => request<import('./types').ChatNavigation>('GET', '/api/v1/chat-navigation'),
  changeChatNavigation: (body: import('./types').ChatNavigationInput) => request<import('./types').ChatNavigation>('POST', '/api/v1/chat-navigation', body),
  organizeChat: (id: string, action: 'pin' | 'unpin' | 'move' | 'reorder' | 'archive' | 'restore', body: { expectedRevision: number; sectionId?: string; beforeId?: string }) => request<import('./types').Chat>('POST', `/api/v1/chats/${encodeURIComponent(id)}/actions/${action}`, body),
  getChat: (id: string, cursor = '') => request<import('./types').ChatPage>('GET', `/api/v1/chats/${encodeURIComponent(id)}` + (cursor ? '?cursor=' + encodeURIComponent(cursor) : '')),
  sendChat: (id: string | null, body: import('./types').ChatSendInput) => request<import('./types').ChatPage>('POST', id ? `/api/v1/chats/${encodeURIComponent(id)}/messages` : '/api/v1/chats', body),
  renameChat: (id: string, title: string, expectedRevision: number) => request<import('./types').Chat>('PATCH', `/api/v1/chats/${encodeURIComponent(id)}`, { title, expectedRevision }),
  deleteChat: (id: string, expectedRevision: number) => request<import('./types').Chat>('DELETE', `/api/v1/chats/${encodeURIComponent(id)}`, { expectedRevision }),
  chatRunAction: (id: string, runId: string, action: 'interrupt' | 'verify') => request<import('./types').ChatRun>('POST', `/api/v1/chats/${encodeURIComponent(id)}/runs/${encodeURIComponent(runId)}/${action}`, {}),
  editChatMessage: (id: string, messageId: string, text: string) => request<import('./types').ChatPage>('POST', `/api/v1/chats/${encodeURIComponent(id)}/messages/${encodeURIComponent(messageId)}/edit`, { text }),
  editMessage: (messageId: string, text: string) => request<import('./types').SendMessageResult>('POST', `/api/v1/messages/${encodeURIComponent(messageId)}/edit`, { text }),
  replyChatRequest: (id: string, requestId: string, body: { expectedRevision: number; optionId?: string; answers?: Record<string, string[]> }) => request<import('./types').ChatRequest>('POST', `/api/v1/chats/${encodeURIComponent(id)}/requests/${encodeURIComponent(requestId)}/reply`, body),
  openChatPath: (id: string, path: string, reveal = false) => request<{ path: string }>('POST', `/api/v1/chats/${encodeURIComponent(id)}/open-path`, { path, reveal }),
  getStatus: async () => {
    const status = await request<import('./types').Status>('GET', '/api/v1/status');
    if (status.csrfCookieName) csrfCookieName = status.csrfCookieName;
    return status;
  },
  getTeamBudget: (teamId: string) => request<import('./types').TeamBudget>('GET', `/api/v1/teams/${teamId}/budget`),
  setTeamBudget: (teamId: string, limit: number | null) =>
    request<{ limit: number | null; clamped: number }>('PUT', `/api/v1/teams/${teamId}/budget`, { limit }),
  setBotBudget: (botId: string, limit: number | null) =>
    request<{ limit: number | null; clamped: number }>('PUT', `/api/v1/bots/${botId}/budget`, { limit }),
  resetTeamBudget: (teamId: string) => request<{ reset: boolean }>('POST', `/api/v1/teams/${teamId}/budget/reset`, {}),
  resetBotBudget: (botId: string) => request<{ reset: boolean }>('POST', `/api/v1/bots/${botId}/budget/reset`, {}),
  modelCatalog: () => request<import('./types').ModelCatalogStatus>('GET', '/api/v1/model-catalog'),
  updateModelCatalog: () => request<import('./types').ModelCatalogUpdate>('POST', '/api/v1/model-catalog/update', {}),
  applyEngineCatalog: () => request<import('./types').ApplyEngineCatalogResult>('POST', '/api/v1/model-catalog/apply', {}),
  resolveModels: (models: import('./types').ProviderModel[]) =>
    request<{ specs: Record<string, import('./types').ModelSpec> }>('POST', '/api/v1/model-catalog/resolve', { models }),
  checkForUpdate: () => request<import('./types').UpdateCheck>('POST', '/api/v1/updates/check', {}),
  getSettings: () => request<import('./types').Settings>('GET', '/api/v1/settings'),
  updateSettings: (patch: import('./types').SettingsPatch, expectedRevision?: number) =>
    request<import('./types').Settings>('PATCH', '/api/v1/settings', {
      patch,
      ...(expectedRevision !== undefined ? { expectedRevision } : {}),
    }),
  getSnapshot: () => request<import('./types').Snapshot>('GET', '/api/v1/snapshot'),
  listProviders: () => request<import('./types').ProviderListResult>('GET', '/api/v1/providers'),
  listProviderPresets: () => request<import('./types').ProviderPresetListResult>('GET', '/api/v1/provider-presets'),
  getProvider: (id: string) => request<import('./types').Provider>('GET', `/api/v1/providers/${id}`),
  createProvider: (body: import('./types').ProviderRequest) =>
    request<import('./types').Provider>('POST', '/api/v1/providers', body),
  updateProvider: (id: string, body: import('./types').ProviderRequest) =>
    request<import('./types').Provider>('PATCH', `/api/v1/providers/${id}`, body),
  deleteProvider: (id: string) => request<{ deleted: boolean }>('DELETE', `/api/v1/providers/${id}`),
  setProviderEnabled: (id: string, enabled: boolean, expectedRevision?: number) =>
    request<import('./types').Provider>('PUT', `/api/v1/providers/${id}/enabled`, { enabled, expectedRevision }),
  reprobeProvider: (id: string) =>
    request<import('./types').ProviderProbeResults>('POST', `/api/v1/providers/${id}/probe`),
  detectProvider: (apiKey: string) =>
    request<{ presetId: string }>('POST', '/api/v1/providers/detect', { apiKey }),
  // Without a typed key, providerId names a stored provider whose own key
  // the server uses; baseUrl has to be one of that provider's endpoints.
  discoverProviderModels: (baseUrl: string, apiKey?: string, providerId?: string) =>
    request<import('./types').ProviderDiscoverResult>('POST', '/api/v1/providers/discover', {
      baseUrl,
      ...(apiKey ? { apiKey } : providerId ? { providerId } : {}),
    }),
  probeProviderModels: (baseUrl: string, modelIds: string[], apiKey?: string, providerId?: string) =>
    request<import('./types').ProviderProbeResults>('POST', '/api/v1/providers/probe-models', {
      baseUrl,
      modelIds,
      ...(apiKey ? { apiKey } : providerId ? { providerId } : {}),
    }),
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
  setBotModel: (botId: string, providerId: string, model: string, expectedRevision?: number) =>
    request<import('./types').Bot>('PUT', `/api/v1/bots/${botId}/model`, { providerId, model, expectedRevision }),
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
  retryRun: (runId: string) => request<{ runId: string; requeued: number }>('POST', `/api/v1/runs/${runId}/retry`, {}),
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
  getAvatarCatalog: () => request<import('./types').AvatarCatalog>('GET', '/api/v1/avatars/catalog'),
  syncAvatarCatalog: () => request<import('./types').AvatarCatalog>('POST', '/api/v1/avatars/catalog/sync', {}),
  setBotAvatar: (botId: string, avatar: import('./types').AvatarRef | null, expectedRevision: number) =>
    request<import('./types').Bot>('PATCH', `/api/v1/bots/${botId}`, { avatar, expectedRevision }),
  saveAvatarUpload: (data: string) => request<{ file: string }>('POST', '/api/v1/avatars/uploads', { data }),
  /** A stored picture as a blob URL; fetched with the session like any call,
   * which a plain <img src> cannot do in the desktop shell. */
  getAvatarUpload: async (file: string) => {
    const token = bearerToken();
    const resp = await fetch(apiUrl(`/api/v1/avatars/uploads/${encodeURIComponent(file)}`), {
      headers: token ? { Authorization: 'Bearer ' + token } : {},
      credentials: token ? 'omit' : 'include',
    });
    if (!resp.ok) throw new Error(`avatar picture ${resp.status}`);
    return URL.createObjectURL(await resp.blob());
  },
  setRoleAvatar: (roleId: string, avatar: import('./types').AvatarRef | null) =>
    request<{ roleId: string }>('PUT', `/api/v1/roles/${encodeURIComponent(roleId)}/avatar`, { avatar }),
  engineLogin: () => request<import('./types').EngineLogin>('POST', '/api/v1/auth/login', {}),
  engineLoginCancel: () => request<{ loginCancelled: boolean }>('POST', '/api/v1/auth/login/cancel', {}),
  engineLogout: () => request<{ loggedOut: boolean }>('POST', '/api/v1/auth/logout', {}),
  listModels: () => request<import('./types').ModelListResult>('GET', '/api/v1/models'),

  // Requirements, cards and questions.
  listTasks: (params: { teamId: string; ownerId?: string; viewBotId?: string; cursor?: string; limit?: number }) => {
    const query = new URLSearchParams({ teamId: params.teamId });
    if (params.ownerId) query.set('ownerId', params.ownerId);
    if (params.viewBotId) query.set('viewBotId', params.viewBotId);
    if (params.cursor) query.set('cursor', params.cursor);
    if (params.limit !== undefined) query.set('limit', String(params.limit));
    return request<import('./types').TaskListResult>('GET', `/api/v1/teams/${params.teamId}/tasks?${query.toString()}`);
  },
  getTask: (taskId: string) => request<import('./types').TaskDetailResult>('GET', `/api/v1/tasks/${taskId}`),
  listRequirements: (teamId: string) =>
    request<import('./types').RequirementListResult>('GET', `/api/v1/teams/${teamId}/requirements`),
  cancelRequirement: (requirementId: string) =>
    request<import('./types').Requirement>('POST', `/api/v1/requirements/${requirementId}/cancel`, {}),
  listQuestions: (params: { teamId: string; status?: string; viewBotId?: string; cursor?: string; limit?: number }) => {
    const query = new URLSearchParams({ teamId: params.teamId });
    if (params.status) query.set('status', params.status);
    if (params.viewBotId) query.set('viewBotId', params.viewBotId);
    if (params.cursor) query.set('cursor', params.cursor);
    if (params.limit !== undefined) query.set('limit', String(params.limit));
    return request<import('./types').QuestionListResult>('GET', `/api/v1/questions?${query.toString()}`);
  },
  answerQuestion: (questionId: string, body: { answerText: string; decision?: 'accept' | 'revise'; mode?: 'rerun' | 'return_to_me'; expectedRevision: number }) =>
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
  getTeamBrowser: (teamId: string) => request<BrowserPanelState>('GET', `/api/v1/teams/${teamId}/browser`),
  openBrowserTab: (teamId: string, url: string) =>
    request<{ tab: BrowserTab }>('POST', `/api/v1/teams/${teamId}/browser/tabs`, { url }),
  closeBrowserTab: (teamId: string, tab: string) =>
    request<{ ok: boolean }>('DELETE', `/api/v1/teams/${teamId}/browser/tabs/${encodeURIComponent(tab)}`),
  setBrowserTabViewport: (teamId: string, tab: string, body: BrowserViewportRequest) =>
    request<{ viewport: BrowserViewport | null }>('POST', `/api/v1/teams/${teamId}/browser/tabs/${encodeURIComponent(tab)}/viewport`, body),
  checkDirectory: (path: string) =>
    request<import('./types').DirectoryCheckResult>('POST', '/api/v1/directories/check', { path }),
};

import { desktopBridge } from '../platform/native-bridge';

export interface BrowserViewport { width: number; height: number; mobile?: boolean }
export interface BrowserTab {
  id: string;
  /** "member": a member's page; "user": a tab the user opened themselves. */
  owner: 'member' | 'user';
  /** The bot id, present for a member's tab only. */
  memberId?: string;
  url: string;
  title: string;
  /** A member's call is working in this tab right now. */
  busy?: boolean;
  /** The window size the page renders at, once someone set one. */
  viewport?: BrowserViewport;
}
export interface BrowserPanelState { available: boolean; running: boolean; tabs: BrowserTab[] }
/** One set-viewport call: a preset or an explicit size; see POST .../viewport. */
export interface BrowserViewportRequest { preset?: 'desktop' | 'tablet' | 'phone' | 'clear'; width?: number; height?: number; mobile?: boolean }
export interface BrowserFrame { tab: string; width: number; height: number; data: string }
/** One thing the user did in the live view; see POST /teams/{id}/browser/input. */
export interface BrowserInput {
  kind: 'mouse' | 'wheel' | 'key' | 'text' | 'nav';
  action?: string; x?: number; y?: number; button?: string; clickCount?: number; dx?: number; dy?: number;
  key?: string; code?: string; vk?: number; text?: string; modifiers?: number; to?: string;
}

function authHeaders(writing: boolean): Record<string, string> {
  const headers: Record<string, string> = {};
  const token = bearerToken();
  if (token) headers.Authorization = 'Bearer ' + token;
  else if (writing) headers['X-CSRF-Token'] = csrfToken();
  return headers;
}

/** Applies one input to a tab of the team's browser. Inputs are never retried:
 * replaying a click would click twice. */
export async function sendBrowserInput(teamId: string, tab: string, input: BrowserInput): Promise<void> {
  await initializeTransport();
  const resp = await fetch(apiUrl(`/api/v1/teams/${teamId}/browser/input`), {
    method: 'POST',
    headers: { ...authHeaders(true), 'Content-Type': 'application/json' },
    credentials: bearerToken() ? 'omit' : 'include',
    // Every write names itself; an input is never replayed, so each is new.
    body: JSON.stringify({ ...input, tab, requestId: newRequestId() }),
  });
  if (resp.status === 401) { clearBearer(); notifyUnauthorized(); }
  if (!resp.ok) {
    const envelope = (await resp.json().catch(() => undefined)) as ErrorEnvelope | undefined;
    throw new ApiError(resp.status, envelope?.error?.code ?? 'internal', envelope?.error?.messageKey ?? 'error.internal', envelope?.error?.params ?? {});
  }
}

/** Follows a tab's pictures until the signal aborts or the stream ends.
 * Resolves with the reason the server gave when it ended the stream. */
export async function streamBrowserTab(teamId: string, tab: string, onFrame: (frame: BrowserFrame) => void, signal: AbortSignal): Promise<string | null> {
  await initializeTransport();
  const resp = await fetch(apiUrl(`/api/v1/teams/${teamId}/browser/stream?tab=${encodeURIComponent(tab)}`), {
    headers: { ...authHeaders(false), Accept: 'text/event-stream' },
    credentials: bearerToken() ? 'omit' : 'include',
    signal,
  });
  if (resp.status === 401) { clearBearer(); notifyUnauthorized(); }
  if (!resp.ok || !resp.body) {
    const envelope = (await resp.json().catch(() => undefined)) as ErrorEnvelope | undefined;
    throw new ApiError(resp.status, envelope?.error?.code ?? 'internal', envelope?.error?.messageKey ?? 'error.internal', envelope?.error?.params ?? {});
  }
  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  for (;;) {
    const { done, value } = await reader.read();
    if (done) return null;
    buffer += decoder.decode(value, { stream: true });
    let idx = buffer.indexOf('\n\n');
    while (idx >= 0) {
      const block = buffer.slice(0, idx);
      buffer = buffer.slice(idx + 2);
      const lines = block.split('\n');
      const data = lines.find((line) => line.startsWith('data: '))?.slice(6);
      if (data && lines.includes('event: frame')) onFrame(JSON.parse(data) as BrowserFrame);
      else if (data && lines.includes('event: ended')) return JSON.parse(data) as string;
      idx = buffer.indexOf('\n\n');
    }
  }
}

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
