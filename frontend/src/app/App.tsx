import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Navigate, Route, Routes, useLocation } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { api, devLinkSecret, desktopSecret, onUnauthorized } from '../api/client';
import { queryKeys, useEventStream, useSnapshot, useStatus, useTeams } from '../api/queries';
import ChatLayout from '../features/chats/ChatLayout';
import ChatRoute from '../features/chats/ChatPage';
import { savedMode, lastChatPath, writeLocal } from './mode';
import ModeSwitch from './ModeSwitch';
import Layout from './Layout';
import { SelectedBotProvider } from './SelectedBotContext';
import { useAppearance } from './appearance';
import { AppModeHeader, AppTitleBar, BrandMark, Button, Card, Icons, Spinner, ToastProvider } from '../components/ui';
import { useDesktopWindow, type DesktopWindow } from '../platform/desktopWindow';
import { ShellProvider, ShellToggle, useShell } from './shell';
import { authStateFrom, type AuthState } from './authState';
import HomePage from '../features/home/HomePage';
import SettingsPage from '../features/settings/SettingsPage';

/** Redeems a startup secret if there is one: the URL fragment (product
 * flow), the desktop bridge, or — in development only — the Vite dev
 * server.
 *
 * Getting no secret is not a failure. The core issues the page a session as
 * it loads, so the ordinary case needs nothing from here. */
async function obtainAndExchangeSecret(): Promise<void> {
  const params = new URLSearchParams(window.location.hash.slice(1));
  const fragmentSecret = params.get('cobot-bootstrap');
  if (fragmentSecret) {
    // The fragment is spent either way: a secret that has expired would
    // otherwise be offered again on every reload, and fail again.
    history.replaceState(null, '', window.location.pathname + window.location.search);
    await api.exchangeBootstrap(fragmentSecret);
    return;
  }
  const nativeSecret = await desktopSecret();
  if (nativeSecret) { await api.exchangeBootstrap(nativeSecret); return; }
  const devSecret = await devLinkSecret();
  if (devSecret) { await api.exchangeBootstrap(devSecret); }
}

export default function App() {
  const desktop = useDesktopWindow();
  // The desktop shell hides the system title bar; the app draws its own,
  // in CoBot's theme, above every screen.
  return (
    <ShellProvider titleBar>
      <div className="flex h-full flex-col">
        <TitleBar desktop={desktop} />
        <div className="min-h-0 flex-1">
          <Screens />
        </div>
      </div>
    </ShellProvider>
  );
}

function TitleBar({ desktop }: { desktop: DesktopWindow | null }) {
  const { t } = useTranslation();
  // The toggle belongs to the workspace; Settings has no side panels.
  const path = useLocation().pathname;
  const { listVisible } = useShell();
  const workspace = !path.startsWith('/settings');
  if (!desktop && !workspace) return null;
  return (
    <AppTitleBar
      platform={desktop?.platform ?? 'windows'}
      fullscreen={desktop?.fullscreen}
      flushLeft
      left={workspace ? <AppModeHeader listVisible={listVisible} toggle={<ShellToggle place="titlebar" />}><ModeSwitch value={path.startsWith('/chat') ? 'chat' : 'work'} /></AppModeHeader> : undefined}
      windowControls={desktop ? {
        minimizeLabel: t('window.minimize'),
        maximizeLabel: t('window.maximize'),
        restoreLabel: t('window.restore'),
        closeLabel: t('window.close'),
        maximized: desktop.maximized,
        onMinimize: desktop.minimize,
        onToggleMaximize: desktop.toggleMaximize,
        onClose: desktop.close,
      } : undefined}
    />
  );
}

