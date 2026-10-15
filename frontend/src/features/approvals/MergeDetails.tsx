import { useTranslation } from 'react-i18next';
import type { Approval } from '../../api/types';

export default function MergeDetails({ approval }: { approval: Approval }) {
  const { t } = useTranslation();
  const details = approval.details;
  if (!details) return null;
  return <div className="space-y-2 break-words text-xs">
    <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-2 gap-y-1">
      <dt>{t('approvals.candidate')}</dt><dd className="break-all font-mono">{details.candidateRef}</dd>
      <dt>{t('approvals.target')}</dt><dd className="break-all font-mono">{details.targetRef}</dd>
      <dt>{t('approvals.executionDirectory')}</dt><dd className="break-all">{details.executionDirectory}</dd>
      <dt>{t('approvals.boundTask')}</dt><dd className="break-all">{details.taskId} · v{details.taskRevision}</dd>
    </dl>
    <p className="text-neutral-500">{t('approvals.reportedEvidence')}</p>
    {details.evidence.map((evidence) => <div key={evidence.id} className="space-y-1 rounded border border-neutral-200 p-2 dark:border-neutral-700">
      <p>{evidence.summary}</p>
      {evidence.testResult && <p className="whitespace-pre-wrap">{t('approvals.testReport')}: {evidence.testResult}</p>}
      {evidence.reviewResult && <p className="whitespace-pre-wrap">{t('approvals.reviewReport')}: {evidence.reviewResult}</p>}
      {evidence.artifactRefs.map((ref) => <p className="break-all font-mono" key={ref}>{ref}</p>)}
    </div>)}
  </div>;
}
