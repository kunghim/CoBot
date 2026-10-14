// WorkPanel is the right column. For M2 only the 审批 tab is real (read-only
// decided history); 任务/动态/协作/记忆 honestly say they arrive in later
// milestones instead of showing fabricated data.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { X } from 'lucide-react';
import { useTeams } from '../api/queries';
import { useSelectedBot } from '../app/SelectedBotContext';
import ApprovalHistoryList from '../features/approvals/ApprovalHistoryList';
import { findBotContext } from '../features/teams/teamUtils';

type TabId = 'tasks' | 'approvals' | 'activity' | 'collab' | 'memory';

const TABS: TabId[] = ['tasks', 'approvals', 'activity', 'collab', 'memory'];

export default function WorkPanel({ onCollapse }: { onCollapse: () => void }) {
  const { t } = useTranslation();
  const { botId } = useSelectedBot();
  const teams = useTeams(true);
  const [tab, setTab] = useState<TabId>('approvals');

  const context = botId && teams.data ? findBotContext(teams.data.items, botId) : null;
  const scope = context ? `${context.bot.name} · ${context.team.name}` : '';

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-2 border-b border-neutral-200 px-3 py-2 dark:border-neutral-800">
        <div className="min-w-0 flex-1">
          <p className="truncate text-xs font-semibold">{context ? scope : t('panel.noSelection')}</p>
        </div>
        <button
          type="button"
          onClick={onCollapse}
          title={t('nav.toggleWorkPanel')}
          aria-label={t('nav.toggleWorkPanel')}
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-neutral-500 transition-colors hover:bg-neutral-200 hover:text-neutral-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-neutral-400 dark:text-neutral-400 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
        >
          <X size={16} aria-hidden="true" />
        </button>
      </div>

      <div role="tablist" aria-label={t('approvals.historyTitle')} className="flex gap-1 border-b border-neutral-200 px-2 py-1.5 dark:border-neutral-800">
        {TABS.map((id) => (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={tab === id}
            onClick={() => setTab(id)}
            className={`rounded-md px-2 py-1 text-xs transition-colors ${
              tab === id
                ? 'bg-neutral-200 font-medium text-neutral-900 dark:bg-neutral-800 dark:text-neutral-100'
                : 'text-neutral-500 hover:text-neutral-900 dark:text-neutral-400 dark:hover:text-neutral-100'
            }`}
          >
            {t(`panel.tabs.${id}`)}
          </button>
        ))}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {!context && <p className="px-3 py-2 text-sm text-neutral-500 dark:text-neutral-400">{t('panel.noSelection')}</p>}
        {context && tab === 'approvals' && <ApprovalHistoryList key={context.team.id} teamId={context.team.id} />}
        {context && tab !== 'approvals' && (
          <p className="px-3 py-2 text-sm text-neutral-500 dark:text-neutral-400">{t('panel.notReadyYet')}</p>
        )}
      </div>
    </div>
  );
}
