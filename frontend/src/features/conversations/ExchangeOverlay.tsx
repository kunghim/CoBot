// ExchangeOverlay lays the whole exchange between the open member and one
// peer over the conversation: every message they sent each other, the open
// member on the right. It opens at the message that was clicked and walks back
// through older pages until it has it; "latest" jumps to the end. Read-only:
// talking to a member happens in that member's own conversation.
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useExchanges } from '../../api/queries';
import { messageText } from './delivery';
import Markdown from './Markdown';
import { usePerson } from './ExchangeLine';
import { Alert, Button, IconButton, Icons, Spinner } from '../../components/ui';
import type { PersonRef } from '../../components/ui';

// Older pages fetched at most while looking for the clicked message.
const maxSearchPages = 20;

export default function ExchangeOverlay({
  teamId,
  botId,
  peerId,
  anchorId,
  nameOf,
  onClose,
}: {
  teamId: string;
  botId: string;
  peerId: string;
  anchorId: string;
  nameOf: (botId: string) => string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const person = usePerson();
  const exchange = useExchanges(teamId, botId, peerId);
  const { messages } = exchange;
  const scrollRef = useRef<HTMLDivElement>(null);
  const anchorRef = useRef<HTMLLIElement>(null);
  const searched = useRef(0);
  const [placed, setPlaced] = useState(false);
  const [atBottom, setAtBottom] = useState(true);
  const hasAnchor = messages.some((message) => message.id === anchorId);

  // Walk back until the clicked message is loaded.
  useEffect(() => {
    if (hasAnchor || exchange.isFetching || !exchange.hasNextPage || searched.current >= maxSearchPages) return;
    searched.current += 1;
    void exchange.fetchNextPage();
  }, [hasAnchor, exchange.isFetching, exchange.hasNextPage, exchange]);

  // Open at the clicked message once; otherwise at the newest.
  useEffect(() => {
    if (placed || exchange.isLoading) return;
    if (hasAnchor && anchorRef.current) {
      anchorRef.current.scrollIntoView({ block: 'center' });
      setPlaced(true);
    } else if (!exchange.hasNextPage || searched.current >= maxSearchPages) {
      const element = scrollRef.current;
      if (element) element.scrollTop = element.scrollHeight;
      setPlaced(true);
    }
  }, [placed, hasAnchor, exchange.isLoading, exchange.hasNextPage, messages.length]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const loadOlder = async () => {
    const element = scrollRef.current;
    const before = element ? { height: element.scrollHeight, top: element.scrollTop } : null;
    await exchange.fetchNextPage();
    requestAnimationFrame(() => {
      if (element && before) element.scrollTop = before.top + element.scrollHeight - before.height;
    });
  };

  return (
    <section
      role="dialog"
      aria-label={t('conversation.exchange.title', { a: nameOf(botId), b: nameOf(peerId) })}
      className="absolute inset-0 z-20 flex flex-col bg-white dark:bg-neutral-950"
      data-testid="exchange-overlay"
    >
      <header className="flex min-h-12 items-center gap-2 border-b border-neutral-200 px-4 py-2 text-sm dark:border-neutral-800">
        <Person entry={person(botId, nameOf(botId))} />
        <span className="text-xs text-neutral-400" aria-hidden="true">⇄</span>
        <Person entry={person(peerId, nameOf(peerId))} />
        <span className="ml-auto">
          <IconButton icon={Icons.close} label={t('conversation.exchange.close')} size="sm" shape="circle" onClick={onClose} />
        </span>
      </header>
      <div
        ref={scrollRef}
        onScroll={(event) => {
          const element = event.currentTarget;
          setAtBottom(element.scrollHeight - element.scrollTop - element.clientHeight < 48);
        }}
        className="min-h-0 flex-1 overflow-y-auto px-4 py-3"
      >
        {exchange.hasNextPage && (
          <div className="mb-2 flex justify-center">
            <Button variant="ghost" size="sm" shape="pill" onClick={() => void loadOlder()} loading={exchange.isFetchingNextPage}>
              {t('conversation.exchange.older')}
            </Button>
          </div>
        )}
        {exchange.isLoading && (
          <p className="flex justify-center text-neutral-400">
            <Spinner label={t('conversation.exchange.loading')} />
          </p>
        )}
        {exchange.isError && <Alert tone="danger">{t('status.error')}</Alert>}
        {!exchange.isLoading && messages.length === 0 && !exchange.isError && (
          <p className="text-center text-xs text-neutral-500 dark:text-neutral-400">{t('conversation.exchange.empty')}</p>
        )}
        <ul className="space-y-3">
          {messages.map((message) => {
            const mine = message.sourceBotId === botId;
            const anchor = message.id === anchorId;
            return (
              <li key={message.id} ref={anchor ? anchorRef : undefined} className={`flex ${mine ? 'justify-end' : 'justify-start'}`}>
                <div className={`max-w-[85%] space-y-1 ${mine ? 'items-end text-right' : ''}`}>
                  <div className={`flex items-center gap-1.5 text-[11px] text-neutral-500 dark:text-neutral-400 ${mine ? 'justify-end' : ''}`}>
                    <Person entry={person(message.sourceBotId ?? '', nameOf(message.sourceBotId ?? ''))} />
                    <time dateTime={message.createdAt}>{new Date(message.createdAt).toLocaleString()}</time>
                  </div>
                  <div
                    className={`break-words rounded-xl px-3 py-2 text-left text-sm text-neutral-900 dark:text-neutral-100 ${
                      mine ? 'bg-neutral-200/70 dark:bg-neutral-800' : 'bg-neutral-100 dark:bg-neutral-900'
                    } ${anchor ? 'ring-2 ring-signal-500/60' : ''}`}
                    data-anchor={anchor || undefined}
                  >
                    <Markdown text={messageText(message)} teamId={teamId} />
                  </div>
                </div>
              </li>
            );
          })}
        </ul>
      </div>
      {!atBottom && (
        <div className="absolute bottom-4 left-1/2 -translate-x-1/2 shadow-sm">
          <Button
            size="sm"
            shape="pill"
            icon={Icons.expand}
            onClick={() => {
              const element = scrollRef.current;
              if (element) element.scrollTo({ top: element.scrollHeight, behavior: 'smooth' });
            }}
          >
            {t('conversation.exchange.latest')}
          </Button>
        </div>
      )}
    </section>
  );
}

function Person({ entry }: { entry: PersonRef }) {
  return (
    <span className="inline-flex items-center gap-1.5 font-medium text-neutral-700 dark:text-neutral-200">
      {entry.avatar}
      {entry.name}
    </span>
  );
}
