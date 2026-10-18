// ConversationView is the center column: one user↔bot session. It shows the
// real message history interleaved with what the member did in each turn
// (RunProcess), the honest queue/delivery state, pending approval cards
// above the composer, and the interrupt control while a run is active.
// User text is shown verbatim; member replies render as Markdown without
// raw HTML.
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ArrowUp, Loader2, Square, X } from 'lucide-react';
import { useCancelMessage, useConversation, useInterruptRun, useSendMessage } from '../../api/queries';
import type { Bot, Message, Run, RunWithSteps, TeamWithBots } from '../../api/types';
import { forgetLiveRuns, useLiveRuns } from '../../api/live';
import { api } from '../../api/client';
import { apiErrorMessage } from '../../i18n/errors';
import ApprovalCardStack from '../approvals/ApprovalCardStack';
import { deliveryLabelKey, messageText } from './delivery';
import Markdown from './Markdown';
import PermissionModeSelect from './PermissionModeSelect';
import ReasoningEffortSelect from './ReasoningEffortSelect';
import SessionStatsBar from './SessionStatsBar';
import RunProcess from './RunProcess';
import { ExchangeLine, RoleBadge, TeamMembersContext, isExchange } from './ExchangeLine';
import { MemberMark } from '../teams/roleTone';
import ExchangeOverlay from './ExchangeOverlay';
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
  // The exchange with another member laid over the conversation, opened at
  // the message that was clicked.
  const [exchange, setExchange] = useState<{ peerId: string; anchorId: string } | null>(null);
  useEffect(() => setExchange(null), [bot.id]);
  const scrollRef = useRef<HTMLDivElement>(null);
  const composerRef = useRef<HTMLTextAreaElement>(null);
  // The composer grows with its text up to its max height, like Codex's.
  useLayoutEffect(() => {
    const element = composerRef.current;
    if (!element) return;
    element.style.height = 'auto';
    element.style.height = `${element.scrollHeight}px`;
  }, [draft, bot.id]);
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

  // Opening a team starts its engine in the background, so the first
  // message does not wait for engine start-up.
  useEffect(() => {
    if (team.status === 'active') api.warmTeam(team.id).catch(() => undefined);
  }, [team.id, team.status]);

  const liveRuns = useLiveRuns();
  const runs = conv.data?.runs ?? [];
  const timeline = buildTimeline(messages, runs, activeRun, bot.id);
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
  const membersByName = new Map(item.bots.map((entry) => [entry.name, entry]));
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

  return (
    <TeamMembersContext.Provider value={membersByName}>
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex min-h-14 items-center gap-3 border-b border-neutral-200 px-5 py-2 lg:pr-14 dark:border-neutral-800">
        <MemberMark bot={bot} size="lg" />
        <div className="min-w-0">
          <p className="truncate text-[15px] font-semibold leading-5">{headerTitle}</p>
          <p className="flex min-w-0 items-baseline gap-1.5 text-xs leading-5 text-neutral-500 dark:text-neutral-400">
            <span className="shrink-0">{team.name}</span>
            {bot.isLeader && team.directory && (
              <>
                <span aria-hidden="true">·</span>
                <span className="truncate font-mono text-[11px]" title={team.directory}>{team.directory}</span>
              </>
            )}
          </p>
        </div>
      </header>

      <div className="relative flex min-h-0 flex-1 flex-col">
      {exchange && (
        <ExchangeOverlay
          key={`${exchange.peerId}:${exchange.anchorId}`}
          teamId={team.id}
          botId={bot.id}
          peerId={exchange.peerId}
          anchorId={exchange.anchorId}
          nameOf={nameOf}
          onClose={() => setExchange(null)}
        />
      )}
      <div
        ref={scrollRef}
        onScroll={(event) => {
          const element = event.currentTarget;
          stickToBottom.current = element.scrollHeight - element.scrollTop - element.clientHeight < 48;
        }}
        className="min-h-0 flex-1 overflow-y-auto px-5 py-5"
      >
        <LoadMore page={{ ...conv, fetchNextPage: loadEarlier }} />
        {conv.isError && (
          <p className="text-center text-sm text-red-600" role="alert">
            {t('status.error')}
          </p>
        )}
        <fieldset disabled={readOnly} className="m-0 min-w-0 border-0 p-0">
        {conv.isSuccess && timeline.length === 0 && !preparing && (
          <div className="mx-auto mt-[12vh] flex max-w-sm flex-col items-center text-center">
            <MemberMark bot={bot} size="lg" />
            <p className="mt-4 text-sm font-medium">{t('conversation.emptyTitle', { name: bot.name })}</p>
            <p className="mt-1.5 text-xs leading-5 text-neutral-500 dark:text-neutral-400">
              {t(bot.isLeader ? 'conversation.emptyLeader' : 'conversation.emptyMember')}
            </p>
          </div>
        )}
        <ul className="mx-auto max-w-3xl space-y-4">
          {timeline.map((entry) =>
            entry.kind === 'run' ? (
              <li key={`run-${entry.run.id}`}>
                <RunProcess run={entry.run} active={entry.run.id === activeRun?.id} live={liveRuns[entry.run.id]} teamId={team.id} nameOf={nameOf} />
              </li>
            ) : (
              <li key={entry.message.id}>
                <MessageRow
                  message={entry.message}
                  recipients={entry.recipients}
                  onOpenExchange={(peerId) => setExchange({ peerId, anchorId: entry.message.id })}
                  botId={bot.id}
                  nameOf={nameOf}
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
      </div>

      {/* The exchange between two members is read-only: while it is open the
          composer and approval cards step aside (kept mounted, so nothing
          typed or pending is lost). */}
      <div className={`mx-auto w-full max-w-3xl space-y-2 px-5 pb-4 pt-2 ${exchange ? 'hidden' : ''}`}>
        {queuedCount > 0 && (
          <p className="px-1 text-xs text-neutral-500 dark:text-neutral-400" role="status">
            {t('conversation.queuedBanner', { count: queuedCount })}
          </p>
        )}

        <fieldset disabled={readOnly} className="m-0 min-w-0 space-y-2 border-0 p-0">
          <ApprovalCardStack key={bot.id} teamId={team.id} viewBotId={bot.id} botNameById={botNameById} />

          <div className="rounded-2xl border border-neutral-200 bg-white shadow-[0_1px_2px_rgb(23_30_27/0.06),0_8px_24px_-12px_rgb(23_30_27/0.18)] transition-colors focus-within:border-neutral-400 dark:border-neutral-700 dark:bg-neutral-900 dark:shadow-none dark:focus-within:border-neutral-500">
            <textarea
              ref={composerRef}
              aria-label={t('conversation.placeholder', { name: headerTitle })}
              placeholder={t('conversation.placeholder', { name: headerTitle })}
              value={draft}
              disabled={blockReasonKey !== null}
              rows={1}
              onChange={(event) => setDraft(bot.id, event.target.value)}
              onKeyDown={(event) => {
                // Enter sends, Shift+Enter inserts a newline; IME composition
                // must never submit mid-composition.
                if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return;
                event.preventDefault();
                sendNow();
              }}
              className="block max-h-60 min-h-[44px] w-full resize-none bg-transparent px-4 pb-1 pt-3 text-sm leading-6 placeholder:text-neutral-400 focus:outline-none disabled:opacity-60"
            />
            <div className="flex items-center justify-between gap-2 px-2 pb-2">
              <div className="flex min-w-0 items-center gap-1">
                <PermissionModeSelect team={team} disabled={readOnly || blockReasonKey !== null} />
                <ReasoningEffortSelect team={team} disabled={readOnly || blockReasonKey !== null} />
              </div>
              {activeRun && draft.trim() === '' ? (
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
                  title={stopping ? t('conversation.interruptRequested') : t('conversation.interrupt')}
                  aria-label={stopping ? t('conversation.interruptRequested') : t('conversation.interrupt')}
                  className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-neutral-900 text-white transition-colors hover:bg-neutral-700 disabled:opacity-60 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
                >
                  {stopping ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : <Square size={11} fill="currentColor" aria-hidden="true" />}
                </button>
              ) : (
                <button
                  type="button"
                  onClick={sendNow}
                  disabled={blockReasonKey !== null || draft.trim() === '' || send.isPending}
                  title={t('conversation.send')}
                  aria-label={t('conversation.send')}
                  className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-neutral-900 text-white transition-colors hover:bg-neutral-700 disabled:bg-neutral-200 disabled:text-neutral-400 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300 dark:disabled:bg-neutral-800 dark:disabled:text-neutral-500"
                >
                  <ArrowUp size={16} aria-hidden="true" />
                </button>
              )}
            </div>
          </div>
        </fieldset>

        <SessionStatsBar botId={bot.id} />
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
    </TeamMembersContext.Provider>
  );
}

type TimelineEntry =
  | { kind: 'message'; message: Message; recipients?: string[]; at: number }
  | { kind: 'run'; run: RunWithSteps; at: number };

// One message sent to several members is stored once per recipient, moments
// apart; within this window the copies read as one line.
const broadcastWindowMs = 5000;

/** Messages and turns in time order: a turn sits where it started, so its
 * process comes after the message that woke it and before its reply. Copies
 * of one message sent to several members merge into a single "sent to" line. */
export function buildTimeline(messages: Message[], runs: RunWithSteps[], activeRun: Run | null, botId = ''): TimelineEntry[] {
  const entries: TimelineEntry[] = [];
  let lastBroadcast: { entry: Extract<TimelineEntry, { kind: 'message' }>; text: string } | null = null;
  for (const message of messages) {
    const at = Date.parse(message.createdAt);
    const outgoing = message.kind === 'collaboration' && message.sourceBotId === botId && message.botId !== botId;
    if (outgoing) {
      const text = messageText(message);
      if (lastBroadcast && lastBroadcast.text === text && at - lastBroadcast.entry.at <= broadcastWindowMs && !lastBroadcast.entry.recipients?.includes(message.botId)) {
        lastBroadcast.entry.recipients = [...(lastBroadcast.entry.recipients ?? []), message.botId];
        continue;
      }
      const entry = { kind: 'message' as const, message, recipients: [message.botId], at };
      entries.push(entry);
      lastBroadcast = { entry, text };
      continue;
    }
    entries.push({ kind: 'message', message, at });
  }
  const seen = new Set<string>();
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
  recipients,
  onOpenExchange,
  botId,
  nameOf,
  teamId,
  isLastUser,
  showCancel,
  cancelPending,
  onCancelQueued,
}: {
  message: Message;
  recipients?: string[];
  onOpenExchange: (peerId: string) => void;
  botId: string;
  nameOf: (botId: string) => string;
  teamId: string;
  isLastUser: boolean;
  showCancel: boolean;
  cancelPending: boolean;
  onCancelQueued: () => void;
}) {
  const { t } = useTranslation();
  const text = messageText(message);

  if (isExchange(message)) {
    // In the sender's conversation it is what the member sent; in the
    // recipient's, what it received. A task the user assigned has no member
    // sender and reads as coming from the user.
    const outgoing = message.sourceBotId === botId && message.botId !== botId;
    return (
      <ExchangeLine
        direction={outgoing ? 'to' : 'from'}
        peers={outgoing ? (recipients ?? [message.botId]) : message.sourceBotId ? [message.sourceBotId] : []}
        nameOf={nameOf}
        onOpen={onOpenExchange}
      />
    );
  }

  if (message.kind === 'system') {
    return (
      <div className="flex justify-center">
        <span className="max-w-[85%] whitespace-pre-wrap text-center text-xs text-neutral-500 dark:text-neutral-400">{text}</span>
      </div>
    );
  }

  const isUser = message.kind === 'user';
  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
      <div className="max-w-[85%] space-y-1">
        {!isUser && <RoleBadge name={nameOf(message.botId)} />}
        <div
          className={`break-words text-sm leading-6 ${
            isUser
              ? 'whitespace-pre-wrap rounded-2xl rounded-br-md bg-neutral-900 px-3.5 py-2 text-white dark:bg-neutral-100 dark:text-neutral-900'
              : 'rounded-2xl rounded-tl-md bg-white px-4 py-2.5 text-neutral-900 ring-1 ring-neutral-200 dark:bg-neutral-900 dark:text-neutral-100 dark:ring-neutral-800'
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
