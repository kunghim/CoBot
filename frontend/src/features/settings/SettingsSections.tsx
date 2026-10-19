import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  useAuthorizations,
  useAvatarCatalog,
  useEngine,
  useEngineCheck,
  useEngineLogin,
  useEngineLoginCancel,
  useEngineLogout,
  useEngineModels,
  useRevokeAuthorization,
  useStatus,
  useRoles,
  useSyncAvatarCatalog,
} from '../../api/queries';
import { RoleAvatarRow } from '../avatars/AvatarEditors';
import type { EngineModel, Settings, SettingsPatch } from '../../api/types';
import { apiErrorMessage, errorMessage } from '../../i18n/errors';
import { applyAppearance, type Appearance, type ColorMode, type Theme } from '../../app/appearance';
import {
  Alert,
  Button,
  Card,
  DescriptionList,
  EmptyState,
  Field,
  Icons,
  List,
  ListRow,
  NumberInput,
  RadioCards,
  SegmentedControl,
  Select,
  Skeleton,
  StatusDot,
  type Tone,
} from '../../components/ui';

type Save = (field: string, patch: SettingsPatch, rollback?: (persisted: Settings) => void) => void;
type SectionProps = { settings: Settings; save: Save; errors: Record<string, string> };

/**
 * NumberField keeps a draft while the value changes and saves it once the
 * changes settle (or the field loses focus), so typing "15" or a run of
 * stepper clicks makes one save, not one per keystroke.
 */
function NumberField(props: {
  label: string;
  value: number;
  min: number;
  max?: number;
  disabled?: boolean;
  error?: string;
  onCommit: (value: number, revert: () => void) => void;
}) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState(props.value);
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => setDraft(props.value), [props.value]);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const commit = (next: number) => {
    window.clearTimeout(timer.current);
    if (!Number.isFinite(next) || next === props.value) return;
    props.onCommit(next, () => setDraft(props.value));
  };
  return (
    <Field label={props.label} error={props.error}>
      <div onBlur={() => commit(draft)}>
        <NumberInput
          value={draft}
          min={props.min}
          max={props.max}
          disabled={props.disabled}
          decreaseLabel={t('common.decrease')}
          increaseLabel={t('common.increase')}
          onChange={(next) => {
            setDraft(next);
            window.clearTimeout(timer.current);
            timer.current = window.setTimeout(() => commit(next), 700);
          }}
        />
      </div>
    </Field>
  );
}

export function GeneralSection({ settings, save, errors }: SectionProps) {
  const { t } = useTranslation();
  const [language, setLanguage] = useState(settings.language);
  useEffect(() => setLanguage(settings.language), [settings.language]);
  return (
    <Card>
      <div className="max-w-xs">
        <Field label={t('settings.language')} error={errors.language}>
          <Select
            value={language}
            options={[
              { value: 'zh-CN', label: '简体中文' },
              { value: 'en', label: 'English' },
            ]}
            onChange={(next) => {
              const value = next as Settings['language'];
              setLanguage(value);
              save('language', { language: value }, (persisted) => setLanguage(persisted.language));
            }}
          />
        </Field>
      </div>
    </Card>
  );
}

const MODE_ICONS = { system: Icons.system, light: Icons.light, dark: Icons.dark } as const;

// What each theme swatch draws: its rail, paper, a card and its signal color.
const THEMES: Array<{ id: Theme; rail: string; paper: string; card: string; line: string; signal: string }> = [
  { id: 'classic', rail: '#22211e', paper: '#f4f3ef', card: '#fbfaf7', line: '#e1dfd9', signal: '#34322e' },
  { id: 'graphite', rail: '#171e1b', paper: '#f4f6f3', card: '#ffffff', line: '#dbe0da', signal: '#5568f0' },
  { id: 'harbor', rail: '#141c24', paper: '#f2f5f8', card: '#ffffff', line: '#d5dee6', signal: '#1c9f92' },
  { id: 'dusk', rail: '#1f171c', paper: '#f7f4f5', card: '#ffffff', line: '#e1d7dc', signal: '#b04f86' },
];

