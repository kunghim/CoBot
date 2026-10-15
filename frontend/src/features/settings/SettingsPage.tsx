import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import {
  queryKeys,
  useAuthorizations,
  useEngine,
  useEngineLogin,
  useEngineLoginCancel,
  useEngineLogout,
  useEngineModels,
  useRevokeAuthorization,
  useSettings,
  useStatus,
  useUpdateSettings,
} from '../../api/queries';
import { api } from '../../api/client';
import type { EngineModel, SettingsPatch } from '../../api/types';
import { apiErrorMessage, errorMessage } from '../../i18n/errors';

type Feedback = 'idle' | 'saved' | 'failed';

/**
 * SettingsPage edits persisted business settings through the API. Every
 * control maps to a real stored value; the engine section reads the live
 * GET /engine projection, drives the official Codex login flow (polling
 * every 3s for up to 2 minutes) and patches the default/eval models from the
 * engine-provided model list.
 */
export default function SettingsPage() {
  const { t, i18n } = useTranslation();
  const status = useStatus();
  const settings = useSettings(true);
  const update = useUpdateSettings();
  const queryClient = useQueryClient();

  const engine = useEngine();
  const login = useEngineLogin();
  const loginCancel = useEngineLoginCancel();
  const logout = useEngineLogout();
  const models = useEngineModels(engine.data?.status === 'ready');

  const [language, setLanguage] = useState('zh-CN');
  const [enginePath, setEnginePath] = useState('');
  const [defaultModel, setDefaultModel] = useState('');
  const [evalModel, setEvalModel] = useState('');
  const [memberLimit, setMemberLimit] = useState(6);
  const [chainLimit, setChainLimit] = useState(6);
  const [budgetScope, setBudgetScope] = useState('');
  const [budgetTokens, setBudgetTokens] = useState(0);
  const [feedback, setFeedback] = useState<Feedback>('idle');
  const [errorKey, setErrorKey] = useState<string>('');
  const [modelFeedback, setModelFeedback] = useState<Feedback>('idle');
  const [modelErrorText, setModelErrorText] = useState('');

  const authorizations = useAuthorizations(true);
  const revoke = useRevokeAuthorization();
  const [revokeError, setRevokeError] = useState('');
  const [loggingOut, setLoggingOut] = useState(false);
  const [logoutError, setLogoutError] = useState('');

  const [loginPending, setLoginPending] = useState(false);
  const [authUrl, setAuthUrl] = useState('');
  const [loginTimedOut, setLoginTimedOut] = useState(false);
  const [engineActionError, setEngineActionError] = useState('');

  useEffect(() => {
    const s = settings.data;
    if (!s) return;
    setLanguage(s.language);
    setEnginePath(s.enginePath);
    setDefaultModel(s.defaultModel);
    setEvalModel(s.evalModel);
    setMemberLimit(s.memberLimit);
    setChainLimit(s.chainLimit);
    setBudgetScope(s.budgetScope);
    setBudgetTokens(s.budgetTokens);
  }, [settings.data]);

  // Keep the latest refetch function in a ref so the polling interval does
  // not restart on every render.
  const refetchEngine = useRef(engine.refetch);
  refetchEngine.current = engine.refetch;

  // Poll GET /engine every 3s while a login is pending, at most 2 minutes.
  useEffect(() => {
    if (!loginPending) return;
    const deadline = Date.now() + 120_000;
    const interval = window.setInterval(() => {
      if (Date.now() >= deadline) {
        setLoginTimedOut(true);
        window.clearInterval(interval);
        return;
      }
      void refetchEngine.current();
    }, 3_000);
    return () => window.clearInterval(interval);
  }, [loginPending]);

  // The engine reports loggedIn only after the real handshake; that ends
  // the pending state without any optimistic guessing.
  useEffect(() => {
    if (engine.data?.loggedIn) {
      setLoginPending(false);
      setAuthUrl('');
      setLoginTimedOut(false);
    }
  }, [engine.data?.loggedIn]);

  if (settings.isPending) {
    return <div className="p-8 text-sm text-neutral-500">{t('status.loading')}</div>;
  }
  if (settings.isError) {
    return <div className="p-8 text-sm text-red-600">{t('status.error')}</div>;
  }

  const save = () => {
    const patch: SettingsPatch = {
      language: language as SettingsPatch['language'],
      enginePath,
      memberLimit,
      chainLimit,
      budgetScope: budgetScope as SettingsPatch['budgetScope'],
      budgetTokens,
    };
    update.mutate(
      { patch, expectedRevision: settings.data?.revision },
      {
        onSuccess: () => {
          setFeedback('saved');
          setErrorKey('');
          // A saved engine path must be re-probed before the status is
          // trusted again; the models list belongs to the same probe.
          void queryClient.invalidateQueries({ queryKey: queryKeys.engine });
          void queryClient.invalidateQueries({ queryKey: queryKeys.models });
        },
        onError: (err) => {
          setFeedback('failed');
          setErrorKey(apiErrorMessage(t, err));
        },
      },
    );
  };

  const changeModel = (field: 'defaultModel' | 'evalModel', value: string) => {
    if (field === 'defaultModel') setDefaultModel(value);
    else setEvalModel(value);
    const patch: SettingsPatch = field === 'defaultModel' ? { defaultModel: value } : { evalModel: value };
    setModelFeedback('idle');
    setModelErrorText('');
    update.mutate(
      { patch, expectedRevision: settings.data?.revision },
      {
        onSuccess: () => setModelFeedback('saved'),
        onError: (err) => {
          setModelFeedback('failed');
          setModelErrorText(apiErrorMessage(t, err));
          // Roll the select back to the persisted value: the UI must never
          // keep showing a model that was not saved (review C).
          const persisted = settings.data;
          if (persisted) {
            if (field === 'defaultModel') setDefaultModel(persisted.defaultModel);
            else setEvalModel(persisted.evalModel);
          }
        },
      },
    );
  };

  const startLogin = () => {
    setEngineActionError('');
    login.mutate(undefined, {
      onSuccess: (result) => {
        setLoginPending(true);
        setAuthUrl(result.authUrl);
        setLoginTimedOut(false);
      },
      onError: (err) => setEngineActionError(apiErrorMessage(t, err)),
    });
  };

  const cancelLogin = () => {
    setEngineActionError('');
    loginCancel.mutate(undefined, {
      onSuccess: () => {
        setLoginPending(false);
        setAuthUrl('');
        void refetchEngine.current();
      },
      onError: (err) => setEngineActionError(apiErrorMessage(t, err)),
    });
  };

  const engineLogout = () => {
    setEngineActionError('');
    logout.mutate(undefined, {
      onSuccess: () => {
        setLoginPending(false);
        void refetchEngine.current();
      },
      onError: (err) => setEngineActionError(apiErrorMessage(t, err)),
    });
  };

  const engineInfo = engine.data;
  const engineStatusText = !engineInfo
    ? t('status.loading')
    : engineInfo.status === 'ready'
      ? t('settings.engineReady')
      : engineInfo.status === 'error'
        ? t('settings.engineError')
        : engineInfo.status === 'starting'
          ? t('settings.engineStarting')
          : t('settings.engineNotConfigured');
  const loggedIn = engineInfo?.loggedIn ?? false;

  const modelOptions = (current: string): EngineModel[] => {
    const list = models.data?.models ?? [];
    const known = new Set(list.map((entry) => entry.slug ?? entry.id ?? ''));
    if (current !== '' && !known.has(current)) return [{ slug: current, displayName: current }, ...list];
    return list;
  };

  return (
    <div className="h-full overflow-y-auto p-8">
      <div className="mx-auto max-w-2xl space-y-8">
        <h1 className="text-lg font-semibold">{t('settings.title')}</h1>

        <section aria-labelledby="settings-profile" className="rounded-xl border border-neutral-200 bg-white p-5 dark:border-neutral-800 dark:bg-neutral-900">
          <h2 id="settings-profile" className="text-sm font-semibold">
            {t('settings.profile')}
          </h2>
          <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
            <dt className="text-neutral-500">{t('settings.profile')}</dt>
            <dd>
              {status.data?.profile === 'production' ? t('settings.profileProduction') : t('settings.profileDevelopment')}
            </dd>
            <dt className="text-neutral-500">{t('settings.dataRoot')}</dt>
            <dd className="break-all font-mono text-xs">{status.data?.dataRoot}</dd>
          </dl>
        </section>

        <section aria-labelledby="settings-language" className="rounded-xl border border-neutral-200 bg-white p-5 dark:border-neutral-800 dark:bg-neutral-900">
          <h2 id="settings-language" className="text-sm font-semibold">
            {t('settings.language')}
          </h2>
          <select
            aria-label={t('settings.language')}
            value={language}
            onChange={(e) => setLanguage(e.target.value)}
            className="mt-3 rounded-md border border-neutral-300 bg-transparent px-3 py-1.5 text-sm dark:border-neutral-700"
          >
            <option value="zh-CN">简体中文</option>
            <option value="en">English</option>
          </select>
        </section>

        <section aria-labelledby="settings-engine" className="rounded-xl border border-neutral-200 bg-white p-5 dark:border-neutral-800 dark:bg-neutral-900">
          <h2 id="settings-engine" className="text-sm font-semibold">
            {t('settings.engine')}
          </h2>
          <p className="mt-2 text-sm text-neutral-500" role="status">
            {t('settings.engineStatus')}: {engineStatusText}
            {engineInfo?.version ? ` (${engineInfo.version})` : ''} ·{' '}
            {loggedIn ? t('settings.engineLoggedIn') : t('settings.engineLoggedOut')}
          </p>
          {engineInfo?.binaryPath && (
            <p className="mt-1 break-all font-mono text-xs text-neutral-400">
              {t('settings.engineBinary')}: {engineInfo.binaryPath}
              {engineInfo.protocol ? ` · ${t('settings.engineProtocol')}: ${engineInfo.protocol}` : ''}
            </p>
          )}
          {engineInfo?.errorKey && (
            <p className="mt-2 text-sm text-red-600" role="alert">
              {errorMessage(t, engineInfo.errorKey)}
            </p>
          )}

          <div className="mt-3 flex flex-wrap items-center gap-2">
            {!loggedIn && engineInfo?.status === 'ready' && !loginPending && (
              <button
                type="button"
                onClick={startLogin}
                disabled={login.isPending}
                className="rounded-md bg-neutral-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-neutral-700 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
              >
                {t('engine.login')}
              </button>
            )}
            {loginPending && (
              <>
                {authUrl && <a href={authUrl} target="_blank" rel="noreferrer" className="text-sm underline">{t('engine.openLogin')}</a>}
                <span className="text-sm text-neutral-500" role="status">
                  {t('engine.waitingLogin')}
                </span>
                <button
                  type="button"
                  onClick={cancelLogin}
                  disabled={loginCancel.isPending}
                  className="rounded-md border border-neutral-300 px-3 py-1.5 text-sm text-neutral-600 hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800"
                >
                  {t('engine.cancelLogin')}
                </button>
              </>
            )}
            {loggedIn && (
              <button
                type="button"
                onClick={engineLogout}
                disabled={logout.isPending}
                className="rounded-md border border-neutral-300 px-3 py-1.5 text-sm text-neutral-600 hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800"
              >
                {t('engine.logout')}
              </button>
            )}
          </div>
          {loginTimedOut && (
            <p className="mt-2 text-sm text-amber-600" role="alert">
              {t('engine.loginTimeout')}
            </p>
          )}
          {engineActionError && (
            <p className="mt-2 text-sm text-red-600" role="alert">
              {engineActionError}
            </p>
          )}

          <label className="mt-4 block text-sm">
            <span className="text-neutral-500">{t('settings.enginePath')}</span>
            <input
              value={enginePath}
              onChange={(e) => setEnginePath(e.target.value)}
              placeholder="codex"
              className="mt-1 w-full rounded-md border border-neutral-300 bg-transparent px-3 py-1.5 font-mono text-sm dark:border-neutral-700"
            />
            <span className="mt-1 block text-xs text-neutral-400">{t('settings.enginePathHint')}</span>
          </label>

          {models.data && (
            <div className="mt-4 grid gap-4 sm:grid-cols-2">
              <label className="block text-sm">
                <span className="text-neutral-500">{t('settings.defaultModel')}</span>
                <select
                  value={defaultModel}
                  onChange={(e) => changeModel('defaultModel', e.target.value)}
                  className="mt-1 w-full rounded-md border border-neutral-300 bg-transparent px-3 py-1.5 text-sm dark:border-neutral-700"
                >
                  <option value="">{t('settings.modelUnset')}</option>
                  {modelOptions(defaultModel).map((model) => {
                    const value = model.slug ?? model.id ?? '';
                    if (value === '') return null;
                    return (
                      <option key={value} value={value}>
                        {model.displayName ?? value}
                      </option>
                    );
                  })}
                </select>
              </label>
              <label className="block text-sm">
                <span className="text-neutral-500">{t('settings.evalModel')}</span>
                <select
                  value={evalModel}
                  onChange={(e) => changeModel('evalModel', e.target.value)}
                  className="mt-1 w-full rounded-md border border-neutral-300 bg-transparent px-3 py-1.5 text-sm dark:border-neutral-700"
                >
                  <option value="">{t('settings.modelUnset')}</option>
                  {modelOptions(evalModel).map((model) => {
                    const value = model.slug ?? model.id ?? '';
                    if (value === '') return null;
                    return (
                      <option key={value} value={value}>
                        {model.displayName ?? value}
                      </option>
                    );
                  })}
                </select>
              </label>
              {modelFeedback === 'saved' && (
                <p className="text-sm text-green-600 sm:col-span-2" role="status">
                  {t('settings.modelChanged')}
                </p>
              )}
              {modelFeedback === 'failed' && (
                <p className="text-sm text-red-600 sm:col-span-2" role="alert">
                  {modelErrorText}
                </p>
              )}
            </div>
          )}
        </section>

        <section aria-labelledby="settings-limits" className="rounded-xl border border-neutral-200 bg-white p-5 dark:border-neutral-800 dark:bg-neutral-900">
          <h2 id="settings-limits" className="text-sm font-semibold">
            {t('settings.budget')}
          </h2>
          <div className="mt-3 grid gap-4 sm:grid-cols-2">
            <label className="block text-sm">
              <span className="text-neutral-500">{t('settings.memberLimit')}</span>
              <input
                type="number"
                min={1}
                max={20}
                value={memberLimit}
                onChange={(e) => setMemberLimit(Number(e.target.value))}
                className="mt-1 w-full rounded-md border border-neutral-300 bg-transparent px-3 py-1.5 text-sm dark:border-neutral-700"
              />
            </label>
            <label className="block text-sm">
              <span className="text-neutral-500">{t('settings.chainLimit')}</span>
              <input
                type="number"
                min={1}
                max={100}
                value={chainLimit}
                onChange={(e) => setChainLimit(Number(e.target.value))}
                className="mt-1 w-full rounded-md border border-neutral-300 bg-transparent px-3 py-1.5 text-sm dark:border-neutral-700"
              />
            </label>
            <label className="block text-sm">
              <span className="text-neutral-500">{t('settings.budgetScope')}</span>
              <select
                value={budgetScope}
                onChange={(e) => setBudgetScope(e.target.value)}
                className="mt-1 w-full rounded-md border border-neutral-300 bg-transparent px-3 py-1.5 text-sm dark:border-neutral-700"
              >
                <option value="">{t('settings.budgetNone')}</option>
                <option value="bot">{t('settings.budgetBot')}</option>
                <option value="team">{t('settings.budgetTeam')}</option>
              </select>
            </label>
            <label className="block text-sm">
              <span className="text-neutral-500">{t('settings.budgetTokens')}</span>
              <input
                type="number"
                min={0}
                value={budgetTokens}
                disabled={budgetScope === ''}
                onChange={(e) => setBudgetTokens(Number(e.target.value))}
                className="mt-1 w-full rounded-md border border-neutral-300 bg-transparent px-3 py-1.5 text-sm disabled:opacity-50 dark:border-neutral-700"
              />
            </label>
          </div>
        </section>

        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={save}
            disabled={update.isPending}
            className="rounded-md bg-neutral-900 px-4 py-2 text-sm font-medium text-white hover:bg-neutral-700 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
          >
            {t('settings.save')}
          </button>
          {feedback === 'saved' && (
            <span className="text-sm text-green-600" role="status">
              {t('settings.saved')}
            </span>
          )}
          {feedback === 'failed' && (
            <span className="text-sm text-red-600" role="alert">
              {t('settings.saveFailed')}: {errorKey}
            </span>
          )}
        </div>

        <section aria-labelledby="settings-authorizations" className="rounded-xl border border-neutral-200 bg-white p-5 dark:border-neutral-800 dark:bg-neutral-900">
          <h2 id="settings-authorizations" className="text-sm font-semibold">
            {t('settings.authorizations')}
          </h2>
          <p className="mt-1 text-xs text-neutral-500">{t('settings.authorizationsHint')}</p>
          {authorizations.isPending && (
            <p className="mt-3 text-sm text-neutral-500" role="status">
              {t('status.loading')}
            </p>
          )}
          {authorizations.isSuccess && (authorizations.data?.items ?? []).length === 0 && (
            <p className="mt-3 text-sm text-neutral-500">{t('settings.authorizationsEmpty')}</p>
          )}
          {authorizations.isSuccess && (authorizations.data?.items ?? []).length > 0 && (
            <ul className="mt-3 divide-y divide-neutral-100 dark:divide-neutral-800">
              {(authorizations.data?.items ?? []).map((item) => (
                <li key={item.botId + item.engineEpoch} className="flex items-center justify-between gap-3 py-2">
                  <span className="min-w-0">
                    <span className="block truncate font-mono text-xs">{item.botId.slice(0, 8)}</span>
                    <span className="block text-[11px] text-neutral-500">
                      {item.engineEpoch.slice(0, 8)} · {new Date(item.expiresAt).toLocaleTimeString(i18n.language)}
                    </span>
                  </span>
                  <button
                    type="button"
                    onClick={() => {
                      setRevokeError('');
                      revoke.mutate(item.botId, {
                        onError: (error) => setRevokeError(apiErrorMessage(t, error)),
                      });
                    }}
                    className="shrink-0 rounded-md border border-neutral-300 px-2 py-1 text-xs text-red-600 hover:bg-red-50 disabled:opacity-50 dark:border-neutral-700 dark:hover:bg-red-950"
                  >
                    {t('settings.revoke')}
                  </button>
                </li>
              ))}
            </ul>
          )}
          {revokeError && (
            <p className="mt-2 text-sm text-red-600" role="alert">
              {revokeError}
            </p>
          )}
        </section>

        {logoutError && (
          <p className="text-sm text-red-600" role="alert">
            {logoutError}
          </p>
        )}

        <div className="border-t border-neutral-200 pt-4 dark:border-neutral-800">
          <button
            type="button"
            onClick={() => {
              setLogoutError('');
              setLoggingOut(true);
              void api
                .logout()
                .then(() => window.location.reload())
                .catch((error) => {
                  setLoggingOut(false);
                  setLogoutError(apiErrorMessage(t, error));
                });
            }}
            disabled={loggingOut}
            className="text-sm text-neutral-500 underline-offset-2 hover:underline"
          >
            {t('settings.logout')}
          </button>
        </div>
      </div>
    </div>
  );
}
