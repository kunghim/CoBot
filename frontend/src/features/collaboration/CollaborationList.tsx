import LoadMore from '../../components/LoadMore';
// CollaborationList is the "协作记录" page of the right panel: the selected
// member's read-only exchange, participants labeled on every row. Sending is
// not this page's job; the user talks to a bot in the conversation area, and
// bots talk through their own tools.
import { useTranslation } from 'react-i18next';
import { useCollaboration } from '../../api/queries';
import type { Bot, Message } from '../../api/types';
import { messageText } from '../conversations/delivery';

export default function CollaborationList({ botId, bots }: { botId: string; bots: Bot[] }) {
  const { t, i18n } = useTranslation();
  const record = useCollaboration(botId);

  if (record.isPending) {
    return <p className="px-3 py-2 text-sm text-neutral-500" role="status">{t('status.loading')}</p>;
  }
  if (record.isError && !record.data) {
    return <p className="px-3 py-2 text-sm text-red-600" role="alert">{t('collab.loadFailed')}</p>;
  }

  const items = record.data?.items ?? [];
  if (items.length === 0) {
    return <p className="px-3 py-2 text-sm text-neutral-500 dark:text-neutral-400">{t('collab.empty')}</p>;
  }

  // A member that was deleted keeps its messages in others' history.
  const nameOf = (botID: string) => bots.find((bot) => bot.id === botID)?.name ?? t('team.deletedMember');

  return (
    <div>
      <p className="px-3 pb-1 pt-2 text-[11px] text-neutral-400">{t('collab.readOnly')}</p>
      <ul className="space-y-2 px-3 pb-3">
        {items.map((message) => (
          <CollaborationRow key={message.id} message={message} nameOf={nameOf} userLabel={t('tasks.ownerUser')} formatTime={(iso) => new Date(iso).toLocaleString(i18n.language)} />
        ))}
      </ul>
      <LoadMore page={record} />
    </div>
  );
}

function CollaborationRow({
  message,
  nameOf,
  userLabel,
  formatTime,
}: {
  message: Message;
  nameOf: (botID: string) => string;
  userLabel: string;
  formatTime: (iso: string) => string;
}) {
  const { t } = useTranslation();
  const source = message.sourceBotId ? nameOf(message.sourceBotId) : userLabel;
  return (
    <li className="rounded-lg border border-neutral-200 p-2 text-sm dark:border-neutral-700">
      <p className="text-xs font-medium text-neutral-500 dark:text-neutral-400">
        {source} → {nameOf(message.botId)}
        <span className="ml-1 font-normal text-neutral-400">· {formatTime(message.createdAt)}</span>
      </p>
      <p className="mt-1 whitespace-pre-wrap break-words text-neutral-800 dark:text-neutral-200">{messageText(message)}</p>
      <span className="sr-only">{t('collab.readOnly')}</span>
    </li>
  );
}
