import LoadMore from '../../components/LoadMore';
// ActivityList is the 动态 tab: the team's durable event record, newest
// first. It is a record of what happened, never a chat and never a place to
// act (the plan's rule: 结论仍需写回任务).
import { useTranslation } from 'react-i18next';
import { useActivity } from '../../api/queries';
import type { ActivityItem } from '../../api/types';

export default function ActivityList({ teamId, viewBotId }: { teamId: string; viewBotId?: string }) {
  const { t, i18n } = useTranslation();
  const activity = useActivity(teamId, true, viewBotId);

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
  const formatTime = (iso: string) => new Date(iso).toLocaleString(i18n.language);

  return (
    <><ul className="space-y-1 px-3 py-2">
      {items.map((item) => (
        <ActivityRow key={item.id} item={item} formatTime={formatTime} />
      ))}
    </ul><LoadMore page={activity} /></>
  );
}

function ActivityRow({ item, formatTime }: { item: ActivityItem; formatTime: (iso: string) => string }) {
  const { t } = useTranslation();
  return (
    <li className="rounded-md border border-neutral-100 px-2 py-1.5 text-xs dark:border-neutral-800">
      <span className="font-medium text-neutral-700 dark:text-neutral-300">
        {t(`activity.type.${item.type}`, { defaultValue: item.type })}
      </span>
      <span className="ml-1 text-neutral-400">· {formatTime(item.createdAt)}</span>
      {item.resourceId && (
        <span className="block truncate text-[11px] text-neutral-400" title={item.resourceId}>
          {item.resourceId.slice(0, 8)}
        </span>
      )}
    </li>
  );
}
