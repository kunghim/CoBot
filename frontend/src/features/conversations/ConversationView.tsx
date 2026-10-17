// ConversationView is the center column: one user↔bot session. It shows the
// real message history interleaved with what the member did in each turn
// (RunProcess), the honest queue/delivery state, pending approval cards
// above the composer, and the interrupt control while a run is active.
// User text is shown verbatim; member replies render as Markdown without
// raw HTML.
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Loader2, Send, Square, X } from 'lucide-react';
import { useCancelMessage, useConversation, useInterruptRun, useSendMessage } from '../../api/queries';
import type { Bot, Message, Run, RunWithSteps, TeamWithBots } from '../../api/types';
import { forgetLiveRuns, useLiveRuns } from '../../api/live';
import { apiErrorMessage } from '../../i18n/errors';
import ApprovalCardStack from '../approvals/ApprovalCardStack';
import { deliveryLabelKey, messageText } from './delivery';
import Markdown from './Markdown';
import PermissionModeSelect from './PermissionModeSelect';
import RunProcess from './RunProcess';
import { sendBlockReasonKey } from '../teams/teamUtils';
import { useSelectedBot } from '../../app/SelectedBotContext';
import LoadMore from '../../components/LoadMore';

export default function ConversationView({ item, bot }: { item: TeamWithBots; bot: Bot }) {
  const { t } = useTranslation();
  const { team } = item;
  const conv = useConversation(bot.id);
  const send = useSendMessage(bot.id);
  const cancel = useCancelMessage(bot.id);
  const interrupt = useInterruptRun(bot.id);

  const { drafts, setDraft, clearSentDraft } = useSelectedBot();
  const draft = drafts[bot.id] ?? '';
  const [sendError, setSendError] = useState('');
  // Cancel/interrupt failures are surfaced too: a failed retract must never
  // look like a successful one (review C).
  const [actionError, setActionError] = useState('');
  const [stopping, setStopping] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const loadingOlder = useRef(false);
  const loadEarlier = async () => {
    const element = scrollRef.current;
    const before = element ? { height: element.scrollHeight, top: element.scrollTop } : null;
    loadingOlder.current = true;
    await conv.fetchNextPage();
    requestAnimationFrame(() => {
      if (element && before) element.scrollTop = before.top + element.scrollHeight - before.height;
      loadingOlder.current = false;
    });
  };

  const messages = conv.data?.messages ?? [];
  const activeRun = conv.data?.activeRun ?? null;
  const queuedCount = conv.data?.queuedCount ?? 0;

  const liveRuns = useLiveRuns();
  const runs = conv.data?.runs ?? [];
  const timeline = buildTimeline(messages, runs, activeRun);
  const activeLive = activeRun ? liveRuns[activeRun.id] : undefined;

  // "Stopping" is only honest while the run still exists.
  useEffect(() => {
    if (!activeRun) setStopping(false);
  }, [activeRun]);

  // Live progress of this member's finished runs is superseded by the
  // stored steps the reload brought.
  const finishedKey = runs.filter((run) => run.id !== activeRun?.id).map((run) => run.id).join(',');
  useEffect(() => {
    const finished = new Set(finishedKey.split(','));
    forgetLiveRuns((runId) => !finished.has(runId));
  }, [finishedKey]);

  // Follow the conversation while the user is at the bottom: on load, new
  // messages, new steps and streamed text. Scrolling up to read stops it.
  const stickToBottom = useRef(true);
  const progressKey = `${messages.length}:${runs.length}:${activeRun?.status ?? ''}:${Object.keys(activeLive?.steps ?? {}).length}:${activeLive?.reply.length ?? 0}`;
  useLayoutEffect(() => {
    const element = scrollRef.current;
    if (element && !loadingOlder.current && stickToBottom.current) element.scrollTop = element.scrollHeight;
  }, [progressKey]);
  useEffect(() => {
    stickToBottom.current = true;
    const element = scrollRef.current;
    if (element) element.scrollTop = element.scrollHeight;
  }, [bot.id]);

  const blockReasonKey = sendBlockReasonKey(team.status);
  // While the team's folder is gone the conversation is read-only: history
  // stays readable, every action is disabled.
  const readOnly = team.status === 'directory_missing';
  const lastUserMessage = [...messages].reverse().find((message) => message.kind === 'user') ?? null;
  const lastQueuedMessage =
    [...messages].reverse().find((message) => message.kind === 'user' && message.deliveryStatus === 'queued') ?? null;

  const botNameById = new Map(item.bots.map((entry) => [entry.id, entry.name]));
  const nameOf = (botId: string) => botNameById.get(botId) ?? botId;
  // Between sending and the member's turn starting (engine start, thread
  // creation) the flow still says something is happening.
  const preparing = !activeRun && queuedCount > 0 && (lastUserMessage?.deliveryStatus === 'queued' || lastUserMessage?.deliveryStatus === 'dispatching');

  const sendNow = () => {
    const text = draft.trim();
    if (text === '' || blockReasonKey) return;
    setSendError('');
    setActionError('');
    send.mutate(text, {
      onSuccess: () => clearSentDraft(bot.id, draft),
      onError: (error) => setSendError(apiErrorMessage(t, error)),
    });
  };

  const headerTitle = bot.name;
  const headerSubtitle =
    bot.isLeader && team.directory ? `${team.name} · ${team.directory}` : team.name;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex min-h-12 items-center gap-3 border-b border-neutral-200 px-4 py-1.5 dark:border-neutral-800">
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold">{headerTitle}</p>
          <p className="truncate text-xs text-neutral-500 dark:text-neutral-400" title={bot.isLeader ? team.directory : undefined}>
            {headerSubtitle}
          </p>
        </div>
      </header>

      <div
        ref={scrollRef}
        onScroll={(event) => {
          const element = event.currentTarget;
          stickToBottom.current = element.scrollHeight - element.scrollTop - element.clientHeight < 48;
        }}
        className="min-h-0 flex-1 overflow-y-auto px-4 py-3"
      >
        <LoadMore page={{ ...conv, fetchNextPage: loadEarlier }} />
        {conv.isError && (
          <p className="text-center text-sm text-red-600" role="alert">
            {t('status.error')}
          </p>
        )}
        <fieldset disabled={readOnly} className="m-0 min-w-0 border-0 p-0">
        <ul className="space-y-2">
          {timeline.map((entry) =>
            entry.kind === 'run' ? (
              <li key={`run-${entry.run.id}`}>
                <RunProcess run={entry.run} active={entry.run.id === activeRun?.id} live={liveRuns[entry.run.id]} teamId={team.id} nameOf={nameOf} />
              </li>
            ) : (
              <li key={entry.message.id}>
                <MessageRow
                  message={entry.message}
                  teamId={team.id}
                  isLastUser={lastUserMessage?.id === entry.message.id}
                  showCancel={lastQueuedMessage?.id === entry.message.id}
                  cancelPending={cancel.isPending}
                  onCancelQueued={() => {
                    setActionError('');
                    cancel.mutate(entry.message.id, {
                      onError: (error) => setActionError(apiErrorMessage(t, error)),
                    });
                  }}
                />
              </li>
            ),
          )}
          {preparing && (
            <li>
              <p role="status" className="flex items-center gap-1.5 text-xs text-neutral-500 dark:text-neutral-400">
                <Loader2 size={13} className="animate-spin" aria-hidden="true" />
                {t('conversation.phase.preparing')}
              </p>
            </li>
          )}
        </ul>
        </fieldset>
      </div>

      <div className="space-y-2 border-t border-neutral-200 p-3 dark:border-neutral-800">
        {queuedCount > 0 && (
          <p className="text-xs text-neutral-500 dark:text-neutral-400" role="status">
            {t('conversation.queuedBanner', { count: queuedCount })}
          </p>
        )}

        <fieldset disabled={readOnly} className="m-0 min-w-0 border-0 p-0 space-y-2">
        <ApprovalCardStack key={bot.id} teamId={team.id} viewBotId={bot.id} botNameById={botNameById} />

        <div className="flex items-end gap-2">
          <textarea
            aria-label={t('conversation.placeholder', { name: headerTitle })}
            placeholder={t('conversation.placeholder', { name: headerTitle })}
            value={draft}
            disabled={blockReasonKey !== null}
            rows={2}
            onChange={(event) => setDraft(bot.id, event.target.value)}
            onKeyDown={(event) => {
              // Enter sends, Shift+Enter inserts a newline; IME composition
              // must never submit mid-composition.
              if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return;
              event.preventDefault();
              sendNow();
            }}
            className="min-h-0 flex-1 resize-y rounded-md border border-neutral-300 bg-transparent px-3 py-2 text-sm disabled:opacity-60 dark:border-neutral-700"
          />
          {activeRun && (
            <button
              type="button"
              onClick={() => {
                if (!activeRun.id) return;
                setStopping(true);
                interrupt.mutate(activeRun.id, {
                  onSuccess: (result) => {
                    if (result.responseState === 'unknown') { setStopping(false); setActionError(t('errors.error.engine.interrupt_unconfirmed')); }
                  },
                  onError: (error) => {
                    setStopping(false);
                    setActionError(apiErrorMessage(t, error));
                  },
                });
              }}
              disabled={stopping || interrupt.isPending}
              title={t('conversation.interrupt')}
              aria-label={stopping ? t('conversation.interruptRequested') : t('conversation.interrupt')}
              className="flex h-10 items-center gap-1.5 rounded-md border border-neutral-300 px-3 text-sm text-neutral-600 transition-colors hover:bg-neutral-100 hover:text-neutral-900 disabled:opacity-60 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
            >
              <Square size={12} aria-hidden="true" />
              {stopping ? t('conversation.interruptRequested') : t('conversation.interrupt')}
            </button>
          )}
          <button
            type="button"
            onClick={sendNow}
            disabled={blockReasonKey !== null || draft.trim() === '' || send.isPending}
            title={t('conversation.send')}
            aria-label={t('conversation.send')}
            className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-neutral-900 text-white transition-colors hover:bg-neutral-700 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
          >
            <Send size={16} aria-hidden="true" />
          </button>
        </div>
        </fieldset>

        <PermissionModeSelect team={team} disabled={readOnly || blockReasonKey !== null} />

        {blockReasonKey && (
          <p className="text-xs text-neutral-500 dark:text-neutral-400" role="status">
            {t(blockReasonKey, { status: team.status })}
          </p>
        )}
        {(sendError || actionError) && (
          <p className="text-xs text-red-600" role="alert">
            {sendError || actionError}
          </p>
        )}
      </div>
    </div>
  );
}

