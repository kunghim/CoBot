import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type QueryKey } from '@tanstack/react-query';
import { useEffect, useRef } from 'react';
import { applyLiveSignal } from './live';
import type { AvatarRef, Event, LiveSignal, PermissionMode, Settings, SettingsPatch } from './types';
import { api, apiUrl, bearerToken, reportUnauthorized } from './client';

export const queryKeys = {
  status: ['status'] as const,
  settings: ['settings'] as const,
  snapshot: ['snapshot'] as const,
  teams: ['teams'] as const,
  roles: ['roles'] as const,
  engine: ['engine'] as const,
  avatarCatalog: ['avatarCatalog'] as const,
  models: ['models'] as const,
  providers: ['providers'] as const,
  providerPresets: ['providerPresets'] as const,
  messages: (botId: string) => ['messages', botId] as const,
  exchanges: (botId: string, peerBotId: string) => ['exchanges', botId, peerBotId] as const,
  approvals: (teamId: string, state: 'pending' | 'history') => ['approvals', teamId, state] as const,
  tasks: (teamId: string) => ['tasks', teamId] as const,
  taskDetail: (taskId: string) => ['taskDetail', taskId] as const,
  requirements: (teamId: string) => ['requirements', teamId] as const,
  questions: (teamId: string, status: string) => ['questions', teamId, status] as const,
  conversationSearch: (q: string) => ['conversationSearch', q] as const,
};

export function useStatus() {
  return useQuery({ queryKey: queryKeys.status, queryFn: api.getStatus, retry: false });
}

/** One lookup per settled query; an empty query closes the field without a request. */
export function useConversationSearch(query: string) {
  return useQuery({ queryKey: queryKeys.conversationSearch(query), queryFn: () => api.searchConversations(query), enabled: query !== '', staleTime: 30_000 });
}

/** GET /teams/{id}/budget: a team's daily limit, each member's, and today's spend. */
export function useTeamBudget(teamId: string, enabled = true) {
  return useQuery({ queryKey: ['teamBudget', teamId], queryFn: () => api.getTeamBudget(teamId), enabled, staleTime: 0 });
}

/** Sets the team's own daily limit (null follows the default, 0 is none). */
export function useSetTeamBudget(teamId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (limit: number | null) => api.setTeamBudget(teamId, limit),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['teamBudget', teamId] }),
  });
}

/** Sets a member's own daily limit the same way. */
export function useSetBotBudget(teamId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ botId, limit }: { botId: string; limit: number | null }) => api.setBotBudget(botId, limit),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['teamBudget', teamId] }),
  });
}

/** Starts counting today's spend again from now, for the team and its members. */
export function useResetTeamBudget(teamId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.resetTeamBudget(teamId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['teamBudget', teamId] }),
  });
}

/** Starts counting one member's spend again from now. */
export function useResetBotBudget(teamId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (botId: string) => api.resetBotBudget(botId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['teamBudget', teamId] }),
  });
}

/** GET /model-catalog: which model catalog is in use. */
export function useModelCatalog() {
  return useQuery({ queryKey: ['modelCatalog'], queryFn: api.modelCatalog });
}

/** POST /model-catalog/apply: stops the idle engines that run an older model catalog. */
export function useApplyEngineCatalog() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.applyEngineCatalog(),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['modelCatalog'] }),
  });
}

/** POST /model-catalog/update: downloads the newest model catalog when the user presses the button. */
export function useUpdateModelCatalog() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.updateModelCatalog(),
    onSuccess: (result) => {
      if (result.state !== 'updated') return;
      qc.invalidateQueries({ queryKey: ['modelCatalog'] });
      // What providers know about their models comes from the catalog.
      qc.invalidateQueries({ queryKey: queryKeys.providers });
    },
  });
}

/** POST /updates/check: asks GitHub for the newest release when the user presses the button. */
export function useCheckForUpdate() {
  return useMutation({ mutationFn: () => api.checkForUpdate() });
}

export function useSnapshot(enabled: boolean) {
  return useQuery({ queryKey: queryKeys.snapshot, queryFn: api.getSnapshot, enabled });
}

export function useSettings(enabled: boolean) {
  return useQuery({ queryKey: queryKeys.settings, queryFn: api.getSettings, enabled });
}

