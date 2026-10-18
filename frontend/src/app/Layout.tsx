import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Home, PanelLeftOpen, PanelRightOpen, Settings } from 'lucide-react';
import { NavLink, Outlet, useLocation } from 'react-router-dom';
import { useTeams } from '../api/queries';
import WorkPanel from '../components/WorkPanel';
import { useWorkPanelRequests } from './workPanelNav';
import TeamList from '../features/teams/TeamList';
import { useSelectedBot } from './SelectedBotContext';
import { rememberWorkspacePath } from './workspacePath';

function IconButton({
  label,
  onClick,
  expanded,
  children,
}: {
  label: string;
  onClick?: () => void;
  expanded?: boolean;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={label}
      aria-label={label}
      aria-expanded={expanded}
      className="flex h-9 w-9 items-center justify-center rounded-md text-neutral-500 transition-colors hover:bg-neutral-200 hover:text-neutral-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-neutral-400 dark:text-neutral-400 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
    >
      {children}
    </button>
  );
}

/**
 * Layout implements the product shell: a permanent icon rail, an independent
 * collapsible team list, the conversation area (always the current user
 * session, rendered by the routed page) and an independent collapsible work
 * panel. Both panels stay reachable; on narrow screens they overlay the
 * conversation.
 */
// On narrow screens both side panels overlay the conversation, so they
// start closed: a first load must show the conversation, not a panel covering
// it. The toggles reopen each one (I05).
function startsOpen(): boolean {
  return typeof window === 'undefined' || window.matchMedia('(min-width: 1024px)').matches;
}

export default function Layout() {
  const { t } = useTranslation();
  const [listOpen, setListOpen] = useState(startsOpen);
  const [panelOpen, setPanelOpen] = useState(startsOpen);
  // Showing something in the work panel opens it first.
  useWorkPanelRequests(useCallback(() => setPanelOpen(true), []));
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

  return (
    <div className="relative flex h-full bg-neutral-50 text-neutral-900 dark:bg-neutral-950 dark:text-neutral-100">
      {/* Primary icon rail: logo + home on top, settings at the bottom. */}
      {/* The rail is the dark spine of the desk in both themes. */}
      <nav
        aria-label={t('app.title')}
        className="flex w-14 shrink-0 flex-col items-center bg-neutral-900 py-3 dark:bg-black/40"
      >
        <div
          className="flex h-9 w-9 items-center justify-center rounded-lg text-[13px] font-bold tracking-tight text-white ring-1 ring-white/15"
          title="CoBot"
          aria-hidden="true"
        >
          Co
        </div>
        <div className="mt-4 flex flex-col items-center gap-1">
          <NavLink to="/" end aria-label={t('nav.home')} title={t('nav.home')} className="rounded-md focus-visible:outline-signal-400">
            {({ isActive }) => (
              <span
                className={`flex h-9 w-9 items-center justify-center rounded-md ${
                  isActive ? 'bg-white/12 text-white' : 'text-neutral-400 hover:bg-white/8 hover:text-white'
                }`}
              >
                <Home size={18} aria-hidden="true" />
              </span>
            )}
          </NavLink>
        </div>
        <div className="mt-auto">
          <NavLink to="/settings" aria-label={t('nav.settings')} title={t('nav.settings')} className="rounded-md focus-visible:outline-signal-400">
            {({ isActive }) => (
              <span
                className={`flex h-9 w-9 items-center justify-center rounded-md ${
                  isActive ? 'bg-white/12 text-white' : 'text-neutral-400 hover:bg-white/8 hover:text-white'
                }`}
              >
                <Settings size={18} aria-hidden="true" />
              </span>
            )}
          </NavLink>
        </div>
      </nav>

      {/* Team list panel: team tree plus the new-team action. */}
      {listOpen && (
        <aside
          aria-label={t('nav.teamList')}
          className="absolute left-14 z-10 h-full w-64 border-r border-neutral-200 bg-neutral-100 shadow-lg dark:border-neutral-800 dark:bg-neutral-900 lg:static lg:z-auto lg:shadow-none"
        >
          <TeamList onCollapse={() => setListOpen(false)} />
        </aside>
      )}
      {!listOpen && (
        <div className="flex w-11 shrink-0 items-start justify-center pt-3">
          <IconButton label={t('nav.toggleTeamList')} expanded={listOpen} onClick={() => setListOpen(true)}>
            <PanelLeftOpen size={18} aria-hidden="true" />
          </IconButton>
        </div>
      )}

      {/* Conversation area: the routed conversation page. */}
      <main className="relative flex min-w-0 flex-1 flex-col">
        {!panelOpen && (
          <div className="absolute right-2 top-2.5 z-10 hidden lg:block">
            <IconButton label={t('nav.toggleWorkPanel')} expanded={panelOpen} onClick={() => setPanelOpen(true)}>
              <PanelRightOpen size={18} aria-hidden="true" />
            </IconButton>
          </div>
        )}
        <Outlet />
      </main>

      {/* Work panel: four tabs (tasks, approvals, activity, memories).
          Member-to-member exchanges open over the conversation instead. */}
      {panelOpen && (
        <aside
          aria-label={t('nav.workPanel')}
          // On narrow screens the panel overlays the conversation (like the
          // team list); below lg it is static in-flow. It must never be
          // "open" yet unreachable, which the old hidden-lg:block did on
          // narrow screens (I05).
          className="absolute right-0 z-10 h-full w-72 border-l border-neutral-200 bg-white shadow-lg dark:border-neutral-800 dark:bg-neutral-900 lg:static lg:z-auto lg:w-72 lg:shrink-0 lg:shadow-none"
        >
          <WorkPanel onCollapse={() => setPanelOpen(false)} />
        </aside>
      )}

      {/* Reopen entry for the work panel on narrow screens (hidden on lg
          where the floating button inside main covers it). */}
      {!panelOpen && (
        <div className="flex w-11 shrink-0 items-start justify-center pt-3 lg:hidden">
          <IconButton label={t('nav.toggleWorkPanel')} expanded={panelOpen} onClick={() => setPanelOpen(true)}>
            <PanelRightOpen size={18} aria-hidden="true" />
          </IconButton>
        </div>
      )}
    </div>
  );
}
