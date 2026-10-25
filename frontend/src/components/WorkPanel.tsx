// WorkPanel holds the team's pages: 任务 (team board with questions the user
// owes), 审批 (decided history), 动态 (what the team did, member-to-member
// messages included) and 记忆 (the member's working methods). Activity
// entries can ask it to show a tab or open a task. It floats over the
// conversation as the info card; the browser lives in the workspace.
import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { EmptyState, Icons, TabPanel, Tabs } from './ui';
import { useTeams } from '../api/queries';
import { useSelectedBot } from '../app/SelectedBotContext';
import ActivityList from '../features/activity/ActivityList';
import ApprovalHistoryList from '../features/approvals/ApprovalHistoryList';
import MemoryList from '../features/memories/MemoryList';
import TaskBoardList from '../features/tasks/TaskBoardList';
import { findBotContext } from '../features/teams/teamUtils';
import { takePendingWorkPanelRequest, useWorkPanelRequests, type WorkPanelRequest } from '../app/workPanelNav';

type TabId = 'tasks' | 'approvals' | 'activity' | 'memory';

const TABS: TabId[] = ['tasks', 'approvals', 'activity', 'memory'];

export default function WorkPanel() {
  const { t } = useTranslation();
  const { botId } = useSelectedBot();
  const teams = useTeams(true);
  const [initial] = useState(takePendingWorkPanelRequest);
  const [tab, setTab] = useState<TabId>(initial?.tab ?? 'tasks');
  const [focus, setFocus] = useState<WorkPanelRequest | null>(initial);
  useWorkPanelRequests(
    useCallback((request: WorkPanelRequest) => {
      takePendingWorkPanelRequest();
      setTab(request.tab);
      setFocus(request);
    }, []),
  );

  const context = botId && teams.data ? findBotContext(teams.data.items, botId) : null;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* The conversation header already names the member and the team, so
          the panel opens straight on its tabs. The trailing padding keeps
          the tabs clear of the card's close button on the same line. */}
      <div className="flex items-center gap-2 py-2 pl-3 pr-12">
        <div className="min-w-0 flex-1">
          {context ? (
            <Tabs variant="pill" idPrefix="workpanel" label={t('panel.tablist')} value={tab} onChange={setTab} tabs={TABS.map((id) => ({ id, label: t(`panel.tabs.${id}`) }))} />
          ) : (
            <span className="px-1 text-2xs font-semibold uppercase tracking-[0.12em] text-neutral-500 dark:text-neutral-400">{t('nav.workPanel')}</span>
          )}
        </div>
      </div>

      <TabPanel idPrefix="workpanel" value={tab} className="min-h-0 flex-1 overflow-y-auto">
        {!context && <EmptyState icon={<Icons.member size={20} aria-hidden />} title={t('panel.noSelection')} />}
        {/* A team whose folder is gone is read-only: lists stay, actions are off. */}
        <fieldset disabled={context?.team.status === 'directory_missing'} className="m-0 min-w-0 border-0 p-0">
        {context && tab === 'tasks' && (
          <div key={context.bot.id}>
            <TaskBoardList teamId={context.team.id} viewBotId={context.bot.id} bots={context.item.bots} focusTaskId={focus?.taskId} focusNonce={focus?.nonce} />
          </div>
        )}
        {context && tab === 'approvals' && <ApprovalHistoryList key={context.bot.id} teamId={context.team.id} viewBotId={context.bot.id} />}
        {context && tab === 'activity' && <ActivityList key={context.bot.id} teamId={context.team.id} viewBotId={context.bot.id} bots={context.item.bots} />}
        {context && tab === 'memory' && <MemoryList key={context.bot.id} botId={context.bot.id} />}
        </fieldset>
        {context && <p className="py-2 text-center text-2xs text-neutral-400 dark:text-neutral-600">{t('panel.end')}</p>}
      </TabPanel>
    </div>
  );
}