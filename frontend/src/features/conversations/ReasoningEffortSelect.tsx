// ReasoningEffortSelect is the composer's model and thinking-effort picker,
// like Codex's: a compact trigger opens the current effort and model, a
// stepped slider for the efforts the model offers, a model list and a reset
// to the model's own default. The effort belongs to the team and applies from
// each member's next turn; the model belongs to the member and can come from
// any enabled provider.
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useEngine, useEngineModels, useProviders, useSetBotModel, useSetTeamReasoningEffort, useSettings } from '../../api/queries';
import type { Bot, EngineModel, Provider, Team } from '../../api/types';
import { AlertDialog, ModelEffortPicker, useToast } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { choiceKey, DEFAULT_EFFORTS, engineDefaultModel, isOfficial, memberChoice, selectableModels } from '../providers/providerModels';
import { useProviderName } from '../providers/useProviderName';
import { createSettledSave, type SettledSave } from './settledSave';
import { useOpenModelPicker } from './modelPicker';

/** The model members run with: the configured default model, else the
 * engine's own default. */
export function currentModel(models: EngineModel[], configured: string | undefined): EngineModel | undefined {
  return models.find((m) => configured && (m.slug === configured || m.id === configured)) ?? models.find((m) => m.isDefault) ?? models[0];
}

export interface EffortChoices {
  levels: Array<{ effort: string }>;
  /** Thinking can be switched off; the team's level is then "none". */
  toggle: boolean;
  /** The level the model uses when none is chosen; empty when it has none. */
  defaultEffort: string;
}

/**
 * The thinking levels a model offers. The ChatGPT account's models list their
 * own, which the engine reports. Another vendor's model has the ones its
 * configuration lists, or low, medium and high when it lists none: CoBot
 * cannot tell what a gateway accepts, so the rest is the user's to declare.
 */
export function effortChoices(provider: Provider | undefined, model: string, engineList: EngineModel[]): EffortChoices {
  if (!provider || isOfficial(provider)) {
    const known = provider ? currentModel(engineList, model) : currentModel(engineList, undefined);
    return { levels: known?.reasoningEfforts ?? [], toggle: false, defaultEffort: known?.defaultReasoningEffort ?? '' };
  }
  const own = provider.models.find((entry) => entry.id === model);
  if (!own) return { levels: [], toggle: false, defaultEffort: '' };
  // What the core worked out from the model catalog and the user's entries.
  if (own.spec) {
    return { levels: own.spec.thinking.efforts.map((effort) => ({ effort })), toggle: own.spec.thinking.toggle, defaultEffort: own.spec.thinking.defaultEffort ?? '' };
  }
  const declared = own.supportedEfforts?.length ? own.supportedEfforts : DEFAULT_EFFORTS;
  return { levels: declared.map((effort) => ({ effort })), toggle: own.thinkingToggle ?? false, defaultEffort: own.defaultEffort ?? '' };
}