type TimelineEntry = { kind: 'message'; message: Message; at: number } | { kind: 'run'; run: RunWithSteps; at: number };

/** Messages and turns in time order: a turn sits where it started, so its
 * process comes after the message that woke it and before its reply. */
export function buildTimeline(messages: Message[], runs: RunWithSteps[], activeRun: Run | null): TimelineEntry[] {
  const seen = new Set<string>();
  const entries: TimelineEntry[] = messages.map((message) => ({ kind: 'message', message, at: Date.parse(message.createdAt) }));
  for (const run of runs) {
    if (seen.has(run.id)) continue;
    seen.add(run.id);
    // The active run's status is the freshest read of it.
    const current = run.id === activeRun?.id ? { ...run, status: activeRun.status } : run;
    entries.push({ kind: 'run', run: current, at: Date.parse(run.startedAt) });
  }
  if (activeRun?.id && !seen.has(activeRun.id)) {
    entries.push({ kind: 'run', run: { ...activeRun, steps: [] }, at: Date.parse(activeRun.startedAt) });
  }
  return entries.sort((a, b) => a.at - b.at || (a.kind === b.kind ? 0 : a.kind === 'message' ? -1 : 1));
}

function MessageRow({
  message,
  teamId,
  isLastUser,
  showCancel,
  cancelPending,
  onCancelQueued,
}: {
  message: Message;
  teamId: string;
  isLastUser: boolean;
  showCancel: boolean;
  cancelPending: boolean;
  onCancelQueued: () => void;
}) {
  const { t } = useTranslation();
  const text = messageText(message);

  const feedbackBlock = message.blocks.find((block) => block.type === 'approval_result');
  if (feedbackBlock) {
    try {
      const feedback = JSON.parse(feedbackBlock.text) as { approvalType: string; decision: string; comment?: string; details?: { candidateRef?: string; targetRef?: string } };
      return <div className="space-y-1 rounded-md border border-neutral-200 px-3 py-2 text-xs text-neutral-500 dark:border-neutral-700">
        <p>{t('approvals.feedback', { type: t(`approvals.type.${feedback.approvalType}`), decision: t(`approvals.feedbackDecision.${feedback.decision}`, { defaultValue: feedback.decision }) })}</p>
        {feedback.details?.candidateRef && <p className="break-all font-mono">{feedback.details.candidateRef} → {feedback.details.targetRef}</p>}
        {feedback.comment && <p className="whitespace-pre-wrap">{feedback.comment}</p>}
      </div>;
    } catch { /* A malformed record remains visible as escaped text. */ }
  }

  if (message.kind === 'collaboration' || message.kind === 'system') {
    return (
      <div className="flex justify-center">
        <span className="max-w-[85%] whitespace-pre-wrap text-center text-xs text-neutral-400">{text}</span>
      </div>
    );
  }

  const isUser = message.kind === 'user';
  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
      <div className="max-w-[85%] space-y-0.5">
        <div
          className={`break-words rounded-xl px-3 py-2 text-sm ${
            isUser
              ? 'whitespace-pre-wrap bg-neutral-900 text-white dark:bg-neutral-100 dark:text-neutral-900'
              : 'bg-neutral-100 text-neutral-900 dark:bg-neutral-800 dark:text-neutral-100'
          }`}
        >
          {isUser ? text : <Markdown text={text} teamId={teamId} />}
        </div>
        <div className={`flex items-center gap-2 text-[11px] text-neutral-400 ${isUser ? 'justify-end' : 'justify-start'}`}>
          {isLastUser && message.deliveryStatus && (
            <span>{t(deliveryLabelKey(message.deliveryStatus))}</span>
          )}
          {showCancel && (
            <button
              type="button"
              onClick={onCancelQueued}
              disabled={cancelPending}
              aria-label={t('conversation.cancelQueued')}
              title={t('conversation.cancelQueued')}
              className="flex items-center gap-1 rounded px-1 text-neutral-400 underline-offset-2 hover:text-red-600 disabled:opacity-50"
            >
              <X size={11} aria-hidden="true" />
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
