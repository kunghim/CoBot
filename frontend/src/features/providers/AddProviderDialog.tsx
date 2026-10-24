// The one way in and out of a model provider: pick a vendor, give it a key,
// choose the models to keep. The same dialog edits a provider that is
// already configured.
//
// The key never comes back from the server, so an edit that leaves the key
// field empty reuses the stored one — and the check then runs on the
// server (POST /providers/{id}/probe) instead of the stateless one, which
// would need the key in the browser.
//
// A piece of interface that more than one view needs belongs in
// components/ui and is registered in the gallery (frontend/src/gallery);
// what stays here belongs to the model provider feature alone.
import { useEffect, useMemo, useState } from 'react';
import type { TFunction } from 'i18next';
import { Trans, useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import { useReprobeProvider, useSaveProvider } from '../../api/queries';
import type { Provider, ProviderEndpoint, ProviderModel, ProviderPreset, ProviderProbeResult, ProviderRequest } from '../../api/types';
import { Alert, Badge, Button, Dialog, Field, Icons, LogoTile, SearchInput, TextInput, cx, useToast } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import ModelPick, { modelsToProbe, withSelection } from './ModelPick';

// ---------------------------------------------------------------------------
// The vocabulary of dialects and engines, shared with the settings section.
// ---------------------------------------------------------------------------

/** The dialect Codex speaks. */
export const CODEX_DIALECT = 'openai-responses';
/** The dialect Claude Code speaks. */
export const CLAUDE_DIALECT = 'anthropic-messages';

/** The engine that can speak a dialect. */
export function engineOf(dialect: string): string {
  if (dialect === CODEX_DIALECT) return 'Codex';
  if (dialect === CLAUDE_DIALECT) return 'Claude Code';
  return dialect;
}

/** What a dialect is called in the interface. */
export function dialectLabel(dialect: string, t: TFunction): string {
  if (dialect === CODEX_DIALECT) return t('providers.dialectResponses');
  if (dialect === CLAUDE_DIALECT) return t('providers.dialectAnthropic');
  return dialect;
}

/** A base URL the engine can use: absolute, https unless the source says otherwise. */
export function absoluteBase(base: string): string {
  const value = base.trim();
  if (!value) return '';
  return /^https?:\/\//i.test(value) ? value : `https://${value}`;
}

/** A vendor's name in the interface language; the catalogue keeps the English one. */
export function presetName(preset: ProviderPreset | undefined, t: TFunction): string {
  if (!preset) return '';
  return t(`preset.${preset.id}.name`, { defaultValue: preset.name });
}

/** The mark on the logo tile: the vendor's own, or the name of a gateway. */
export function providerMark(provider: Provider, preset: ProviderPreset | undefined): string {
  if (preset) return preset.short;
  const name = provider.name.trim();
  if (!name) return '?';
  return /^[\x00-\x7F]+$/.test(name) ? name.slice(0, 2).toUpperCase() : name.slice(0, 1);
}

/** A gateway the user described gets an id the engine can hold. */
function customProviderId(name: string): string {
  const slug = name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 40);
  const suffix = Math.random().toString(36).slice(2, 6);
  return `custom-${slug || 'gateway'}-${suffix}`;
}

export interface AddProviderDialogProps {
  open: boolean;
  onClose: () => void;
  /** The built-in vendor catalogue. */
  presets: ProviderPreset[];
  /** The providers this profile already has. */
  providers: Provider[];
  /** The provider being edited, or null when a new one is added. */
  provider: Provider | null;
  /** Called once the provider was written. */
  onSaved: (saved: Provider, modelCount: number) => void;
}

