import { useTranslation } from 'react-i18next';
import { useEngine, useEngineModels, useProviders } from '../../api/queries';
import type { Settings, SettingsPatch } from '../../api/types';
import { Card, EmptyState, Field, Icons, Select, Skeleton } from '../../components/ui';
import { choiceKey, defaultChoice, engineDefaultModel, selectableModels } from './providerModels';
import { useProviderName } from './useProviderName';

/** The part of a settings section the providers section reads. */
export type ModelSettingsProps = {
  settings: Settings;
  save: (field: string, patch: SettingsPatch, rollback?: (persisted: Settings) => void) => void;
  errors: Record<string, string>;
};

/**
 * DefaultModelCard names the default model: the one a member runs with until
 * it chooses its own in the conversation. Any model of an enabled provider
 * can be the default; the choice is one write of the provider and the model
 * together, so the two never disagree. It lives inside the providers
 * section, above the provider list, and shows the model the members
 * actually follow when nothing is named (the runtime fallback), not an
 * empty picker.
 */
export function DefaultModelCard({ settings, save, errors }: ModelSettingsProps) {
  const { t } = useTranslation();
  const engine = useEngine();
  const providers = useProviders();
  const models = useEngineModels(engine.data?.status === 'ready');
  const providerName = useProviderName();

  // Until the provider list and the engine's answers are in, there is no
  // telling what is on offer: the official account's models and whether it is
  // signed in both come from the engine, and reading them as absent would say
  // "no model" for the moment before they arrive.
  if (providers.isPending || engine.isPending || (engine.data?.status === 'ready' && models.isPending)) {
    return (
      <Card>
        <Skeleton className="h-9 w-64" />
      </Card>
    );
  }
  const list = providers.data?.providers;
  const offered = selectableModels(list, models.data?.models, engine.data?.loggedIn ?? false, providerName);
  if (offered.length === 0) {
    return (
      <Card>
        <EmptyState icon={<Icons.engine size={20} aria-hidden />} title={t('settings.modelsNeedProvider')} />
      </Card>
    );
  }
  const current = defaultChoice(settings, list, engineDefaultModel(models.data?.models), engine.data?.loggedIn ?? false);
  const value = choiceKey(current.providerId, current.model);
  const unavailable = t('settings.modelUnavailable');
  const options = offered.map((entry) => ({
    value: entry.key,
    label: `${entry.group} · ${entry.unavailable ? `${entry.label} · ${unavailable}` : entry.label}`,
    disabled: entry.unavailable,
  }));
  if (!options.some((option) => option.value === value) && current.model) {
    options.unshift({ value, label: current.model, disabled: false });
  }

  return (
    <Card>
      <div className="max-w-md" data-testid="default-model-card">
        <Field label={t('settings.defaultModel')} hint={t('settings.defaultModelHint')} info={t('settings.defaultModelInfo')} error={errors.defaultModel}>
          <Select
            value={value}
            options={options}
            onChange={(next) => {
              const chosen = offered.find((entry) => entry.key === next);
              if (!chosen) return;
              save('defaultModel', { defaultProvider: chosen.providerId, defaultModel: chosen.model });
            }}
          />
        </Field>
      </div>
    </Card>
  );
}
