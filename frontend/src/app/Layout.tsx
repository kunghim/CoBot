import { useEffect, useRef, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useTeams } from '../api/queries';
import WorkPanel from '../components/WorkPanel';
import { Icons, RailItem, Sheet } from '../components/ui';
import TeamList from '../features/teams/TeamList';
import { useSelectedBot } from './SelectedBotContext';
import { ShellProvider, useShell } from './shell';
import { rememberWorkspacePath } from './workspacePath';

/**
 * Layout implements the product shell: the icon rail, the team list, the
 * conversation (the routed page) and the work panel. On wide windows the
 * panels sit beside the conversation; on narrow ones they slide over it.
 * Their toggles are at the two ends of the page header (see shell.tsx).
 */
export default function Layout() {
  return (
    <ShellProvider>
      <Workspace />
    </ShellProvider>
  );
}

function Workspace() {
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
  }, [teams.data, botId, setBotId]);

  const teamList = (
    <aside aria-label={t('nav.teamList')} className="flex h-full w-full flex-col bg-neutral-100 dark:bg-neutral-900">
      <TeamList onClose={shell.narrow ? () => shell.setListOpen(false) : undefined} />
    </aside>
  );
  const workPanel = (
    <aside aria-label={t('nav.workPanel')} className="flex h-full w-full flex-col bg-white dark:bg-neutral-900">
      <WorkPanel onClose={shell.narrow ? () => shell.setPanelOpen(false) : undefined} />
    </aside>
  );

  return (
    <div className="flex h-full bg-neutral-50 text-neutral-900 dark:bg-neutral-950 dark:text-neutral-100">
      {/* Primary icon rail: the mark and home on top, settings at the bottom. */}
      <nav aria-label={t('app.title')} className="flex w-14 shrink-0 flex-col items-center gap-1 border-r border-neutral-200 bg-neutral-100 py-3 dark:border-neutral-800 dark:bg-neutral-900">
        <div className="mb-3 flex h-9 w-9 items-center justify-center rounded-lg bg-neutral-900 text-[13px] font-bold tracking-tight text-white dark:bg-neutral-100 dark:text-neutral-900" title="CoBot" aria-hidden="true">
          Co
        </div>
        <RailItem icon={Icons.home} label={t('nav.home')} current={location.pathname === '/'} onClick={() => navigate('/')} />
        <div className="mt-auto">
          <RailItem icon={Icons.settings} label={t('nav.settings')} current={location.pathname.startsWith('/settings')} onClick={() => navigate('/settings')} />
        </div>
      </nav>

      {shell.narrow ? (
        <Sheet open={shell.listOpen} onClose={() => shell.setListOpen(false)} side="left" label={t('nav.teamList')}>
          {teamList}
        </Sheet>
      ) : (
        shell.listOpen && <Column side="left">{teamList}</Column>
      )}

      {/* Conversation area: the routed page. */}
      <main className="relative flex min-w-0 flex-1 flex-col">
        <Outlet />
      </main>

      {shell.narrow ? (
        <Sheet open={shell.panelOpen} onClose={() => shell.setPanelOpen(false)} side="right" label={t('nav.workPanel')}>
          {workPanel}
        </Sheet>
      ) : (
        shell.panelOpen && <Column side="right">{workPanel}</Column>
      )}
    </div>
  );
}

function Column({ side, children }: { side: 'left' | 'right'; children: ReactNode }) {
  return <div className={side === 'left' ? 'w-64 shrink-0 border-r border-neutral-200 dark:border-neutral-800' : 'w-72 shrink-0 border-l border-neutral-200 dark:border-neutral-800'}>{children}</div>;
}
