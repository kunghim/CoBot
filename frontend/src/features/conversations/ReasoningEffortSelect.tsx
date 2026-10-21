// ReasoningEffortSelect is the composer's model and thinking-effort picker,
// like Codex's: a compact trigger opens the current effort and model, a
// stepped slider for the efforts the model offers, a model list and a reset
// to the model's own default. The effort belongs to the team and applies from
// each member's next turn; the model is the one the provider the profile runs
// with serves, which every team's members run on.
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useEngineModels, useProviders, useSaveProvider, useSetTeamReasoningEffort, useSettings } from '../../api/queries';
import type { EngineModel, Team } from '../../api/types';
import { ModelEffortPicker, useToast } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { gatewayModelID, gatewayModelRequest, gatewayModels, gatewayOf, type ModelOption } from '../providers/providerModels';
import { useSettingSaver } from '../settings/useSettingSaver';
import { createSettledSave, type SettledSave } from './settledSave';

/** The model members run on: the configured default model, else the
 * engine's own default. */
export function currentModel(models: EngineModel[], configured: string | undefined): EngineModel | undefined {
  return models.find((m) => configured && (m.slug === configured || m.id === configured)) ?? models.find((m) => m.isDefault) ?? models[0];
}

const modelId = (model: EngineModel) => model.slug ?? model.id ?? '';

export default function ReasoningEffortSelect({ team, disabled }: { team: Team; disabled?: boolean }) {
  const { t } = useTranslation();
  const toast = useToast();
  const settings = useSettings(true);
  const providers = useProviders();
  const models = useEngineModels(true);
  const setEffort = useSetTeamReasoningEffort();
  const saveProvider = useSaveProvider();
  const { save } = useSettingSaver();
  const engineList = models.data?.models ?? [];

  // A third-party gateway serves its own models; the engine's list belongs
  // to the engine's own vendor and the core would not ask the gateway for
  // one of those.
  const gateway = gatewayOf(providers.data?.providers, settings.data?.defaultProvider ?? '');
  const options: ModelOption[] = gateway
    ? gatewayModels(gateway, t('settings.modelUnavailable'))
    : engineList.flatMap((entry) => {
        const id = modelId(entry);
        return id === '' ? [] : [{ id, label: entry.displayName ?? id, description: entry.description }];
      });
  const model = currentModel(engineList, settings.data?.defaultModel);
  // The efforts CoBot can send are the ones the engine knows; a gateway's
  // own model does not change what the engine accepts.
  const levels = model?.reasoningEfforts ?? [];
  // An engine that lists no models leaves nothing selected; the picker then
  // hides itself below rather than reading a model that is not there.
  const selected = gateway ? gatewayModelID(gateway) : model ? modelId(model) : '';
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

  // Without a model list there is nothing to choose from.
  if (options.length === 0) return null;

  const current = chosen ?? stored;
  // Following the model's default shows the model's default level.
  const shown = current || model?.defaultReasoningEffort || levels[0]?.effort || '';
  const chooseEffort = (effort: string, now = false) => {
    if (effort === current) return;
    setChosen(effort);
    saver.current?.set(effort);
    if (now) saver.current?.flush();
  };

  const chosenLabel = options.find((option) => option.id === selected)?.label ?? selected;

  return (
    <ModelEffortPicker
      disabled={disabled}
      triggerLabel={`${t('effort.label')}：${current ? label(current) : t('effort.default')} · ${chosenLabel}`}
      models={options}
      model={selected}
      onModel={(id) => {
        if (gateway) {
          // The gateway serves the model, so the gateway is what changes.
          saveProvider.mutate(
            { id: gateway.id, body: gatewayModelRequest(gateway, id) },
            { onError: (error) => toast.show({ tone: 'danger', title: t('effort.saveFailed'), description: apiErrorMessage(t, error) }) },
          );
          return;
        }
        if (id !== selected) save('defaultModel', { defaultModel: id });
      }}
      levels={levels.map((option) => ({ value: option.effort, label: label(option.effort) }))}
      effort={shown}
      onEffort={chooseEffort}
      isDefault={current === ''}
      onReset={() => chooseEffort('', true)}
      labels={{ effort: t('effort.label'), reset: t('effort.reset'), chooseModel: t('effort.chooseModel'), back: t('effort.back') }}
      note={`${t('effort.note')} ${t('effort.modelNote')}`}
    />
  );
}
