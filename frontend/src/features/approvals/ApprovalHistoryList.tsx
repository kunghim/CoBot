// Decision history is read-only and ordered by decidedAt + ID.
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useApprovals, useTeams } from '../../api/queries';
import { useSelectedBot } from '../../app/SelectedBotContext';
import LoadMore from '../../components/LoadMore';
import { approvalSummaryText, optionFallbackText, optionLabelKeys } from './pager';
import MergeDetails from './MergeDetails';

export default function ApprovalHistoryList({ teamId, viewBotId }: { teamId: string; viewBotId?: string }) {
  const { t, i18n } = useTranslation();
  const history = useApprovals(teamId, 'history', true, viewBotId);
  const teams = useTeams();
  const { setBotId } = useSelectedBot();
  const navigate = useNavigate();
  if (history.isPending) return <p className="px-3 py-2 text-sm text-neutral-500" role="status">{t('status.loading')}</p>;
  if (history.isError && !history.data) return <p className="px-3 py-2 text-sm text-red-600" role="alert">{t('approvals.historyLoadFailed')}</p>;
  const items = history.data?.items ?? [];
  if (!items.length) return <p className="px-3 py-2 text-sm text-neutral-500">{t('approvals.historyEmpty')}</p>;
  const bots = teams.data?.items.find((item) => item.team.id === teamId)?.bots ?? [];
  const formatTime = (iso: string) => new Date(iso).toLocaleString(i18n.language);
  return <div><ul className="space-y-2 px-3 py-2">
    {items.map((approval) => {
      const decision = approval.decision;
      if (!decision) return null;
      const requester = bots.find((bot) => bot.id === approval.requesterBotId);
      return <li key={approval.id} className="space-y-1 break-words rounded-lg border border-neutral-200 p-2 text-xs dark:border-neutral-700">
        <p className="font-medium">{t(`approvals.type.${approval.type}`, { defaultValue: t('approvals.title') })}</p>
        <p>{t('approvals.requester', { name: requester?.name ?? approval.requesterBotId })}</p>
        <pre className="whitespace-pre-wrap break-all font-mono">{approvalSummaryText(t, approval.summary)}</pre>
        {approval.reason && <p>{t('approvals.reason')}: {approval.reason}</p>}
        <MergeDetails approval={approval} />
        <p className="text-neutral-500">{t('approvals.requestedAt', { time: formatTime(approval.createdAt) })}</p>
        <ul className="flex flex-wrap gap-1" aria-label={t('approvals.originalOptions')}>
          {approval.options.map((option) => <li key={option.id} className={`rounded border px-1.5 py-0.5 ${option.id === decision.optionId ? 'border-neutral-500 font-semibold' : 'border-neutral-200 text-neutral-500 dark:border-neutral-700'}`}>{t(optionLabelKeys(option), { defaultValue: optionFallbackText(option) })}{option.id === decision.optionId ? ` · ${t('approvals.chosenOption')}` : ''}</li>)}
        </ul>
        <p>{t('approvals.decidedBy', { actor: decision.actor, time: formatTime(decision.decidedAt) })}</p>
        {decision.comment && <p className="whitespace-pre-wrap">{t('approvals.comment')}: {decision.comment}</p>}
        <p>{t('approvals.responseState')}: {t(`approvals.response.${approval.responseState ?? 'unknown'}`)}</p>
        {approval.requestState === 'invalidated' && <p>{t('approvals.laterInvalidated')}</p>}
        {requester && <button type="button" className="underline underline-offset-2" onClick={() => { setBotId(requester.id); navigate('/'); }}>{t('approvals.openConversation')}</button>}
      </li>;
    })}
  </ul><LoadMore page={history} /></div>;
}
