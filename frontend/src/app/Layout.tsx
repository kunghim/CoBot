import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Home, PanelLeftClose, PanelLeftOpen, PanelRightClose, PanelRightOpen, Settings } from 'lucide-react';
import { NavLink, Outlet } from 'react-router-dom';

function IconButton({
  label,
  active,
  onClick,
  children,
}: {
  label: string;
  active?: boolean;
  onClick?: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={label}
      aria-label={label}
      aria-pressed={active}
      className="flex h-9 w-9 items-center justify-center rounded-md text-neutral-500 transition-colors hover:bg-neutral-200 hover:text-neutral-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-neutral-400 dark:text-neutral-400 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
    >
      {children}
    </button>
  );
}

/**
 * Layout implements the product shell: a permanent icon rail, an independent
 * collapsible team list, the conversation area (always the current user
 * session) and an independent collapsible work panel. Both panels collapse to
 * icons and stay reachable; on narrow screens they overlay the conversation.
 */
export default function Layout() {
  const { t } = useTranslation();
  const [listOpen, setListOpen] = useState(true);
  const [panelOpen, setPanelOpen] = useState(true);

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

      {/* Team list panel (M2 fills the team tree; M1 shows the real empty state). */}
      {listOpen && (
        <aside
          aria-label={t('nav.toggleTeamList')}
          className="absolute left-14 z-10 h-full w-64 border-r border-neutral-200 bg-white dark:border-neutral-800 dark:bg-neutral-900 lg:static lg:z-auto"
        >
          <div className="flex items-center justify-between px-3 py-3">
            <span className="rounded-full bg-neutral-900 px-2 py-0.5 text-xs font-semibold text-white dark:bg-neutral-100 dark:text-neutral-900">
              cobot
            </span>
            <IconButton label={t('nav.toggleTeamList')} onClick={() => setListOpen(false)}>
              <PanelLeftClose size={18} aria-hidden="true" />
            </IconButton>
          </div>
          <p className="px-4 py-2 text-sm text-neutral-500 dark:text-neutral-400">{t('home.noTeams')}</p>
        </aside>
      )}
      {!listOpen && (
        <div className="flex w-11 shrink-0 items-start justify-center pt-3">
          <IconButton label={t('nav.toggleTeamList')} onClick={() => setListOpen(true)}>
            <PanelLeftOpen size={18} aria-hidden="true" />
          </IconButton>
        </div>
      )}

      {/* Conversation area: the user session with the current bot. */}
      <main className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-12 items-center border-b border-neutral-200 px-4 dark:border-neutral-800">
          <span className="text-sm text-neutral-500 dark:text-neutral-400">{t('home.selectBot')}</span>
          {!panelOpen && (
            <div className="ml-auto">
              <IconButton label={t('nav.toggleWorkPanel')} onClick={() => setPanelOpen(true)}>
                <PanelRightOpen size={18} aria-hidden="true" />
              </IconButton>
            </div>
          )}
        </header>
        <div className="flex flex-1 items-center justify-center p-6 text-sm text-neutral-500 dark:text-neutral-400">
          {t('home.welcome')}
        </div>
      </main>

      {/* Work panel: tasks/approvals/activity/collaboration/memories arrive
          with the team feature; M1 keeps the collapse behavior real. */}
      {panelOpen && (
        <aside
          aria-label={t('nav.toggleWorkPanel')}
          className="hidden w-72 shrink-0 border-l border-neutral-200 bg-white dark:border-neutral-800 dark:bg-neutral-900 lg:block"
        >
          <div className="flex items-center justify-end px-2 py-2">
            <IconButton label={t('nav.toggleWorkPanel')} onClick={() => setPanelOpen(false)}>
              <PanelRightClose size={18} aria-hidden="true" />
            </IconButton>
          </div>
        </aside>
      )}

      <Outlet />
    </div>
  );
}
