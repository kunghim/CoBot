// The workspace: a strip of closable tabs and the pane of the selected one.
// It sits beside the conversation with no gap, one shade deeper, and is as
// wide as the user dragged it. A page a member opens becomes a tab without
// stealing the view; the last tab closing takes the workspace down.
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useTeamBrowser } from '../../api/queries';
import type { Bot } from '../../api/types';
import { EmptyState, Icons, TabStrip } from '../../components/ui';
import BrowserTab from './BrowserTab';
import NewTabPage from './NewTabPage';
import { useWorkspace } from './WorkspaceProvider';
import type { WorkspaceTabKind } from './state';

const KIND_ICONS: Record<WorkspaceTabKind, typeof Icons.add> = {
  new: Icons.add,
  browser: Icons.webSearch,
  file: Icons.view,
  changes: Icons.diff,
};

export default function Workspace({ teamId, bots }: { teamId: string; bots: Bot[] }) {
  const { t } = useTranslation();
  const { state, newTab, openTab, focusTab, closeTab, collapse } = useWorkspace();
  const browser = useTeamBrowser(teamId);

  // A page a member opens shows up as a tab (its title follows the page);
  // the view stays where the user put it. A page the user opened keeps its
  // tab's title in step with the page too, but is never opened here: it
  // already has its tab, or the user closed it.
  const openIds = state.tabs.map((tab) => tab.id).join('|');
  useEffect(() => {
    const tabs = browser.data?.tabs;
    if (!tabs) return;
    const open = new Set(openIds.split('|'));
    const memberName = (id: string) => bots.find((bot) => bot.id === id)?.name ?? id;
    for (const page of tabs) {
      const id = `browser:${page.id}`;
      if (page.owner === 'member') {
        const title = `${memberName(page.memberId ?? '')} · ${page.title || page.url || t('browser.blank')}`;
        openTab({ id, kind: 'browser', title, ref: page.id });
      } else if (open.has(id) && (page.title || page.url)) {
        openTab({ id, kind: 'browser', title: page.title || page.url, ref: page.id });
      }
    }
  }, [browser.data, bots, openTab, openIds, t]);

  const active = state.tabs.find((tab) => tab.id === state.activeId) ?? null;

  return (
    <div className="flex h-full min-h-0 flex-col bg-side">
      <TabStrip
        tabs={state.tabs.map((tab) => {
          const Icon = KIND_ICONS[tab.kind];
          return {
            id: tab.id,
            label: tab.kind === 'new' ? t('workspace.newTab') : tab.title || t('browser.blank'),
            icon: <Icon size={13} aria-hidden />,
          };
        })}
        activeId={state.activeId}
        label={t('workspace.tablist')}
        closeTabLabel={t('workspace.closeTab')}
        onSelect={focusTab}
        onClose={closeTab}
        onNew={newTab}
        newTabLabel={t('workspace.newTab')}
        onCollapse={collapse}
        collapseLabel={t('workspace.collapse')}
      />
      <div role="tabpanel" aria-label={t('workspace.tablist')} id={`tabpanel-${state.activeId ?? 'none'}`} className="min-h-0 flex-1 overflow-hidden rounded-tl-xl bg-canvas">
        {active?.kind === 'new' && <NewTabPage teamId={teamId} bots={bots} />}
        {active?.kind === 'browser' && (
          <BrowserTab teamId={teamId} panelTabId={active.ref} onClose={() => closeTab(active.id)} />
        )}
        {(active?.kind === 'file' || active?.kind === 'changes') && (
          <EmptyState icon={<Icons.view size={20} aria-hidden />} title={t('workspace.newPage.later')} />
        )}
        {!active && <EmptyState icon={<Icons.add size={20} aria-hidden />} title={t('workspace.newPage.later')} />}
      </div>
    </div>
  );
}
