// WorkPanel is the right column. All five tabs are real: 任务 (team board
// with questions the user owes), 审批 (decided history), 动态 (the durable
// event record), 协作 (the selected member's read-only record) and 记忆 (the
// member's working methods).
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { X } from 'lucide-react';
import { useTeams } from '../api/queries';
import { useSelectedBot } from '../app/SelectedBotContext';
import ActivityList from '../features/activity/ActivityList';
import ApprovalHistoryList from '../features/approvals/ApprovalHistoryList';
import CollaborationList from '../features/collaboration/CollaborationList';
import MemoryList from '../features/memories/MemoryList';
import QuestionList from '../features/questions/QuestionList';
import TaskBoardList from '../features/tasks/TaskBoardList';
import { findBotContext } from '../features/teams/teamUtils';

type TabId = 'tasks' | 'approvals' | 'activity' | 'collab' | 'memory';

const TABS: TabId[] = ['tasks', 'approvals', 'activity', 'collab', 'memory'];

export default function WorkPanel({ onCollapse }: { onCollapse: () => void }) {
  const { t } = useTranslation();
  const { botId } = useSelectedBot();
  const teams = useTeams(true);
  const [tab, setTab] = useState<TabId>('tasks');

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
          aria-expanded={true}
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-neutral-500 transition-colors hover:bg-neutral-200 hover:text-neutral-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-neutral-400 dark:text-neutral-400 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
        >
          <X size={16} aria-hidden="true" />
        </button>
      </div>

      <div
        role="tablist"
        aria-label={t('panel.tablist')}
        className="flex gap-1 border-b border-neutral-200 px-2 py-1.5 dark:border-neutral-800"
        onKeyDown={(event) => {
          const index = TABS.indexOf(tab);
          let next: number;
          if (event.key === 'ArrowRight') next = (index + 1) % TABS.length;
          else if (event.key === 'ArrowLeft') next = (index - 1 + TABS.length) % TABS.length;
          else if (event.key === 'Home') next = 0;
          else if (event.key === 'End') next = TABS.length - 1;
          else return;
          event.preventDefault();
          setTab(TABS[next]);
          // Roving tabindex: focus follows the selection (review C).
          requestAnimationFrame(() => {
            document.getElementById(`workpanel-tab-${TABS[next]}`)?.focus();
          });
        }}
      >
        {TABS.map((id) => (
          <button
            key={id}
            id={`workpanel-tab-${id}`}
            type="button"
            role="tab"
            aria-selected={tab === id}
            aria-controls="workpanel-tabpanel"
            tabIndex={tab === id ? 0 : -1}
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

      <div
        id="workpanel-tabpanel"
        role="tabpanel"
        aria-labelledby={`workpanel-tab-${tab}`}
        className="min-h-0 flex-1 overflow-y-auto"
      >
        {!context && <p className="px-3 py-2 text-sm text-neutral-500 dark:text-neutral-400">{t('panel.noSelection')}</p>}
        {context && tab === 'tasks' && (
          <div key={context.team.id}>
            <QuestionList teamId={context.team.id} />
            <TaskBoardList teamId={context.team.id} bots={context.item.bots} />
          </div>
        )}
        {context && tab === 'approvals' && <ApprovalHistoryList key={context.team.id} teamId={context.team.id} />}
        {context && tab === 'collab' && (
          <CollaborationList key={context.bot.id} botId={context.bot.id} bots={context.item.bots} />
        )}
        {context && tab === 'activity' && <ActivityList key={context.team.id} teamId={context.team.id} />}
        {context && tab === 'memory' && <MemoryList key={context.bot.id} botId={context.bot.id} />}
      </div>
    </div>
  );
}
