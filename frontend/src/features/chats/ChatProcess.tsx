// A chat's turn, read in the order it happened like a team's (RunProcess): what
// the model said stays in view between stretches of work, each folded to a line.
// A chat has no members, so none of the team's parts come with it: no name or
// avatar over it, no hand-offs, no retry of a whole turn.
import { useTranslation } from 'react-i18next';
import type { ChatRun, RunStep } from '../../api/types';
import type { LiveRun } from '../../api/live';
import { errorMessage } from '../../i18n/errors';
import RunProcess, { runHasContent, type ProcessRun } from '../conversations/RunProcess';
import { isChatRunning } from './state';

/** A chat run as the turn view reads a run: its steps are the engine's, the same kinds. */
function processOf(run: ChatRun): ProcessRun {
  return {
    id: run.id,
    // A chat's model belongs to the chat, not to a member: its turn view
    // has no switch-model entry, so the run stays anonymous here.
    botId: '',
    status: run.status as ProcessRun['status'],
    startedAt: run.startedAt,
    endedAt: run.endedAt,
    steps: (run.steps ?? []).map((step, index): RunStep => ({
      id: step.id,
      runId: run.id,
      teamId: '',
      botId: '',
      seq: step.seq ?? index,
      kind: step.kind as RunStep['kind'],
      status: step.status as RunStep['status'],
      detail: (step.detail ?? {}) as RunStep['detail'],
      startedAt: run.startedAt,
      updatedAt: run.endedAt || run.startedAt,
    })),
  };
}

/** Whether a chat's turn has anything to show above its reply. */
export function chatRunHasContent(run: ChatRun, replyText: string): boolean {
  return !!run.errorKey || runHasContent(processOf(run), isChatRunning(run.status), replyText);
}

export default function ChatProcess({ run, chatId, replyText = '', live }: { run: ChatRun; chatId: string; replyText?: string; live?: LiveRun }) {
  const { t } = useTranslation();
  const active = isChatRunning(run.status);
  const process = processOf(run);
  return (
    <div className="space-y-2">
      {runHasContent(process, active, replyText) && (
        <RunProcess run={process} active={active} live={live} scope={{ chatId }} nameOf={(id) => id} replyText={replyText} />
      )}
      {run.errorKey && <p className="text-xs text-red-600 dark:text-red-400">{errorMessage(t, run.errorKey)}</p>}
    </div>
  );
}
