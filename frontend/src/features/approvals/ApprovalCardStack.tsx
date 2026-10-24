// ApprovalCardStack shows pending approvals of the current team one card at
// a time above the composer, in the waiting-card frame Codex uses: who asks
// and what, then the answers as pill buttons on the right, the recommended
// one last. There is no autoplay: the pager only moves on explicit user
// input, and a decided card disappears from the stack while the right panel
// keeps the history. Cards arrive oldest-first from the API.
import { useContext, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useApprovals, useDecideApproval } from '../../api/queries';
import type { ApprovalOption } from '../../api/types';
import { Alert, Button, Icons, WaitingCard } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { TeamMembersContext } from '../conversations/ExchangeLine';
import Avatar from '../avatars/Avatar';
import { approvalSummaryText, clampPagerIndex, optionFallbackText, optionLabelKeys } from './pager';
import NativeApprovalBody from './NativeApprovalBody';

export default function ApprovalCardStack({
  teamId,
  viewBotId,
  botNameById,
}: {
  teamId: string;
  viewBotId?: string;
  botNameById: ReadonlyMap<string, string>;
}) {
  const { t } = useTranslation();
  const members = useContext(TeamMembersContext);
  const approvals = useApprovals(teamId, 'pending', true, viewBotId);
  const decide = useDecideApproval();
  const [index, setIndex] = useState(0);
  const [decideError, setDecideError] = useState('');

  const items = approvals.data?.items ?? [];
  const selectedId = items[clampPagerIndex(index, items.length)]?.id;
  useEffect(() => setDecideError(''), [selectedId]);
  if (approvals.isError && !approvals.data) return <Alert tone="danger">{t('approvals.loadFailed')}</Alert>;
  if (items.length === 0) return null;
  const current = clampPagerIndex(index, items.length);
  const approval = items[current];
  const total = approvals.data?.total ?? items.length;

  const decideOption = (option: ApprovalOption) => {
    setDecideError('');
    decide.mutate(
      { approvalId: approval.id, optionId: option.id, expectedRevision: approval.revision },
      { onError: (error) => setDecideError(apiErrorMessage(t, error)) },
    );
  };

  const requester = members.get(approval.requesterBotId);
  const requesterName = requester?.name ?? botNameById.get(approval.requesterBotId) ?? approval.requesterBotId;
  const typeLabel = t(`approvals.type.${approval.type}`, { defaultValue: t('approvals.title') });
  // The API lists the recommended answer first; it goes last, on the right.
  const answers = [...approval.options].reverse();
  const unavailable = [...new Set(approval.options.map((option) => option.disabledReasonKey).filter(Boolean))];

  return (
    <WaitingCard
      icon={<Icons.permission size={16} />}
      title={typeLabel}
      position={{ index: current + 1, total }}
      onPrevious={() => setIndex(current - 1)}
      onNext={() => {
        if (current < items.length - 1) setIndex(current + 1);
        else if (approvals.hasNextPage) void approvals.fetchNextPage().then((result) => { if (!result.isError) setIndex(current + 1); });
      }}
      previousLabel={t('approvals.prev')}
      nextLabel={t('approvals.next')}
      actions={answers.map((option) => (
        <Button
          key={option.id}
          shape="pill"
          size="sm"
          variant={option === approval.options[0] ? 'primary' : 'secondary'}
          disabled={decide.isPending || option.disabled}
          title={option.disabledReasonKey ? t(`errors.${option.disabledReasonKey}`, { defaultValue: t('approvals.optionUnavailable') }) : undefined}
          onClick={() => decideOption(option)}
        >
          {t(optionLabelKeys(option), { defaultValue: optionFallbackText(option) })}
        </Button>
      ))}
    >
      <p className="flex flex-wrap items-center gap-1.5 text-neutral-600 dark:text-neutral-300">
        <span className="inline-flex items-center gap-1.5 font-semibold text-neutral-900 dark:text-neutral-50">
          {requester && <Avatar bot={requester} size="sm" />}
          {requesterName}
        </span>
        <span>{t('approvals.requesterAsks')}</span>
      </p>
      {approval.type === 'native_tool' ? (
        <NativeApprovalBody summary={approval.summary} fallback={approvalSummaryText(t, approval.summary)} />
      ) : (
        <pre className="max-h-52 overflow-y-auto whitespace-pre-wrap break-all rounded-xl bg-neutral-900/[0.04] px-3 py-2 font-mono text-xs leading-5 text-neutral-800 dark:bg-black/30 dark:text-neutral-100">
          {approvalSummaryText(t, approval.summary)}
        </pre>
      )}
      {approval.reason && (
        <p>
          <span className="text-neutral-500 dark:text-neutral-400">{t('approvals.reason')}: </span>
          <span className="whitespace-pre-wrap">{t(`approvals.reasonCode.${approval.reason}`, { defaultValue: approval.reason })}</span>
        </p>
      )}
      {unavailable.map((reason) => (
        <p className="text-xs text-neutral-500 dark:text-neutral-400" key={reason}>
          {t(`errors.${reason}`, { defaultValue: t('approvals.optionUnavailable') })}
        </p>
      ))}
      {decideError && <Alert tone="danger">{decideError}</Alert>}
      {approvals.isFetchNextPageError && <Alert tone="danger">{t('paging.failed')}</Alert>}
      {decide.isPending && (
        <p className="text-xs text-neutral-500 dark:text-neutral-400" role="status">
          {t('approvals.deciding')}
        </p>
      )}
    </WaitingCard>
  );
}
