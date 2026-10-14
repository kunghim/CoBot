// ApprovalHistoryList is the read-only "审批" page of the right panel: every
// decided or invalidated record of the team, newest first, with the option
// that was chosen, the actor, the time and the honest response state. Pending
// requests never appear here and are never actionable in this list.
import { useTranslation } from 'react-i18next';
import { useApprovals } from '../../api/queries';
import type { Approval, ApprovalOption } from '../../api/types';
import { optionFallbackText, optionLabelKeys } from './pager';

export default function ApprovalHistoryList({ teamId }: { teamId: string }) {
  const { t, i18n } = useTranslation();
  const history = useApprovals(teamId, 'history');

  if (history.isPending) {
    return <p className="px-3 py-2 text-sm text-neutral-500" role="status">{t('status.loading')}</p>;
  }
  if (history.isError) {
    return <p className="px-3 py-2 text-sm text-red-600" role="alert">{t('approvals.historyLoadFailed')}</p>;
  }

  const items = history.data?.items ?? [];
  if (items.length === 0) {
    return <p className="px-3 py-2 text-sm text-neutral-500 dark:text-neutral-400">{t('approvals.historyEmpty')}</p>;
  }

  const formatTime = (iso: string) => new Date(iso).toLocaleString(i18n.language);

  return (
    <ul className="space-y-2 px-3 py-2">
      {items.map((approval) => (
        <li key={approval.id} className="rounded-lg border border-neutral-200 p-2 text-sm dark:border-neutral-700">
          <p className="text-xs font-medium text-neutral-500 dark:text-neutral-400">
            {t(`approvals.type.${approval.type}`, { defaultValue: t('approvals.title') })}
          </p>
          <pre className="mt-1 whitespace-pre-wrap break-all font-mono text-xs text-neutral-700 dark:text-neutral-300">
            {approval.summary}
          </pre>
          <HistoryFields approval={approval} formatTime={formatTime} />
        </li>
      ))}
    </ul>
  );
}

function HistoryFields({
  approval,
  formatTime,
}: {
  approval: Approval;
  formatTime: (iso: string) => string;
}) {
  const { t } = useTranslation();
  const decision = approval.decision;
  if (!decision) {
    return (
      <p className="mt-1 text-xs text-neutral-400">
        {t('approvals.responseState')}: {t(`approvals.response.${approval.responseState ?? 'unknown'}`, { defaultValue: t('approvals.response.unknown') })}
      </p>
    );
  }
  const chosen = approval.options.find((option: ApprovalOption) => option.id === decision.optionId);
  return (
    <div className="mt-1 space-y-0.5 text-xs">
      <p>
        <span className="text-neutral-500 dark:text-neutral-400">{t('approvals.chosenOption')}: </span>
        {chosen ? t(optionLabelKeys(chosen), { defaultValue: optionFallbackText(chosen) }) : decision.optionId}
      </p>
      <p className="text-neutral-500 dark:text-neutral-400">
        {t('approvals.decidedBy', { actor: decision.actor, time: formatTime(decision.decidedAt) })}
      </p>
      <p>
        <span className="text-neutral-500 dark:text-neutral-400">{t('approvals.responseState')}: </span>
        {t(`approvals.response.${approval.responseState ?? 'unknown'}`, { defaultValue: t('approvals.response.unknown') })}
      </p>
    </div>
  );
}