function ThemeSwatch({ theme }: { theme: (typeof THEMES)[number] }) {
  return (
    <span aria-hidden="true" className="flex h-10 w-14 shrink-0 overflow-hidden rounded-md ring-1 ring-black/10" style={{ background: theme.paper }}>
      <span className="w-2.5" style={{ background: theme.rail }} />
      <span className="flex flex-1 flex-col justify-center gap-1 px-1.5">
        <span className="h-1 w-3/5 rounded-full" style={{ background: theme.line }} />
        <span className="flex items-center gap-1 rounded px-1 py-0.5" style={{ background: theme.card, boxShadow: `inset 0 0 0 1px ${theme.line}` }}>
          <span className="h-1.5 w-1.5 rounded-full" style={{ background: theme.signal }} />
          <span className="h-1 flex-1 rounded-full" style={{ background: theme.line }} />
        </span>
      </span>
    </span>
  );
}

/** Light, dark or following the system, and the palette; applied at once. */
export function AppearanceSection({ settings, save, errors }: SectionProps) {
  const { t } = useTranslation();
  const [appearance, setAppearance] = useState<Appearance>({ colorMode: settings.colorMode, theme: settings.theme });
  useEffect(() => setAppearance({ colorMode: settings.colorMode, theme: settings.theme }), [settings.colorMode, settings.theme]);
  const change = (field: 'colorMode' | 'theme', next: Appearance) => {
    if (next[field] === appearance[field]) return;
    setAppearance(next);
    applyAppearance(next);
    save(field, { [field]: next[field] }, (persisted) => {
      const restored = { colorMode: persisted.colorMode, theme: persisted.theme };
      setAppearance(restored);
      applyAppearance(restored);
    });
  };
  return (
    <>
      <Card>
        <div className="space-y-6">
          <Field label={t('settings.colorMode')} error={errors.colorMode}>
            <div>
              <SegmentedControl<ColorMode>
                label={t('settings.colorMode')}
                value={appearance.colorMode}
                onChange={(mode) => change('colorMode', { ...appearance, colorMode: mode })}
                options={(['system', 'light', 'dark'] as const).map((mode) => ({ value: mode, label: t(`settings.colorModes.${mode}`), icon: MODE_ICONS[mode] }))}
              />
            </div>
          </Field>
          <Field label={t('settings.theme')} error={errors.theme}>
            <RadioCards<Theme>
              label={t('settings.theme')}
              columns={2}
              value={appearance.theme}
              onChange={(theme) => change('theme', { ...appearance, theme })}
              options={THEMES.map((theme) => ({
                value: theme.id,
                label: t(`settings.themes.${theme.id}.name`),
                description: t(`settings.themes.${theme.id}.summary`),
                media: <ThemeSwatch theme={theme} />,
              }))}
            />
          </Field>
        </div>
      </Card>
      <AvatarCatalogCard />
    </>
  );
}