function Screens() {
  const { t, i18n } = useTranslation();
  const status = useStatus();
  const queryClient = useQueryClient();
  const [authState, setAuthState] = useState<AuthState>('checking');
  const attemptedRef = useRef(false);

  // A lost session (SSE 401, expired cookie) drops back to the auth screen.
  useEffect(() => {
    return onUnauthorized(() => {
      attemptedRef.current = false;
      setAuthState('unauthorized');
      void queryClient.invalidateQueries({ queryKey: queryKeys.status });
    });
  }, [queryClient]);

  // One authorization attempt per fetch cycle: fragment/bridge/dev secret.
  useEffect(() => {
    if (!status.isFetched || status.isFetching) return;
    if (status.data?.authorized) {
      setAuthState('authorized');
      return;
    }
    if (attemptedRef.current) {
      setAuthState(authStateFrom({ authorized: status.data?.authorized, failed: status.isError }));
      return;
    }
    attemptedRef.current = true;
    let cancelled = false;
    void (async () => {
      // A secret that has expired or was already used is no reason to lock
      // the page out: the core issues this page a session as it loads, and
      // only the core's answer decides whether the page is in.
      try {
        await obtainAndExchangeSecret();
      } catch {
        // Ignored on purpose; the status below is the authority.
      }
      if (cancelled) return;
      const result = await status.refetch();
      if (cancelled) return;
      setAuthState(authStateFrom({ authorized: result.data?.authorized, failed: result.isError }));
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status.isFetched, status.isFetching, status.isError, status.data?.authorized]);

  const snapshot = useSnapshot(authState === 'authorized');
  useEffect(() => {
    const lang = snapshot.data?.settings.language;
    if (lang) void i18n.changeLanguage(lang);
  }, [snapshot.data?.settings.language, i18n]);
  useAppearance(snapshot.data?.settings);

  const invalidateAll = useCallback(() => {
    void queryClient.invalidateQueries();
  }, [queryClient]);
  useEventStream(authState === 'authorized' && snapshot.data !== undefined, invalidateAll, snapshot.data?.seq ?? 0);

  if (authState !== 'authorized') {
    if (authState === 'checking') {
      return (
        <div className="flex h-full items-center justify-center gap-2 bg-neutral-50 text-sm text-neutral-500 dark:bg-neutral-950 dark:text-neutral-400" role="status">
          <Spinner size={16} />
          {t('auth.authorizing')}
        </div>
      );
    }
    return (
      <div className="flex h-full items-center justify-center bg-neutral-50 p-6 dark:bg-neutral-950">
        <Card className="w-full max-w-md">
          <div className="flex items-start gap-3">
            <BrandMark size={40} label={t('app.title')} />
            <div className="min-w-0 flex-1">
              <h1 className="text-lg font-semibold tracking-tight text-neutral-900 dark:text-neutral-50">
                {t(authState === 'unreachable' ? 'auth.unreachableTitle' : 'auth.unauthorizedTitle')}
              </h1>
              <p className="mt-2 text-sm leading-6 text-neutral-600 dark:text-neutral-300">
                {t(authState === 'unreachable' ? 'auth.unreachableBody' : 'auth.unauthorizedBody')}
              </p>
              <div className="mt-5">
                <Button
                  variant="primary"
                  icon={Icons.retry}
                  onClick={() => {
                    attemptedRef.current = false;
                    setAuthState('checking');
                    void status.refetch();
                  }}
                >
                  {t('auth.retry')}
                </Button>
              </div>
            </div>
          </div>
        </Card>
      </div>
    );
  }

  return (
    <ToastProvider closeLabel={t('common.close')}>
    <SelectedBotProvider>
      <Routes>
        <Route path="/settings" element={<Navigate to="/settings/general" replace />} />
        <Route path="/settings/:section" element={<SettingsPage />} />
        <Route path="/chat" element={<ChatLayout />}>
          <Route index element={<Navigate to="new" replace />} />
          <Route path="new" element={<ChatRoute />} />
          <Route path=":chatId" element={<ChatRoute />} />
        </Route>
        <Route element={<Layout />}>
          <Route path="/" element={<HomeEntry />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </SelectedBotProvider>
    </ToastProvider>
  );
}

function HomeEntry() {
  const teams = useTeams(true);
  const stored = savedMode();
  const mode = stored ?? (teams.isSuccess ? (teams.data?.items.length ? 'work' : 'chat') : null);
  useEffect(() => { if (mode) writeLocal('cobot.mode', mode); }, [mode]);
  if (!mode) return <div className="m-auto" role="status"><Spinner size={20} /></div>;
  if (mode === 'chat') return <Navigate to={lastChatPath()} replace />;
  return <HomePage />;
}
