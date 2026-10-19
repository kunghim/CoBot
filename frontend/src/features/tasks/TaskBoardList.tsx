import LoadMore from '../../components/LoadMore';
// TaskBoardList is the "任务" page of the right panel: the team's
// requirements, each with the route the user confirmed, where the work is on
// it, the project manager's requirement document and the cards handed out so
// far. Cards move only on the route; the one thing the user does here is
// cancel a requirement.
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Check, ChevronDown, ChevronRight } from 'lucide-react';
import { useCancelRequirement, useRequirements, useTasks } from '../../api/queries';
import type { Bot, Requirement, Task } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import Markdown from '../conversations/Markdown';
import TaskDetail from './TaskDetail';
import { canCancel, cardsOf, currentStage, ownerLabel, requirementOrder, requirementTone, statusTone } from './taskFlow';

export default function TaskBoardList({
  teamId,
  viewBotId,
  bots,
  focusTaskId,
  focusNonce,
}: {
  teamId: string;
  viewBotId?: string;
  bots: Bot[];
  /** A card a conversation card or activity entry asked to show. */
  focusTaskId?: string;
  focusNonce?: number;
}) {
  const { t } = useTranslation();
  const requirements = useRequirements(teamId);
  const tasks = useTasks(teamId, true, viewBotId);
  const cards = tasks.data?.items ?? [];
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [cardId, setCardId] = useState<string | null>(null);
  useEffect(() => {
    if (!focusTaskId) return;
    const focused = cards.find((item) => item.id === focusTaskId);
    if (focused?.requirementId) setExpandedId(focused.requirementId);
    setCardId(focusTaskId);
    requestAnimationFrame(() => document.getElementById(`task-${focusTaskId}`)?.scrollIntoView({ block: 'nearest' }));
    // Cards load after the focus request; follow them in.
  }, [focusTaskId, focusNonce, cards.length]); // eslint-disable-line react-hooks/exhaustive-deps

  if (requirements.isPending || tasks.isPending) {
    return <p className="px-3 py-2 text-sm text-neutral-500" role="status">{t('status.loading')}</p>;
  }
  if ((requirements.isError && !requirements.data) || (tasks.isError && !tasks.data)) {
    return <p className="px-3 py-2 text-sm text-red-600" role="alert">{t('tasks.loadFailed')}</p>;
  }

  // A member's view shows the requirements it holds cards of.
  const all = requirementOrder(requirements.data?.items ?? []);
  const items = viewBotId ? all.filter((req) => cards.some((item) => item.requirementId === req.id)) : all;

  return (
    <div>
      {items.length === 0 && <p className="px-3 py-2 text-sm text-neutral-500 dark:text-neutral-400">{t('tasks.empty')}</p>}
      <ul className="space-y-1 px-2 py-2">
        {items.map((req) => (
          <RequirementRow
            key={req.id}
            requirement={req}
            cards={cardsOf(req.id, cards)}
            bots={bots}
            teamId={teamId}
            expanded={expandedId === req.id}
            onToggle={() => setExpandedId((current) => (current === req.id ? null : req.id))}
            cardId={cardId}
            onCard={(id) => setCardId((current) => (current === id ? null : id))}
          />
        ))}
      </ul>
      <LoadMore page={tasks} />
    </div>
  );
}

