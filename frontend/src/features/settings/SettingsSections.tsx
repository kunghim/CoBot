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
import { Monitor, Moon, Sun } from 'lucide-react';
import { applyAppearance, type Appearance, type ColorMode, type Theme } from '../../app/appearance';

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

const COLOR_MODES: ColorMode[] = ['system', 'light', 'dark'];
const MODE_ICONS = { system: Monitor, light: Sun, dark: Moon } as const;

// What each theme swatch draws: its rail, paper, a card and its signal color.
const THEMES: Array<{ id: Theme; rail: string; paper: string; card: string; line: string; signal: string }> = [
  { id: 'graphite', rail: '#171e1b', paper: '#f4f6f3', card: '#ffffff', line: '#dbe0da', signal: '#5568f0' },
  { id: 'harbor', rail: '#141c24', paper: '#f2f5f8', card: '#ffffff', line: '#d5dee6', signal: '#1c9f92' },
  { id: 'dusk', rail: '#1f171c', paper: '#f7f4f5', card: '#ffffff', line: '#e1d7dc', signal: '#e5654b' },
];

/** Light, dark or following the system, and the palette; applied at once. */
export function AppearanceSection({ settings, save, states }: SectionProps) {
  const { t } = useTranslation();
  const [appearance, setAppearance] = useState<Appearance>({ colorMode: settings.colorMode, theme: settings.theme });
  useEffect(() => setAppearance({ colorMode: settings.colorMode, theme: settings.theme }), [settings.colorMode, settings.theme]);
  const change = (field: 'colorMode' | 'theme', next: Appearance) => {
    setAppearance(next);
    applyAppearance(next);
    save(field, { [field]: next[field] }, (persisted) => {
      const restored = { colorMode: persisted.colorMode, theme: persisted.theme };
      setAppearance(restored);
      applyAppearance(restored);
    });
  };
  const optionClass = (active: boolean) =>
    `rounded-lg text-sm transition-colors ${
      active
        ? 'bg-white font-medium text-neutral-900 shadow-sm ring-1 ring-neutral-300 dark:bg-neutral-800 dark:text-neutral-50 dark:ring-neutral-600'
        : 'text-neutral-600 hover:text-neutral-900 dark:text-neutral-300 dark:hover:text-neutral-50'
    }`;
  return (
    <Card>
      <div className="space-y-6">
        <div>
          <div className="flex items-center justify-between gap-2 text-sm">
            <span id="color-mode-label" className="text-neutral-500">{t('settings.colorMode')}</span>
            <SaveIndicator state={states.colorMode} />
          </div>
          <div role="radiogroup" aria-labelledby="color-mode-label" className="mt-2 inline-flex gap-1 rounded-xl bg-neutral-100 p-1 dark:bg-neutral-950">
            {COLOR_MODES.map((mode) => {
              const Icon = MODE_ICONS[mode];
              const active = appearance.colorMode === mode;
              return (
                <button
                  key={mode}
                  type="button"
                  role="radio"
                  aria-checked={active}
                  onClick={() => !active && change('colorMode', { ...appearance, colorMode: mode })}
                  className={`${optionClass(active)} flex items-center gap-1.5 px-3 py-1.5`}
                >
                  <Icon size={14} aria-hidden="true" />
                  {t(`settings.colorModes.${mode}`)}
                </button>
              );
            })}
          </div>
        </div>
        <div>
          <div className="flex items-center justify-between gap-2 text-sm">
            <span id="theme-label" className="text-neutral-500">{t('settings.theme')}</span>
            <SaveIndicator state={states.theme} />
          </div>
          <div role="radiogroup" aria-labelledby="theme-label" className="mt-2 grid gap-3 sm:grid-cols-3">
            {THEMES.map((theme) => {
              const active = appearance.theme === theme.id;
              return (
                <button
                  key={theme.id}
                  type="button"
                  role="radio"
                  aria-checked={active}
                  aria-label={t(`settings.themes.${theme.id}.name`)}
                  onClick={() => !active && change('theme', { ...appearance, theme: theme.id })}
                  className={`overflow-hidden rounded-xl border-2 text-left transition-colors ${
                    active ? 'border-signal-500' : 'border-neutral-200 hover:border-neutral-300 dark:border-neutral-700 dark:hover:border-neutral-600'
                  }`}
                >
                  <span aria-hidden="true" className="flex h-16" style={{ background: theme.paper }}>
                    <span className="w-4" style={{ background: theme.rail }} />
                    <span className="flex flex-1 flex-col justify-center gap-1.5 px-3">
                      <span className="h-2 w-3/5 rounded-full" style={{ background: theme.line }} />
                      <span className="flex items-center gap-1.5 rounded-md px-1.5 py-1" style={{ background: theme.card, boxShadow: `inset 0 0 0 1px ${theme.line}` }}>
                        <span className="h-2 w-2 rounded-full" style={{ background: theme.signal }} />
                        <span className="h-1.5 flex-1 rounded-full" style={{ background: theme.line }} />
                      </span>
                    </span>
                  </span>
                  <span className="block px-3 py-2">
                    <span className="block text-sm font-medium">{t(`settings.themes.${theme.id}.name`)}</span>
                    <span className="mt-0.5 block text-xs text-neutral-500 dark:text-neutral-400">{t(`settings.themes.${theme.id}.summary`)}</span>
                  </span>
                </button>
              );
            })}
          </div>
        </div>
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
          <NumberField
            label={t('settings.reviewRejectLimit')}
            value={settings.reviewRejectLimit}
            min={1}
            max={20}
            state={states.reviewRejectLimit}
            onCommit={(value, revert) => save('reviewRejectLimit', { reviewRejectLimit: value }, revert)}
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
