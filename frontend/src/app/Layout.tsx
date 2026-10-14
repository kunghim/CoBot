import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Home, PanelLeftOpen, PanelRightOpen, Settings } from 'lucide-react';
import { NavLink, Outlet } from 'react-router-dom';
import { useTeams } from '../api/queries';
import WorkPanel from '../components/WorkPanel';
import TeamList from '../features/teams/TeamList';
import { useSelectedBot } from './SelectedBotContext';

function IconButton({
  label,
  onClick,
  children,
}: {
  label: string;
  onClick?: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={label}
      aria-label={label}
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
export default function Layout() {
  const { t } = useTranslation();
  const [listOpen, setListOpen] = useState(true);
  const [panelOpen, setPanelOpen] = useState(true);
  const { botId, setBotId } = useSelectedBot();
  const teams = useTeams(true);

  // A selection whose bot no longer exists (team deleted elsewhere) clears
  // itself instead of pointing at nothing.
  useEffect(() => {
    if (!botId || !teams.data) return;
    const exists = teams.data.items.some((item) => item.bots.some((bot) => bot.id === botId));
    if (!exists) setBotId(null);
  }, [teams.data, botId, setBotId]);

  return (
    <div className="flex h-full bg-neutral-50 text-neutral-900 dark:bg-neutral-950 dark:text-neutral-100">
      {/* Primary icon rail: logo + home on top, settings at the bottom. */}
      <nav
        aria-label={t('app.title')}
        className="flex w-14 shrink-0 flex-col items-center border-r border-neutral-200 py-3 dark:border-neutral-800"
      >
        <div
          className="flex h-9 w-9 items-center justify-center rounded-lg bg-neutral-900 text-sm font-bold text-white dark:bg-neutral-100 dark:text-neutral-900"
          title="CoBot"
          aria-hidden="true"
        >
          C
        </div>
        <div className="mt-4 flex flex-col items-center gap-1">
          <NavLink to="/" end aria-label={t('nav.home')} title={t('nav.home')} className="rounded-md">
            {({ isActive }) => (
              <span
                className={`flex h-9 w-9 items-center justify-center rounded-md ${
                  isActive ? 'bg-neutral-200 text-neutral-900 dark:bg-neutral-800 dark:text-neutral-100' : 'text-neutral-500 dark:text-neutral-400'
                }`}
              >
                <Home size={18} aria-hidden="true" />
              </span>
            )}
          </NavLink>
        </div>
        <div className="mt-auto">
          <NavLink to="/settings" aria-label={t('nav.settings')} title={t('nav.settings')} className="rounded-md">
            {({ isActive }) => (
              <span
                className={`flex h-9 w-9 items-center justify-center rounded-md ${
                  isActive ? 'bg-neutral-200 text-neutral-900 dark:bg-neutral-800 dark:text-neutral-100' : 'text-neutral-500 dark:text-neutral-400'
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
          aria-label={t('nav.toggleTeamList')}
          className="absolute left-14 z-10 h-full w-64 border-r border-neutral-200 bg-white dark:border-neutral-800 dark:bg-neutral-900 lg:static lg:z-auto"
        >
          <TeamList onCollapse={() => setListOpen(false)} />
        </aside>
      )}
      {!listOpen && (
        <div className="flex w-11 shrink-0 items-start justify-center pt-3">
          <IconButton label={t('nav.toggleTeamList')} onClick={() => setListOpen(true)}>
            <PanelLeftOpen size={18} aria-hidden="true" />
          </IconButton>
        </div>
      )}

      {/* Conversation area: the routed page (conversation or settings). */}
      <main className="relative flex min-w-0 flex-1 flex-col">
        {!panelOpen && (
          <div className="absolute right-2 top-2 z-10">
            <IconButton label={t('nav.toggleWorkPanel')} onClick={() => setPanelOpen(true)}>
              <PanelRightOpen size={18} aria-hidden="true" />
            </IconButton>
          </div>
        )}
        <Outlet />
      </main>

      {/* Work panel: tasks/approvals/activity/collaboration/memories; only
          approvals is real in M2, the rest say so honestly. */}
      {panelOpen && (
        <aside
          aria-label={t('nav.toggleWorkPanel')}
          className="hidden w-72 shrink-0 border-l border-neutral-200 bg-white dark:border-neutral-800 dark:bg-neutral-900 lg:block"
        >
          <WorkPanel onCollapse={() => setPanelOpen(false)} />
        </aside>
      )}

      {/* Reopen entry for the work panel on narrow screens (hidden on lg
          where the floating button inside main covers it). */}
      {!panelOpen && (
        <div className="flex w-11 shrink-0 items-start justify-center pt-3 lg:hidden">
          <IconButton label={t('nav.toggleWorkPanel')} onClick={() => setPanelOpen(true)}>
            <PanelRightOpen size={18} aria-hidden="true" />
          </IconButton>
        </div>
      )}
    </div>
  );
}
