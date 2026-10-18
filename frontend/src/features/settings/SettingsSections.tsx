import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import {
  useAuthorizations,
  useEngine,
  useEngineCheck,
  useEngineLogin,
  useEngineLoginCancel,
  useEngineLogout,
  useEngineModels,
  useRevokeAuthorization,
  useStatus,
} from '../../api/queries';
import type { EngineModel, Settings, SettingsPatch } from '../../api/types';
import { apiErrorMessage, errorMessage } from '../../i18n/errors';
import type { SaveState } from './useSettingSaver';

type Save = (field: string, patch: SettingsPatch, rollback?: (persisted: Settings) => void) => void;
type SectionProps = { settings: Settings; save: Save; states: Record<string, SaveState> };

const inputClass =
  'mt-1 w-full rounded-md border border-neutral-300 bg-transparent px-3 py-1.5 text-sm disabled:opacity-50 dark:border-neutral-700';
const secondaryButton =
  'rounded-md border border-neutral-300 px-3 py-1.5 text-sm text-neutral-600 hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800';

function Card({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <section className="rounded-xl border border-neutral-200 bg-white p-5 dark:border-neutral-800 dark:bg-neutral-900">
      {title && <h2 className="mb-3 text-sm font-semibold">{title}</h2>}
      {children}
    </section>
  );
}

/** SaveIndicator reports one field's auto-save next to its label. */
function SaveIndicator({ state }: { state?: SaveState }) {
  const { t } = useTranslation();
  if (!state) return null;
  if (state.status === 'saving') {
    return (
      <span className="text-xs text-neutral-400" role="status">
        {t('settings.saving')}
      </span>
    );
  }
  if (state.status === 'saved') {
    return (
      <span className="text-xs text-green-600" role="status">
        {t('settings.saved')}
      </span>
    );
  }
  return (
    <span className="text-xs text-red-600" role="alert">
      {t('settings.saveFailed')}: {state.message}
    </span>
  );
}

function Field({ label, state, children }: { label: string; state?: SaveState; children: ReactNode }) {
  return (
    <label className="block text-sm">
      <span className="flex items-center justify-between gap-2">
        <span className="text-neutral-500">{label}</span>
        <SaveIndicator state={state} />
      </span>
      {children}
    </label>
  );
}

/** NumberField keeps a draft while typing and saves on blur or Enter. */
function NumberField(props: {
  label: string;
  value: number;
  min: number;
  max?: number;
  disabled?: boolean;
  state?: SaveState;
  onCommit: (value: number, revert: () => void) => void;
}) {
  const [draft, setDraft] = useState(String(props.value));
  useEffect(() => setDraft(String(props.value)), [props.value]);
  const commit = () => {
    const next = Number(draft);
    if (draft.trim() === '' || !Number.isFinite(next) || next === props.value) {
      setDraft(String(props.value));
      return;
    }
    props.onCommit(next, () => setDraft(String(props.value)));
  };
  return (
    <Field label={props.label} state={props.state}>
      <input
        type="number"
        min={props.min}
        max={props.max}
        value={draft}
        disabled={props.disabled}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === 'Enter') commit();
        }}
        className={inputClass}
      />
    </Field>
  );
}

export function GeneralSection({ settings, save, states }: SectionProps) {
  const { t } = useTranslation();
  const [language, setLanguage] = useState(settings.language);
  useEffect(() => setLanguage(settings.language), [settings.language]);
  return (
    <Card>
      <div className="max-w-xs">
        <Field label={t('settings.language')} state={states.language}>
          <select
            value={language}
            onChange={(e) => {
              const value = e.target.value as Settings['language'];
              setLanguage(value);
              save('language', { language: value }, (persisted) => setLanguage(persisted.language));
            }}
            className={inputClass}
          >
            <option value="zh-CN">简体中文</option>
            <option value="en">English</option>
          </select>
        </Field>
      </div>
    </Card>
  );
}

type Tone = 'ok' | 'warn' | 'bad' | 'idle';