export default function ReasoningEffortSelect({ team, bot, bots, disabled }: { team: Team; bot: Bot; bots?: Bot[]; disabled?: boolean }) {
  const { t } = useTranslation();
  const toast = useToast();
  const settings = useSettings(true);
  const providers = useProviders();
  const engine = useEngine();
  const models = useEngineModels(true);
  const setEffort = useSetTeamReasoningEffort();
  const setModel = useSetBotModel();
  const providerName = useProviderName();
  const engineList = models.data?.models ?? [];
  const providerList = providers.data?.providers;
  // A failed turn's switch-model entry opens this picker from where the
  // failure shows.
  const [pickerOpen, setPickerOpen] = useState(false);
  useOpenModelPicker(bot.id, () => setPickerOpen(true));
  // A switch that leaves teammates on the member's old provider offers to
  // carry them along, so one quota that ran out does not fail them one by
  // one.
  const [batch, setBatch] = useState<{ targets: Bot[]; providerId: string; model: string; label: string } | null>(null);
  const [batchError, setBatchError] = useState('');

  const unavailable = t('settings.modelUnavailable');
  const offered = selectableModels(providerList, engineList, engine.data?.loggedIn ?? false, providerName);
  const choice = memberChoice(bot, team, settings.data, providerList, engineDefaultModel(engineList), engine.data?.loggedIn ?? false);
  const selected = choiceKey(choice.providerId, choice.model);
  const options = offered.map((entry) => ({
    id: entry.key,
    label: entry.unavailable ? `${entry.label} · ${unavailable}` : entry.label,
    group: entry.group,
    disabled: entry.unavailable,
    badge: entry.contextBadge,
  }));
  const chosenProvider = providerList?.find((entry) => entry.id === choice.providerId);
  const { levels, toggle, defaultEffort } = effortChoices(chosenProvider, choice.model, engineList);
  const stored = team.reasoningEffort ?? '';
  const label = (effort: string) => t(`effort.level.${effort}`, { defaultValue: effort });

  // Dragging the slider moves it here at once; the save follows once the
  // value settles, one at a time, each from the revision the last returned,
  // so a drag across several steps never sends conflicting saves.
  const [chosen, setChosen] = useState<string | null>(null);
  const revision = useRef(team.revision);
  useEffect(() => {
    revision.current = Math.max(revision.current, team.revision);
  }, [team.revision]);
  useEffect(() => {
    if (chosen !== null && chosen === stored && !setEffort.isPending) setChosen(null);
  }, [chosen, stored, setEffort.isPending]);
  const latest = useRef({ teamId: team.id, mutateAsync: setEffort.mutateAsync, toast, t });
  latest.current = { teamId: team.id, mutateAsync: setEffort.mutateAsync, toast, t };
  const saver = useRef<SettledSave<string> | null>(null);
  if (!saver.current) {
    saver.current = createSettledSave(async (effort: string) => {
      const { teamId, mutateAsync, toast: show, t: tr } = latest.current;
      try {
        const updated = await mutateAsync({ teamId, effort, expectedRevision: revision.current });
        revision.current = updated.revision;
      } catch (err) {
        setChosen(null);
        show.show({ tone: 'danger', title: tr('effort.saveFailed'), description: apiErrorMessage(tr, err) });
      }
    });
  }
  // Leaving the conversation must submit its last choice, including one
  // queued behind an in-flight save. Flushing also keeps the same saver
  // usable when StrictMode replays effect cleanup during development.
  useEffect(() => () => saver.current?.flush(), []);

  // Without a model on offer there is nothing to choose from.
  if (options.length === 0) return null;

  // A level the team chose on another model, which this one does not offer,
  // counts as none: this model runs with its own default.
  const wanted = chosen ?? stored;
  const off = toggle && wanted === 'none';
  const current = off ? '' : levels.some((level) => level.effort === wanted) ? wanted : '';
  // Following the model's default shows the model's default level.
  const shown = current || defaultEffort || levels[0]?.effort || '';
  const chooseEffort = (effort: string, now = false) => {
    if (effort === wanted) return;
    setChosen(effort);
    saver.current?.set(effort);
    if (now) saver.current?.flush();
  };

  const chosenLabel = offered.find((entry) => entry.key === selected)?.label ?? choice.model;

  const onModel = (key: string) => {
    const next = offered.find((entry) => entry.key === key);
    if (!next || key === selected) return;
    // Teammates resolved to the provider the member just left — by their own
    // choice or through the default model — would hit what the member ran
    // from. Switching them along stays a user decision.
    const targets = (bots ?? []).filter(
      (member) =>
        member.id !== bot.id &&
        memberChoice(member, team, settings.data, providerList, engineDefaultModel(engineList), engine.data?.loggedIn ?? false).providerId === choice.providerId,
    );
    setModel.mutate(
      { botId: bot.id, providerId: next.providerId, model: next.model, expectedRevision: bot.revision },
      {
        onSuccess: () => {
          toast.show({ tone: 'success', title: t('effort.modelSwitched', { name: bot.name, model: next.label, provider: next.group }) });
          if (targets.length > 0) setBatch({ targets, providerId: next.providerId, model: next.model, label: next.label });
        },
        onError: (error) => toast.show({ tone: 'danger', title: t('effort.saveFailed'), description: apiErrorMessage(t, error) }),
      },
    );
  };

  const switchAll = async () => {
    if (!batch) return;
    setBatchError('');
    const failures: string[] = [];
    await Promise.all(
      batch.targets.map(async (member) => {
        try {
          await setModel.mutateAsync({ botId: member.id, providerId: batch.providerId, model: batch.model, expectedRevision: member.revision });
        } catch {
          failures.push(member.name);
        }
      }),
    );
    if (failures.length > 0) {
      setBatchError(t('effort.modelSwitchedAllFailed', { names: failures.join('、') }));
      return;
    }
    const count = batch.targets.length;
    const label = batch.label;
    setBatch(null);
    toast.show({ tone: 'success', title: t('effort.modelSwitchedAll', { count, model: label }) });
  };

  return (
    <>
    <ModelEffortPicker
      disabled={disabled}
      open={pickerOpen}
      onOpenChange={setPickerOpen}
      triggerLabel={
        levels.length > 0 || toggle
          ? `${t('effort.label')}：${off ? t('effort.off') : current ? label(current) : t('effort.default')} · ${chosenLabel}`
          : chosenLabel
      }
      models={options}
      model={selected}
      onModel={onModel}
      levels={levels.map((option) => ({ value: option.effort, label: label(option.effort) }))}
      effort={shown}
      onEffort={chooseEffort}
      isDefault={current === '' && !off}
      thinking={toggle ? { on: !off, label: off ? t('effort.off') : t('effort.on'), onChange: (on) => chooseEffort(on ? '' : 'none', true) } : undefined}
      onReset={() => chooseEffort('', true)}
      labels={{ effort: t('effort.label'), reset: t('effort.reset'), chooseModel: t('effort.chooseModel'), back: t('effort.back') }}
      note={levels.length > 0 || toggle ? `${t('effort.note')} ${t('effort.modelNote')}` : t('effort.modelNote')}
    />
    <AlertDialog
      open={batch !== null}
      onCancel={() => setBatch(null)}
      onConfirm={switchAll}
      danger={false}
      title={t('effort.switchOthersTitle', { count: batch?.targets.length ?? 0, model: batch?.label ?? '' })}
      description={t('effort.switchOthersNote')}
      confirmLabel={t('effort.switchAll')}
      cancelLabel={t('effort.switchThisOnly')}
      busy={setModel.isPending}
      error={batchError}
    />
    </>
  );
}
