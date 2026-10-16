import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import { queryKeys, useUpdateSettings } from '../../api/queries';
import type { Settings, SettingsPatch } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';

export type SaveState = { status: 'saving' | 'saved' | 'failed'; message?: string };

/**
 * useSettingSaver saves each setting as soon as it changes. Saves run one at
 * a time and each reads the latest cached revision, so quick successive
 * changes never trip the optimistic-concurrency check against each other.
 * A failed save reports next to its field and calls rollback, so the UI
 * never keeps showing a value that was not stored.
 */
export function useSettingSaver() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const update = useUpdateSettings();
  const queue = useRef<Promise<void>>(Promise.resolve());
  const [states, setStates] = useState<Record<string, SaveState>>({});

  const save = (field: string, patch: SettingsPatch, rollback?: (persisted: Settings) => void) => {
    setStates((prev) => ({ ...prev, [field]: { status: 'saving' } }));
    queue.current = queue.current.then(async () => {
      const current = qc.getQueryData<Settings>(queryKeys.settings);
      try {
        await update.mutateAsync({ patch, expectedRevision: current?.revision });
        setStates((prev) => ({ ...prev, [field]: { status: 'saved' } }));
      } catch (err) {
        setStates((prev) => ({ ...prev, [field]: { status: 'failed', message: apiErrorMessage(t, err) } }));
        const persisted = qc.getQueryData<Settings>(queryKeys.settings);
        if (persisted && rollback) rollback(persisted);
      }
    });
  };

  return { save, states };
}