/** Which avatar catalog this profile has, and a way to check for a newer one. */
function AvatarCatalogCard() {
  const { t, i18n } = useTranslation();
  const catalog = useAvatarCatalog();
  const sync = useSyncAvatarCatalog();
  const status = catalog.data?.status;
  const syncing = sync.isPending || status?.syncing === true;
  const name = (names: Record<string, string>) => names[i18n.language] ?? names.en ?? Object.values(names)[0] ?? '';
  return (
    <Card
      title={t('settings.avatarCatalog')}
      action={
        <Button size="sm" icon={Icons.refresh} loading={syncing} onClick={() => sync.mutate()} disabled={syncing}>
          {t('settings.avatarCatalogCheck')}
        </Button>
      }
    >
      <div className="space-y-2 text-sm">
        {catalog.data && catalog.data.series.length === 0 && <p className="text-neutral-600 dark:text-neutral-300">{t('settings.avatarCatalogNone')}</p>}
        {catalog.data?.series.map((series) => (
          <p key={series.id} className="text-neutral-800 dark:text-neutral-100">
            {t('settings.avatarCatalogSeries', { name: name(series.name), version: series.version })}
          </p>
        ))}
        {syncing ? (
          <p className="text-xs text-neutral-500 dark:text-neutral-400" role="status">{t('settings.avatarCatalogSyncing')}</p>
        ) : status?.lastError ? (
          <Alert tone="warning">
            <span className="break-all">{t('settings.avatarCatalogFailed', { error: status.lastError })}</span>
          </Alert>
        ) : status?.lastSyncAt ? (
          <p className="text-xs text-neutral-500 dark:text-neutral-400" role="status">{t('settings.avatarCatalogChecked', { time: new Date(status.lastSyncAt).toLocaleString(i18n.language) })}</p>
        ) : null}
        {sync.isError && <Alert tone="danger">{apiErrorMessage(t, sync.error)}</Alert>}
      </div>
    </Card>
  );
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
  const engineTone: Tone = !info ? 'neutral' : info.status === 'ready' ? 'success' : info.status === 'starting' ? 'neutral' : 'danger';
  const hookState = info?.capabilities?.find((value) => value.startsWith('codexHook:'))?.slice('codexHook:'.length) ?? 'unavailable';
  const hookKey = hookState === 'trusted' ? 'Trusted' : hookState === 'failed' ? 'Failed' : 'Untrusted';
  const hookTone: Tone = hookState === 'trusted' ? 'success' : hookState === 'failed' ? 'danger' : 'warning';

  const details = [
    info?.binaryPath ? { term: t('settings.engineBinary'), value: info.binaryPath, mono: true } : null,
    info?.protocol ? { term: t('settings.engineProtocol'), value: info.protocol, mono: true } : null,
  ].filter((item) => item !== null);

  return (
    <div className="space-y-6">
      <Card title="Codex">
        <div className="space-y-4">
          <p className="flex items-center gap-2 text-sm" role="status">
            <StatusDot tone={engineTone} label={statusText} />
            <span>
              {statusText}
              {info?.version ? ` · ${info.version}` : ''}
              {info?.status === 'ready' ? ` · ${loggedIn ? t('settings.engineLoggedIn') : t('settings.engineLoggedOut')}` : ''}
            </span>
          </p>
          {details.length > 0 && <DescriptionList items={details} />}
          {info?.errorKey && <Alert tone="danger">{errorMessage(t, info.errorKey)}</Alert>}
          <div className="flex flex-wrap items-center gap-2">
            {!loggedIn && info?.status === 'ready' && !loginPending && (
              <Button variant="primary" onClick={startLogin} loading={login.isPending}>
                {t('engine.login')}
              </Button>
            )}
            {loginPending && (
              <>
                {authUrl && (
                  <a href={authUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-sm font-medium text-signal-600 hover:underline dark:text-signal-400">
                    {t('engine.openLogin')}
                    <Icons.openExternal size={14} aria-hidden />
                  </a>
                )}
                <span className="text-sm text-neutral-500 dark:text-neutral-400" role="status">
                  {t('engine.waitingLogin')}
                </span>
                <Button onClick={cancelLogin} loading={loginCancel.isPending}>
                  {t('engine.cancelLogin')}
                </Button>
              </>
            )}
            {loggedIn && (
              <Button onClick={engineLogout} loading={logout.isPending}>
                {t('engine.logout')}
              </Button>
            )}
            {info && info.status !== 'ready' && (
              <Button icon={Icons.retry} onClick={recheck} loading={check.isPending}>
                {check.isPending ? t('engine.rechecking') : t('engine.recheck')}
              </Button>
            )}
          </div>
          {loginTimedOut && <Alert tone="warning">{t('engine.loginTimeout')}</Alert>}
        </div>
      </Card>

      {info?.status === 'ready' && (
        <Card
          title={t('engine.hook')}
          action={
            hookState !== 'trusted' && (
              <Button size="sm" icon={Icons.retry} onClick={recheck} loading={check.isPending}>
                {check.isPending ? t('engine.hookRetrying') : t('engine.hookRetry')}
              </Button>
            )
          }
        >
          <p className="flex items-center gap-2 text-sm" role="status">
            <StatusDot tone={hookTone} label={t(`engine.hook${hookKey}`)} />
            <span>{t(`engine.hook${hookKey}`)}</span>
          </p>
          <p className="mt-1 pl-4 text-xs leading-5 text-neutral-500 dark:text-neutral-400">{t(`engine.hook${hookKey}Hint`)}</p>
        </Card>
      )}

      {actionError && <Alert tone="danger">{actionError}</Alert>}
    </div>
  );
}

export function ModelsSection({ settings, save, errors }: SectionProps) {
  const { t } = useTranslation();
  const engine = useEngine();
  const models = useEngineModels(engine.data?.status === 'ready');
  const [defaultModel, setDefaultModel] = useState(settings.defaultModel);
  useEffect(() => setDefaultModel(settings.defaultModel), [settings.defaultModel]);

  if (engine.data && engine.data.status !== 'ready') {
    return (
      <Card>
        <EmptyState icon={<Icons.engine size={20} aria-hidden />} title={t('settings.modelsNeedEngine')} />
      </Card>
    );
  }

  const options = (current: string): EngineModel[] => {
    const list = models.data?.models ?? [];
    const known = new Set(list.map((entry) => entry.slug ?? entry.id ?? ''));
    if (current !== '' && !known.has(current)) return [{ slug: current, displayName: current }, ...list];
    return list;
  };

  return (
    <Card>
      <div className="max-w-sm">
        <Field label={t('settings.defaultModel')} hint={t('settings.defaultModelHint')} error={errors.defaultModel}>
          <Select
            value={defaultModel}
            options={[
              { value: '', label: t('settings.modelUnset') },
              ...options(defaultModel).flatMap((model) => {
                const id = model.slug ?? model.id ?? '';
                return id === '' ? [] : [{ value: id, label: model.displayName ?? id }];
              }),
            ]}
            onChange={(next) => {
              setDefaultModel(next);
              save('defaultModel', { defaultModel: next }, (persisted) => setDefaultModel(persisted.defaultModel));
            }}
          />
        </Field>
      </div>
    </Card>
  );
}

export function CollaborationSection({ settings, save, errors }: SectionProps) {
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
            error={errors.memberLimit}
            onCommit={(value, revert) => save('memberLimit', { memberLimit: value }, revert)}
          />
          <NumberField
            label={t('settings.chainLimit')}
            value={settings.chainLimit}
            min={1}
            max={100}
            error={errors.chainLimit}
            onCommit={(value, revert) => save('chainLimit', { chainLimit: value }, revert)}
          />
          <NumberField
            label={t('settings.reviewRejectLimit')}
            value={settings.reviewRejectLimit}
            min={1}
            max={20}
            error={errors.reviewRejectLimit}
            onCommit={(value, revert) => save('reviewRejectLimit', { reviewRejectLimit: value }, revert)}
          />
        </div>
      </Card>
      <Card title={t('settings.budget')}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t('settings.budgetScope')} error={errors.budgetScope}>
            <Select
              value={budgetScope}
              options={[
                { value: '', label: t('settings.budgetNone') },
                { value: 'bot', label: t('settings.budgetBot') },
                { value: 'team', label: t('settings.budgetTeam') },
              ]}
              onChange={(next) => {
                const value = next as Settings['budgetScope'];
                setBudgetScope(value);
                save('budgetScope', { budgetScope: value }, (persisted) => setBudgetScope(persisted.budgetScope));
              }}
            />
          </Field>
          <NumberField
            label={t('settings.budgetTokens')}
            value={settings.budgetTokens}
            min={0}
            disabled={budgetScope === ''}
            error={errors.budgetTokens}
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
    <Card title={t('settings.authorizations')} description={t('settings.authorizationsHint')}>
      {authorizations.isPending && <Skeleton className="h-10 w-full" />}
      {authorizations.isSuccess && items.length === 0 && (
        <EmptyState icon={<Icons.permission size={20} aria-hidden />} title={t('settings.authorizationsEmpty')} />
      )}
      {authorizations.isSuccess && items.length > 0 && (
        <List>
          {items.map((item) => (
            <ListRow
              key={item.botId + item.engineEpoch}
              title={<span className="font-mono text-xs">{item.botId.slice(0, 8)}</span>}
              description={
                <span className="font-mono">
                  {item.engineEpoch.slice(0, 8)} · {new Date(item.expiresAt).toLocaleTimeString(i18n.language)}
                </span>
              }
              trailing={
                <Button
                  size="sm"
                  variant="danger"
                  onClick={() => {
                    setRevokeError('');
                    revoke.mutate(item.botId, { onError: (error) => setRevokeError(apiErrorMessage(t, error)) });
                  }}
                >
                  {t('settings.revoke')}
                </Button>
              }
            />
          ))}
        </List>
      )}
      {revokeError && (
        <div className="mt-3">
          <Alert tone="danger">{revokeError}</Alert>
        </div>
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
      <DescriptionList
        items={[
          { term: t('settings.version'), value: s?.version, mono: true },
          { term: t('settings.profile'), value: s?.profile === 'production' ? t('settings.profileProduction') : t('settings.profileDevelopment') },
          { term: t('settings.dataRoot'), value: s?.dataRoot, mono: true },
        ]}
      />
    </Card>
  );
}

/** The role library with each role's avatar; members without their own follow it. */
export function RolesSection() {
  const { t } = useTranslation();
  const roles = useRoles();
  return (
    <Card>
      {roles.isPending && <Skeleton className="h-12 w-full" />}
      {roles.isError && <Alert tone="danger">{t('status.error')}</Alert>}
      {roles.data && (
        <List>
          {roles.data.items.map((role) => (
            <RoleAvatarRow key={role.id} role={role} />
          ))}
        </List>
      )}
    </Card>
  );
}