const toneClass: Record<Tone, string> = {
  ok: 'bg-green-500',
  warn: 'bg-amber-500',
  bad: 'bg-red-500',
  idle: 'bg-neutral-400',
};

function StatusDot({ tone }: { tone: Tone }) {
  return <span aria-hidden="true" className={`inline-block h-2 w-2 shrink-0 rounded-full ${toneClass[tone]}`} />;
}

/**
 * EngineSection shows what the startup self-check found: Codex and the
 * native hook are detected and authorized by CoBot, never configured by
 * hand. It drives the official Codex login flow, polling every 3s for up to
 * 2 minutes.
 */
export function EngineSection() {
  const { t } = useTranslation();
  const engine = useEngine();
  const login = useEngineLogin();
  const loginCancel = useEngineLoginCancel();
  const logout = useEngineLogout();
  const check = useEngineCheck();
  const [loginPending, setLoginPending] = useState(false);
  const [authUrl, setAuthUrl] = useState('');
  const [loginTimedOut, setLoginTimedOut] = useState(false);
  const [actionError, setActionError] = useState('');

  // Keep the latest refetch function in a ref so the polling interval does
  // not restart on every render.
  const refetchEngine = useRef(engine.refetch);
  refetchEngine.current = engine.refetch;

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

  const startLogin = () => {
    setActionError('');
    login.mutate(undefined, {
      onSuccess: (result) => {
        setLoginPending(true);
        setAuthUrl(result.authUrl);
        setLoginTimedOut(false);
      },
      onError: (err) => setActionError(apiErrorMessage(t, err)),
    });
  };
  const cancelLogin = () => {
    setActionError('');
    loginCancel.mutate(undefined, {
      onSuccess: () => {
        setLoginPending(false);
        setAuthUrl('');
        void refetchEngine.current();
      },
      onError: (err) => setActionError(apiErrorMessage(t, err)),
    });
  };
  const engineLogout = () => {
    setActionError('');
    logout.mutate(undefined, {
      onSuccess: () => {
        setLoginPending(false);
        void refetchEngine.current();
      },
      onError: (err) => setActionError(apiErrorMessage(t, err)),
    });
  };
  const recheck = () => {
    setActionError('');
    check.mutate(undefined, { onError: (err) => setActionError(apiErrorMessage(t, err)) });
  };

  const info = engine.data;
  const statusText = !info
    ? t('status.loading')
    : info.status === 'ready'
      ? t('settings.engineReady')
      : info.status === 'error'
        ? t('settings.engineError')
        : info.status === 'starting'
          ? t('settings.engineStarting')
          : t('settings.engineNotConfigured');
  const loggedIn = info?.loggedIn ?? false;
  const engineTone: Tone = !info ? 'idle' : info.status === 'ready' ? 'ok' : info.status === 'starting' ? 'idle' : 'bad';
  const hookState = info?.capabilities?.find((value) => value.startsWith('codexHook:'))?.slice('codexHook:'.length) ?? 'unavailable';
  const hookKey = hookState === 'trusted' ? 'Trusted' : hookState === 'failed' ? 'Failed' : 'Untrusted';

  return (
    <div className="space-y-6">
      <Card title="Codex">
        <p className="flex items-center gap-2 text-sm" role="status">
          <StatusDot tone={engineTone} />
          <span>
            {statusText}
            {info?.version ? ` · ${info.version}` : ''}
            {info?.status === 'ready' ? ` · ${loggedIn ? t('settings.engineLoggedIn') : t('settings.engineLoggedOut')}` : ''}
          </span>
        </p>
        {info?.binaryPath && (
          <p className="mt-1 break-all pl-4 font-mono text-xs text-neutral-400">
            {t('settings.engineBinary')}: {info.binaryPath}
            {info.protocol ? ` · ${t('settings.engineProtocol')}: ${info.protocol}` : ''}
          </p>
        )}
        {info?.errorKey && (
          <p className="mt-2 text-sm text-red-600" role="alert">
            {errorMessage(t, info.errorKey)}
          </p>
        )}
        <div className="mt-4 flex flex-wrap items-center gap-2">
          {!loggedIn && info?.status === 'ready' && !loginPending && (
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
              {authUrl && (
                <a href={authUrl} target="_blank" rel="noreferrer" className="text-sm underline">
                  {t('engine.openLogin')}
                </a>
              )}
              <span className="text-sm text-neutral-500" role="status">
                {t('engine.waitingLogin')}
              </span>
              <button type="button" onClick={cancelLogin} disabled={loginCancel.isPending} className={secondaryButton}>
                {t('engine.cancelLogin')}
              </button>
            </>
          )}
          {loggedIn && (
            <button type="button" onClick={engineLogout} disabled={logout.isPending} className={secondaryButton}>
              {t('engine.logout')}
            </button>
          )}
          {info && info.status !== 'ready' && (
            <button type="button" onClick={recheck} disabled={check.isPending} className={secondaryButton}>
              {check.isPending ? t('engine.rechecking') : t('engine.recheck')}
            </button>
          )}
        </div>
        {loginTimedOut && (
          <p className="mt-2 text-sm text-amber-600" role="alert">
            {t('engine.loginTimeout')}
          </p>
        )}
      </Card>

      {info?.status === 'ready' && (
        <Card title={t('engine.hook')}>
          <p className="flex flex-wrap items-center gap-2 text-sm" role="status">
            <StatusDot tone={hookState === 'trusted' ? 'ok' : hookState === 'failed' ? 'bad' : 'warn'} />
            <span>{t(`engine.hook${hookKey}`)}</span>
            {hookState !== 'trusted' && (
              <button
                type="button"
                onClick={recheck}
                disabled={check.isPending}
                className="rounded-md border border-neutral-300 px-2 py-0.5 text-xs text-neutral-600 hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800"
              >
                {check.isPending ? t('engine.hookRetrying') : t('engine.hookRetry')}
              </button>
            )}
          </p>
          <p className="mt-1 pl-4 text-xs text-neutral-400">{t(`engine.hook${hookKey}Hint`)}</p>
        </Card>
      )}

      {actionError && (
        <p className="text-sm text-red-600" role="alert">
          {actionError}
        </p>
      )}
    </div>
  );
}

