import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';
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
import { api, bearerToken, reportUnauthorized } from './client';

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
export function useEventStream(enabled: boolean, onEvent: (event: Event) => void): void {
  useEffect(() => {
    if (!enabled) return;
    let lastSeq = 0;
    let stopped = false;

    const handleFrame = (payload: string) => {
      try {
        const event = JSON.parse(payload) as Event;
        lastSeq = Math.max(lastSeq, event.seq);
        onEvent(event);
      } catch {
        // A malformed frame never kills the stream; snapshot re-sync covers gaps.
      }
    };

    if (!bearerToken()) {
      const source = new EventSource(`/api/v1/events?after=${lastSeq}`);
      source.addEventListener('cobot.event', (message) => handleFrame((message as MessageEvent).data));
      return () => {
        stopped = true;
        source.close();
      };
    }

    // Bearer (desktop) mode: fetch-based SSE with explicit reconnect.
    const controller = new AbortController();
    const run = async () => {
      while (!stopped) {
        try {
          const resp = await fetch(`/api/v1/events?after=${lastSeq}`, {
            headers: { Accept: 'text/event-stream', Authorization: 'Bearer ' + bearerToken() },
            credentials: 'include',
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
  }, [enabled, onEvent]);
}

// ---------------------------------------------------------------------------
// M2: teams, conversations, approvals, engine, models
// ---------------------------------------------------------------------------

/** GET /teams: teams with their members, refreshed by the SSE invalidation. */
export function useTeams(enabled = true) {
  return useQuery({ queryKey: queryKeys.teams, queryFn: api.listTeams, enabled });
}

export function useCreateTeam() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { name: string; directory: string; initialRoleIds: string[]; adoptMarker?: boolean }) => api.createTeam(input),
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
  return useQuery({
    queryKey: queryKeys.messages(botId ?? ''),
    queryFn: () => api.listMessages(botId as string),
    enabled: botId !== null,
  });
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
export function useApprovals(teamId: string | null, state: 'pending' | 'history', enabled = true) {
  return useQuery({
    queryKey: queryKeys.approvals(teamId ?? '', state),
    queryFn: () => api.listApprovals({ teamId: teamId as string, state }),
    enabled: teamId !== null && enabled,
  });
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
export function useTasks(teamId: string | null, enabled = true) {
  return useQuery({
    queryKey: queryKeys.tasks(teamId ?? ''),
    queryFn: () => api.listTasks({ teamId: teamId as string }),
    enabled: teamId !== null && enabled,
  });
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
    mutationFn: (transferId: string) => api.acceptTransfer(transferId),
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
export function useQuestions(teamId: string | null, status = 'open', enabled = true) {
  return useQuery({
    queryKey: queryKeys.questions(teamId ?? '', status),
    queryFn: () => api.listQuestions({ teamId: teamId as string, status }),
    enabled: teamId !== null && enabled,
  });
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
  return useQuery({
    queryKey: queryKeys.collaboration(botId ?? ''),
    queryFn: () => api.listCollaboration(botId as string),
    enabled: botId !== null && enabled,
  });
}


// ---------------------------------------------------------------------------
// M5: activity, memories, team lifecycle, authorizations
// ---------------------------------------------------------------------------

/** GET /teams/{id}/activity: the durable event record, newest first. */
export function useActivity(teamId: string | null, enabled = true) {
  return useQuery({
    queryKey: ['activity', teamId ?? ''],
    queryFn: () => api.listActivity(teamId as string, 100),
    enabled: teamId !== null && enabled,
  });
}

/** GET /bots/{id}/memories: one member's live memories. */
export function useMemories(botId: string | null, enabled = true) {
  return useQuery({
    queryKey: ['memories', botId ?? ''],
    queryFn: () => api.listMemories(botId as string),
    enabled: botId !== null && enabled,
  });
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

export function useRebindDirectory() {
  const invalidate = useInvalidateTeamScope();
  return useMutation({
    mutationFn: ({ teamId, directory, adoptMarker }: { teamId: string; directory: string; adoptMarker?: boolean }) =>
      api.rebindDirectory(teamId, { directory, adoptMarker }),
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
