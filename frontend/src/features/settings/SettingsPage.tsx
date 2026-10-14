import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useStatus, useUpdateSettings } from '../../api/queries';
import { useSettings } from '../../api/queries';
import { api, ApiError } from '../../api/client';
import type { SettingsPatch } from '../../api/types';

/**
 * SettingsPage edits persisted business settings through the API. Every
 * control maps to a real stored value; nothing here is a placeholder. Engine
 * models and login flows extend this page in the engine milestone.
 */
export default function SettingsPage() {
  const { t } = useTranslation();
  const status = useStatus();
  const settings = useSettings(true);
  const update = useUpdateSettings();

  const [language, setLanguage] = useState('zh-CN');
  const [enginePath, setEnginePath] = useState('');
  const [memberLimit, setMemberLimit] = useState(6);
  const [chainLimit, setChainLimit] = useState(6);
  const [budgetScope, setBudgetScope] = useState('');
  const [budgetTokens, setBudgetTokens] = useState(0);
  const [feedback, setFeedback] = useState<'idle' | 'saved' | 'failed'>('idle');
  const [errorKey, setErrorKey] = useState<string>('');

  useEffect(() => {
    const s = settings.data;
    if (!s) return;
    setLanguage(s.language);
    setEnginePath(s.enginePath);
    setMemberLimit(s.memberLimit);
    setChainLimit(s.chainLimit);
    setBudgetScope(s.budgetScope);
    setBudgetTokens(s.budgetTokens);
  }, [settings.data]);

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
        },
        onError: (err) => {
          setFeedback('failed');
          setErrorKey(err instanceof ApiError ? err.messageKey : 'error.internal');
        },
      },
    );
  };

  const engine = status.data?.engine;

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
          <p className="mt-2 text-sm text-neutral-500">
            {t('settings.engineStatus')}:{' '}
            {engine?.status === 'ready' ? t('settings.engineReady') : engine?.status === 'error' ? t('settings.engineError') : t('settings.engineNotConfigured')}
            {engine?.version ? ` (${engine.version})` : ''}
          </p>
          <label className="mt-3 block text-sm">
            <span className="text-neutral-500">{t('settings.enginePath')}</span>
            <input
              value={enginePath}
              onChange={(e) => setEnginePath(e.target.value)}
              placeholder="codex"
              className="mt-1 w-full rounded-md border border-neutral-300 bg-transparent px-3 py-1.5 font-mono text-sm dark:border-neutral-700"
            />
            <span className="mt-1 block text-xs text-neutral-400">{t('settings.enginePathHint')}</span>
          </label>
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
              {t('settings.saveFailed')}: {t(errorKey || 'error.internal', errorKey)}
            </span>
          )}
        </div>

        <div className="border-t border-neutral-200 pt-4 dark:border-neutral-800">
          <button
            type="button"
            onClick={() => void api.logout().then(() => window.location.reload())}
            className="text-sm text-neutral-500 underline-offset-2 hover:underline"
          >
            {t('settings.logout')}
          </button>
        </div>
      </div>
    </div>
  );
}
