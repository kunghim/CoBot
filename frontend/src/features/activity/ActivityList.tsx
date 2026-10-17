// ActivityList is the 动态 tab: what the team did, newest first, each entry
// said in plain words (who did what to which task, who told whom what). It
// is a record, never a place to act; entries link to the task or the
// conversation they are about.
import { useTranslation } from 'react-i18next';
import { useActivity } from '../../api/queries';
import type { ActivityItem, Bot } from '../../api/types';
import { useSelectedBot } from '../../app/SelectedBotContext';
import { showInWorkPanel } from '../../app/workPanelNav';
import LoadMore from '../../components/LoadMore';
import { describeActivity } from './describe';

export default function ActivityList({ teamId, viewBotId, bots }: { teamId: string; viewBotId?: string; bots: Bot[] }) {
  const { t, i18n } = useTranslation();
  const activity = useActivity(teamId, true, viewBotId);
  const { setBotId } = useSelectedBot();

  if (activity.isPending) {
    return <p className="px-3 py-2 text-sm text-neutral-500" role="status">{t('status.loading')}</p>;
  }
  if (activity.isError && !activity.data) {
    return <p className="px-3 py-2 text-sm text-red-600" role="alert">{t('activity.loadFailed')}</p>;
  }
  const items = activity.data?.items ?? [];
  if (items.length === 0) {
    return <p className="px-3 py-2 text-sm text-neutral-500 dark:text-neutral-400">{t('activity.empty')}</p>;
  }
  const names = new Map(bots.map((bot) => [bot.id, bot.name]));
  const nameOf = (id: unknown) => (typeof id === 'string' && id ? names.get(id) ?? t('activity.someone') : t('activity.someone'));
  const formatTime = (iso: string) => {
    const date = new Date(iso);
    const sameDay = date.toDateString() === new Date().toDateString();
    return sameDay ? date.toLocaleTimeString(i18n.language, { hour: '2-digit', minute: '2-digit' }) : date.toLocaleString(i18n.language, { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  };

  return (
    <>
      <ul className="divide-y divide-neutral-100 px-3 py-1 dark:divide-neutral-800">
        {items.map((item) => (
          <ActivityRow
            key={item.id}
            item={item}
            time={formatTime(item.createdAt)}
            nameOf={nameOf}
            openConversation={(botId) => setBotId(botId)}
          />
        ))}
      </ul>
      <LoadMore page={activity} />
    </>
  );
}

function ActivityRow({
  item,
  time,
  nameOf,
  openConversation,
}: {
  item: ActivityItem;
  time: string;
  nameOf: (id: unknown) => string;
  openConversation: (botId: string) => void;
}) {
  const { t } = useTranslation();
  const line = describeActivity(item, nameOf, t);
  const target = line.taskId
    ? () => showInWorkPanel('tasks', line.taskId)
    : line.conversationBotId
      ? () => openConversation(line.conversationBotId!)
      : line.approvals
        ? () => showInWorkPanel('approvals')
        : undefined;
  const body = (
    <>
      <span className="flex items-baseline gap-2">
        <span className="min-w-0 flex-1 text-neutral-800 dark:text-neutral-200">{line.text}</span>
        <span className="shrink-0 text-[11px] text-neutral-400">{time}</span>
      </span>
      {line.quote && <span className="mt-0.5 line-clamp-2 break-all text-neutral-500 dark:text-neutral-400">{line.quote}</span>}
    </>
  );
  return (
    <li className="py-2 text-xs">
      {target ? (
        <button type="button" onClick={target} className="block w-full rounded text-left hover:bg-neutral-50 dark:hover:bg-neutral-800/60">
          {body}
        </button>
      ) : (
        <div>{body}</div>
      )}
    </li>
  );
}
