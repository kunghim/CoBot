import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type QueryKey } from '@tanstack/react-query';
import { useEffect, useRef } from 'react';
import type {
  Approval,
  Bot,
  CollaborationListResult,
  ConversationResult,
  CreateTeamResult,
  EngineStatus,
  Event,
  Evidence,
  ModelListResult,
  Question,
  QuestionListResult,
  RoleListResult,
  SendMessageResult,
  Settings,
  SettingsPatch,
  Snapshot,
  Status,
  Task,
  TaskDetailResult,
  TaskListResult,
  TeamListResult,
  Transfer,
} from './types';
import { api, apiUrl, bearerToken, reportUnauthorized } from './client';

export const queryKeys = {
  status: ['status'] as const,
  settings: ['settings'] as const,
  snapshot: ['snapshot'] as const,
  teams: ['teams'] as const,
  roles: ['roles'] as const,
  engine: ['engine'] as const,
  models: ['models'] as const,
  messages: (botId: string) => ['messages', botId] as const,
  approvals: (teamId: string, state: 'pending' | 'history') => ['approvals', teamId, state] as const,
  tasks: (teamId: string) => ['tasks', teamId] as const,
  taskDetail: (taskId: string) => ['taskDetail', taskId] as const,
  questions: (teamId: string, status: string) => ['questions', teamId, status] as const,
  collaboration: (botId: string) => ['collaboration', botId] as const,
};

export function useStatus() {
  return useQuery({ queryKey: queryKeys.status, queryFn: api.getStatus, retry: false });
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

    if (!bearerToken()) {
      let source: EventSource | null = null;
      let retryTimer = 0;
      const connect = () => {
        if (stopped) return;
        // lastSeq is shared with handleFrame: every reconnect resumes from
        // the cursor instead of replaying from zero (review C).
        source = new EventSource(apiUrl(`/api/v1/events?after=${lastSeq}`));
        source.addEventListener('cobot.event', (message) => handleFrame((message as MessageEvent).data));
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
              const dataLine = frame.split('\n').find((line) => line.startsWith('data: '));
              if (dataLine) handleFrame(dataLine.slice(6));
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

/** GET /bots/{id}/messages: one conversation plus queue depth and run state. */
export function useConversation(botId: string | null) {
  const query = useInfiniteQuery({ queryKey: queryKeys.messages(botId ?? ''), queryFn: ({ pageParam }) => api.listMessages(botId as string, { before: pageParam || undefined }), initialPageParam: '', getNextPageParam: (page) => page.nextCursor || undefined, enabled: botId !== null });
  const pages = query.data?.pages;
  return { ...query, data: pages?.[0] ? { ...pages[0], messages: [...pages].reverse().flatMap((page) => page.messages) } : undefined };
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
    mutationFn: ({ approvalId, optionId, expectedRevision }: { approvalId: string; optionId: string; expectedRevision: number }) =>
      api.decideApproval(approvalId, { optionId, expectedRevision }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['approvals'] });
      void qc.invalidateQueries({ queryKey: queryKeys.snapshot });
      void qc.invalidateQueries({ queryKey: queryKeys.teams });
    },
  });
}

/** GET /engine: the real engine projection; the snapshot copy is only a
 * cheap cached hint. */
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
// M3: tasks, transfers, questions, collaboration records
// ---------------------------------------------------------------------------

/** GET /teams/{id}/tasks: the team board; the server narrows a member's
 * scope, the page never decides who sees what. */
export function useTasks(teamId: string | null, enabled = true, viewBotId?: string) {
  return usePagedItems([...queryKeys.tasks(teamId ?? ''), viewBotId ?? ''], (cursor) => api.listTasks({ teamId: teamId as string, viewBotId, cursor }), teamId !== null && enabled);
}

/** GET /tasks/{id}: one task with evidence and transfer history, loaded only
 * while the row is expanded. */
export function useTaskDetail(taskId: string | null, enabled = true) {
  return useQuery({
    queryKey: queryKeys.taskDetail(taskId ?? ''),
    queryFn: () => api.getTask(taskId as string),
    enabled: taskId !== null && enabled,
  });
}

