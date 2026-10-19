import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Navigate, Route, Routes } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { api, devLinkSecret, desktopSecret, onUnauthorized } from '../api/client';
import { queryKeys, useEventStream, useSnapshot, useStatus } from '../api/queries';
import Layout from './Layout';
import { SelectedBotProvider } from './SelectedBotContext';
import { useAppearance } from './appearance';
import { Button, Card, Icons, Spinner, ToastProvider } from '../components/ui';
import HomePage from '../features/home/HomePage';
import SettingsPage from '../features/settings/SettingsPage';

type AuthState = 'checking' | 'authorized' | 'unauthorized';

/** Reads a startup secret from the URL fragment (product flow), asks the
 * desktop bridge, or — in development only — the Vite dev server. */
async function obtainAndExchangeSecret(): Promise<boolean> {
  const params = new URLSearchParams(window.location.hash.slice(1));
  const fragmentSecret = params.get('cobot-bootstrap');
  if (fragmentSecret) {
    await api.exchangeBootstrap(fragmentSecret);
    history.replaceState(null, '', window.location.pathname + window.location.search);
    return true;
  }
  const nativeSecret = await desktopSecret();
  if (nativeSecret) { await api.exchangeBootstrap(nativeSecret); return true; }
  const devSecret = await devLinkSecret();
  if (devSecret) {
    await api.exchangeBootstrap(devSecret);
    return true;
  }
  return false;
}

export default function App() {
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
      setAuthState('unauthorized');
      return;
    }
    attemptedRef.current = true;
    let cancelled = false;
    void (async () => {
      try {
        const ok = await obtainAndExchangeSecret();
        if (cancelled) return;
        if (!ok) {
          setAuthState('unauthorized');
          return;
        }
        const result = await status.refetch();
        if (cancelled) return;
        setAuthState(result.data?.authorized ? 'authorized' : 'unauthorized');
      } catch {
        if (!cancelled) setAuthState('unauthorized');
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status.isFetched, status.isFetching, status.data?.authorized]);

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
            <span className="mt-0.5 text-neutral-400"><Icons.locked size={20} aria-hidden /></span>
            <div className="min-w-0 flex-1">
              <h1 className="text-lg font-semibold tracking-tight text-neutral-900 dark:text-neutral-50">{t('auth.unauthorizedTitle')}</h1>
              <p className="mt-2 text-sm leading-6 text-neutral-600 dark:text-neutral-300">{t('auth.unauthorizedBody')}</p>
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
        <Route element={<Layout />}>
          <Route path="/" element={<HomePage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </SelectedBotProvider>
    </ToastProvider>
  );
}
