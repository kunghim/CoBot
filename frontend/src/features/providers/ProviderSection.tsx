// The model provider section of the settings: what is in use now, what else
// is configured, and the one button that adds another.
//
// Which provider is in use is the server's answer — settings.defaultProvider
// — and so is every change to it: "enable" writes that field, and the row
// under "in use" is the provider it names. Nothing about the choice is kept
// in this browser, so the page cannot drift from the profile.
//
// A piece of interface that more than one view needs belongs in
// components/ui and is registered in the gallery (frontend/src/gallery);
// what stays here belongs to the model provider feature alone.
import { useMemo, useState } from 'react';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import { queryKeys, useDeleteProvider, useProviderPresets, useProviders, useSettings, useUpdateSettings } from '../../api/queries';
import type { Provider, ProviderPreset, Settings } from '../../api/types';
import { Alert, AlertDialog, Badge, Button, Card, Icons, LogoTile, Skeleton, cx, useToast } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import AddProviderDialog, { CODEX_DIALECT, engineOf, presetName, providerMark } from './AddProviderDialog';

interface RowProps {
  provider: Provider;
  preset: ProviderPreset | undefined;
  current: boolean;
  t: TFunction;
  onEdit: () => void;
  onEnable?: () => void;
  onRemove: () => void;
}

