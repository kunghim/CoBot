// TaskDetail is the expanded board row: description, dependencies, evidence,
// the two-phase transfer record and the actions the current state allows.
// Every mutation sends expectedRevision, so a concurrent change fails loudly
// instead of overwriting the other writer.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  useAcceptTransfer,
  useCancelTransfer,
  useConfirmTask,
  useOfferTransfer,
  usePatchTask,
  useSubmitEvidence,
  useTaskDetail,
} from '../../api/queries';
import type { Bot, Task } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import { canConfirm, canOfferTransfer, canRequestReview, canSubmitEvidence, dependencyLabels, nextStatuses, ownerLabel } from './taskFlow';
import { Alert, Button, Icons, Select, Tag, TextInput } from '../../components/ui';

export default function TaskDetail({ task, tasks, bots }: { task: Task; tasks: Task[]; bots: Bot[] }) {
  const { t } = useTranslation();
  const detail = useTaskDetail(task.id);
  const patch = usePatchTask();
  const submitEvidence = useSubmitEvidence();
  const confirm = useConfirmTask();
  const offer = useOfferTransfer();
  const accept = useAcceptTransfer();
  const cancelTransfer = useCancelTransfer();

  const [errorText, setErrorText] = useState('');
  const [evidenceSummary, setEvidenceSummary] = useState('');
  const [testResult, setTestResult] = useState('');
  const [artifactRefs, setArtifactRefs] = useState('');
  const [transferTarget, setTransferTarget] = useState('');
  const [transferNote, setTransferNote] = useState('');

  // Prefer the freshest copy: the detail fetch carries the current revision.
  const current = detail.data?.task ?? task;
  const evidence = detail.data?.evidence ?? [];
  const transfers = detail.data?.transfers ?? [];
  const liveOffer = transfers.find((item) => item.status === 'offered');
  const hasEvidence = evidence.length > 0;
  const userLabel = t('tasks.ownerUser');

  const fail = (error: unknown) => setErrorText(apiErrorMessage(t, error));

  const patchStatus = (status: string) => {
    setErrorText('');
    patch.mutate({ taskId: current.id, status, expectedRevision: current.revision }, { onError: fail });
  };

  const submitNewEvidence = () => {
    const summary = evidenceSummary.trim();
    if (!summary) return;
    setErrorText('');
    submitEvidence.mutate(
      {
        taskId: current.id,
        summary,
        testResult: testResult.trim() || undefined,
        artifactRefs: artifactRefs
          .split(',')
          .map((ref) => ref.trim())
          .filter(Boolean),
      },
      {
        onSuccess: () => {
          setEvidenceSummary('');
          setTestResult('');
          setArtifactRefs('');
        },
        onError: fail,
      },
    );
  };

  const confirmTask = () => {
    setErrorText('');
    confirm.mutate(
      { taskId: current.id, evidenceIds: evidence.map((item) => item.id), expectedRevision: current.revision },
      { onError: fail },
    );
  };

  const offerTransfer = () => {
    if (!transferTarget) return;
    setErrorText('');
    offer.mutate(
      { taskId: current.id, toBotId: transferTarget, note: transferNote.trim() || undefined },
      {
        onSuccess: () => {
          setTransferTarget('');
          setTransferNote('');
        },
        onError: fail,
      },
    );
  };

  const transitionStatuses = nextStatuses(current.status).filter(
    (status) => status !== 'review' || canRequestReview(current, hasEvidence),
  );

  return (
    <div className="space-y-3 border-t border-neutral-200 px-3 py-3 text-sm dark:border-neutral-800">
      {current.description && <p className="whitespace-pre-wrap text-neutral-700 dark:text-neutral-300">{current.description}</p>}
      {current.acceptanceCriteria && (
        <p className="text-xs text-neutral-500 dark:text-neutral-400">
          {t('tasks.acceptance')}: {current.acceptanceCriteria}
        </p>
      )}
      {current.reviewerId && (
        <p className="text-xs text-neutral-500 dark:text-neutral-400">
          {t('tasks.reviewer', { name: ownerLabel(current.reviewerId, bots, userLabel), count: current.rejectCount })}
        </p>
      )}
      {current.blockedReason && (
        <Alert tone="warning">
          {t('tasks.blocked')}: {current.blockedReason}
        </Alert>
      )}
      {(current.dependencyIds ?? []).length > 0 && (
        <p className="flex flex-wrap items-center gap-1 text-xs text-neutral-500 dark:text-neutral-400">
          {t('tasks.dependenciesTitle')}
          {dependencyLabels(current, tasks).map((label) => (
            <Tag key={label}>{label}</Tag>
          ))}
        </p>
      )}

      {/* Evidence: the record that gates review and confirm. */}
      <section aria-label={t('tasks.evidenceTitle')} className="space-y-1.5">
        <p className="text-xs font-semibold text-neutral-700 dark:text-neutral-200">{t('tasks.evidenceTitle')}</p>
        {evidence.length === 0 && <p className="text-xs text-neutral-500 dark:text-neutral-400">{t('tasks.evidenceEmpty')}</p>}
        <ul className="space-y-1">
          {evidence.map((item) => (
            <li key={item.id} className="rounded-lg bg-neutral-900/[0.03] px-2.5 py-1.5 text-xs dark:bg-white/[0.04]">
              <p className="whitespace-pre-wrap text-neutral-700 dark:text-neutral-300">{item.summary}</p>
              {item.testResult && <p className="text-neutral-500 dark:text-neutral-400">{t('tasks.testResult')}: {item.testResult}</p>}
              {(item.artifactRefs ?? []).length > 0 && (
                <p className="break-all font-mono text-neutral-500 dark:text-neutral-400">{(item.artifactRefs ?? []).join(', ')}</p>
              )}
            </li>
          ))}
        </ul>
        {canSubmitEvidence(current.status) && (
          <form
            className="space-y-1.5"
            onSubmit={(event) => {
              event.preventDefault();
              submitNewEvidence();
            }}
          >
            <TextInput size="sm" value={evidenceSummary} onChange={(event) => setEvidenceSummary(event.target.value)} placeholder={t('tasks.evidenceSummaryPlaceholder')} aria-label={t('tasks.evidenceSummaryPlaceholder')} />
            <TextInput size="sm" value={testResult} onChange={(event) => setTestResult(event.target.value)} placeholder={t('tasks.testResultPlaceholder')} aria-label={t('tasks.testResultPlaceholder')} />
            <TextInput size="sm" mono value={artifactRefs} onChange={(event) => setArtifactRefs(event.target.value)} placeholder={t('tasks.artifactRefsPlaceholder')} aria-label={t('tasks.artifactRefsPlaceholder')} />
            <Button type="submit" size="sm" block disabled={!evidenceSummary.trim()} loading={submitEvidence.isPending}>
              {t('tasks.evidenceSubmit')}
            </Button>
          </form>
        )}
      </section>

      {/* Transfers: offered, then accepted; the owner only changes on accept. */}
      <section aria-label={t('tasks.transfersTitle')} className="space-y-1.5">
        <p className="text-xs font-semibold text-neutral-700 dark:text-neutral-200">{t('tasks.transfersTitle')}</p>
        {transfers.length === 0 && <p className="text-xs text-neutral-500 dark:text-neutral-400">{t('tasks.transfersEmpty')}</p>}
        <ul className="space-y-1">
          {transfers.map((item) => (
            <li key={item.id} className="rounded-lg bg-neutral-900/[0.03] px-2.5 py-1.5 text-xs dark:bg-white/[0.04]">
              <span className="text-neutral-500 dark:text-neutral-400">
                {t(`tasks.transfer.${item.status}`, { defaultValue: item.status })} ·{' '}
              </span>
              {ownerLabel(item.fromBotId, bots, userLabel)} → {ownerLabel(item.toBotId, bots, userLabel)}
              {item.note && <p className="whitespace-pre-wrap text-neutral-600 dark:text-neutral-300">{item.note}</p>}
              {item.status === 'offered' && (
                <span className="mt-1.5 flex gap-1.5">
                  <Button
                    size="sm"
                    variant="primary"
                    loading={accept.isPending}
                    onClick={() => {
                      setErrorText('');
                      accept.mutate({ transferId: item.id, expectedRevision: current.revision }, { onError: fail });
                    }}
                  >
                    {t('tasks.transferAccept')}
                  </Button>
                  <Button
                    size="sm"
                    loading={cancelTransfer.isPending}
                    onClick={() => {
                      setErrorText('');
                      cancelTransfer.mutate(item.id, { onError: fail });
                    }}
                  >
                    {t('tasks.transferCancel')}
                  </Button>
                </span>
              )}
            </li>
          ))}
        </ul>
        {canOfferTransfer(current) && !liveOffer && (
          <form
            className="space-y-1.5"
            onSubmit={(event) => {
              event.preventDefault();
              offerTransfer();
            }}
          >
            <Select
              size="sm"
              label={t('tasks.transferTargetLabel')}
              value={transferTarget}
              placeholder={t('tasks.transferTargetPlaceholder')}
              onChange={setTransferTarget}
              options={bots.filter((bot) => bot.id !== current.ownerId).map((bot) => ({ value: bot.id, label: bot.name }))}
            />
            <TextInput size="sm" value={transferNote} onChange={(event) => setTransferNote(event.target.value)} placeholder={t('tasks.transferNotePlaceholder')} aria-label={t('tasks.transferNotePlaceholder')} />
            <Button type="submit" size="sm" block disabled={!transferTarget} loading={offer.isPending}>
              {t('tasks.transferOffer')}
            </Button>
          </form>
        )}
      </section>

      {errorText && <Alert tone="danger">{errorText}</Alert>}

      {/* Status actions: the server rejects anything the rules do not allow;
          these buttons only reflect the same rules to keep the UI honest. */}
      {(transitionStatuses.length > 0 || canConfirm(current, hasEvidence)) && (
        <div className="flex flex-wrap justify-end gap-1.5">
          {transitionStatuses.map((status) => (
            <Button key={status} size="sm" variant={status === 'cancelled' ? 'ghost' : 'secondary'} onClick={() => patchStatus(status)} disabled={patch.isPending}>
              {t(`tasks.action.${status}`)}
            </Button>
          ))}
          {canConfirm(current, hasEvidence) && (
            <Button size="sm" variant="primary" icon={Icons.done} onClick={confirmTask} loading={confirm.isPending}>
              {t('tasks.action.done')}
            </Button>
          )}
        </div>
      )}
      {current.status === 'review' && !hasEvidence && <p className="text-xs text-neutral-500 dark:text-neutral-400">{t('tasks.reviewNeedsEvidence')}</p>}
    </div>
  );
}