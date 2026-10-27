import { useEffect, useRef, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useTeams } from '../api/queries';
import { BrandMark, Icons, RailItem, SidebarHead, SplitHandle, cx } from '../components/ui';
import TeamList from '../features/teams/TeamList';
import Workspace from '../features/workspace/Workspace';
import { WorkspaceProvider, useWorkspace } from '../features/workspace/WorkspaceProvider';
import { MAX_WIDTH, MIN_WIDTH, layoutColumns } from '../features/workspace/state';
import { findBotContext } from '../features/teams/teamUtils';
import { useSelectedBot } from './SelectedBotContext';
import { ConversationHeader, MenuTools, SidebarHeader, WindowCorner, useMacCorner, useMacWindowed, useNativeTitle } from './WindowChrome';
import { useShell, useShellListLayout } from './shell';
import { rememberWorkspacePath } from './workspacePath';
import { useViewportWidth } from './useViewportWidth';

/**
 * Layout implements the product shell: the icon rail, the team list, the
 * conversation (the routed page), and the workspace. The workspace sits
 * beside the conversation with no gap, one shade deeper, and is as wide as
 * the user dragged it. The team list toggle is at the left end of the
 * title bar or the page header (see shell.tsx); the workspace opens from
 * the conversation's own buttons, so its state lives below, per team.
 */
export default function Layout() {
  const { t } = useTranslation();
  const shell = useShell();
  const { botId, setBotId } = useSelectedBot();
  const location = useLocation();
  useEffect(() => rememberWorkspacePath(location.pathname + location.search), [location.pathname, location.search]);
  const teams = useTeams(true);
  // Ids known to have existed: a selection restored from the last visit
  // counts, so a member deleted since then is cleared too.
  const knownSelections = useRef(new Set<string>(botId ? [botId] : []));

  // A selection whose bot no longer exists (team deleted elsewhere) clears
  // itself instead of pointing at nothing.
  useEffect(() => {
    if (!botId || !teams.data) return;
    const exists = teams.data.items.some((item) => item.bots.some((bot) => bot.id === botId));
    if (exists) knownSelections.current.add(botId);
    else if (teams.hasNextPage) { if (!teams.isFetchingNextPage) void teams.fetchNextPage(); }
    else if (knownSelections.current.has(botId) && !teams.isFetching) setBotId(null);
  }, [teams.data, botId, setBotId, teams.hasNextPage, teams.isFetchingNextPage, teams.isFetching]);

  const context = botId && teams.data ? findBotContext(teams.data.items, botId) : null;
  useNativeTitle(context?.team.name ?? t('app.title'));

  const teamList = (
    <aside aria-label={t('nav.teamList')} className="flex min-h-0 w-full flex-1 flex-col bg-side">
      <TeamList />
    </aside>
  );

  return (
    <WorkspaceProvider teamId={context?.team.id ?? null} botId={botId}>
      <Columns context={context} teamList={teamList} listOpen={shell.listOpen} />
    </WorkspaceProvider>
  );
}

/**
 * The columns themselves. They share the window by rule (layoutColumns):
 * the conversation keeps its minimum, the workspace narrows first, then
 * the team list steps out, and on a very small window the workspace and
 * the conversation take turns. What the user set — the list open, the
 * workspace's width — is kept for when the window grows again.
 */
function Columns({ context, teamList, listOpen }: { context: ReturnType<typeof findBotContext> | null; teamList: ReactNode; listOpen: boolean }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { state, collapse } = useWorkspace();
  const viewport = useViewportWidth();
  const layout = layoutColumns(viewport, listOpen, state.open && !!context, state.width, state.fullView);
  useShellListLayout(layout.showList, collapse);
  const macWindowed = useMacWindowed();
  const macCorner = useMacCorner();
  return (
      <div className="relative flex h-full bg-side text-neutral-900 dark:text-neutral-100">
        <WindowCorner />
        {/* Primary icon rail: home on top, settings at the bottom; on macOS
            the traffic-light corner stays clear of its top in every state. */}
        <nav aria-label={t('app.title')} className={cx('flex w-11 shrink-0 flex-col items-center gap-1 bg-rail py-3', macWindowed && 'pt-10')}>
          <div data-testid="rail-logo" className="flex h-10 w-full shrink-0 items-center justify-center"><BrandMark size={26} label={t('app.title')} /></div>
          <RailItem icon={Icons.home} label={t('nav.home')} current={location.pathname === '/'} onClick={() => navigate('/')} />
          <div className="mt-auto">
            <RailItem icon={Icons.settings} label={t('nav.settings')} current={location.pathname.startsWith('/settings')} onClick={() => navigate('/settings')} />
          </div>
        </nav>

        {layout.showList && <Column>{macCorner && <SidebarHead slim aria-hidden />}<MenuTools value={location.pathname.startsWith('/chat') ? 'chat' : 'work'} />{teamList}</Column>}
        <SidebarHeader />

        {/* Conversation area: the routed page. It stays mounted while the
            workspace takes its turn, so a draft and the scroll survive. */}
        <main className={cx('flex min-w-0 flex-1 flex-col', layout.showConversation ? 'flex' : 'hidden')}>
          <ConversationHeader />
          <div className="relative mt-2 flex min-h-0 flex-1 flex-col rounded-t-2xl bg-canvas shadow-sheet"><Outlet /></div>
        </main>

        <WorkspaceColumn context={context} width={layout.workspaceWidth} fill={!layout.showConversation || state.fullView} />
      </div>
  );
}

// The side columns stay in the layout at every width; on a small window
// they give way so the conversation keeps most of the room.
function Column({ children }: { children: ReactNode }) {
  return <div className="flex w-[min(16rem,40vw)] shrink-0 flex-col">{children}</div>;
}

/**
 * The workspace column: the drag line and the area itself, present only
 * while the workspace is open on a team the user is looking at.
 */
function WorkspaceColumn({ context, width, fill }: { context: ReturnType<typeof findBotContext> | null; width: number; fill: boolean }) {
  const { t } = useTranslation();
  const { state, setWidth } = useWorkspace();
  if (!state.open || !context || width <= 0) return null;
  return (
    <>
      <SplitHandle value={state.width} min={MIN_WIDTH} max={MAX_WIDTH} label={t('workspace.resize')} onChange={setWidth} />
      <div style={fill ? undefined : { width }} className={cx('min-h-0', fill ? 'min-w-0 flex-1' : 'shrink-0')}>
        <Workspace teamId={context.team.id} bots={context.item.bots} />
      </div>
    </>
  );
}
