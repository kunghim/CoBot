// ReasoningEffortSelect is the composer's model and thinking-effort picker,
// like Codex's: a compact trigger opens the current effort and model, a
// stepped slider for the efforts the model offers, a model list and a reset
// to the model's own default. The effort belongs to the team and applies from
// each member's next turn; the model is CoBot's default model, which every
// team's members run on.
import { useTranslation } from 'react-i18next';
import { useEngineModels, useSetTeamReasoningEffort, useSettings } from '../../api/queries';
import type { EngineModel, Team } from '../../api/types';
import { ModelEffortPicker, useToast } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { useSettingSaver } from '../settings/useSettingSaver';

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
  const models = useEngineModels(true);
  const setEffort = useSetTeamReasoningEffort();
  const { save } = useSettingSaver();
  const list = models.data?.models ?? [];
  const model = currentModel(list, settings.data?.defaultModel);
  const options = model?.reasoningEfforts ?? [];
  const current = team.reasoningEffort ?? '';
  const label = (effort: string) => t(`effort.level.${effort}`, { defaultValue: effort });

  // Without a model list there is nothing to choose from.
  if (!model || options.length === 0) return null;

  // Following the model's default shows the model's default level.
  const shown = current || model.defaultReasoningEffort || options[0].effort;
  const chooseEffort = (effort: string) => {
    if (effort === current) return;
    setEffort.mutate(
      { teamId: team.id, effort, expectedRevision: team.revision },
      { onError: (err) => toast.show({ tone: 'danger', title: t('effort.saveFailed'), description: apiErrorMessage(t, err) }) },
    );
  };

  return (
    <ModelEffortPicker
      disabled={disabled || setEffort.isPending}
      triggerLabel={`${t('effort.label')}：${current ? label(current) : t('effort.default')} · ${model.displayName ?? modelId(model)}`}
      models={list.flatMap((entry) => (modelId(entry) ? [{ id: modelId(entry), label: entry.displayName ?? modelId(entry), description: entry.description }] : []))}
      model={modelId(model)}
      onModel={(id) => {
        if (id !== modelId(model)) save('defaultModel', { defaultModel: id });
      }}
      levels={options.map((option) => ({ value: option.effort, label: label(option.effort) }))}
      effort={shown}
      onEffort={chooseEffort}
      isDefault={current === ''}
      onReset={() => chooseEffort('')}
      labels={{ effort: t('effort.label'), reset: t('effort.reset'), chooseModel: t('effort.chooseModel'), back: t('effort.back') }}
      note={`${t('effort.note')} ${t('effort.modelNote')}`}
    />
  );
}