export function useUpdateSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ patch, expectedRevision }: { patch: SettingsPatch; expectedRevision?: number }) =>
      api.updateSettings(patch, expectedRevision),
    onSuccess: (settings: Settings) => {
      qc.setQueryData(queryKeys.settings, settings);
      qc.invalidateQueries({ queryKey: queryKeys.snapshot });
    },
  });
}

/**
 * useEventStream subscribes to the durable SSE stream and invokes onEvent for
 * every frame. The cursor survives reconnects; the server may legally skip
 * seq numbers under scope filters, so only the cursor itself gates resync.
 *
 * Browser sessions use EventSource (cookie auth). The desktop shell cannot
 * attach headers to EventSource, so bearer sessions use a fetch reader.
 */
export function useEventStream(enabled: boolean, onEvent: (event: Event) => void, snapshotSequence = 0): void {
  const cursor = useRef(0);
  useEffect(() => {
    if (!enabled) { cursor.current = 0; return; }
    let lastSeq = Math.max(cursor.current, snapshotSequence);
    let stopped = false;

    const handleFrame = (payload: string) => {
      try {
        const event = JSON.parse(payload) as Event;
        lastSeq = Math.max(lastSeq, event.seq);
        cursor.current = lastSeq;
        onEvent(event);
      } catch {
        // A malformed frame never kills the stream; snapshot re-sync covers gaps.
      }
    };
    // Live frames carry no cursor; a lost one is covered by the stored steps.
    const handleLive = (payload: string) => {
      try {
        applyLiveSignal(JSON.parse(payload) as LiveSignal);
      } catch {
        // ignore malformed progress
      }
    };

    if (!bearerToken()) {
      let source: EventSource | null = null;
      let retryTimer = 0;
      const connect = () => {
        if (stopped) return;
        // lastSeq is shared with handleFrame: every reconnect resumes from
        // the cursor instead of replaying from zero (review C).
        source = new EventSource(apiUrl(`/api/v1/events?after=${lastSeq}`));
        source.addEventListener('cobot.event', (message) => handleFrame((message as MessageEvent).data));
        source.addEventListener('cobot.live', (message) => handleLive((message as MessageEvent).data));
        source.onerror = () => {
          if (stopped) return;
          source?.close();
          // The browser cannot read the error status from EventSource, so the
          // session is probed once before reconnecting: an expired session
          // becomes a re-auth, a transient blip retries with backoff.
          void fetch(apiUrl('/api/v1/status'), { credentials: 'include' })
            .then(async (resp) => {
              if (stopped) return;
              if (resp.status === 401) {
                reportUnauthorized();
                return;
              }
              const body = (await resp.json().catch(() => null)) as { data?: { authorized?: boolean } } | null;
              if (stopped) return;
              if (!body?.data?.authorized) {
                reportUnauthorized();
                return;
              }
              retryTimer = window.setTimeout(connect, 1500);
            })
            .catch(() => {
              if (!stopped) retryTimer = window.setTimeout(connect, 1500);
            });
        };
      };
      connect();
      return () => {
        stopped = true;
        window.clearTimeout(retryTimer);
        source?.close();
      };
    }

    // Bearer (desktop) mode: fetch-based SSE with explicit reconnect.
    const controller = new AbortController();
    const run = async () => {
      while (!stopped) {
        try {
          const resp = await fetch(apiUrl(`/api/v1/events?after=${lastSeq}`), {
            headers: { Accept: 'text/event-stream', Authorization: 'Bearer ' + bearerToken() },
            credentials: 'omit',
            signal: controller.signal,
          });
          if (resp.status === 401) {
            reportUnauthorized();
            return;
          }
          if (!resp.ok || !resp.body) throw new Error('stream unavailable');
          const reader = resp.body.getReader();
          const decoder = new TextDecoder();
          let buffer = '';
          for (;;) {
            const { done, value } = await reader.read();
            if (done) break;
            buffer += decoder.decode(value, { stream: true });
            let idx = buffer.indexOf('\n\n');
            while (idx >= 0) {
              const frame = buffer.slice(0, idx);
              buffer = buffer.slice(idx + 2);
              const lines = frame.split('\n');
              const dataLine = lines.find((line) => line.startsWith('data: '));
              const live = lines.some((line) => line === 'event: cobot.live');
              if (dataLine) (live ? handleLive : handleFrame)(dataLine.slice(6));
              idx = buffer.indexOf('\n\n');
            }
          }
        } catch {
          // fall through to reconnect delay
        }
        if (stopped) return;
        await new Promise((resolve) => setTimeout(resolve, 1500));
      }
    };
    void run();
    return () => {
      stopped = true;
      controller.abort();
    };
    // A snapshot seeds a new stream. Later query refreshes must not tear it
    // down; reconnects continue from the cursor advanced by received frames.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, onEvent]);
}

// ---------------------------------------------------------------------------
// M2: teams, conversations, approvals, engine, models
// ---------------------------------------------------------------------------

/** GET /teams: teams with their members, refreshed by the SSE invalidation. */
function usePagedItems<P extends { items: unknown[]; nextCursor: string }>(key: QueryKey, load: (cursor: string) => Promise<P>, enabled: boolean, chronological = false) {
  const query = useInfiniteQuery({ queryKey: key, queryFn: ({ pageParam }) => load(pageParam), initialPageParam: '', getNextPageParam: (page) => page.nextCursor || undefined, enabled });
  const pages = query.data?.pages;
  const ordered = chronological && pages ? [...pages].reverse() : pages;
  const data = pages?.[0] ? { ...pages[0], items: ordered!.flatMap((page) => page.items), nextCursor: pages[pages.length - 1].nextCursor } as P : undefined;
  return { ...query, data };
}

export function useTeams(enabled = true) {
  return usePagedItems(queryKeys.teams, (cursor) => api.listTeams(cursor), enabled);
}

/** The tabs of a team's browser; polled, because tabs open and close as members work. */
export function useTeamBrowser(teamId: string, enabled = true) {
  return useQuery({ queryKey: ['teamBrowser', teamId], queryFn: () => api.getTeamBrowser(teamId), enabled, refetchInterval: 2000 });
}

/** Opens a page in a tab the user owns; the browser comes up when it is down. */
export function useOpenBrowserTab(teamId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (url: string) => api.openBrowserTab(teamId, url),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['teamBrowser', teamId] }),
  });
}

