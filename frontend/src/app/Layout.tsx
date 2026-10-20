import { useEffect, useRef, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useTeams } from '../api/queries';
import { BrandMark, Icons, RailItem, SplitHandle } from '../components/ui';
import TeamList from '../features/teams/TeamList';
import Workspace from '../features/workspace/Workspace';
import { WorkspaceProvider, useWorkspace } from '../features/workspace/WorkspaceProvider';
import { MAX_WIDTH, MIN_WIDTH } from '../features/workspace/state';
import { findBotContext } from '../features/teams/teamUtils';
import { useSelectedBot } from './SelectedBotContext';
import { useShell } from './shell';
import { rememberWorkspacePath } from './workspacePath';

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
  const navigate = useNavigate();
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

  const teamList = (
    <aside aria-label={t('nav.teamList')} className="flex h-full w-full flex-col bg-side">
      <TeamList />
    </aside>
  );

  return (
    <WorkspaceProvider teamId={context?.team.id ?? null} botId={botId}>
      <div className="flex h-full bg-side text-neutral-900 dark:text-neutral-100">
        {/* Primary icon rail: the mark and home on top, settings at the bottom. */}
        <nav aria-label={t('app.title')} className="flex w-14 shrink-0 flex-col items-center gap-1 bg-rail py-3">
          <div className="mb-3" title={t('app.title')} aria-hidden="true">
            <BrandMark />
          </div>
          <RailItem icon={Icons.home} label={t('nav.home')} current={location.pathname === '/'} onClick={() => navigate('/')} />
          <div className="mt-auto">
            <RailItem icon={Icons.settings} label={t('nav.settings')} current={location.pathname.startsWith('/settings')} onClick={() => navigate('/settings')} />
          </div>
        </nav>

        {shell.listOpen && <Column>{teamList}</Column>}

        {/* Conversation area: the routed page. */}
        <main className="relative mt-2 flex min-w-0 flex-1 flex-col rounded-t-2xl bg-canvas shadow-sheet">
          <Outlet />
        </main>

        <WorkspaceColumn context={context} />
      </div>
    </WorkspaceProvider>
  );
}

// The side columns stay in the layout at every width; on a small window
// they give way so the conversation keeps most of the room.
function Column({ children }: { children: ReactNode }) {
  return <div className="w-[min(16rem,40vw)] shrink-0">{children}</div>;
}

/**
 * The workspace column: the drag line and the area itself, present only
 * while the workspace is open on a team the user is looking at.
 */
function WorkspaceColumn({ context }: { context: ReturnType<typeof findBotContext> | null }) {
  const { t } = useTranslation();
  const { state, setWidth } = useWorkspace();
  if (!state.open || !context) return null;
  return (
    <>
      <SplitHandle value={state.width} min={MIN_WIDTH} max={MAX_WIDTH} label={t('workspace.resize')} onChange={setWidth} />
      <div style={{ width: state.width }} className="min-h-0 shrink-0">
        <Workspace teamId={context.team.id} bots={context.item.bots} />
      </div>
    </>
  );
}
