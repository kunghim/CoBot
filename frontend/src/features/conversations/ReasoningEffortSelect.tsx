// ReasoningEffortSelect is the composer's thinking-effort switch, like
// Codex's: a pill that opens the efforts the current model offers, plus the
// model's own default. The effort belongs to the team and applies from each
// member's next turn.
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Brain, Check, ChevronDown } from 'lucide-react';
import { useEngineModels, useSetTeamReasoningEffort, useSettings } from '../../api/queries';
import type { EngineModel, Team } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';

/** The model members run on: the configured default model, else the
 * engine's own default. */
export function currentModel(models: EngineModel[], configured: string | undefined): EngineModel | undefined {
  return models.find((m) => configured && (m.slug === configured || m.id === configured)) ?? models.find((m) => m.isDefault) ?? models[0];
}

export default function ReasoningEffortSelect({ team, disabled }: { team: Team; disabled?: boolean }) {
  const { t } = useTranslation();
  const settings = useSettings(true);
  const models = useEngineModels(true);
  const setEffort = useSetTeamReasoningEffort();
  const [open, setOpen] = useState(false);
  const [error, setError] = useState('');
  const rootRef = useRef<HTMLDivElement>(null);
  const model = currentModel(models.data?.models ?? [], settings.data?.defaultModel);
  const options = model?.reasoningEfforts ?? [];
  const current = team.reasoningEffort ?? '';
  const label = (effort: string) => t(`effort.level.${effort}`, { defaultValue: effort });

  useEffect(() => {
    if (!open) return;
    const close = (event: MouseEvent | KeyboardEvent) => {
      if (event instanceof KeyboardEvent ? event.key === 'Escape' : !rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', close);
    document.addEventListener('keydown', close);
    return () => {
      document.removeEventListener('mousedown', close);
      document.removeEventListener('keydown', close);
    };
  }, [open]);

  // Without a model list there is nothing to choose from; a chosen effort
  // still shows so it is never hidden from the user.
  if (options.length === 0 && current === '') return null;

  const choose = (effort: string) => {
    setOpen(false);
    if (effort === current) return;
    setError('');
    setEffort.mutate({ teamId: team.id, effort, expectedRevision: team.revision }, { onError: (err) => setError(apiErrorMessage(t, err)) });
  };
  const pill = current ? label(current) : t('effort.default');
  const items: Array<{ effort: string; title: string; hint?: string }> = [
    { effort: '', title: t('effort.default'), hint: model?.defaultReasoningEffort ? t('effort.modelDefault', { level: label(model.defaultReasoningEffort) }) : undefined },
    ...options.map((o) => ({ effort: o.effort, title: label(o.effort), hint: o.description })),
  ];

  return (
    <div ref={rootRef} className="relative flex min-w-0 items-center gap-2">
      <button
        type="button"
        disabled={disabled || setEffort.isPending}
        onClick={() => setOpen(!open)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`${t('effort.label')}：${pill}`}
        title={t('effort.label')}
        className="inline-flex h-7 items-center gap-1 rounded-full px-2.5 text-xs font-medium text-neutral-600 transition-colors hover:bg-neutral-100 hover:text-neutral-900 disabled:opacity-50 dark:text-neutral-300 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
      >
        <Brain size={14} aria-hidden="true" />
        {pill}
        <ChevronDown size={12} aria-hidden="true" className="opacity-60" />
      </button>
      {open && (
        <div role="menu" aria-label={t('effort.label')} className="absolute bottom-full left-0 z-20 mb-2 w-72 rounded-xl border border-neutral-200 bg-white p-1 shadow-lg dark:border-neutral-700 dark:bg-neutral-900">
          {items.map((item) => (
            <button
              key={item.effort || 'default'}
              type="button"
              role="menuitemradio"
              aria-checked={item.effort === current}
              onClick={() => choose(item.effort)}
              className="flex w-full items-start gap-2 rounded-lg px-2.5 py-2 text-left text-sm hover:bg-neutral-100 dark:hover:bg-neutral-800"
            >
              <span className="mt-0.5 w-4 shrink-0">{item.effort === current && <Check size={14} aria-hidden="true" />}</span>
              <span className="min-w-0">
                <span className="block font-medium text-neutral-900 dark:text-neutral-100">{item.title}</span>
                {item.hint && <span className="block text-xs text-neutral-500 dark:text-neutral-400">{item.hint}</span>}
              </span>
            </button>
          ))}
          <p className="px-2.5 pb-1.5 pt-1 text-[11px] text-neutral-400">{t('effort.note')}</p>
        </div>
      )}
      {error && <span role="alert" className="truncate text-xs text-red-600">{error}</span>}
    </div>
  );
}