export default function AddProviderDialog({ open, onClose, presets, providers, provider, onSaved }: AddProviderDialogProps) {
  const { t } = useTranslation();
  const toast = useToast();
  const save = useSaveProvider();
  const reprobe = useReprobeProvider();

  const [view, setView] = useState<'pick' | 'form'>('pick');
  const [preset, setPreset] = useState<ProviderPreset | null>(null);
  const [query, setQuery] = useState('');
  const [name, setName] = useState('');
  const [openaiBase, setOpenaiBase] = useState('');
  const [anthropicBase, setAnthropicBase] = useState('');
  const [apiKey, setApiKey] = useState('');
  const [models, setModels] = useState<string[]>([]);
  const [selected, setSelected] = useState<string[]>([]);
  const [statuses, setStatuses] = useState<Record<string, ProviderProbeResult>>({});
  const [discovering, setDiscovering] = useState(false);
  const [probing, setProbing] = useState(false);
  const [probingIds, setProbingIds] = useState<string[]>([]);
  const [probed, setProbed] = useState(false);
  const [saving, setSaving] = useState(false);
  const [errorText, setErrorText] = useState('');
  const [note, setNote] = useState('');

  const editing = provider ?? null;
  // The form starts fresh every time the dialog opens.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const resetKey = open ? (editing?.id ?? 'new') : null;
  useEffect(() => {
    if (resetKey === null) return;
    setView(editing ? 'form' : 'pick');
    setPreset(null);
    setQuery('');
    setApiKey('');
    setErrorText('');
    setNote('');
    setProbed(false);
    setStatuses({});
    if (editing) {
      setPreset(presets.find((entry) => entry.id === editing.presetId) ?? null);
      setName(editing.name);
      setOpenaiBase(editing.endpoints.find((endpoint) => endpoint.dialect === CODEX_DIALECT)?.baseUrl ?? '');
      setAnthropicBase(editing.endpoints.find((endpoint) => endpoint.dialect === CLAUDE_DIALECT)?.baseUrl ?? '');
      const ids = editing.models.map((model) => model.id);
      setModels(ids);
      setSelected(ids);
      setStatuses(
        Object.fromEntries(
          editing.models
            .filter((model) => model.status && model.status !== 'unknown')
            .map((model) => [model.id, { ok: model.status === 'ok', code: model.failure, message: model.message }]),
        ),
      );
    } else {
      setName('');
      setOpenaiBase('');
      setAnthropicBase('');
      setModels([]);
      setSelected([]);
    }
  }, [resetKey]);

  const addedIds = useMemo(() => new Set(providers.map((entry) => entry.presetId).filter((id): id is string => !!id)), [providers]);

  // The ChatGPT account is always in the provider list, so it is not one
  // of the vendors to add.
  const visiblePresets = useMemo(() => {
    const needle = query.trim().toLowerCase();
    const addable = presets.filter((entry) => !entry.official);
    if (!needle) return addable;
    return addable.filter((entry) => {
      const label = presetName(entry, t).toLowerCase();
      return label.includes(needle) || entry.id.includes(needle);
    });
  }, [presets, query, t]);

  // The endpoints the provider will be stored with: what the catalogue
  // declares, or what the user typed for a gateway of their own.
  const endpoints = useMemo<ProviderEndpoint[]>(() => {
    if (preset) return preset.endpoints.map((endpoint) => ({ dialect: endpoint.dialect, baseUrl: absoluteBase(endpoint.baseUrl) }));
    const out: ProviderEndpoint[] = [];
    if (openaiBase.trim()) out.push({ dialect: CODEX_DIALECT, baseUrl: openaiBase.trim() });
    if (anthropicBase.trim()) out.push({ dialect: CLAUDE_DIALECT, baseUrl: anthropicBase.trim() });
    return out;
  }, [preset, openaiBase, anthropicBase]);

  const codexEndpoint = endpoints.find((endpoint) => endpoint.dialect === CODEX_DIALECT) ?? null;
  const key = apiKey.trim();
  // The probe's code names the reason; its message is the core's own
  // Chinese wording, kept for a code this build does not know.
  const reasonOf = (status: ProviderProbeResult | undefined) =>
    status ? t(`providers.dialog.reason.${status.code || 'unknown'}`, { defaultValue: status.message ?? '' }) : '';
  const okCount = selected.filter((id) => statuses[id]?.ok).length;
  const failedModels = selected.filter((id) => statuses[id] && !statuses[id].ok);
  // Without an endpoint Codex can use there is nothing to check against.
  const probeImpossible = codexEndpoint === null;
  const canSave = probeImpossible || (probed && okCount > 0);

  const choosePreset = (entry: ProviderPreset) => {
    if (addedIds.has(entry.id)) {
      toast.show({ tone: 'info', title: t('providers.dialog.alreadyAdded') });
      return;
    }
    if (preset?.id === entry.id) {
      // Back to the vendor list and into the same vendor again: the
      // models fetched and ticked there are still the user's.
      setView('form');
      return;
    }
    setPreset(entry);
    setModels((entry.models ?? []).map((model) => model.id));
    setSelected([]);
    setStatuses({});
    setProbed(false);
    setView('form');
  };

  const startCustom = () => {
    setPreset(null);
    setModels([]);
    setSelected([]);
    setStatuses({});
    setProbed(false);
    setView('form');
  };

  // A model that answered keeps its answer when it is ticked off and on again:
  // the answer belongs to the endpoint and the key, and changing either one
  // clears it (see forgetAnswers).
  const toggleModel = (id: string) => {
    setSelected((current) => withSelection(current, [id], !current.includes(id)));
    setProbed(false);
  };

  const selectMany = (ids: string[], on: boolean) => {
    setSelected((current) => withSelection(current, ids, on));
    setProbed(false);
  };

  // The answers were given by one endpoint with one key; with another they
  // say nothing.
  const forgetAnswers = () => {
    setStatuses({});
    setProbed(false);
  };

  const addModel = (id: string) => {
    if (models.includes(id)) return;
    setModels((current) => [...current, id]);
    setSelected((current) => (current.includes(id) ? current : [...current, id]));
  };

  const discover = async () => {
    if (!codexEndpoint) {
      toast.show({ tone: 'warning', title: t('providers.dialog.needEndpoint') });
      return;
    }
    if (!key && !editing) {
      toast.show({ tone: 'warning', title: t('providers.dialog.needKey') });
      return;
    }
    setDiscovering(true);
    setErrorText('');
    try {
      const result = await api.discoverProviderModels(codexEndpoint.baseUrl, key || undefined, editing?.id);
      setModels(result.models);
      setSelected((current) => current.filter((id) => result.models.includes(id)));
      // What a model answered stays true while it is still on offer.
      setStatuses((current) => Object.fromEntries(Object.entries(current).filter(([id]) => result.models.includes(id))));
      setProbed(false);
      setNote(t('providers.dialog.discovered', { base: codexEndpoint.baseUrl.replace(/^https?:\/\//, ''), count: result.models.length }));
    } catch (error) {
      setErrorText(apiErrorMessage(t, error));
      setNote('');
    } finally {
      setDiscovering(false);
    }
  };

  const runProbe = async () => {
    if (!selected.length) {
      toast.show({ tone: 'warning', title: t('providers.dialog.needModels') });
      return;
    }
    if (!editing && !key) {
      toast.show({ tone: 'warning', title: t('providers.dialog.needKey') });
      return;
    }
    if (!codexEndpoint && !editing) {
      toast.show({ tone: 'warning', title: t('providers.dialog.needEndpoint') });
      return;
    }
    setProbing(true);
    setErrorText('');
    try {
      // A key the user pasted is used here; without one the server checks
      // with the key the stored provider keeps. Either way it is the models
      // ticked now that are checked, not the ones stored last time, and only
      // those that have not already passed.
      const todo = modelsToProbe(selected, statuses);
      const fresh =
        todo.length === 0
          ? {}
          : editing && !key && !codexEndpoint
            ? (await reprobe.mutateAsync(editing.id)).results
            : (await api.probeProviderModels(codexEndpoint?.baseUrl ?? '', todo, key || undefined, editing?.id)).results;
      const merged = Object.fromEntries(
        selected.flatMap((id) => {
          const answer = fresh[id] ?? statuses[id];
          return answer ? [[id, answer] as const] : [];
        }),
      );
      setStatuses(merged);
      setProbed(true);
      // All of them answering is the only case that needs no word from the
      // user. Otherwise the notice below says which did not, and saving then
      // is the user's own press of the button.
      if (selected.every((id) => merged[id]?.ok)) {
        setProbing(false);
        await write(false, merged);
      }
    } catch (error) {
      setErrorText(apiErrorMessage(t, error));
    } finally {
      setProbing(false);
    }
  };

  // Checks one model on its own, from the button on its chip.
  const probeOne = async (id: string) => {
    if (!codexEndpoint && !editing) {
      toast.show({ tone: 'warning', title: t('providers.dialog.needEndpoint') });
      return;
    }
    if (!editing && !key) {
      toast.show({ tone: 'warning', title: t('providers.dialog.needKey') });
      return;
    }
    setProbingIds((current) => [...current, id]);
    setErrorText('');
    try {
      const { results } = await api.probeProviderModels(codexEndpoint?.baseUrl ?? '', [id], key || undefined, editing?.id);
      const answer = results[id];
      if (answer) {
        setStatuses((current) => ({ ...current, [id]: answer }));
        // The button's own answer: the chip changes too, but a mark that small
        // is easy to miss.
        if (answer.ok) toast.show({ tone: 'success', title: t('providers.dialog.modelOk', { id }) });
        // Not 'danger': that one stays until it is closed, and a model that does
        // not answer is an answer, not a fault of the page.
        else toast.show({ tone: 'warning', duration: 5000, title: t('providers.dialog.modelFailed', { id }), description: reasonOf(answer) });
      }
      setProbed(false);
    } catch (error) {
      setErrorText(apiErrorMessage(t, error));
    } finally {
      setProbingIds((current) => current.filter((entry) => entry !== id));
    }
  };

  const write = async (force: boolean, answers: Record<string, ProviderProbeResult> = statuses) => {
    if (!preset && !name.trim()) {
      toast.show({ tone: 'warning', title: t('providers.dialog.needName') });
      return;
    }
    if (!endpoints.length) {
      toast.show({ tone: 'warning', title: t('providers.dialog.needEndpoint') });
      return;
    }
    if (!selected.length) {
      toast.show({ tone: 'warning', title: t('providers.dialog.needModels') });
      return;
    }
    const kept: ProviderModel[] = selected.map((id) => {
      const status = answers[id];
      if (force) return { id, status: 'failed', failure: status?.code ?? '', message: status?.message ?? '' };
      if (!status) return { id, status: 'unknown' };
      if (status.ok) return { id, status: 'ok' };
      return { id, status: 'failed', failure: status.code, message: status.message };
    });
    const body: ProviderRequest = {
      // A vendor from the catalogue keeps its own id; a gateway of the
      // user's own gets one the engine can hold.
      id: editing?.id ?? (preset ? preset.id : customProviderId(name)),
      name: preset ? preset.name : name.trim(),
      kind: preset ? 'official' : 'custom',
      authMode: 'api_key',
      endpoints,
      models: kept,
      defaultModel: kept.find((model) => model.status === 'ok')?.id ?? kept[0]?.id,
      ...(preset ? { presetId: preset.id } : {}),
      ...(key ? { apiKey: key } : {}),
      ...(editing ? { expectedRevision: editing.revision } : {}),
    };
    setSaving(true);
    setErrorText('');
    try {
      const saved = await save.mutateAsync({ id: editing?.id, body });
      onSaved(saved, kept.length);
    } catch (error) {
      setErrorText(apiErrorMessage(t, error));
    } finally {
      setSaving(false);
    }
  };

  const pickView = (
    <div className="space-y-4">
      <SearchInput
        value={query}
        onChange={setQuery}
        placeholder={t('providers.dialog.searchPlaceholder')}
        label={t('providers.dialog.searchLabel')}
        clearLabel={t('common.clearSearch')}
      />
      <div className="grid grid-cols-[repeat(auto-fill,minmax(11rem,1fr))] gap-2">
        {visiblePresets.map((entry) => {
          const added = addedIds.has(entry.id);
          return (
            <button
              key={entry.id}
              type="button"
              onClick={() => choosePreset(entry)}
              className={cx(
                'flex items-start gap-2 rounded-xl border p-2.5 text-left transition-colors',
                added
                  ? 'border-neutral-200 opacity-60 dark:border-neutral-800'
                  : 'border-neutral-200 hover:border-neutral-300 hover:bg-neutral-900/[0.04] dark:border-neutral-700 dark:hover:bg-white/[0.06]',
              )}
            >
              <LogoTile mark={entry.short} color={entry.color} />
              <span className="min-w-0 flex-1">
                <span className="flex flex-wrap items-center gap-1">
                  <span className="text-xs font-semibold text-neutral-900 dark:text-neutral-50">{presetName(entry, t)}</span>
                  {added && <Badge>{t('providers.dialog.addedBadge')}</Badge>}
                </span>
                <span className="mt-1 flex flex-wrap items-center gap-1">
                  {entry.endpoints.some((endpoint) => endpoint.dialect === CODEX_DIALECT) && <Badge>{t('providers.badgeCodex')}</Badge>}
                  {entry.endpoints.some((endpoint) => endpoint.dialect === CLAUDE_DIALECT) && <Badge>{t('providers.badgeClaudeCode')}</Badge>}
                </span>
              </span>
            </button>
          );
        })}
      </div>
      <div className="border-t border-neutral-200 pt-4 dark:border-neutral-800">
        <button
          type="button"
          onClick={startCustom}
          className="flex w-full items-center gap-2.5 rounded-xl border border-neutral-200 p-2.5 text-left transition-colors hover:border-neutral-300 hover:bg-neutral-900/[0.04] dark:border-neutral-700 dark:hover:bg-white/[0.06]"
        >
          <LogoTile mark="+" />
          <span className="min-w-0">
            <span className="block text-xs font-semibold text-neutral-900 dark:text-neutral-50">{t('providers.dialog.customName')}</span>
            <span className="mt-0.5 block text-2xs text-neutral-600 dark:text-neutral-300">{t('providers.dialog.customHint')}</span>
          </span>
        </button>
      </div>
    </div>
  );

  const formView = (
    <div className="space-y-4">
      {/* Said once the user has described a provider the engine cannot check, not before they have typed anything. */}
      {probeImpossible && (preset ? true : anthropicBase.trim() !== '') && (
        <Alert tone="warning">{t('providers.noCodexEndpoint')}</Alert>
      )}

      {preset ? (
        <div>
          <p className="text-sm font-medium text-neutral-800 dark:text-neutral-100">{t('providers.dialog.endpoints')}</p>
          <div className="mt-1">
            {endpoints.map((endpoint) => (
              <div key={endpoint.dialect} className="flex items-center gap-2 border-b border-dashed border-neutral-200 py-1.5 last:border-0 dark:border-neutral-800">
                <span className="w-28 shrink-0 text-xs font-semibold text-neutral-700 dark:text-neutral-200">{dialectLabel(endpoint.dialect, t)}</span>
                <span className="min-w-0 flex-1 truncate font-mono text-xs text-neutral-600 dark:text-neutral-300">{endpoint.baseUrl.replace(/^https?:\/\//, '')}</span>
                <Badge tone={endpoint.dialect === CODEX_DIALECT ? 'success' : 'neutral'}>{engineOf(endpoint.dialect)}</Badge>
              </div>
            ))}
          </div>
        </div>
      ) : (
        <div className="space-y-3">
          <Field label={t('providers.dialog.displayName')}>
            <TextInput value={name} onChange={(event) => setName(event.target.value)} placeholder={t('providers.dialog.displayNamePlaceholder')} />
          </Field>
          <Field label={t('providers.dialog.openaiEndpoint')} hint={t('providers.dialog.openaiEndpointHint')}>
            <TextInput mono value={openaiBase} onChange={(event) => {
                setOpenaiBase(event.target.value);
                forgetAnswers();
              }} placeholder="https://gateway.example/v1" />
          </Field>
          <Field label={t('providers.dialog.anthropicEndpoint')} hint={t('providers.dialog.anthropicEndpointHint')}>
            <TextInput mono value={anthropicBase} onChange={(event) => setAnthropicBase(event.target.value)} placeholder="https://gateway.example/anthropic" />
          </Field>
          <p className="text-xs leading-5 text-neutral-500 dark:text-neutral-400">{t('providers.dialog.endpointHint')}</p>
        </div>
      )}

      <Field
        label={t('providers.dialog.apiKey')}
        required
        hint={
          preset?.keyUrl ? (
            <Trans
              i18nKey="providers.dialog.keyHint"
              values={{ url: preset.keyUrl }}
              components={{
                url: (
                  <a
                    href={`https://${preset.keyUrl}`}
                    target="_blank"
                    rel="noreferrer"
                    className="font-mono text-signal-600 hover:underline dark:text-signal-400"
                  />
                ),
              }}
            />
          ) : undefined
        }
      >
        <TextInput
          mono
          type="password"
          value={apiKey}
          onChange={(event) => {
            setApiKey(event.target.value);
            forgetAnswers();
          }}
          placeholder={t('providers.dialog.apiKeyPlaceholder')}
        />
      </Field>
      {editing?.hasCredential && (
        <p className="text-xs text-neutral-500 dark:text-neutral-400">{t('providers.dialog.keyStored')}</p>
      )}
      {!editing && preset?.keyPatterns?.length ? (
        <p className="text-xs text-neutral-500 dark:text-neutral-400">{t('providers.dialog.envHint')}</p>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <Button size="sm" icon={Icons.refresh} onClick={() => void discover()} loading={discovering}>
          {t('providers.dialog.discover')}
        </Button>
        {note && <span className="text-xs text-neutral-500 dark:text-neutral-400">{note}</span>}
      </div>

      <ModelPick
        models={models}
        selected={selected}
        statuses={statuses}
        onToggle={toggleModel}
        onProbe={probeImpossible ? undefined : (id) => void probeOne(id)}
        probing={probingIds}
        onSelectMany={selectMany}
        onAdd={addModel}
        labels={{
          title: t('providers.dialog.models'),
          count: t('providers.dialog.selectedCount', { count: selected.length, total: models.length }),
          search: t('providers.dialog.searchModels'),
          addAs: (id) => t('providers.dialog.addModelAs', { id }),
          noMatch: t('providers.dialog.noModelMatch'),
          ok: t('providers.dialog.statusOk'),
          failed: t('providers.dialog.statusFailed'),
          reason: reasonOf,
          noModels: t('providers.dialog.noModels'),
          note: t('providers.dialog.modelsHint'),
          selectAll: t('providers.dialog.selectAll'),
          clearAll: t('providers.dialog.clearAll'),
          testOne: (id) => t('providers.dialog.testOne', { id }),
          retryOne: (id) => t('providers.dialog.retryOne', { id }),
        }}
      />

      {probed && okCount === 0 && (
        <div className="space-y-2">
          <Alert tone="warning">
            {t('providers.dialog.probeAllFailed', {
              models: failedModels.map((id) => `${id}（${reasonOf(statuses[id])}）`).join('、'),
            })}
          </Alert>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              onClick={() => {
                setProbed(false);
                setStatuses({});
              }}
            >
              {t('providers.dialog.backToEdit')}
            </Button>
            <Button size="sm" onClick={() => void write(true)} loading={saving}>
              {t('providers.dialog.forceSave')}
            </Button>
          </div>
        </div>
      )}

      {probed && okCount > 0 && (
        <div className="space-y-2">
          <p className="text-xs text-neutral-600 dark:text-neutral-300">
            {t('providers.dialog.probeOk', { count: okCount, total: selected.length })}
          </p>
          {failedModels.length > 0 && (
            <Alert tone="warning">
              {t('providers.dialog.probeSomeFailed', {
                count: failedModels.length,
                models: failedModels.map((id) => `${id}（${reasonOf(statuses[id])}）`).join('、'),
              })}
            </Alert>
          )}
        </div>
      )}

      {errorText && <Alert tone="danger" onClose={() => setErrorText('')} closeLabel={t('common.close')}>{errorText}</Alert>}
    </div>
  );

  const footer =
    view === 'pick' ? (
      <Button variant="ghost" onClick={onClose}>
        {t('common.cancel')}
      </Button>
    ) : (
      <>
        <Button variant="ghost" onClick={() => setView('pick')}>
          {t('providers.dialog.back')}
        </Button>
        <Button
          variant="primary"
          loading={probing || saving}
          onClick={() => (canSave ? void write(false) : void runProbe())}
        >
          {probeImpossible
            ? t('providers.dialog.addDirectly')
            : probed && okCount > 0
              ? t('providers.dialog.save')
              : editing
                ? t('providers.dialog.testAndSave')
                : t('providers.dialog.testAndAdd')}
        </Button>
      </>
    );

  return (
    <Dialog
      open={open}
      onClose={onClose}
      busy={saving}
      closeLabel={t('common.close')}
      title={editing ? t('providers.dialog.editTitle', { name: editing.name }) : t('providers.dialog.addTitle')}
      description={view === 'pick' ? t('providers.dialog.pickHint') : undefined}
      footer={footer}
      size={view === 'pick' ? 'lg' : 'md'}
    >
      {view === 'pick' ? pickView : formView}
    </Dialog>
  );
}
