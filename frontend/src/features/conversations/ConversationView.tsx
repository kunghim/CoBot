// ConversationView is the center column: one user↔bot session. It shows the
// real message history, the honest queue/delivery state, pending approval
// cards above the composer, and the interrupt control while a run is active.
// Message text is rendered as escaped plain text only — no markdown library,
// no raw HTML.
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Send, Square, X } from 'lucide-react';
import { useCancelMessage, useConversation, useInterruptRun, useSendMessage } from '../../api/queries';
import type { Bot, Message, TeamWithBots } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import ApprovalCardStack from '../approvals/ApprovalCardStack';
import { deliveryLabelKey, messageText, runStatusLabelKey } from './delivery';
import { sendBlockReasonKey } from '../teams/teamUtils';

export default function ConversationView({ item, bot }: { item: TeamWithBots; bot: Bot }) {
  const { t } = useTranslation();
  const { team } = item;
  const conv = useConversation(bot.id);
  const send = useSendMessage(bot.id);
  const cancel = useCancelMessage(bot.id);
  const interrupt = useInterruptRun(bot.id);

  const [draft, setDraft] = useState('');
  const [sendError, setSendError] = useState('');
  const [stopping, setStopping] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);

  const messages = conv.data?.messages ?? [];
  const activeRun = conv.data?.activeRun ?? null;
  const queuedCount = conv.data?.queuedCount ?? 0;

  // "Stopping" is only honest while the run still exists.
  useEffect(() => {
    if (!activeRun) setStopping(false);
  }, [activeRun]);

  // Keep the newest message visible on load and on new messages.
  useEffect(() => {
    const element = scrollRef.current;
    if (element) element.scrollTop = element.scrollHeight;
  }, [messages.length, bot.id]);

  const blockReasonKey = sendBlockReasonKey(team.status);
  const lastUserMessage = [...messages].reverse().find((message) => message.kind === 'user') ?? null;
  const lastQueuedMessage =
    [...messages].reverse().find((message) => message.kind === 'user' && message.deliveryStatus === 'queued') ?? null;

  const botNameById = new Map(item.bots.map((entry) => [entry.id, entry.name]));

  const sendNow = () => {
    const text = draft.trim();
    if (text === '' || blockReasonKey) return;
    setSendError('');
    send.mutate(text, {
      onSuccess: () => setDraft(''),
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
        {activeRun && (
          <p
            role="status"
            className="ml-auto shrink-0 rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-800 dark:bg-amber-900 dark:text-amber-200"
          >
            {t('conversation.activeRun', { status: t(runStatusLabelKey(activeRun.status)) })}
          </p>
        )}
      </header>

      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
        {conv.isError && (
          <p className="text-center text-sm text-red-600" role="alert">
            {t('status.error')}
          </p>
        )}
        <ul className="space-y-2">
          {messages.map((message) => (
            <li key={message.id}>
              <MessageRow
                message={message}
                isLastUser={lastUserMessage?.id === message.id}
                showCancel={lastQueuedMessage?.id === message.id}
                cancelPending={cancel.isPending}
                onCancelQueued={() => cancel.mutate(message.id)}
              />
            </li>
          ))}
        </ul>
      </div>

      <div className="space-y-2 border-t border-neutral-200 p-3 dark:border-neutral-800">
        {queuedCount > 0 && (
          <p className="text-xs text-neutral-500 dark:text-neutral-400" role="status">
            {t('conversation.queuedBanner', { count: queuedCount })}
          </p>
        )}

        <ApprovalCardStack key={team.id} teamId={team.id} botNameById={botNameById} />

        <div className="flex items-end gap-2">
          <textarea
            aria-label={t('conversation.placeholder', { name: headerTitle })}
            placeholder={t('conversation.placeholder', { name: headerTitle })}
            value={draft}
            disabled={blockReasonKey !== null}
            rows={2}
            onChange={(event) => setDraft(event.target.value)}
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
                interrupt.mutate(activeRun.id, { onError: () => setStopping(false) });
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

        {blockReasonKey && (
          <p className="text-xs text-neutral-500 dark:text-neutral-400" role="status">
            {t(blockReasonKey, { status: team.status })}
          </p>
        )}
        {sendError && (
          <p className="text-xs text-red-600" role="alert">
            {sendError}
          </p>
        )}
      </div>
    </div>
  );
}

function MessageRow({
  message,
  isLastUser,
  showCancel,
  cancelPending,
  onCancelQueued,
}: {
  message: Message;
  isLastUser: boolean;
  showCancel: boolean;
  cancelPending: boolean;
  onCancelQueued: () => void;
}) {
  const { t } = useTranslation();
  const text = messageText(message);

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
          className={`whitespace-pre-wrap break-words rounded-xl px-3 py-2 text-sm ${
            isUser
              ? 'bg-neutral-900 text-white dark:bg-neutral-100 dark:text-neutral-900'
              : 'bg-neutral-100 text-neutral-900 dark:bg-neutral-800 dark:text-neutral-100'
          }`}
        >
          {text}
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
