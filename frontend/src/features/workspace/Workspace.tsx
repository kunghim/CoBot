// The workspace: a strip of closable tabs and the pane of the selected one.
// It sits beside the conversation with no gap, one shade deeper, and is as
// wide as the user dragged it. A page a member opens becomes a tab; the view
// follows the member the user is already reading when it starts using the
// browser, and stays put otherwise (the tab still appears, with a dot). The
// last tab closing takes the workspace down.
import { useEffect, useRef } from 'react';
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

export default function Workspace({ teamId, bots, watchedBotId }: { teamId: string; bots: Bot[]; watchedBotId?: string }) {
  const { t } = useTranslation();
  const { state, newTab, openTab, focusTab, closeTab, collapse, toggleFullView } = useWorkspace();
  const browser = useTeamBrowser(teamId);
  // When the user last acted on the workspace, in ticks of the effect that
  // watches it: a fresh action keeps an auto-follow from yanking the view.
  const lastUserAction = useRef(0);
  // Anything the user does to the workspace - pressing a tab, closing one,
  // dragging the width - says "I am looking on my own"; the auto-follow
  // keeps its hands off for a while after.
  const stripRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const strip = stripRef.current;
    if (!strip) return;
    const mark = () => {
      lastUserAction.current = Date.now();
    };
    strip.addEventListener('pointerdown', mark);
    return () => strip.removeEventListener('pointerdown', mark);
  }, []);

  // A page a member opens shows up as a tab (its title follows the page).
  // The view follows only what the user is reading: when they watch a member
  // that starts using its browser, the workspace opens on that page - the
  // first page of this turn, not every later one. A user busy in the
  // workspace for the last few seconds is left alone; the tab still appears,
  // and the strip marks the page in use.
  const openIds = state.tabs.map((tab) => tab.id).join('|');
  const followMarkRef = useRef<{ botId: string; pageId: string }>({ botId: '', pageId: '' });
  useEffect(() => {
    const tabs = browser.data?.tabs;
    if (!tabs) return;
    const open = new Set(openIds.split('|'));
    const memberName = (id: string) => bots.find((bot) => bot.id === id)?.name ?? id;
    const recentlyTouched = Date.now() - lastUserAction.current < 4000;
    for (const page of tabs) {
      const id = `browser:${page.id}`;
      if (page.owner === 'member') {
        const title = `${memberName(page.memberId ?? '')} · ${page.title || page.url || t('browser.blank')}`;
        // Follow once per (member, page): the watched member's busy page the
        // user is not yet reading opens the view, once; a page already open,
        // a later step in the same page, and a workspace the user touched in
        // the last few seconds never steal the view back.
        const mark = followMarkRef.current;
        const follow = Boolean(
          watchedBotId && page.busy && page.memberId === watchedBotId && !open.has(id) &&
          !(mark.botId === watchedBotId && mark.pageId === page.id) && !recentlyTouched,
        );
        openTab({ id, kind: 'browser', title, ref: page.id }, follow || undefined);
        if (follow && watchedBotId) followMarkRef.current = { botId: watchedBotId, pageId: page.id };
      } else if (open.has(id) && (page.title || page.url)) {
        openTab({ id, kind: 'browser', title: page.title || page.url, ref: page.id });
      }
    }
  }, [browser.data, bots, openTab, openIds, t, watchedBotId, state.open, focusTab]);

  const active = state.tabs.find((tab) => tab.id === state.activeId) ?? null;

  // Full view is a zcode-style flip between the two rooms: ⇧⌘F toggles it
  // while the workspace is open, Escape hands the room back.
  useEffect(() => {
    if (!state.open) return;
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'f' && event.shiftKey && (event.metaKey || event.ctrlKey)) {
        event.preventDefault();
        toggleFullView();
      } else if (event.key === 'Escape' && state.fullView) {
        event.preventDefault();
        toggleFullView();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [state.open, state.fullView, toggleFullView]);

  return (
    <div ref={stripRef} className="flex h-full min-h-0 flex-col bg-side">
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
        fullView={state.fullView}
        onToggleFullView={toggleFullView}
        fullViewLabel={t('workspace.fullView')}
        exitFullViewLabel={t('workspace.exitFullView')}
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
