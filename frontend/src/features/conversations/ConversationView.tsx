// ConversationView is the center column: one user↔bot session. It shows the
// real message history interleaved with what the member did in each turn
// (RunProcess), the honest queue/delivery state, pending approval cards
// above the composer, and the interrupt control while a run is active.
// User text is shown verbatim; member replies render as Markdown without
// raw HTML.
import { ShellToggle } from '../../app/shell';
import { useContext, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useCancelMessage, useConversation, useEditMessage, useInterruptRun, useRetryRun, useSendMessage } from '../../api/queries';
import type { Bot, Message, Run, RunWithSteps, TeamWithBots } from '../../api/types';
import { forgetLiveRuns, useLiveRuns, type LiveRun } from '../../api/live';
import { api } from '../../api/client';
import { apiErrorMessage } from '../../i18n/errors';
import ApprovalCardStack from '../approvals/ApprovalCardStack';
import QuestionCardStack from '../questions/QuestionCardStack';
import { deliveryLabelKey, messageText } from './delivery';
import { stripToolCallText } from './toolCallText';
import Markdown from './Markdown';
import MessageEditBox from './MessageEditBox';
import DocumentCard from './DocumentCard';
import PermissionModeSelect from './PermissionModeSelect';
import ReasoningEffortSelect from './ReasoningEffortSelect';
import SessionStatsBar from './SessionStatsBar';
import RunProcess, { runHasContent } from './RunProcess';
import { ExchangeLine, TeamMembersContext, isExchange } from './ExchangeLine';
import { Alert, Composer, FloatingCard, IconButton, Icons, MessageActions, MessageBubble, Spinner, SystemNote } from '../../components/ui';
import WorkPanel from '../../components/WorkPanel';
import { useWorkspace } from '../workspace/WorkspaceProvider';
import Avatar from '../avatars/Avatar';
import { MemberAvatarButton } from '../avatars/AvatarEditors';
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
  const editMessage = useEditMessage(bot.id);
  const interrupt = useInterruptRun(bot.id);
  const retryRun = useRetryRun(bot.id);
  const workspace = useWorkspace();
  const infoButtonRef = useRef<HTMLButtonElement>(null);
  // Closing the card returns the focus to the button that opened it.
  const closeCard = () => {
    workspace.setCardOpen(false);
    infoButtonRef.current?.focus();
  };

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
  const loadingOlder = useRef(false);
  // Waiting cards float over the composer; whatever sticks out above it
  // covers messages, so the scroll area gets that much room at its end and
  // the covered content can scroll clear of the card.
  const cardsRef = useRef<HTMLDivElement>(null);
  const composerRef = useRef<HTMLDivElement>(null);
  const [cardScrollExtra, setCardScrollExtra] = useState(0);
  useLayoutEffect(() => {
    const cards = cardsRef.current;
    const composer = composerRef.current;
    if (!cards || !composer) return;
    const measure = () => {
      if (cards.offsetHeight === 0) {
        setCardScrollExtra(0);
        return;
      }
      setCardScrollExtra(Math.max(0, cards.offsetHeight - composer.offsetHeight) + 12);
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(cards);
    observer.observe(composer);
    return () => observer.disconnect();
  }, []);
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
  // A turn is one thing to read: what the member did and said, then its reply,
  // under one name. The reply is given to its turn here and not listed again.
  const runIds = new Set(timeline.flatMap((entry) => (entry.kind === 'run' ? [entry.run.id] : [])));
  const repliesByRun = new Map<string, Message[]>();
  for (const entry of timeline) {
    if (entry.kind !== 'message' || entry.message.kind !== 'assistant' || !entry.message.runId || !runIds.has(entry.message.runId)) continue;
    repliesByRun.set(entry.message.runId, [...(repliesByRun.get(entry.message.runId) ?? []), entry.message]);
  }
  const listed = timeline.filter((entry) => entry.kind === 'run' || !(entry.message.kind === 'assistant' && entry.message.runId && repliesByRun.has(entry.message.runId)));
  // Only the member's latest turn can be retried.
  const latestRunId = [...timeline].reverse().find((entry) => entry.kind === 'run')?.run.id;
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
  const membersById = new Map(item.bots.map((entry) => [entry.id, entry]));
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
    <TeamMembersContext.Provider value={membersById}>
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex min-h-14 items-center gap-3 px-4 py-2">
        <ShellToggle />
        <MemberAvatarButton bot={bot} disabled={readOnly} />
        <div className="min-w-0 flex-1">
          <p className="truncate text-base font-semibold leading-5">{headerTitle}</p>
          <p className="flex min-w-0 items-baseline gap-1.5 text-xs leading-5 text-neutral-500 dark:text-neutral-400">
            <span className="shrink-0">{team.name}</span>
            {bot.isLeader && team.directory && (
              <>
                <span aria-hidden="true">·</span>
                <span className="truncate font-mono text-2xs" title={team.directory}>{team.directory}</span>
              </>
            )}
          </p>
        </div>
        {/* The conversation's own controls: the info card, and the way into
            the workspace. The new-tab button stands aside while the
            workspace is open (its strip has its own +). */}
        <div className="flex shrink-0 items-center gap-0.5">
          <IconButton
            ref={infoButtonRef}
            icon={Icons.tasks}
            size="sm"
            aria-expanded={workspace.cardOpen}
            label={workspace.cardOpen ? t('workspace.closeInfoPanel') : t('workspace.openInfoPanel')}
            onClick={() => (workspace.cardOpen ? closeCard() : workspace.setCardOpen(true))}
          />
          {!workspace.state.open && (
            <IconButton icon={Icons.add} size="sm" label={t('workspace.createTab')} onClick={workspace.newTab} />
          )}
        </div>
      </header>

      <div className="relative flex min-h-0 flex-1 flex-col">
      {workspace.cardOpen && (
        <FloatingCard
          label={t('workspace.infoPanel')}
          closeLabel={t('common.close')}
          onClose={closeCard}
          // Opens under the header's info button and grows downward from
          // there; it overlays the conversation instead of squeezing it,
          // and stops above the conversation's bottom edge.
          className="animate-pop-in absolute -top-2 right-3 z-10 max-h-[calc(100%+0.5rem)] origin-top-right"
        >
          <WorkPanel />
        </FloatingCard>
      )}
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
        className="min-h-0 flex-1 overflow-y-auto px-8 py-4"
      >
        <LoadMore page={{ ...conv, fetchNextPage: loadEarlier }} />
        {conv.isError && (
          <div className="mx-auto max-w-3xl">
            <Alert tone="danger">{t('status.error')}</Alert>
          </div>
        )}
        <fieldset disabled={readOnly} className="m-0 min-w-0 border-0 p-0">
        {conv.isSuccess && timeline.length === 0 && !preparing && (
          <div className="mx-auto mt-[12vh] flex max-w-sm flex-col items-center text-center">
            <Avatar bot={bot} size="xl" />
            <p className="mt-4 text-sm font-medium">{t('conversation.emptyTitle', { name: bot.name })}</p>
            <p className="mt-1.5 text-xs leading-5 text-neutral-500 dark:text-neutral-400">
              {t(bot.isLeader ? 'conversation.emptyLeader' : 'conversation.emptyMember')}
            </p>
          </div>
        )}
        <ul className="mx-auto max-w-3xl space-y-6">
          {listed.map((entry) =>
            entry.kind === 'run' ? (
              <li key={`run-${entry.run.id}`}>
                <TurnRow
                  run={entry.run}
                  replies={repliesByRun.get(entry.run.id) ?? []}
                  active={entry.run.id === activeRun?.id}
                  live={liveRuns[entry.run.id]}
                  teamId={team.id}
                  nameOf={nameOf}
                  onRetry={!activeRun && entry.run.id === latestRunId ? () => retryRun.mutate(entry.run.id) : undefined}
                  retrying={retryRun.isPending}
                />
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
                  onEdit={
                    lastUserMessage?.id === entry.message.id
                      ? (text) => editMessage.mutateAsync({ messageId: entry.message.id, text })
                      : undefined
                  }
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
                <Spinner size={13} />
                {t('conversation.phase.preparing')}
              </p>
            </li>
          )}
        </ul>
        <div aria-hidden style={{ height: cardScrollExtra }} />
        </fieldset>
      </div>
      </div>

      {/* The exchange between two members is read-only: while it is open the
          composer and approval cards step aside (kept mounted, so nothing
          typed or pending is lost). Waiting cards float over the composer
          instead of stacking above it: the visible height stays, the draft
          survives, and the messages behind fade into the card. */}
      <div className={`mx-auto w-full max-w-3xl space-y-2.5 px-8 pb-4 pt-2 ${exchange ? 'hidden' : ''}`}>
        {queuedCount > 0 && (
          <p className="px-1 text-xs text-neutral-500 dark:text-neutral-400" role="status">
            {t('conversation.queuedBanner', { count: queuedCount })}
          </p>
        )}

        <fieldset disabled={readOnly} className="relative m-0 min-w-0 border-0 p-0">
          <div ref={composerRef}>
            <Composer
              value={draft}
              onChange={(value) => setDraft(bot.id, value)}
              onSend={sendNow}
              placeholder={t('conversation.placeholder', { name: headerTitle })}
              disabled={blockReasonKey !== null}
              sending={send.isPending}
              sendLabel={t('conversation.send')}
              running={!!activeRun}
              stopping={stopping || interrupt.isPending}
              stopLabel={stopping ? t('conversation.interruptRequested') : t('conversation.interrupt')}
              onStop={() => {
                if (!activeRun?.id) return;
                setStopping(true);
                interrupt.mutate(activeRun.id, {
                  onSuccess: (result) => {
                    if (result.responseState === 'unknown') {
                      setStopping(false);
                      setActionError(t('errors.error.engine.interrupt_unconfirmed'));
                    }
                  },
                  onError: (error) => {
                    setStopping(false);
                    setActionError(apiErrorMessage(t, error));
                  },
                });
              }}
              options={
                <>
                  <PermissionModeSelect team={team} disabled={readOnly || blockReasonKey !== null} />
                  <ReasoningEffortSelect team={team} bot={bot} bots={item.bots} disabled={readOnly || blockReasonKey !== null} />
                </>
              }
            />
          </div>
          {/* The mist the card rises out of; clicks pass through, and with
              no card waiting there is no mist to veil the composer. */}
          <div ref={cardsRef} className="empty:before:hidden absolute inset-x-0 bottom-0 z-20 space-y-2 before:pointer-events-none before:absolute before:inset-x-0 before:bottom-full before:h-10 before:bg-gradient-to-t before:from-card before:to-transparent before:content-['']">
            <ApprovalCardStack key={bot.id} teamId={team.id} viewBotId={bot.id} botNameById={botNameById} />
            <QuestionCardStack key={`q-${bot.id}`} teamId={team.id} viewBotId={bot.id} />
          </div>
        </fieldset>

        <SessionStatsBar botId={bot.id} />
        {blockReasonKey && (
          <p className="px-1 text-xs text-neutral-500 dark:text-neutral-400" role="status">
            {t(blockReasonKey, { status: team.status })}
          </p>
        )}
        {(sendError || actionError) && (
          <Alert
            tone="danger"
            onClose={() => {
              setSendError('');
              setActionError('');
            }}
            closeLabel={t('common.close')}
          >
            {sendError || actionError}
          </Alert>
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

/** One turn of a member: its name once, what it did and said as it went, and
 * the reply that closes it. */
function TurnRow({
  run,
  replies,
  active,
  live,
  teamId,
  nameOf,
  onRetry,
  retrying,
}: {
  run: RunWithSteps;
  replies: Message[];
  active: boolean;
  live: LiveRun | undefined;
  teamId: string;
  nameOf: (botId: string) => string;
  onRetry?: () => void;
  retrying: boolean;
}) {
  const membersById = useContext(TeamMembersContext);
  const { t, i18n } = useTranslation();
  const author = membersById.get(run.botId);
  const replyText = replies.map((reply) => stripToolCallText(messageText(reply))).filter((text) => text !== '').join('\n\n');
  if (replies.length > 0 && replyText === '' && !runHasContent(run, active, '')) return null;
  const process = runHasContent(run, active, replyText) ? (
    <RunProcess run={run} active={active} live={live} scope={{ teamId }} nameOf={nameOf} replyText={replyText} onRetry={onRetry} retrying={retrying} />
  ) : null;
  if (!process && replyText === '') return null;
  return (
    <div className="group">
      <MessageBubble
        from="member"
        avatar={author ? <Avatar bot={author} size="md" /> : undefined}
        author={nameOf(run.botId)}
        meta={
          replyText !== '' && !active ? (
            // The turn's words may be several messages; the copy takes them
            // all, and the time is the last one's.
            <MessageActions
              value={replyText}
              createdAt={replies[replies.length - 1]?.createdAt ?? ''}
              language={i18n.language}
              copyLabel={t('conversation.message.copy')}
              copiedLabel={t('conversation.message.copied')}
            />
          ) : undefined
        }
      >
        <div className="space-y-2">
          {process}
          {replyText !== '' && <Markdown text={replyText} teamId={teamId} />}
        </div>
      </MessageBubble>
    </div>
  );
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
  onEdit,
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
  /** Given for the conversation's last user message: replace its words and send them again. */
  onEdit?: (text: string) => Promise<unknown>;
}) {
  const { t, i18n } = useTranslation();
  const [editing, setEditing] = useState(false);
  const membersById = useContext(TeamMembersContext);
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

  if (message.kind === 'system') return <SystemNote>{text}</SystemNote>;
  if (message.kind === 'document') return <DocumentCard text={text} teamId={teamId} />;

  const isUser = message.kind === 'user';
  // A tool call the model wrote as text is not shown (see toolCallText.ts); a reply that was only that has nothing to show.
  const shown = isUser ? text : stripToolCallText(text);
  if (!isUser && text !== '' && shown === '') return null;
  const delivery = isLastUser && message.deliveryStatus ? t(deliveryLabelKey(message.deliveryStatus)) : '';
  const meta = (
    <>
      {isUser && isLastUser && onEdit && (
        <IconButton icon={Icons.edit} label={t('conversation.message.edit')} size="sm" onClick={() => setEditing(true)} />
      )}
      <MessageActions value={shown} createdAt={message.createdAt} language={i18n.language} copyLabel={t('conversation.message.copy')} copiedLabel={t('conversation.message.copied')} />
      {delivery && <span>{delivery}</span>}
      {showCancel && <IconButton icon={Icons.close} label={t('conversation.cancelQueued')} size="sm" shape="circle" onClick={onCancelQueued} disabled={cancelPending} />}
    </>
  );
  const author = membersById.get(message.botId);
  if (editing) {
    return (
      <div className="rounded-xl bg-sunk p-3">
        <MessageEditBox
          initial={text}
          onSave={async (next) => {
            await onEdit?.(next);
            setEditing(false);
          }}
          onCancel={() => setEditing(false)}
        />
      </div>
    );
  }
  return (
    <div className="group">
      <MessageBubble
        from={isUser ? 'user' : 'member'}
        avatar={author ? <Avatar bot={author} size="md" /> : undefined}
        author={isUser ? undefined : nameOf(message.botId)}
        meta={meta}
      >
        {isUser ? text : <Markdown text={shown} teamId={teamId} />}
      </MessageBubble>
    </div>
  );
}