export function ModelsSection({ settings, save, states }: SectionProps) {
  const { t } = useTranslation();
  const engine = useEngine();
  const models = useEngineModels(engine.data?.status === 'ready');
  const [defaultModel, setDefaultModel] = useState(settings.defaultModel);
  useEffect(() => setDefaultModel(settings.defaultModel), [settings.defaultModel]);

  if (engine.data && engine.data.status !== 'ready') {
    return (
      <Card>
        <p className="text-sm text-neutral-500">{t('settings.modelsNeedEngine')}</p>
      </Card>
    );
  }

  const options = (current: string): EngineModel[] => {
    const list = models.data?.models ?? [];
    const known = new Set(list.map((entry) => entry.slug ?? entry.id ?? ''));
    if (current !== '' && !known.has(current)) return [{ slug: current, displayName: current }, ...list];
    return list;
  };
  const modelSelect = (field: 'defaultModel', value: string, set: (v: string) => void) => (
    <div>
      <Field label={t(`settings.${field}`)} state={states[field]}>
        <select
          value={value}
          onChange={(e) => {
            const next = e.target.value;
            set(next);
            const patch: SettingsPatch = { defaultModel: next };
            save(field, patch, (persisted) => set(persisted.defaultModel));
          }}
          className={inputClass}
        >
          <option value="">{t('settings.modelUnset')}</option>
          {options(value).map((model) => {
            const id = model.slug ?? model.id ?? '';
            if (id === '') return null;
            return (
              <option key={id} value={id}>
                {model.displayName ?? id}
              </option>
            );
          })}
        </select>
      </Field>
      <p className="mt-1 text-xs text-neutral-400">{t(`settings.${field}Hint`)}</p>
    </div>
  );

  return (
    <Card>
      <div className="grid gap-6 sm:grid-cols-2">
        {modelSelect('defaultModel', defaultModel, setDefaultModel)}
      </div>
    </Card>
  );
}

