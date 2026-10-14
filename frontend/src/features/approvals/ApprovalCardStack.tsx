// ApprovalCardStack shows pending approvals of the current team one card at
// a time above the composer. There is no autoplay: the pager only moves on
// explicit user input, and a decided card disappears from the stack while the
// right panel keeps the history. Cards arrive oldest-first from the API.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { useApprovals, useDecideApproval } from '../../api/queries';
import type { ApprovalOption } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import { clampPagerIndex, optionFallbackText, optionLabelKeys } from './pager';

export default function ApprovalCardStack({
  teamId,
  botNameById,
}: {
  teamId: string;
  botNameById: ReadonlyMap<string, string>;
}) {
  const { t } = useTranslation();
  const approvals = useApprovals(teamId, 'pending');
  const decide = useDecideApproval();
  const [index, setIndex] = useState(0);
  const [decideError, setDecideError] = useState('');

  const items = approvals.data?.items ?? [];
  if (items.length === 0) return null;
  const current = clampPagerIndex(index, items.length);
  const approval = items[current];

  const decideOption = (option: ApprovalOption) => {
    setDecideError('');
    decide.mutate(
      { approvalId: approval.id, optionId: option.id, expectedRevision: approval.revision },
      { onError: (error) => setDecideError(apiErrorMessage(t, error)) },
    );
  };

  const requesterName = botNameById.get(approval.requesterBotId) ?? approval.requesterBotId;
  const typeLabel = t(`approvals.type.${approval.type}`, { defaultValue: t('approvals.title') });

  return (
    <section
      aria-label={typeLabel}
      className="rounded-xl border border-amber-200 bg-amber-50/60 dark:border-amber-800 dark:bg-amber-950/40"
    >
      <header className="flex items-center gap-2 border-b border-amber-200 px-3 py-1.5 dark:border-amber-800">
        <h3 className="text-xs font-semibold text-amber-900 dark:text-amber-100">{typeLabel}</h3>
        {items.length > 1 && (
          <div className="ml-auto flex items-center gap-0.5 text-xs text-amber-800 dark:text-amber-200">
            <button
              type="button"
              onClick={() => setIndex(current - 1)}
              disabled={current === 0}
              title={t('approvals.prev')}
              aria-label={t('approvals.prev')}
              className="flex h-6 w-6 items-center justify-center rounded hover:bg-amber-200 disabled:opacity-40 dark:hover:bg-amber-900"
            >
              <ChevronLeft size={14} aria-hidden="true" />
            </button>
            <span aria-live="polite">{t('approvals.position', { index: current + 1, total: items.length })}</span>
            <button
              type="button"
              onClick={() => setIndex(current + 1)}
              disabled={current === items.length - 1}
              title={t('approvals.next')}
              aria-label={t('approvals.next')}
              className="flex h-6 w-6 items-center justify-center rounded hover:bg-amber-200 disabled:opacity-40 dark:hover:bg-amber-900"
            >
              <ChevronRight size={14} aria-hidden="true" />
            </button>
          </div>
        )}
      </header>

      <div className="space-y-2 px-3 py-2 text-sm">
        <p className="text-xs text-amber-800 dark:text-amber-200">
          {t('approvals.requester', { name: requesterName })}
        </p>
        <pre className="whitespace-pre-wrap break-all rounded-md bg-white/70 px-2 py-1.5 font-mono text-xs text-neutral-800 dark:bg-neutral-900/60 dark:text-neutral-200">
          {approval.summary}
        </pre>
        {approval.reason && (
          <p className="text-sm">
            <span className="text-neutral-500 dark:text-neutral-400">{t('approvals.reason')}: </span>
            <span className="whitespace-pre-wrap">{approval.reason}</span>
          </p>
        )}
        {decideError && (
          <p className="text-sm text-red-600" role="alert">
            {decideError}
          </p>
        )}
        <div className="flex flex-wrap gap-2">
          {approval.options.map((option) => (
            <button
              key={option.id}
              type="button"
              disabled={decide.isPending}
              onClick={() => decideOption(option)}
              className="rounded-md border border-amber-300 bg-white px-3 py-1.5 text-sm font-medium text-amber-900 transition-colors hover:bg-amber-100 disabled:opacity-50 dark:border-amber-700 dark:bg-neutral-900 dark:text-amber-100 dark:hover:bg-amber-900"
            >
              {t(optionLabelKeys(option), { defaultValue: optionFallbackText(option) })}
            </button>
          ))}
        </div>
        {decide.isPending && (
          <p className="text-xs text-amber-700 dark:text-amber-300" role="status">
            {t('approvals.deciding')}
          </p>
        )}
      </div>
    </section>
  );
}