/** Closes one tab of the team's browser, a member's page or the user's own. */
export function useCloseBrowserTab(teamId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (tab: string) => api.closeBrowserTab(teamId, tab),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['teamBrowser', teamId] }),
  });
}

/** Sets the window size the page really renders at, for the user and members alike. */
export function useSetBrowserTabViewport(teamId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ tab, body }: { tab: string; body: import('./client').BrowserViewportRequest }) => api.setBrowserTabViewport(teamId, tab, body),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['teamBrowser', teamId] }),
  });
}

export function useCreateTeam() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { name: string; directory: string; initialRoleIds: string[] }) => api.createTeam(input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.teams });
      void qc.invalidateQueries({ queryKey: queryKeys.snapshot });
      void qc.invalidateQueries({ queryKey: queryKeys.roles });
    },
  });
}

export function useRoles(enabled = true) {
  return useQuery({ queryKey: queryKeys.roles, queryFn: api.listRoles, enabled });
}

/**
 * The model providers of this profile. The list is small and changes rarely,
 * so it is cached until something writes to it.
 */
export function useProviders(enabled = true) {
  return useQuery({ queryKey: queryKeys.providers, queryFn: api.listProviders, enabled });
}

/** The built-in vendor catalogue the settings page offers. */
export function useProviderPresets(enabled = true) {
  return useQuery({ queryKey: queryKeys.providerPresets, queryFn: api.listProviderPresets, enabled, staleTime: Infinity });
}

export function useSaveProvider() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id?: string; body: import('./types').ProviderRequest }) =>
      id ? api.updateProvider(id, body) : api.createProvider(body),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.providers });
      void qc.invalidateQueries({ queryKey: queryKeys.snapshot });
      // A provider's models decide what the engines should be started with.
      void qc.invalidateQueries({ queryKey: ['modelCatalog'] });
    },
  });
}

export function useDeleteProvider() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteProvider(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.providers });
      void qc.invalidateQueries({ queryKey: queryKeys.snapshot });
    },
  });
}

/** Offers a provider's models to members, or stops offering them. */
export function useSetProviderEnabled() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, enabled, expectedRevision }: { id: string; enabled: boolean; expectedRevision?: number }) =>
      api.setProviderEnabled(id, enabled, expectedRevision),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.providers });
      void qc.invalidateQueries({ queryKey: queryKeys.snapshot });
    },
  });
}

/**
 * Checks a stored provider's models again. The key is read on the server, so
 * this works long after the provider was set up.
 */
