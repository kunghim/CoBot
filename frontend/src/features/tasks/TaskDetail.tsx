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
import { canConfirm, canOfferTransfer, canRequestReview, canSubmitEvidence, dependencyLabels, nextStatuses, ownerLabel, statusTone } from './taskFlow';

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
    <div className="space-y-2 border-t border-neutral-200 px-3 py-2 text-sm dark:border-neutral-700">
      {current.description && <p className="whitespace-pre-wrap text-neutral-700 dark:text-neutral-300">{current.description}</p>}
      {current.acceptanceCriteria && (
        <p className="text-xs text-neutral-500 dark:text-neutral-400">
          {t('tasks.acceptance')}: {current.acceptanceCriteria}
        </p>
      )}
      {current.blockedReason && (
        <p className="text-xs text-amber-700 dark:text-amber-400">
          {t('tasks.blocked')}: {current.blockedReason}
        </p>
      )}
      {(current.dependencyIds ?? []).length > 0 && (
        <p className="flex flex-wrap gap-1 text-xs text-neutral-500 dark:text-neutral-400">
          {t('tasks.dependenciesTitle')}
          {dependencyLabels(current, tasks).map((label) => (
            <span key={label} className="rounded-full bg-neutral-100 px-1.5 py-0.5 dark:bg-neutral-800">
              {label}
            </span>
          ))}
        </p>
      )}

      {/* Evidence: the record that gates review and confirm. */}
      <section aria-label={t('tasks.evidenceTitle')}>
        <p className="text-xs font-medium text-neutral-500 dark:text-neutral-400">{t('tasks.evidenceTitle')}</p>
        {evidence.length === 0 && <p className="text-xs text-neutral-400">{t('tasks.evidenceEmpty')}</p>}
        <ul className="mt-1 space-y-1">
          {evidence.map((item) => (
            <li key={item.id} className="rounded bg-neutral-50 px-2 py-1 text-xs dark:bg-neutral-800/60">
              <p className="whitespace-pre-wrap text-neutral-700 dark:text-neutral-300">{item.summary}</p>
              {item.testResult && <p className="text-neutral-500 dark:text-neutral-400">{t('tasks.testResult')}: {item.testResult}</p>}
              {(item.artifactRefs ?? []).length > 0 && (
                <p className="break-all text-neutral-400">{(item.artifactRefs ?? []).join(', ')}</p>
              )}
            </li>
          ))}
        </ul>
        {canSubmitEvidence(current.status) && (
          <form
            className="mt-1 space-y-1"
            onSubmit={(event) => {
              event.preventDefault();
              submitNewEvidence();
            }}
          >
            <input
              value={evidenceSummary}
              onChange={(event) => setEvidenceSummary(event.target.value)}
              placeholder={t('tasks.evidenceSummaryPlaceholder')}
              aria-label={t('tasks.evidenceSummaryPlaceholder')}
              className="w-full rounded-md border border-neutral-300 bg-white px-2 py-1 text-xs focus:border-neutral-500 focus:outline-none dark:border-neutral-600 dark:bg-neutral-800 dark:text-neutral-100"
            />
            <input
              value={testResult}
              onChange={(event) => setTestResult(event.target.value)}
              placeholder={t('tasks.testResultPlaceholder')}
              aria-label={t('tasks.testResultPlaceholder')}
              className="w-full rounded-md border border-neutral-300 bg-white px-2 py-1 text-xs focus:border-neutral-500 focus:outline-none dark:border-neutral-600 dark:bg-neutral-800 dark:text-neutral-100"
            />
            <input
              value={artifactRefs}
              onChange={(event) => setArtifactRefs(event.target.value)}
              placeholder={t('tasks.artifactRefsPlaceholder')}
              aria-label={t('tasks.artifactRefsPlaceholder')}
              className="w-full rounded-md border border-neutral-300 bg-white px-2 py-1 text-xs focus:border-neutral-500 focus:outline-none dark:border-neutral-600 dark:bg-neutral-800 dark:text-neutral-100"
            />
            <button
              type="submit"
              disabled={!evidenceSummary.trim() || submitEvidence.isPending}
              className="w-full rounded-md border border-neutral-300 px-2 py-1 text-xs hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-600 dark:hover:bg-neutral-800"
            >
              {t('tasks.evidenceSubmit')}
            </button>
          </form>
        )}
      </section>

      {/* Transfers: offered, then accepted; the owner only changes on accept. */}
      <section aria-label={t('tasks.transfersTitle')}>
        <p className="text-xs font-medium text-neutral-500 dark:text-neutral-400">{t('tasks.transfersTitle')}</p>
        {transfers.length === 0 && <p className="text-xs text-neutral-400">{t('tasks.transfersEmpty')}</p>}
        <ul className="mt-1 space-y-1">
          {transfers.map((item) => (
            <li key={item.id} className="rounded bg-neutral-50 px-2 py-1 text-xs dark:bg-neutral-800/60">
              <span className="text-neutral-500 dark:text-neutral-400">
                {t(`tasks.transfer.${item.status}`, { defaultValue: item.status })} ·{' '}
              </span>
              {ownerLabel(item.fromBotId, bots, userLabel)} → {ownerLabel(item.toBotId, bots, userLabel)}
              {item.note && <p className="whitespace-pre-wrap text-neutral-600 dark:text-neutral-300">{item.note}</p>}
              {item.status === 'offered' && (
                <span className="mt-1 flex gap-1">
                  <button
                    type="button"
                    onClick={() => {
                      setErrorText('');
                      accept.mutate(item.id, { onError: fail });
                    }}
                    className="rounded border border-neutral-300 px-2 py-0.5 hover:bg-neutral-100 dark:border-neutral-600 dark:hover:bg-neutral-800"
                  >
                    {t('tasks.transferAccept')}
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setErrorText('');
                      cancelTransfer.mutate(item.id, { onError: fail });
                    }}
                    className="rounded border border-neutral-300 px-2 py-0.5 hover:bg-neutral-100 dark:border-neutral-600 dark:hover:bg-neutral-800"
                  >
                    {t('tasks.transferCancel')}
                  </button>
                </span>
              )}
            </li>
          ))}
        </ul>
        {canOfferTransfer(current) && !liveOffer && (
          <form
            className="mt-1 space-y-1"
            onSubmit={(event) => {
              event.preventDefault();
              offerTransfer();
            }}
          >
            <select
              value={transferTarget}
              onChange={(event) => setTransferTarget(event.target.value)}
              aria-label={t('tasks.transferTargetLabel')}
              className="w-full rounded-md border border-neutral-300 bg-white px-2 py-1 text-xs focus:border-neutral-500 focus:outline-none dark:border-neutral-600 dark:bg-neutral-800 dark:text-neutral-100"
            >
              <option value="">{t('tasks.transferTargetPlaceholder')}</option>
              {bots
                .filter((bot) => bot.id !== current.ownerId)
                .map((bot) => (
                  <option key={bot.id} value={bot.id}>
                    {bot.name}
                  </option>
                ))}
            </select>
            <input
              value={transferNote}
              onChange={(event) => setTransferNote(event.target.value)}
              placeholder={t('tasks.transferNotePlaceholder')}
              aria-label={t('tasks.transferNotePlaceholder')}
              className="w-full rounded-md border border-neutral-300 bg-white px-2 py-1 text-xs focus:border-neutral-500 focus:outline-none dark:border-neutral-600 dark:bg-neutral-800 dark:text-neutral-100"
            />
            <button
              type="submit"
              disabled={!transferTarget || offer.isPending}
              className="w-full rounded-md border border-neutral-300 px-2 py-1 text-xs hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-600 dark:hover:bg-neutral-800"
            >
              {t('tasks.transferOffer')}
            </button>
          </form>
        )}
      </section>

      {errorText && (
        <p className="text-xs text-red-600" role="alert">
          {errorText}
        </p>
      )}

      {/* Status actions: the server rejects anything the rules do not allow;
          these buttons only reflect the same rules to keep the UI honest. */}
      {(transitionStatuses.length > 0 || canConfirm(current, hasEvidence)) && (
        <div className="flex flex-wrap gap-1">
          {transitionStatuses.map((status) => (
            <button
              key={status}
              type="button"
              onClick={() => patchStatus(status)}
              className={`rounded-md px-2 py-1 text-xs font-medium ${statusTone(status)} hover:opacity-80`}
            >
              {t(`tasks.action.${status}`)}
            </button>
          ))}
          {canConfirm(current, hasEvidence) && (
            <button
              type="button"
              onClick={confirmTask}
              className={`rounded-md px-2 py-1 text-xs font-medium ${statusTone('done')} hover:opacity-80`}
            >
              {t('tasks.action.done')}
            </button>
          )}
        </div>
      )}
      {current.status === 'review' && !hasEvidence && (
        <p className="text-xs text-neutral-400">{t('tasks.reviewNeedsEvidence')}</p>
      )}
    </div>
  );
}
