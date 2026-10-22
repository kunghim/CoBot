// The ChatGPT account's sign-in, as the official provider's card drives it.
//
// The account belongs to the provider list now, not to the engine: the
// engine runs whichever provider a member chose, and this is the one
// provider whose credential is a browser sign-in rather than a key. The
// engine reports loggedIn only after the real handshake, so the pending state
// ends on that answer and never on a guess. Polling runs every 3s for up to
// 2 minutes.
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useEngine, useEngineLogin, useEngineLoginCancel, useEngineLogout } from '../../api/queries';
import { apiErrorMessage } from '../../i18n/errors';
import { desktopBridge } from '../../platform/native-bridge';

export function useChatGPTAccount() {
  const { t } = useTranslation();
  const engine = useEngine();
  const login = useEngineLogin();
  const loginCancel = useEngineLoginCancel();
  const logout = useEngineLogout();
  const [pending, setPending] = useState(false);
  const [authUrl, setAuthUrl] = useState('');
  const [timedOut, setTimedOut] = useState(false);
  const [error, setError] = useState('');

  // Keep the latest refetch function in a ref so the polling interval does
  // not restart on every render.
  const refetchEngine = useRef(engine.refetch);
  refetchEngine.current = engine.refetch;

  useEffect(() => {
    if (!pending) return;
    const deadline = Date.now() + 120_000;
    const interval = window.setInterval(() => {
      if (Date.now() >= deadline) {
        setTimedOut(true);
        window.clearInterval(interval);
        return;
      }
      void refetchEngine.current();
    }, 3_000);
    return () => window.clearInterval(interval);
  }, [pending]);

  useEffect(() => {
    if (engine.data?.loggedIn) {
      setPending(false);
      setAuthUrl('');
      setTimedOut(false);
    }
  }, [engine.data?.loggedIn]);

  const openNative = async (url: string) => {
    try {
      await desktopBridge()!.OpenLoginURL(url);
    } catch {
      setError(t('engine.openLoginFailed'));
    }
  };
  const start = () => {
    setError('');
    login.mutate(undefined, {
      onSuccess: (result) => {
        setPending(true);
        setAuthUrl(result.authUrl);
        setTimedOut(false);
        if (desktopBridge()) void openNative(result.authUrl);
      },
      onError: (err) => setError(apiErrorMessage(t, err)),
    });
  };
  const cancel = () => {
    setError('');
    loginCancel.mutate(undefined, {
      onSuccess: () => {
        setPending(false);
        setAuthUrl('');
        void refetchEngine.current();
      },
      onError: (err) => setError(apiErrorMessage(t, err)),
    });
  };
  const signOut = () => {
    setError('');
    logout.mutate(undefined, {
      onSuccess: () => {
        setPending(false);
        void refetchEngine.current();
      },
      onError: (err) => setError(apiErrorMessage(t, err)),
    });
  };

  return {
    /** The engine is up, so a sign-in can start at all. */
    ready: engine.data?.status === 'ready',
    signedIn: engine.data?.loggedIn ?? false,
    pending,
    authUrl,
    timedOut,
    error,
    clearError: () => setError(''),
    start,
    cancel,
    signOut,
    openNative,
    starting: login.isPending,
    cancelling: loginCancel.isPending,
    signingOut: logout.isPending,
  };
}
