// ReasoningEffortSelect is the composer's model and thinking-effort picker,
// like Codex's: a compact trigger opens the current effort and model, a
// stepped slider for the efforts the model offers, a model list and a reset
// to the model's own default. The effort belongs to the team and applies from
// each member's next turn; the model is CoBot's default model, which every
// team's members run on.
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useEngineModels, useSetTeamReasoningEffort, useSettings } from '../../api/queries';
import type { EngineModel, Team } from '../../api/types';
import { ModelEffortPicker, useToast } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
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
  const models = useEngineModels(true);
  const setEffort = useSetTeamReasoningEffort();
  const { save } = useSettingSaver();
  const list = models.data?.models ?? [];
  const model = currentModel(list, settings.data?.defaultModel);
  const options = model?.reasoningEfforts ?? [];
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
  if (!model || options.length === 0) return null;

  const current = chosen ?? stored;
  // Following the model's default shows the model's default level.
  const shown = current || model.defaultReasoningEffort || options[0].effort;
  const chooseEffort = (effort: string, now = false) => {
    if (effort === current) return;
    setChosen(effort);
    saver.current?.set(effort);
    if (now) saver.current?.flush();
  };

  return (
    <ModelEffortPicker
      disabled={disabled}
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
      onReset={() => chooseEffort('', true)}
      labels={{ effort: t('effort.label'), reset: t('effort.reset'), chooseModel: t('effort.chooseModel'), back: t('effort.back') }}
      note={`${t('effort.note')} ${t('effort.modelNote')}`}
    />
  );
}
