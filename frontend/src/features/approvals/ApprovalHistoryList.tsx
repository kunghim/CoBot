// Decision history is read-only and ordered by decidedAt + ID. Each entry is
// one line — what was asked, by whom, what was decided — and expands into
// the full request.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { useApprovals, useTeams } from '../../api/queries';
import type { Approval, Bot } from '../../api/types';
import { useSelectedBot } from '../../app/SelectedBotContext';
import LoadMore from '../../components/LoadMore';
import { displayCommand } from '../conversations/steps';
import MergeDetails from './MergeDetails';
import NativeApprovalBody from './NativeApprovalBody';
import { parseNativeRequest } from './nativeRequest';
import { approvalSummaryText, optionFallbackText, optionLabelKeys } from './pager';

export default function ApprovalHistoryList({ teamId, viewBotId }: { teamId: string; viewBotId?: string }) {
  const { t } = useTranslation();
  const history = useApprovals(teamId, 'history', true, viewBotId);
  const teams = useTeams();
  if (history.isPending) return <p className="px-3 py-2 text-sm text-neutral-500" role="status">{t('status.loading')}</p>;
  if (history.isError && !history.data) return <p className="px-3 py-2 text-sm text-red-600" role="alert">{t('approvals.historyLoadFailed')}</p>;
  const items = history.data?.items ?? [];
  if (!items.length) return <p className="px-3 py-2 text-sm text-neutral-500">{t('approvals.historyEmpty')}</p>;
  const bots = teams.data?.items.find((item) => item.team.id === teamId)?.bots ?? [];
  return (
    <div>
      <ul className="divide-y divide-neutral-100 px-3 py-1 dark:divide-neutral-800">
        {items.map((approval) => approval.decision && <HistoryRow key={approval.id} approval={approval} bots={bots} />)}
      </ul>
      <LoadMore page={history} />
    </div>
  );
}

/** One line saying what the request was about. */
function headline(approval: Approval, t: (key: string, opts?: Record<string, unknown>) => string): string {
  if (approval.type === 'native_tool') {
    const request = parseNativeRequest(approval.summary);
    if (request?.command !== undefined) return displayCommand(request.command);
    if (request && (request.changes.length || request.paths.length)) return t('approvals.native.files', { count: request.changes.length || request.paths.length });
    if (request?.permissions) return t('approvals.native.permissions');
  }
  return approvalSummaryText(t, approval.summary);
}

function HistoryRow({ approval, bots }: { approval: Approval; bots: Bot[] }) {
  const { t, i18n } = useTranslation();
  const { setBotId } = useSelectedBot();
  const [open, setOpen] = useState(false);
  const decision = approval.decision!;
  const requester = bots.find((bot) => bot.id === approval.requesterBotId);
  const chosen = approval.options.find((option) => option.id === decision.optionId);
  const choice = chosen ? t(optionLabelKeys(chosen), { defaultValue: optionFallbackText(chosen) }) : decision.optionId;
  const declined = /decline|deny|reject|cancel|stop|abandon/i.test(decision.optionId);
  const actor = decision.actor === 'user-local' || decision.actor === 'user' ? t('approvals.you') : bots.find((bot) => bot.id === decision.actor)?.name ?? decision.actor;
  const time = new Date(decision.decidedAt).toLocaleString(i18n.language, { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  const reason = approval.type === 'merge' ? '' : approval.reason ? t(`approvals.reasonCode.${approval.reason}`, { defaultValue: approval.reason }) : '';

  return (
    <li className="py-2 text-xs">
      <button type="button" onClick={() => setOpen(!open)} aria-expanded={open} className="flex w-full items-start gap-1.5 rounded text-left hover:bg-neutral-50 dark:hover:bg-neutral-800/60">
        <span className="mt-0.5 shrink-0 text-neutral-400">{open ? <ChevronDown size={12} aria-hidden="true" /> : <ChevronRight size={12} aria-hidden="true" />}</span>
        <span className="min-w-0 flex-1 space-y-0.5">
          <span className="flex items-baseline gap-2">
            <span className="min-w-0 flex-1 truncate font-medium text-neutral-800 dark:text-neutral-200">
              {t(`approvals.type.${approval.type}`, { defaultValue: t('approvals.title') })} · {requester?.name ?? t('activity.someone')}
            </span>
            <span className="shrink-0 text-[11px] text-neutral-400">{time}</span>
          </span>
          <span className="block truncate font-mono text-[11px] text-neutral-500 dark:text-neutral-400">{headline(approval, t)}</span>
          <span className="flex flex-wrap items-center gap-1.5">
            <span className={`rounded-full px-1.5 py-0.5 text-[10px] font-medium ${declined ? 'bg-red-50 text-red-700 dark:bg-red-950/50 dark:text-red-300' : 'bg-green-50 text-green-700 dark:bg-green-950/50 dark:text-green-300'}`}>
              {t('approvals.decisionBy', { actor, choice })}
            </span>
            {approval.requestState === 'invalidated' && <span className="text-[10px] text-neutral-400">{t('approvals.laterInvalidatedShort')}</span>}
            {(approval.responseState === 'unknown' || approval.responseState === 'expired') && (
              <span className="text-[10px] text-amber-600">{t(`approvals.response.${approval.responseState}`)}</span>
            )}
          </span>
        </span>
      </button>
      {open && (
        <div className="mt-2 space-y-1.5 pl-[18px]">
          {approval.type === 'native_tool' ? (
            <NativeApprovalBody summary={approval.summary} fallback={approvalSummaryText(t, approval.summary)} />
          ) : (
            <p className="whitespace-pre-wrap text-neutral-700 dark:text-neutral-300">{approvalSummaryText(t, approval.summary)}</p>
          )}
          {reason && <p className="text-neutral-600 dark:text-neutral-400">{t('approvals.reason')}：{reason}</p>}
          <MergeDetails approval={approval} />
          {decision.comment && <p className="whitespace-pre-wrap">{t('approvals.comment')}：{decision.comment}</p>}
          {requester && (
            <button type="button" className="text-blue-600 underline-offset-2 hover:underline dark:text-blue-400" onClick={() => setBotId(requester.id)}>
              {t('approvals.openConversation')}
            </button>
          )}
        </div>
      )}
    </li>
  );
}
