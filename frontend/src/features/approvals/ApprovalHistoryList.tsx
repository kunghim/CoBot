// Decision history is read-only and ordered by decidedAt + ID. Each entry is
// one line — what was asked, by whom, what was decided — and expands into
// the full request.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Badge, Button, cx, EmptyState, Icons, interactive, Skeleton } from '../../components/ui';
import { useApprovals, useTeams } from '../../api/queries';
import type { Approval, Bot } from '../../api/types';
import { useSelectedBot } from '../../app/SelectedBotContext';
import LoadMore from '../../components/LoadMore';
import { displayCommand } from '../conversations/steps';
import NativeApprovalBody from './NativeApprovalBody';
import { parseNativeRequest } from './nativeRequest';
import { approvalSummaryText, optionFallbackText, optionLabelKeys } from './pager';

export default function ApprovalHistoryList({ teamId, viewBotId }: { teamId: string; viewBotId?: string }) {
  const { t } = useTranslation();
  const history = useApprovals(teamId, 'history', true, viewBotId);
  const teams = useTeams();
  if (history.isPending) return <div className="space-y-2 px-3 py-3" role="status" aria-label={t('status.loading')}><Skeleton className="h-10 w-full" /><Skeleton className="h-10 w-full" /></div>;
  if (history.isError && !history.data) return <div className="px-3 py-3"><Alert tone="danger">{t('approvals.historyLoadFailed')}</Alert></div>;
  const items = history.data?.items ?? [];
  if (!items.length) return <EmptyState icon={<Icons.approvals size={20} aria-hidden />} title={t('approvals.historyEmpty')} />;
  const bots = teams.data?.items.find((item) => item.team.id === teamId)?.bots ?? [];
  return (
    <div>
      <ul className="divide-y divide-neutral-200 px-3 py-1 dark:divide-neutral-800">
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
  const reason = approval.reason ? t(`approvals.reasonCode.${approval.reason}`, { defaultValue: approval.reason }) : '';

  return (
    <li className="py-2 text-xs">
      <button type="button" onClick={() => setOpen(!open)} aria-expanded={open} className={cx('-mx-1.5 flex w-[calc(100%+0.75rem)] items-start gap-1.5 rounded-lg px-1.5 py-1 text-left transition-colors', interactive.hover)}>
        <span className="mt-0.5 shrink-0 text-neutral-400">{open ? <Icons.expand size={12} aria-hidden /> : <Icons.next size={12} aria-hidden />}</span>
        <span className="min-w-0 flex-1 space-y-0.5">
          <span className="flex items-baseline gap-2">
            <span className="min-w-0 flex-1 truncate font-medium text-neutral-800 dark:text-neutral-200">
              {t(`approvals.type.${approval.type}`, { defaultValue: t('approvals.title') })} · {requester?.name ?? t('activity.someone')}
            </span>
            <span className="shrink-0 text-2xs text-neutral-500 dark:text-neutral-400">{time}</span>
          </span>
          <span className="block truncate font-mono text-2xs text-neutral-500 dark:text-neutral-400">{headline(approval, t)}</span>
          <span className="flex flex-wrap items-center gap-1.5">
            <Badge tone={declined ? 'danger' : 'success'}>{t('approvals.decisionBy', { actor, choice })}</Badge>
            {approval.requestState === 'invalidated' && <span className="text-2xs text-neutral-500 dark:text-neutral-400">{t('approvals.laterInvalidatedShort')}</span>}
            {(approval.responseState === 'unknown' || approval.responseState === 'expired') && (
              <span className="text-2xs text-amber-700 dark:text-amber-400">{t(`approvals.response.${approval.responseState}`)}</span>
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
          {decision.comment && <p className="whitespace-pre-wrap">{t('approvals.comment')}：{decision.comment}</p>}
          {requester && (
            <Button variant="ghost" size="sm" icon={Icons.sendMessage} onClick={() => setBotId(requester.id)}>
              {t('approvals.openConversation')}
            </Button>
          )}
        </div>
      )}
    </li>
  );
}