/** One configured provider: who it is, where it points, what to do with it. */
function ProviderRow({ provider, preset, current, t, onEdit, onEnable, onRemove }: RowProps) {
  // The official provider signs in with the ChatGPT account and has neither
  // an endpoint nor a key of its own.
  const official = provider.authMode === 'chatgpt';
  const endpoint = provider.endpoints.find((entry) => entry.dialect === CODEX_DIALECT)?.baseUrl ?? provider.endpoints[0]?.baseUrl ?? '';
  const engines = [...new Set(provider.endpoints.map((entry) => engineOf(entry.dialect)))];
  return (
    <div
      className={cx(
        'flex items-start gap-3 rounded-xl border p-3',
        current
          ? 'border-signal-500 ring-1 ring-signal-500'
          : 'border-neutral-200 bg-neutral-100/60 dark:border-neutral-800 dark:bg-white/[0.02]',
      )}
    >
      <LogoTile mark={providerMark(provider, preset)} color={preset?.color} />
      <div className="min-w-0 flex-1">
        <p className="flex flex-wrap items-center gap-1.5 text-sm font-semibold text-neutral-900 dark:text-neutral-50">
          {preset ? presetName(preset, t) : provider.name}
          {current && provider.defaultModel && <Badge mono>{provider.defaultModel}</Badge>}
        </p>
        {official ? (
          <p className="mt-0.5 text-xs leading-5 text-neutral-600 dark:text-neutral-300">{t('providers.officialLogin')}</p>
        ) : (
          <>
            <p className="mt-0.5 truncate font-mono text-xs text-neutral-500 dark:text-neutral-400">{endpoint}</p>
            {current && (
              <p className="mt-0.5 text-xs leading-5 text-neutral-600 dark:text-neutral-300">
                {engines.length ? t('providers.usableWith', { engines: engines.join(t('providers.listSeparator')) }) : t('providers.noEndpoint')}
              </p>
            )}
          </>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-1.5">
        {onEnable && (
          <Button size="sm" onClick={onEnable}>
            {t('providers.enable')}
          </Button>
        )}
        {!official && (
          <Button size="sm" onClick={onEdit}>
            {t('providers.edit')}
          </Button>
        )}
        <Button size="sm" variant="ghost" onClick={onRemove}>
          {t('providers.remove')}
        </Button>
      </div>
    </div>
  );
}

export default function ProviderSection() {
  const { t } = useTranslation();
  const toast = useToast();
  const qc = useQueryClient();
  const providers = useProviders();
  const presets = useProviderPresets();
  const settings = useSettings(true);
  const update = useUpdateSettings();
  const remove = useDeleteProvider();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<Provider | null>(null);
  const [pendingRemove, setPendingRemove] = useState<Provider | null>(null);
  const [removing, setRemoving] = useState(false);
  const [errorText, setErrorText] = useState('');

  const list = providers.data?.providers ?? [];
  const presetList = useMemo(() => presets.data?.presets ?? [], [presets.data]);
  const presetById = useMemo(() => new Map(presetList.map((entry) => [entry.id, entry])), [presetList]);

  // The provider in use is the one the server names; a profile with none
  // yet has nothing under "in use".
  const inUseId = settings.data?.defaultProvider ?? '';
  const current = list.find((entry) => entry.id === inUseId) ?? null;
  const others = current ? list.filter((entry) => entry.id !== current.id) : list;

  const displayName = (provider: Provider) => {
    const preset = provider.presetId ? presetById.get(provider.presetId) : undefined;
    return preset ? presetName(preset, t) : provider.name;
  };

  // The revision the profile is at right now, read when the write happens
  // and not when the page was drawn, so a setting saved in between does
  // not make this write look stale.
  const revision = () => qc.getQueryData<Settings>(queryKeys.settings)?.revision;

  // Enabling a provider writes the profile's default; the server refuses an
  // id it does not know, so the provider is checked against the list first.
  const enable = (provider: Provider) => {
    if (!list.some((entry) => entry.id === provider.id)) {
      setErrorText(t('providers.enableFailed'));
      return;
    }
    setErrorText('');
    update.mutate(
      { patch: { defaultProvider: provider.id }, expectedRevision: revision() },
      {
        onSuccess: () => toast.show({ tone: 'success', title: t('providers.enabled', { name: displayName(provider) }) }),
        onError: (error) => setErrorText(apiErrorMessage(t, error)),
      },
    );
  };

  const runRemove = async () => {
    if (!pendingRemove) return;
    setRemoving(true);
    setErrorText('');
    try {
      await remove.mutateAsync(pendingRemove.id);
      // The profile must not keep pointing at a provider that is gone.
      if (pendingRemove.id === inUseId) {
        await update.mutateAsync({ patch: { defaultProvider: '' }, expectedRevision: revision() });
      }
      toast.show({ tone: 'success', title: t('providers.removed', { name: displayName(pendingRemove) }) });
      setPendingRemove(null);
    } catch (error) {
      setErrorText(apiErrorMessage(t, error));
    } finally {
      setRemoving(false);
    }
  };

  const onSaved = (saved: Provider, modelCount: number) => {
    // The first provider is the one the profile runs with from then on.
    const becomesDefault = !inUseId;
    setDialogOpen(false);
    setEditing(null);
    if (becomesDefault) {
      update.mutate(
        { patch: { defaultProvider: saved.id }, expectedRevision: revision() },
        { onError: (error) => setErrorText(apiErrorMessage(t, error)) },
      );
    }
    toast.show({
      tone: 'success',
      title: t(editing ? 'providers.updated' : becomesDefault ? 'providers.savedFirst' : 'providers.saved', {
        name: displayName(saved),
        count: modelCount,
      }),
      description: becomesDefault ? t('providers.readyToUse') : undefined,
    });
  };

  const groupLabel = 'text-2xs font-semibold uppercase tracking-[0.12em] text-neutral-500 dark:text-neutral-400';
  const emptyBox = 'rounded-xl border border-dashed border-neutral-300 px-4 py-6 text-center text-sm leading-6 text-neutral-600 dark:border-neutral-700 dark:text-neutral-300';

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
        {providers.isPending && <Skeleton className="h-16 w-full" />}
        {providers.isError && <Alert tone="danger">{t('status.error')}</Alert>}
        {providers.isSuccess && (
          <div className="space-y-5">
            <div>
              <p className={groupLabel}>{t('providers.inUse')}</p>
              <div className="mt-2">
                {settings.isPending ? (
                  <Skeleton className="h-16 w-full" />
                ) : current ? (
                  <ProviderRow
                    provider={current}
                    preset={current.presetId ? presetById.get(current.presetId) : undefined}
                    current
                    t={t}
                    onEdit={() => {
                      setEditing(current);
                      setDialogOpen(true);
                    }}
                    onRemove={() => setPendingRemove(current)}
                  />
                ) : list.length === 0 ? (
                  <div className={emptyBox}>{t('providers.empty')}</div>
                ) : (
                  <div className={emptyBox}>{t('providers.noneInUse')}</div>
                )}
              </div>
            </div>
            <div>
              <p className={groupLabel}>{t('providers.added')}</p>
              <div className="mt-2 space-y-2">
                {others.length === 0 ? (
                  <div className={emptyBox}>{t('providers.emptyAdded')}</div>
                ) : (
                  others.map((provider) => (
                    <ProviderRow
                      key={provider.id}
                      provider={provider}
                      preset={provider.presetId ? presetById.get(provider.presetId) : undefined}
                      current={false}
                      t={t}
                      onEdit={() => {
                        setEditing(provider);
                        setDialogOpen(true);
                      }}
                      onEnable={() => enable(provider)}
                      onRemove={() => setPendingRemove(provider)}
                    />
                  ))
                )}
              </div>
            </div>
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
