// The model provider section of the settings: every provider the profile
// knows, each one switched on or off, and the one button that adds another.
//
// Every enabled provider is on offer at once — its models can be picked in
// any conversation — and a disabled one is out of every picker. The ChatGPT
// account is always in the list: it is the provider whose credential is a
// browser sign-in, and the sign-in happens on its card.
//
// A piece of interface that more than one view needs belongs in
// components/ui and is registered in the gallery (frontend/src/gallery);
// what stays here belongs to the model provider feature alone.
import { useMemo, useState } from 'react';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import { useDeleteProvider, useApplyEngineCatalog, useModelCatalog, useProviderPresets, useProviders, useSetProviderEnabled, useUpdateModelCatalog } from '../../api/queries';
import type { Provider, ProviderPreset } from '../../api/types';
import { Alert, AlertDialog, Badge, Button, Card, Icons, LogoTile, Skeleton, Switch, cx, useToast } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { desktopBridge } from '../../platform/native-bridge';
import AddProviderDialog, { CODEX_DIALECT, engineOf, presetName, providerMark } from './AddProviderDialog';
import { isOfficial } from './providerModels';
import { useChatGPTAccount } from './useChatGPTAccount';

/** The ChatGPT account's own sign-in controls. */
function AccountControls({ t }: { t: TFunction }) {
  const account = useChatGPTAccount();
  return (
    <div className="mt-2 space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        {!account.signedIn && !account.pending && (
          <Button size="sm" variant="primary" onClick={account.start} loading={account.starting} disabled={!account.ready}>
            {t('engine.login')}
          </Button>
        )}
        {account.pending && (
          <>
            {account.authUrl && (
              <a
                href={account.authUrl}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1 text-sm font-medium text-signal-600 hover:underline dark:text-signal-400"
                onClick={(event) => {
                  // The desktop shell opens it in the default browser.
                  if (desktopBridge()) {
                    event.preventDefault();
                    account.clearError();
                    void account.openNative(account.authUrl);
                  }
                }}
              >
                {t('engine.openLogin')}
                <Icons.openExternal size={14} aria-hidden />
              </a>
            )}
            <span className="text-sm text-neutral-500 dark:text-neutral-400" role="status">
              {t('engine.waitingLogin')}
            </span>
            <Button size="sm" onClick={account.cancel} loading={account.cancelling}>
              {t('engine.cancelLogin')}
            </Button>
          </>
        )}
        {account.signedIn && (
          <Button size="sm" onClick={account.signOut} loading={account.signingOut}>
            {t('engine.logout')}
          </Button>
        )}
      </div>
      {!account.ready && <p className="text-xs text-neutral-500 dark:text-neutral-400">{t('providers.accountNeedsEngine')}</p>}
      {account.timedOut && <Alert tone="warning">{t('engine.loginTimeout')}</Alert>}
      {account.error && (
        <Alert tone="danger" onClose={account.clearError} closeLabel={t('common.close')}>
          {account.error}
        </Alert>
      )}
    </div>
  );
}

interface RowProps {
  provider: Provider;
  preset: ProviderPreset | undefined;
  signedIn: boolean;
  switching: boolean;
  t: TFunction;
  onToggle: (enabled: boolean) => void;
  onEdit: () => void;
  onRemove: () => void;
}