export function useReprobeProvider() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.reprobeProvider(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.providers });
    },
  });
}

/** GET /bots/{id}/messages: one conversation plus queue depth and run state. */
export function useConversation(botId: string | null) {
  const query = useInfiniteQuery({ queryKey: queryKeys.messages(botId ?? ''), queryFn: ({ pageParam }) => api.listMessages(botId as string, { before: pageParam || undefined }), initialPageParam: '', getNextPageParam: (page) => page.nextCursor || undefined, enabled: botId !== null });
  const pages = query.data?.pages;
  return { ...query, data: pages?.[0] ? { ...pages[0], messages: [...pages].reverse().flatMap((page) => page.messages), runs: [...pages].reverse().flatMap((page) => page.runs ?? []) } : undefined };
}

/** Everything two members sent each other, oldest first; fetchNextPage
 * reaches further back. */
export function useExchanges(teamId: string, botId: string, peerBotId: string) {
  const query = useInfiniteQuery({
    queryKey: queryKeys.exchanges(botId, peerBotId),
    queryFn: ({ pageParam }) => api.listExchanges({ teamId, botId, peerBotId, cursor: pageParam || undefined }),
    initialPageParam: '',
    getNextPageParam: (page) => page.nextCursor || undefined,
  });
  const messages = [...(query.data?.pages ?? [])].reverse().flatMap((page) => page.items);
  return { ...query, messages };
}

export function useSendMessage(botId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (text: string) => api.sendMessage(botId, text),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.messages(botId) });
      void qc.invalidateQueries({ queryKey: queryKeys.teams });
    },
  });
}

export function useCancelMessage(botId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (messageId: string) => api.cancelMessage(messageId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.messages(botId) });
    },
  });
}

export function useEditMessage(botId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ messageId, text }: { messageId: string; text: string }) => api.editMessage(messageId, text),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.messages(botId) });
      void qc.invalidateQueries({ queryKey: queryKeys.teams });
    },
  });
}

export function useEditChatMessage(chatId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ messageId, text }: { messageId: string; text: string }) => api.editChatMessage(chatId, messageId, text),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['chat', chatId] });
      void qc.invalidateQueries({ queryKey: ['chats'] });
    },
  });
}

export function useRetryRun(botId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (runId: string) => api.retryRun(runId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.messages(botId) });
      void qc.invalidateQueries({ queryKey: queryKeys.teams });
    },
  });
}

export function useInterruptRun(botId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (runId: string) => api.interruptRun(runId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.messages(botId) });
      void qc.invalidateQueries({ queryKey: queryKeys.teams });
    },
  });
}

/** GET /approvals?teamId&state: pending cards or decided history. */
export function useApprovals(teamId: string | null, state: 'pending' | 'history', enabled = true, viewBotId?: string) {
  return usePagedItems([...queryKeys.approvals(teamId ?? '', state), viewBotId ?? ''], (cursor) => api.listApprovals({ teamId: teamId as string, state, viewBotId, cursor }), teamId !== null && enabled);
}

export function useDecideApproval() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ approvalId, optionId, expectedRevision, content }: { approvalId: string; optionId: string; expectedRevision: number; content?: Record<string, unknown> }) =>
      api.decideApproval(approvalId, { optionId, expectedRevision, content }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['approvals'] });
      void qc.invalidateQueries({ queryKey: queryKeys.snapshot });
      void qc.invalidateQueries({ queryKey: queryKeys.teams });
    },
  });
}

/** GET /engine: the real engine projection; the snapshot copy is only a
 * cheap cached hint. */
/** The avatar catalog cached by the core. While a sync runs it is re-read
 * until the sync ends; a finished download also arrives as an event. */
export function useAvatarCatalog(enabled = true) {
  return useQuery({
    queryKey: queryKeys.avatarCatalog,
    queryFn: api.getAvatarCatalog,
    enabled,
    staleTime: 60_000,
    refetchInterval: (query) => (query.state.data?.status.syncing ? 2_000 : false),
  });
}

/** A stored avatar picture as a blob URL; pictures never change. */
export function useAvatarUpload(file: string | undefined) {
  return useQuery({
    queryKey: ['avatarUpload', file ?? ''],
    queryFn: () => api.getAvatarUpload(file!),
    enabled: !!file,
    staleTime: Infinity,
    gcTime: Infinity,
  });
}

