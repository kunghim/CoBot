// ApprovalCardStack shows pending approvals of the current team one card at
// a time above the composer, in the waiting-card frame Codex uses: who asks
// and what, then the answers as pill buttons on the right, the recommended
// one last. There is no autoplay: the pager only moves on explicit user
// input, and a decided card disappears from the stack while the right panel
// keeps the history. Cards arrive oldest-first from the API.
import { useContext, useEffect, useState, type ComponentType } from 'react';
import { useTranslation } from 'react-i18next';
import { useApprovals, useDecideApproval } from '../../api/queries';
import type { Approval, ApprovalOption } from '../../api/types';
import { Alert, Button, Icons, WaitingCard } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { TeamMembersContext } from '../../app/TeamMembersContext';
import Avatar from '../avatars/Avatar';
import { approvalSummaryText, optionFallbackText, optionLabelKeys } from './pager';
import { useCardPager } from '../../components/useCardPager';
import NativeApprovalBody from './NativeApprovalBody';
import ElicitationBody, { parseElicitation } from './ElicitationBody';

interface BodyProps {
  approval: Approval;
  teamId: string;
  // An MCP elicitation's form answers, sent with the accept decision.
  content: Record<string, unknown>;
  onContent: (content: Record<string, unknown>) => void;
}

function NativeBody({ approval, teamId, content, onContent }: BodyProps) {
  const { t } = useTranslation();
  if (approval.method === 'mcpServer/elicitation/request' && parseElicitation(approval.summary)) {
    return <ElicitationBody summary={approval.summary} value={content} onChange={onContent} teamId={teamId} />;
  }
  return <NativeApprovalBody summary={approval.summary} fallback={approvalSummaryText(t, approval.summary)} />;
}

function SummaryBody({ approval }: BodyProps) {
  const { t } = useTranslation();
  return (
    <pre className="max-h-52 overflow-y-auto whitespace-pre-wrap break-all rounded-xl bg-neutral-900/[0.04] px-3 py-2 font-mono text-xs leading-5 text-neutral-800 dark:bg-black/30 dark:text-neutral-100">
      {approvalSummaryText(t, approval.summary)}
    </pre>
  );
}

// What each type of card shows of what it asks. Keyed by the interface's own
// list, so a type added there does not compile until it is here.
const approvalBodies: Record<Approval['type'], ComponentType<BodyProps>> = {
  native_tool: NativeBody,
  recovery: SummaryBody,
  chain: SummaryBody,
  budget: SummaryBody,
};

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
  const [decideError, setDecideError] = useState('');
  const [content, setContent] = useState<Record<string, unknown>>({});

  const items = approvals.data?.items ?? [];
  const { current, onPrevious, onNext } = useCardPager(items.length, approvals);
  const selectedId = items[current]?.id;
  useEffect(() => {
    setDecideError('');
    setContent({});
  }, [selectedId]);
  if (approvals.isError && !approvals.data) return <Alert tone="danger">{t('approvals.loadFailed')}</Alert>;
  if (items.length === 0) return null;
  const approval = items[current];
  const total = approvals.data?.total ?? items.length;

  const elicitation = approval.method === 'mcpServer/elicitation/request';
  const decideOption = (option: ApprovalOption) => {
    setDecideError('');
    decide.mutate(
      {
        approvalId: approval.id,
        optionId: option.id,
        expectedRevision: approval.revision,
        ...(elicitation && option.id === 'accept' ? { content } : {}),
      },
      { onError: (error) => setDecideError(apiErrorMessage(t, error)) },
    );
  };

  const Body = approvalBodies[approval.type] ?? SummaryBody;
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
      initiator={
        <>
          {requester && <Avatar bot={requester} size="sm" />}
          <span className="min-w-0 truncate">{requesterName}</span>
        </>
      }
      position={{ index: current + 1, total }}
      onPrevious={onPrevious}
      onNext={onNext}
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
      <Body approval={approval} teamId={teamId} content={content} onContent={setContent} />
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
