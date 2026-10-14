import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Navigate, Route, Routes } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { api, devLinkSecret, onUnauthorized } from '../api/client';
import { queryKeys, useEventStream, useSnapshot, useStatus } from '../api/queries';
import Layout from './Layout';
import { SelectedBotProvider } from './SelectedBotContext';
import HomePage from '../features/home/HomePage';
import SettingsPage from '../features/settings/SettingsPage';
import { desktopBridge } from '../platform/native-bridge';

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
  const bridge = desktopBridge();
  if (bridge) {
    const handoff = await bridge.Bootstrap().catch(() => null);
    if (handoff?.secret) {
      await api.exchangeBootstrap(handoff.secret);
      return true;
    }
  }
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

  const invalidateAll = useCallback(() => {
    void queryClient.invalidateQueries();
  }, [queryClient]);
  useEventStream(authState === 'authorized', invalidateAll);

  if (authState !== 'authorized') {
    if (authState === 'checking') {
      return (
        <div className="flex h-full items-center justify-center text-sm text-neutral-500" role="status">
          {t('auth.authorizing')}
        </div>
      );
    }
    return (
      <div className="flex h-full items-center justify-center p-6">
        <div className="w-full max-w-md rounded-xl border border-neutral-200 bg-white p-6 shadow-sm dark:border-neutral-700 dark:bg-neutral-900">
          <h1 className="text-lg font-semibold text-neutral-900 dark:text-neutral-100">
            {t('auth.unauthorizedTitle')}
          </h1>
          <p className="mt-2 text-sm leading-6 text-neutral-600 dark:text-neutral-300">{t('auth.unauthorizedBody')}</p>
          <button
            type="button"
            onClick={() => {
              attemptedRef.current = false;
              setAuthState('checking');
              void status.refetch();
            }}
            className="mt-4 rounded-md bg-neutral-900 px-4 py-2 text-sm font-medium text-white hover:bg-neutral-700 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
          >
            {t('auth.retry')}
          </button>
        </div>
      </div>
    );
  }

  return (
    <SelectedBotProvider>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<HomePage />} />
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </SelectedBotProvider>
  );
}