function useInvalidateTasks() {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: ['tasks'] });
    void qc.invalidateQueries({ queryKey: ['taskDetail'] });
    void qc.invalidateQueries({ queryKey: queryKeys.teams });
  };
}

export function useCreateTask(teamId: string) {
  const invalidate = useInvalidateTasks();
  return useMutation({
    mutationFn: (input: { title: string; description?: string; acceptanceCriteria?: string; ownerId: string; dependencyIds?: string[] }) =>
      api.createTask(teamId, input),
    onSuccess: invalidate,
  });
}

export function usePatchTask() {
  const invalidate = useInvalidateTasks();
  return useMutation({
    mutationFn: ({ taskId, ...body }: { taskId: string; status?: string; blockedReason?: string; ownerId?: string; expectedRevision: number }) =>
      api.patchTask(taskId, body),
    onSuccess: invalidate,
  });
}

export function useSubmitEvidence() {
  const invalidate = useInvalidateTasks();
  return useMutation({
    mutationFn: ({ taskId, ...body }: { taskId: string; summary: string; artifactRefs?: string[]; testResult?: string; reviewResult?: string }) =>
      api.submitEvidence(taskId, body),
    onSuccess: invalidate,
  });
}

export function useConfirmTask() {
  const invalidate = useInvalidateTasks();
  return useMutation({
    mutationFn: ({ taskId, evidenceIds, expectedRevision }: { taskId: string; evidenceIds?: string[]; expectedRevision: number }) =>
      api.confirmTask(taskId, { evidenceIds, expectedRevision }),
    onSuccess: invalidate,
  });
}

export function useOfferTransfer() {
  const invalidate = useInvalidateTasks();
  return useMutation({
    mutationFn: ({ taskId, toBotId, note }: { taskId: string; toBotId: string; note?: string }) =>
      api.offerTransfer(taskId, { toBotId, note }),
    onSuccess: invalidate,
  });
}

export function useAcceptTransfer() {
  const invalidate = useInvalidateTasks();
  return useMutation({
    mutationFn: ({ transferId, expectedRevision }: { transferId: string; expectedRevision: number }) => api.acceptTransfer(transferId, expectedRevision),
    onSuccess: invalidate,
  });
}

export function useCancelTransfer() {
  const invalidate = useInvalidateTasks();
  return useMutation({
    mutationFn: (transferId: string) => api.cancelTransfer(transferId),
    onSuccess: invalidate,
  });
}

/** GET /questions?status=open: the answers the user still owes. */
export function useQuestions(teamId: string | null, status = 'open', enabled = true, viewBotId?: string) {
  return usePagedItems([...queryKeys.questions(teamId ?? '', status), viewBotId ?? ''], (cursor) => api.listQuestions({ teamId: teamId as string, status, viewBotId, cursor }), teamId !== null && enabled);
}

export function useAnswerQuestion() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ questionId, answerText, expectedRevision }: { questionId: string; answerText: string; expectedRevision: number }) =>
      api.answerQuestion(questionId, { answerText, expectedRevision }),
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

/** GET /bots/{id}/collaboration: the read-only exchange of the selected
 * member; the user reads a named member's whole record. */
export function useCollaboration(botId: string | null, enabled = true) {
  return usePagedItems(queryKeys.collaboration(botId ?? ''), (cursor) => api.listCollaboration(botId as string, cursor), botId !== null && enabled, true);
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

/** GET /authorizations: live bridge credentials without token values. */
export function useAuthorizations(enabled = true) {
  return useQuery({
    queryKey: ['authorizations'],
    queryFn: api.listAuthorizations,
    enabled,
  });
}

export function useRevokeAuthorization() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (botId: string) => api.revokeAuthorization(botId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['authorizations'] });
    },
  });
}

export type {
  Approval,
  Bot,
  CollaborationListResult,
  ConversationResult,
  CreateTeamResult,
  EngineStatus,
  Event,
  Evidence,
  ModelListResult,
  Question,
  QuestionListResult,
  RoleListResult,
  SendMessageResult,
  Snapshot,
  Status,
  Settings,
  SettingsPatch,
  Task,
  TaskDetailResult,
  TaskListResult,
  TeamListResult,
  Transfer,
};