/** Sets or clears a member's own avatar. */
export function useSetBotAvatar() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ botId, avatar, expectedRevision }: { botId: string; avatar: AvatarRef | null; expectedRevision: number }) =>
      api.setBotAvatar(botId, avatar, expectedRevision),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.teams }),
  });
}

/** Sets or clears a role's avatar. */
export function useSetRoleAvatar() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ roleId, avatar }: { roleId: string; avatar: AvatarRef | null }) => api.setRoleAvatar(roleId, avatar),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.roles }),
  });
}

export function useSyncAvatarCatalog() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.syncAvatarCatalog(),
    onSuccess: (catalog) => qc.setQueryData(queryKeys.avatarCatalog, catalog),
  });
}

export function useEngine(enabled = true) {
  return useQuery({ queryKey: queryKeys.engine, queryFn: api.getEngine, enabled, staleTime: 0 });
}

export function useEngineLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.engineLogin(),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.engine });
    },
  });
}

export function useEngineLoginCancel() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.engineLoginCancel(),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.engine });
    },
  });
}

export function useEngineLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.engineLogout(),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.engine });
    },
  });
}

/** POST /engine/check: re-runs the self-check, including native hook trust,
 * and replaces the cached engine projection with the fresh one. */
export function useEngineCheck() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.checkEngine(),
    onSuccess: (data) => {
      qc.setQueryData(queryKeys.engine, data);
      void qc.invalidateQueries({ queryKey: queryKeys.models });
    },
  });
}

/** GET /models: only meaningful while the engine is ready; the endpoint
 * spawns the control server, so callers enable it conditionally. */
export function useEngineModels(enabled: boolean) {
  return useQuery({ queryKey: queryKeys.models, queryFn: api.listModels, enabled });
}

// ---------------------------------------------------------------------------
// Requirements, cards, questions
// ---------------------------------------------------------------------------

/** GET /teams/{id}/tasks: the team's cards; the server narrows a member's
 * scope, the page never decides who sees what. */
export function useTasks(teamId: string | null, enabled = true, viewBotId?: string) {
  return usePagedItems([...queryKeys.tasks(teamId ?? ''), viewBotId ?? ''], (cursor) => api.listTasks({ teamId: teamId as string, viewBotId, cursor }), teamId !== null && enabled);
}

/** GET /tasks/{id}: one card with its handoff records, loaded only while the
 * row is expanded. */
export function useTaskDetail(taskId: string | null, enabled = true) {
  return useQuery({
    queryKey: queryKeys.taskDetail(taskId ?? ''),
    queryFn: () => api.getTask(taskId as string),
    enabled: taskId !== null && enabled,
  });
}

/** GET /teams/{id}/requirements: the team's requirements with their routes. */
export function useRequirements(teamId: string | null, enabled = true) {
  return useQuery({
    queryKey: queryKeys.requirements(teamId ?? ''),
    queryFn: () => api.listRequirements(teamId as string),
    enabled: teamId !== null && enabled,
  });
}

/** POST /requirements/{id}/cancel: the user stops a requirement. */
export function useCancelRequirement() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (requirementId: string) => api.cancelRequirement(requirementId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['requirements'] });
      void qc.invalidateQueries({ queryKey: ['tasks'] });
      void qc.invalidateQueries({ queryKey: ['taskDetail'] });
      void qc.invalidateQueries({ queryKey: ['questions'] });
      void qc.invalidateQueries({ queryKey: queryKeys.teams });
    },
  });
}

/** GET /questions?status=open: the answers the user still owes. */
export function useQuestions(teamId: string | null, status = 'open', enabled = true, viewBotId?: string) {
  return usePagedItems([...queryKeys.questions(teamId ?? '', status), viewBotId ?? ''], (cursor) => api.listQuestions({ teamId: teamId as string, status, viewBotId, cursor }), teamId !== null && enabled);
}

export function useAnswerQuestion() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      questionId,
      answerText,
      decision,
      mode,
      expectedRevision,
    }: {
      questionId: string;
      answerText: string;
      decision?: 'accept' | 'revise' | 'approve' | 'reject';
      /** For revise on a confirmation: where the fixed work goes. */
      mode?: 'rerun' | 'return_to_me';
      expectedRevision: number;
    }) => api.answerQuestion(questionId, { answerText, decision, mode, expectedRevision }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['questions'] });
      void qc.invalidateQueries({ queryKey: queryKeys.teams });
      // The answer is delivered as a user message to the asking bot.
      void qc.invalidateQueries({ queryKey: ['messages'] });
    },
  });
}

