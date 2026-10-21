// The name a provider is shown under: a catalogue vendor's localised name,
// else the name the user gave it.
import { useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { useProviderPresets } from '../../api/queries';
import type { Provider } from '../../api/types';
import { presetName } from './AddProviderDialog';

export function useProviderName(): (provider: Provider) => string {
  const { t } = useTranslation();
  const presets = useProviderPresets();
  const byId = useMemo(() => new Map((presets.data?.presets ?? []).map((entry) => [entry.id, entry])), [presets.data]);
  return useCallback(
    (provider: Provider) => {
      const preset = provider.presetId ? byId.get(provider.presetId) : undefined;
      return preset ? presetName(preset, t) : provider.name;
    },
    [byId, t],
  );
}