export function CollaborationSection({ settings, save, states }: SectionProps) {
  const { t } = useTranslation();
  const [budgetScope, setBudgetScope] = useState(settings.budgetScope);
  useEffect(() => setBudgetScope(settings.budgetScope), [settings.budgetScope]);
  return (
    <div className="space-y-6">
      <Card title={t('settings.limitsTitle')}>
        <div className="grid gap-4 sm:grid-cols-2">
          <NumberField
            label={t('settings.memberLimit')}
            value={settings.memberLimit}
            min={1}
            max={20}
            state={states.memberLimit}
            onCommit={(value, revert) => save('memberLimit', { memberLimit: value }, revert)}
          />
          <NumberField
            label={t('settings.chainLimit')}
            value={settings.chainLimit}
            min={1}
            max={100}
            state={states.chainLimit}
            onCommit={(value, revert) => save('chainLimit', { chainLimit: value }, revert)}
          />
        </div>
      </Card>
      <Card title={t('settings.budget')}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t('settings.budgetScope')} state={states.budgetScope}>
            <select
              value={budgetScope}
              onChange={(e) => {
                const value = e.target.value as Settings['budgetScope'];
                setBudgetScope(value);
                save('budgetScope', { budgetScope: value }, (persisted) => setBudgetScope(persisted.budgetScope));
              }}
              className={inputClass}
            >
              <option value="">{t('settings.budgetNone')}</option>
              <option value="bot">{t('settings.budgetBot')}</option>
              <option value="team">{t('settings.budgetTeam')}</option>
            </select>
          </Field>
          <NumberField
            label={t('settings.budgetTokens')}
            value={settings.budgetTokens}
            min={0}
            disabled={budgetScope === ''}
            state={states.budgetTokens}
            onCommit={(value, revert) => save('budgetTokens', { budgetTokens: value }, revert)}
          />
        </div>
      </Card>
    </div>
  );
}

export function SecuritySection() {
  const { t, i18n } = useTranslation();
  const authorizations = useAuthorizations(true);
  const revoke = useRevokeAuthorization();
  const [revokeError, setRevokeError] = useState('');
  const items = authorizations.data?.items ?? [];
  return (
    <Card title={t('settings.authorizations')}>
      <p className="text-xs text-neutral-500">{t('settings.authorizationsHint')}</p>
      {authorizations.isPending && (
        <p className="mt-3 text-sm text-neutral-500" role="status">
          {t('status.loading')}
        </p>
      )}
      {authorizations.isSuccess && items.length === 0 && (
        <p className="mt-3 text-sm text-neutral-500">{t('settings.authorizationsEmpty')}</p>
      )}
      {authorizations.isSuccess && items.length > 0 && (
        <ul className="mt-3 divide-y divide-neutral-100 dark:divide-neutral-800">
          {items.map((item) => (
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
                  revoke.mutate(item.botId, { onError: (error) => setRevokeError(apiErrorMessage(t, error)) });
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
    </Card>
  );
}

export function AboutSection() {
  const { t } = useTranslation();
  const status = useStatus();
  const s = status.data;
  return (
    <Card>
      <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
        <dt className="text-neutral-500">{t('settings.version')}</dt>
        <dd className="font-mono text-xs leading-5">{s?.version}</dd>
        <dt className="text-neutral-500">{t('settings.profile')}</dt>
        <dd>{s?.profile === 'production' ? t('settings.profileProduction') : t('settings.profileDevelopment')}</dd>
        <dt className="text-neutral-500">{t('settings.dataRoot')}</dt>
        <dd className="break-all font-mono text-xs leading-5">{s?.dataRoot}</dd>
      </dl>
    </Card>
  );
}