function RequirementRow({
  requirement,
  cards,
  bots,
  teamId,
  expanded,
  onToggle,
  cardId,
  onCard,
}: {
  requirement: Requirement;
  cards: Task[];
  bots: Bot[];
  teamId: string;
  expanded: boolean;
  onToggle: () => void;
  cardId: string | null;
  onCard: (id: string) => void;
}) {
  const { t } = useTranslation();
  const cancel = useCancelRequirement();
  const [confirming, setConfirming] = useState(false);
  const [showDocument, setShowDocument] = useState(false);
  const [errorText, setErrorText] = useState('');
  const at = currentStage(requirement, cards);
  const stage = at >= 0 ? requirement.stages[at] : undefined;
  const userLabel = t('tasks.ownerUser');
  const owner = (ownerId?: string, kind?: string) => (kind === 'confirm' ? userLabel : ownerLabel(ownerId ?? '', bots, userLabel));

  return (
    <li id={`requirement-${requirement.id}`} className="rounded-md border border-neutral-200 dark:border-neutral-700">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        className="flex w-full items-start gap-2 px-2 py-1.5 text-left text-sm transition-colors hover:bg-neutral-100 dark:hover:bg-neutral-800/60"
      >
        <span className="mt-0.5 shrink-0 text-neutral-400">
          {expanded ? <ChevronDown size={14} aria-hidden="true" /> : <ChevronRight size={14} aria-hidden="true" />}
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-1.5">
            <span className={`shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-medium ${requirementTone(requirement.status)}`}>
              {t(`tasks.requirementStatus.${requirement.status}`)}
            </span>
            <span className="min-w-0 truncate">{requirement.title}</span>
          </span>
          {stage && (
            <span className="mt-0.5 block truncate text-xs text-neutral-500 dark:text-neutral-400">
              {t('tasks.at', { index: at + 1, total: requirement.stages.length, name: stage.name, owner: owner(stage.ownerId, stage.kind) })}
            </span>
          )}
        </span>
      </button>
      {expanded && (
        <div className="space-y-2 border-t border-neutral-200 px-3 py-2 text-sm dark:border-neutral-700">
          <section aria-label={t('tasks.route')}>
            <p className="text-xs font-medium text-neutral-500 dark:text-neutral-400">{t('tasks.route')}</p>
            <ol className="mt-1 space-y-0.5">
              {requirement.stages.map((item, index) => (
                <li
                  key={index}
                  aria-current={index === at ? 'step' : undefined}
                  className={`flex items-center gap-1.5 text-xs ${index === at ? 'font-medium text-neutral-900 dark:text-neutral-100' : 'text-neutral-500 dark:text-neutral-400'}`}
                >
                  <span className="w-4 shrink-0 text-center">
                    {index < at || requirement.status === 'done' ? <Check size={12} aria-hidden="true" /> : index === at ? '▶' : index + 1}
                  </span>
                  <span className="min-w-0 truncate">
                    {item.name} · {t(`tasks.stageKind.${item.kind}`)}
                    {item.kind === 'review' && item.reviewType ? `（${t(`tasks.reviewType.${item.reviewType}`)}）` : ''} · {owner(item.ownerId, item.kind)}
                  </span>
                </li>
              ))}
            </ol>
          </section>
          <section aria-label={t('tasks.document')}>
            <button
              type="button"
              onClick={() => setShowDocument((open) => !open)}
              aria-expanded={showDocument}
              className="flex items-center gap-1 text-xs font-medium text-neutral-500 hover:text-neutral-800 dark:text-neutral-400 dark:hover:text-neutral-200"
            >
              {showDocument ? <ChevronDown size={12} aria-hidden="true" /> : <ChevronRight size={12} aria-hidden="true" />}
              {t('tasks.document')}
            </button>
            {showDocument && (
              <div className="mt-1 rounded bg-neutral-50 px-2 py-1 text-xs dark:bg-neutral-800/60">
                <Markdown text={requirement.document} teamId={teamId} />
              </div>
            )}
          </section>
          <section aria-label={t('tasks.cards')}>
            <p className="text-xs font-medium text-neutral-500 dark:text-neutral-400">{t('tasks.cards')}</p>
            {cards.length === 0 && <p className="text-xs text-neutral-400">{t('tasks.cardsEmpty')}</p>}
            <ul className="mt-1 space-y-1">
              {cards.map((item) => (
                <li key={item.id} id={`task-${item.id}`} className="rounded border border-neutral-200 dark:border-neutral-700">
                  <button
                    type="button"
                    onClick={() => onCard(item.id)}
                    aria-expanded={cardId === item.id}
                    className="flex w-full items-center gap-1.5 px-2 py-1 text-left text-xs hover:bg-neutral-100 dark:hover:bg-neutral-800/60"
                  >
                    <span className={`shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-medium ${statusTone(item.status)}`}>
                      {t(`tasks.status.${item.status}`)}
                    </span>
                    <span className="min-w-0 flex-1 truncate">{item.title}</span>
                    <span className="shrink-0 text-neutral-400">{ownerLabel(item.ownerId, bots, userLabel)}</span>
                  </button>
                  {cardId === item.id && <TaskDetail task={item} bots={bots} />}
                </li>
              ))}
            </ul>
          </section>
          {canCancel(requirement) && (
            <div className="flex flex-wrap items-center gap-1">
              {!confirming ? (
                <button
                  type="button"
                  onClick={() => setConfirming(true)}
                  className="rounded-md border border-neutral-300 px-2 py-1 text-xs text-neutral-600 hover:bg-neutral-100 dark:border-neutral-600 dark:text-neutral-300 dark:hover:bg-neutral-800"
                >
                  {t('tasks.cancelRequirement')}
                </button>
              ) : (
                <>
                  <span className="text-xs text-neutral-500 dark:text-neutral-400">{t('tasks.cancelConfirmHint')}</span>
                  <button
                    type="button"
                    disabled={cancel.isPending}
                    onClick={() => {
                      setErrorText('');
                      cancel.mutate(requirement.id, {
                        onSuccess: () => setConfirming(false),
                        onError: (error) => setErrorText(apiErrorMessage(t, error)),
                      });
                    }}
                    className={`rounded-md px-2 py-1 text-xs font-medium ${statusTone('cancelled')} hover:opacity-80 disabled:opacity-50`}
                  >
                    {t('tasks.cancelConfirm')}
                  </button>
                  <button type="button" onClick={() => setConfirming(false)} className="rounded-md px-2 py-1 text-xs text-neutral-500 hover:underline">
                    {t('tasks.cancelKeep')}
                  </button>
                </>
              )}
            </div>
          )}
          {errorText && (
            <p className="text-xs text-red-600" role="alert">
              {errorText}
            </p>
          )}
        </div>
      )}
    </li>
  );
}