export function useCancelQuestion(teamId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ questionId, expectedRevision }: { questionId: string; expectedRevision: number }) =>
      api.cancelQuestion(questionId, { expectedRevision }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.questions(teamId, 'open') });
    },
  });
}

// ---------------------------------------------------------------------------
// M5: activity, memories, team lifecycle, authorizations
// ---------------------------------------------------------------------------

/** GET /teams/{id}/activity: the durable event record, newest first. */
export function useActivity(teamId: string | null, enabled = true, viewBotId?: string) {
  return usePagedItems(['activity', teamId ?? '', viewBotId ?? ''], (cursor) => api.listActivity(teamId as string, viewBotId, cursor), teamId !== null && enabled);
}

export function useMemories(botId: string | null, enabled = true) {
  return usePagedItems(['memories', botId ?? ''], (cursor) => api.listMemories(botId as string, cursor), botId !== null && enabled);
}

export function useDeleteMemory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ botId, memoryId }: { botId: string; memoryId: string }) => api.deleteMemory(botId, memoryId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['memories'] });
    },
  });
}

function useInvalidateTeamScope() {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: queryKeys.teams });
    void qc.invalidateQueries({ queryKey: queryKeys.snapshot });
  };
}

export function useArchiveTeam() {
  const invalidate = useInvalidateTeamScope();
  return useMutation({
    mutationFn: ({ teamId, expectedRevision }: { teamId: string; expectedRevision?: number }) => api.archiveTeam(teamId, expectedRevision),
    onSuccess: invalidate,
  });
}

export function useRestoreTeam() {
  const invalidate = useInvalidateTeamScope();
  return useMutation({
    mutationFn: ({ teamId, expectedRevision }: { teamId: string; expectedRevision?: number }) => api.restoreTeam(teamId, expectedRevision),
    onSuccess: invalidate,
  });
}

export function useRenameTeam() {
  const invalidate = useInvalidateTeamScope();
  return useMutation({
    mutationFn: ({ teamId, name, expectedRevision }: { teamId: string; name: string; expectedRevision?: number }) =>
      api.renameTeam(teamId, name, expectedRevision),
    onSuccess: invalidate,
  });
}

export function useSetTeamPermissionMode() {
  const invalidate = useInvalidateTeamScope();
  return useMutation({
    mutationFn: ({ teamId, mode, expectedRevision }: { teamId: string; mode: PermissionMode; expectedRevision?: number }) =>
      api.setTeamPermissionMode(teamId, mode, expectedRevision),
    onSuccess: invalidate,
  });
}

/** A member's session statistics; refreshed with every change the event
 * stream announces, like the rest of the page. */
export function useBotStats(botId: string) {
  return useQuery({ queryKey: ['botStats', botId], queryFn: () => api.getBotStats(botId), enabled: botId !== '' });
}

export function useSetTeamReasoningEffort() {
  const invalidate = useInvalidateTeamScope();
  return useMutation({
    mutationFn: ({ teamId, effort, expectedRevision }: { teamId: string; effort: string; expectedRevision?: number }) =>
      api.setTeamReasoningEffort(teamId, effort, expectedRevision),
    onSuccess: invalidate,
  });
}

/** Chooses the model a member runs with; an empty provider follows the default. */
export function useSetBotModel() {
  const invalidate = useInvalidateTeamScope();
  return useMutation({
    mutationFn: ({ botId, providerId, model, expectedRevision }: { botId: string; providerId: string; model: string; expectedRevision?: number }) =>
      api.setBotModel(botId, providerId, model, expectedRevision),
    onSuccess: invalidate,
  });
}

export function useDeleteTeam() {
  const invalidate = useInvalidateTeamScope();
  return useMutation({
    mutationFn: ({ teamId, expectedRevision }: { teamId: string; expectedRevision?: number }) => api.deleteTeam(teamId, expectedRevision),
    onSuccess: invalidate,
  });
}

export function useDeleteBot() {
  const invalidate = useInvalidateTeamScope();
  return useMutation({
    mutationFn: ({ botId, expectedRevision }: { botId: string; expectedRevision?: number }) => api.deleteBot(botId, expectedRevision),
    onSuccess: invalidate,
  });
}
