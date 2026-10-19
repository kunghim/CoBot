import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import { queryKeys, useUpdateSettings } from '../../api/queries';
import type { Settings, SettingsPatch } from '../../api/types';
import { useToast } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';

/**
 * useSettingSaver saves each setting as soon as it changes. Saves run one at
 * a time and each reads the latest cached revision, so quick successive
 * changes never trip the optimistic-concurrency check against each other.
 * A save that worked is confirmed by a toast (one at a time, so a run of
 * changes does not stack them); one that failed reports under its field and
 * in a failure toast, and calls rollback, so the UI never keeps showing a
 * value that was not stored.
 */
export function useSettingSaver() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const update = useUpdateSettings();
  const toast = useToast();
  const queue = useRef<Promise<void>>(Promise.resolve());
  const lastToast = useRef<number | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});

  const save = (field: string, patch: SettingsPatch, rollback?: (persisted: Settings) => void) => {
    queue.current = queue.current.then(async () => {
      const current = qc.getQueryData<Settings>(queryKeys.settings);
      try {
        await update.mutateAsync({ patch, expectedRevision: current?.revision });
        setErrors((prev) => {
          if (!(field in prev)) return prev;
          const next = { ...prev };
          delete next[field];
          return next;
        });
        if (lastToast.current !== null) toast.dismiss(lastToast.current);
        lastToast.current = toast.show({ tone: 'success', title: t('settings.saved'), duration: 2000 });
      } catch (err) {
        const message = apiErrorMessage(t, err);
        setErrors((prev) => ({ ...prev, [field]: t('settings.saveFailed') + ': ' + message }));
        if (lastToast.current !== null) toast.dismiss(lastToast.current);
        lastToast.current = toast.show({ tone: 'danger', title: t('settings.saveFailed'), description: message });
        const persisted = qc.getQueryData<Settings>(queryKeys.settings);
        if (persisted && rollback) rollback(persisted);
      }
    });
  };

  return { save, errors };
}
