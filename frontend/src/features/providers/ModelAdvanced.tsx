// A model's advanced settings: its own name, context length, output limit,
// what it takes in, what it can do and how its thinking is controlled.
//
// What the model catalog knows about a model is shown as it is and cannot be
// changed here (the catalog is kept up to date by its source); what it does
// not know, and every model it has never heard of, is the user's to fill in.
// The name is always the user's.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { ModelSpec, ProviderModel } from '../../api/types';
import { Badge, Chip, Field, Icons, Select, Switch, TextInput, cx } from '../../components/ui';
import { contextBadge, EFFORT_LEVELS } from './providerModels';

/** The kinds of input a model can be said to take, text first. */
const INPUT_KINDS = ['text', 'image', 'video', 'pdf'] as const;

export interface ModelAdvancedProps {
  /** The models kept, in the order the user ticked them. */
  selected: string[];
  /** What the user entered for each, sparse: only what they changed. */
  entries: Record<string, ProviderModel>;
  /** What is known about each, from the catalog and the entries together. */
  specs: Record<string, ModelSpec>;
  onChange: (id: string, patch: Partial<ProviderModel>) => void;
}

const digits = (text: string) => Number(text.replace(/\D/g, '')) || 0;

export default function ModelAdvanced({ selected, entries, specs, onChange }: ModelAdvancedProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [picked, setPicked] = useState('');
  const id = selected.includes(picked) ? picked : (selected[0] ?? '');
  const spec = specs[id];
  const entry = entries[id] ?? { id };
  const locked = (field: string) => spec?.locked.includes(field as ModelSpec['locked'][number]) ?? false;

  const nameOf = (modelId: string) => entries[modelId]?.displayName || modelId;
  const context = spec?.contextWindow ?? 0;
  const badge = contextBadge(context);
  const takes = spec?.inputModalities ?? ['text'];
  const toggle = spec?.thinking.toggle ?? false;
  const efforts = spec?.thinking.efforts ?? [];

  return (
    <div>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className="flex w-full items-center gap-1.5 text-left text-sm font-medium text-neutral-800 dark:text-neutral-100"
      >
        <Icons.next size={14} aria-hidden className={cx('transition-transform', open && 'rotate-90')} />
        {t('providers.dialog.adv.title')}
      </button>
      {open && (
        <div className="mt-3 space-y-3">
          {id === '' || !spec ? (
            <p className="text-xs leading-5 text-neutral-500 dark:text-neutral-400">{t('providers.dialog.adv.noModel')}</p>
          ) : (
            <>
              <div className="min-w-0 max-w-full sm:w-72">
                <Select
                  size="sm"
                  label={t('providers.dialog.adv.model')}
                  value={id}
                  options={selected.map((modelId) => ({ value: modelId, label: nameOf(modelId) === modelId ? modelId : `${nameOf(modelId)}（${modelId}）` }))}
                  onChange={setPicked}
                />
              </div>
              {spec.source === 'catalog' ? (
                <p className="text-xs text-neutral-500 dark:text-neutral-400">{t('providers.dialog.adv.fromCatalog', { name: spec.name })}</p>
              ) : (
                <p className="text-xs text-neutral-500 dark:text-neutral-400">{t('providers.dialog.adv.custom')}</p>
              )}

              <Field label={t('providers.dialog.adv.alias')} hint={t('providers.dialog.adv.aliasHint')}>
                <TextInput size="sm" value={entry.displayName ?? ''} placeholder={spec.name || id} onChange={(event) => onChange(id, { displayName: event.target.value })} />
              </Field>

              <div className="grid gap-3 sm:grid-cols-2">
                <Field
                  label={
                    <span className="inline-flex items-center gap-1.5">
                      {t('providers.dialog.adv.context')}
                      {badge && <Badge tone="success">{badge}</Badge>}
                    </span>
                  }
                  hint={locked('contextWindow') ? t('providers.dialog.adv.lockedHint') : t('providers.dialog.adv.contextHint')}
                >
                  <TextInput
                    size="sm"
                    mono
                    inputMode="numeric"
                    readOnly={locked('contextWindow')}
                    value={context ? String(context) : ''}
                    placeholder="128000"
                    onChange={(event) => onChange(id, { contextWindow: digits(event.target.value) })}
                  />
                </Field>
                <Field label={t('providers.dialog.adv.maxOutput')} hint={locked('maxOutput') ? t('providers.dialog.adv.lockedHint') : t('providers.dialog.adv.maxOutputHint')}>
                  <TextInput
                    size="sm"
                    mono
                    inputMode="numeric"
                    readOnly={locked('maxOutput')}
                    value={spec.maxOutput ? String(spec.maxOutput) : ''}
                    placeholder="16384"
                    onChange={(event) => onChange(id, { maxOutput: digits(event.target.value) })}
                  />
                </Field>
              </div>

              <div>
                <p className="text-sm font-medium text-neutral-800 dark:text-neutral-100">{t('providers.dialog.adv.input')}</p>
                <div className="mt-1.5 flex flex-wrap gap-1.5">
                  {INPUT_KINDS.map((kind) => (
                    <Chip
                      key={kind}
                      centered
                      className="min-w-[4.75rem]"
                      pressed={takes.includes(kind)}
                      disabled={kind === 'text' || locked('inputModalities')}
                      onClick={() => {
                        const have = (entry.inputModalities?.length ? entry.inputModalities : takes).filter((value) => value !== 'text');
                        const next = have.includes(kind) ? have.filter((value) => value !== kind) : [...have, kind];
                        onChange(id, { inputModalities: next });
                      }}
                    >
                      {t(`providers.dialog.adv.kind.${kind}`)}
                    </Chip>
                  ))}
                </div>
                <p className="mt-1.5 text-xs leading-5 text-neutral-500 dark:text-neutral-400">{t('providers.dialog.adv.inputHint')}</p>
              </div>

              <div>
                <p className="text-sm font-medium text-neutral-800 dark:text-neutral-100">{t('providers.dialog.adv.abilities')}</p>
                <div className="mt-1.5">
                  <Switch
                    checked={spec.webSearch}
                    onChange={(checked) => onChange(id, { webSearch: checked })}
                    label={t('providers.dialog.adv.webSearch')}
                    description={t('providers.dialog.adv.webSearchHint')}
                  />
                </div>
              </div>

              <div>
                <p className="text-sm font-medium text-neutral-800 dark:text-neutral-100">{t('providers.dialog.adv.thinking')}</p>
                <div className="mt-1.5 space-y-2">
                  <Switch
                    checked={toggle}
                    disabled={locked('thinking')}
                    onChange={(checked) => onChange(id, { thinkingToggle: checked })}
                    label={t('providers.dialog.adv.thinkingToggle')}
                    description={t('providers.dialog.adv.thinkingToggleHint')}
                  />
                  <div className="flex flex-wrap gap-1.5">
                    {EFFORT_LEVELS.map((level) => (
                      <Chip
                        key={level}
                        centered
                        className="min-w-[4.75rem]"
                        pressed={efforts.includes(level)}
                        disabled={locked('thinking')}
                        onClick={() => {
                          const have = entry.supportedEfforts?.length || entry.thinkingToggle ? (entry.supportedEfforts ?? []) : efforts;
                          const next = EFFORT_LEVELS.filter((name) => (name === level ? !have.includes(name) : have.includes(name)));
                          onChange(id, { supportedEfforts: next, thinkingToggle: toggle });
                        }}
                      >
                        {t(`effort.level.${level}`)}
                      </Chip>
                    ))}
                  </div>
                  <p className="text-xs leading-5 text-neutral-500 dark:text-neutral-400">
                    {locked('thinking') ? t('providers.dialog.adv.lockedHint') : t('providers.dialog.adv.thinkingHint')}
                  </p>
                </div>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
}