/** One provider: who it is, where it points, whether it is on offer. */
function ProviderRow({ provider, preset, signedIn, switching, t, onToggle, onEdit, onRemove }: RowProps) {
  const official = isOfficial(provider);
  const name = preset ? presetName(preset, t) : provider.name;
  const endpoint = provider.endpoints.find((entry) => entry.dialect === CODEX_DIALECT)?.baseUrl ?? provider.endpoints[0]?.baseUrl ?? '';
  const engines = [...new Set(provider.endpoints.map((entry) => engineOf(entry.dialect)))];
  const usable = provider.models.filter((entry) => entry.status !== 'failed').length;
  // A provider without any endpoint spoke a dialect CoBot no longer has (the
  // migration that removed it left the row in place, switched off); it stays
  // until the user removes it, and nothing else can be done with it here.
  const legacyClaude = !official && provider.endpoints.length === 0;
  return (
    <div
      className={cx(
        'flex items-start gap-3 rounded-xl border p-3',
        provider.enabled
          ? 'border-neutral-200 bg-neutral-100/60 dark:border-neutral-800 dark:bg-white/[0.02]'
          : 'border-dashed border-neutral-300 dark:border-neutral-700',
      )}
    >
      <LogoTile mark={providerMark(provider, preset)} color={preset?.color} />
      <div className={cx('min-w-0 flex-1', !provider.enabled && 'opacity-60')}>
        <p className="flex flex-wrap items-center gap-1.5 text-sm font-semibold text-neutral-900 dark:text-neutral-50">
          {name}
          {!provider.enabled && <Badge>{t('providers.disabled')}</Badge>}
        </p>
        {official ? (
          <>
            <p className="mt-0.5 text-xs leading-5 text-neutral-600 dark:text-neutral-300">
              {signedIn ? t('providers.accountSignedIn') : t('providers.accountSignedOut')}
            </p>
            <AccountControls t={t} />
          </>
        ) : (
          <>
            {!legacyClaude && (
              <p className="mt-0.5 truncate font-mono text-xs text-neutral-500 dark:text-neutral-400">{endpoint}</p>
            )}
            <p className="mt-0.5 text-xs leading-5 text-neutral-600 dark:text-neutral-300">
              {legacyClaude ? t('providers.unusableClaude') : t('providers.modelSummary', { count: provider.models.length, usable })}
              {engines.length ? ` · ${t('providers.usableWith', { engines: engines.join(t('providers.listSeparator')) })}` : ''}
            </p>
          </>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-1.5">
        {!official && (
          <>
            <Button size="sm" onClick={onEdit}>
              {t('providers.edit')}
            </Button>
            <Button size="sm" className="text-red-600 hover:text-red-700 dark:text-red-400 dark:hover:text-red-300" onClick={onRemove}>
              {t('providers.remove')}
            </Button>
          </>
        )}
        <Switch checked={provider.enabled} disabled={switching || legacyClaude} label={t('providers.enabledLabel')} hideLabel onChange={onToggle} />
      </div>
    </div>
  );
}

/** Which model catalog is in use, and the button that fetches the newest one. */
function ModelCatalogCard() {
  const { t, i18n } = useTranslation();
  const toast = useToast();
  const catalog = useModelCatalog();
  const update = useUpdateModelCatalog();
  const apply = useApplyEngineCatalog();
  const data = catalog.data;
  const result = update.data;
  const when = data?.updatedAt ? new Date(data.updatedAt).toLocaleDateString(i18n.language) : '';
  return (
    <Card
      title={t('providers.catalog.title')}
      action={
        <Button
          icon={Icons.refresh}
          loading={update.isPending}
          onClick={() =>
            update.mutate(undefined, {
              onSuccess: (outcome) => outcome.state === 'updated' && toast.show({ tone: 'success', title: t('providers.catalog.updated', { count: outcome.models }) }),
              onError: (error) => toast.show({ tone: 'warning', duration: 5000, title: apiErrorMessage(t, error) }),
            })
          }
        >
          {t('providers.catalog.update')}
        </Button>
      }
    >
      <p className="text-sm leading-6 text-neutral-600 dark:text-neutral-300">{t('providers.catalog.intro')}</p>
      {data && (
        <p className="mt-2 text-xs text-neutral-500 dark:text-neutral-400">
          {t(`providers.catalog.source.${data.source}`, { count: data.models, date: when })}
        </p>
      )}
      {data && data.engine.stale > 0 && (
        <div className="mt-3 flex flex-wrap items-center gap-3">
          <p className="text-sm text-neutral-600 dark:text-neutral-300">{t('providers.catalog.stale', { count: data.engine.stale })}</p>
          <Button
            size="sm"
            loading={apply.isPending}
            onClick={() =>
              apply.mutate(undefined, {
                onSuccess: (outcome) =>
                  toast.show({ tone: outcome.busy > 0 ? 'info' : 'success', title: t('providers.catalog.applied', { count: outcome.restarted, busy: outcome.busy }) }),
                onError: (error) => toast.show({ tone: 'warning', duration: 5000, title: apiErrorMessage(t, error) }),
              })
            }
          >
            {t('providers.catalog.apply')}
          </Button>
        </div>
      )}
      {result && result.state !== 'updated' && (
        <div className="mt-3">
          <Alert tone="warning">{t(`providers.catalog.${result.state}`)}</Alert>
        </div>
      )}
    </Card>
  );
}

export default function ProviderSection() {
  const { t } = useTranslation();
  const toast = useToast();
  const providers = useProviders();
  const presets = useProviderPresets();
  const account = useChatGPTAccount();
  const remove = useDeleteProvider();
  const setEnabled = useSetProviderEnabled();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<Provider | null>(null);
  const [pendingRemove, setPendingRemove] = useState<Provider | null>(null);
  const [removing, setRemoving] = useState(false);
  const [switchingId, setSwitchingId] = useState('');
  const [errorText, setErrorText] = useState('');

  // The ChatGPT account first, then the others in the order they were added.
  const list = useMemo(() => {
    const all = providers.data?.providers ?? [];
    return [...all.filter(isOfficial), ...all.filter((entry) => !isOfficial(entry))];
  }, [providers.data]);
  const presetList = useMemo(() => presets.data?.presets ?? [], [presets.data]);
  const presetById = useMemo(() => new Map(presetList.map((entry) => [entry.id, entry])), [presetList]);

  const displayName = (provider: Provider) => {
    const preset = provider.presetId ? presetById.get(provider.presetId) : undefined;
    return preset ? presetName(preset, t) : provider.name;
  };

  const toggle = (provider: Provider, enabled: boolean) => {
    setErrorText('');
    setSwitchingId(provider.id);
    setEnabled.mutate(
      { id: provider.id, enabled, expectedRevision: provider.revision },
      {
        onSuccess: () => toast.show({ tone: 'success', title: t(enabled ? 'providers.enabled' : 'providers.disabledToast', { name: displayName(provider) }) }),
        onError: (error) => setErrorText(apiErrorMessage(t, error)),
        onSettled: () => setSwitchingId(''),
      },
    );
  };

  const runRemove = async () => {
    if (!pendingRemove) return;
    setRemoving(true);
    setErrorText('');
    try {
      await remove.mutateAsync(pendingRemove.id);
      toast.show({ tone: 'success', title: t('providers.removed', { name: displayName(pendingRemove) }) });
      setPendingRemove(null);
    } catch (error) {
      setErrorText(apiErrorMessage(t, error));
    } finally {
      setRemoving(false);
    }
  };

  const onSaved = (saved: Provider, modelCount: number) => {
    setDialogOpen(false);
    toast.show({
      tone: 'success',
      title: t(editing ? 'providers.updated' : 'providers.saved', { name: displayName(saved), count: modelCount }),
      description: editing ? undefined : t('providers.readyToUse'),
    });
    setEditing(null);
  };

  return (
    <div className="space-y-6">
      <Card
        title={t('providers.title')}
        action={
          <Button
            variant="primary"
            icon={Icons.add}
            onClick={() => {
              setEditing(null);
              setDialogOpen(true);
            }}
          >
            {t('providers.add')}
          </Button>
        }
      >
        <p className="mb-4 text-sm leading-6 text-neutral-600 dark:text-neutral-300">{t('providers.intro')}</p>
        {providers.isPending && <Skeleton className="h-16 w-full" />}
        {providers.isError && <Alert tone="danger">{t('status.error')}</Alert>}
        {providers.isSuccess && (
          <div className="space-y-2">
            {list.map((provider) => (
              <ProviderRow
                key={provider.id}
                provider={provider}
                preset={provider.presetId ? presetById.get(provider.presetId) : undefined}
                signedIn={account.signedIn}
                switching={switchingId === provider.id}
                t={t}
                onToggle={(enabled) => toggle(provider, enabled)}
                onEdit={() => {
                  setEditing(provider);
                  setDialogOpen(true);
                }}
                onRemove={() => setPendingRemove(provider)}
              />
            ))}
          </div>
        )}
        {errorText && (
          <div className="mt-4">
            <Alert tone="danger" onClose={() => setErrorText('')} closeLabel={t('common.close')}>
              {errorText}
            </Alert>
          </div>
        )}
      </Card>

      <ModelCatalogCard />

      <AddProviderDialog
        open={dialogOpen}
        onClose={() => {
          setDialogOpen(false);
          setEditing(null);
        }}
        presets={presetList}
        providers={list}
        provider={editing}
        onSaved={onSaved}
      />

      <AlertDialog
        open={pendingRemove !== null}
        title={t('providers.removeTitle', { name: pendingRemove ? displayName(pendingRemove) : '' })}
        description={t('providers.removeBody')}
        confirmLabel={t('providers.remove')}
        cancelLabel={t('common.cancel')}
        busy={removing}
        onCancel={() => setPendingRemove(null)}
        onConfirm={() => void runRemove()}
      />
    </div>
  );
}
