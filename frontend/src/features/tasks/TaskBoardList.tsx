import LoadMore from '../../components/LoadMore';
// TaskBoardList is the "任务" page of the right panel: the team's
// requirements, each with the route the user confirmed, where the work is on
// it, the project manager's requirement document and the cards handed out so
// far. Cards move only on the route; the one thing the user does here is
// cancel a requirement.
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useCancelRequirement, useRequirements, useTasks } from '../../api/queries';
import type { Bot, Requirement, Task } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import Markdown from '../conversations/Markdown';
import { Alert, AlertDialog, Badge, Button, cx, EmptyState, Icons, interactive, Skeleton } from '../../components/ui';
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
    return (
      <div className="space-y-2 px-3 py-3" role="status" aria-label={t('status.loading')}>
        <Skeleton className="h-12 w-full" />
        <Skeleton className="h-12 w-full" />
      </div>
    );
  }
  if ((requirements.isError && !requirements.data) || (tasks.isError && !tasks.data)) {
    return (
      <div className="px-3 py-3">
        <Alert tone="danger">{t('tasks.loadFailed')}</Alert>
      </div>
    );
  }

  // A member's view shows the requirements it holds cards of.
  const all = requirementOrder(requirements.data?.items ?? []);
  const items = viewBotId ? all.filter((req) => cards.some((item) => item.requirementId === req.id)) : all;

  return (
    <div>
      {items.length === 0 && <EmptyState icon={<Icons.tasks size={20} aria-hidden />} title={t('tasks.empty')} />}
      <ul className="space-y-0.5 px-2 py-1">
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
    <li id={`requirement-${requirement.id}`} className={cx('overflow-hidden rounded-xl transition-colors', expanded && 'bg-card shadow-sm ring-1 ring-neutral-900/[0.06] dark:ring-white/[0.08]')}>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        className={cx('flex w-full items-start gap-2 px-2.5 py-2 text-left text-sm transition-colors', interactive.hover)}
      >
        <span className="mt-0.5 shrink-0 text-neutral-400">{expanded ? <Icons.expand size={14} aria-hidden /> : <Icons.next size={14} aria-hidden />}</span>
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-1.5">
            <Badge tone={requirementTone(requirement.status)}>{t(`tasks.requirementStatus.${requirement.status}`)}</Badge>
            <span className="min-w-0 truncate font-medium text-neutral-900 dark:text-neutral-50">{requirement.title}</span>
          </span>
          {stage && (
            <span className="mt-0.5 block truncate text-xs text-neutral-500 dark:text-neutral-400">
              {t('tasks.at', { index: at + 1, total: requirement.stages.length, name: stage.name, owner: owner(stage.ownerId, stage.kind) })}
            </span>
          )}
        </span>
      </button>
      {expanded && (
        <div className="space-y-3 border-t border-neutral-900/[0.06] px-3 py-3 text-sm dark:border-white/[0.08]">
          <section aria-label={t('tasks.route')}>
            <p className="text-xs font-semibold text-neutral-700 dark:text-neutral-200">{t('tasks.route')}</p>
            <ol className="mt-1.5 space-y-1">
              {requirement.stages.map((item, index) => {
                const done = index < at || requirement.status === 'done';
                const current = index === at;
                return (
                  <li
                    key={index}
                    aria-current={current ? 'step' : undefined}
                    className={cx('flex items-center gap-2 text-xs', current ? 'font-medium text-neutral-900 dark:text-neutral-50' : 'text-neutral-500 dark:text-neutral-400')}
                  >
                    <span
                      className={cx(
                        'flex h-4 w-4 shrink-0 items-center justify-center rounded-full text-2xs tabular-nums',
                        done && 'bg-neutral-900/[0.08] dark:bg-white/[0.1]',
                        current && 'bg-signal-500 text-on-signal',
                        !done && !current && 'ring-1 ring-neutral-900/15 dark:ring-white/20',
                      )}
                    >
                      {done ? <Icons.done size={10} aria-hidden /> : index + 1}
                    </span>
                    <span className="min-w-0 truncate">
                      {item.name} · {t(`tasks.stageKind.${item.kind}`)}
                      {item.kind === 'review' && item.reviewType ? `（${t(`tasks.reviewType.${item.reviewType}`)}）` : ''} · {owner(item.ownerId, item.kind)}
                    </span>
                  </li>
                );
              })}
            </ol>
          </section>
          <section aria-label={t('tasks.document')}>
            <button
              type="button"
              onClick={() => setShowDocument((open) => !open)}
              aria-expanded={showDocument}
              className={cx('-mx-1 flex items-center gap-1 rounded-md px-1 py-0.5 text-xs font-semibold text-neutral-700 transition-colors dark:text-neutral-200', interactive.hover)}
            >
              {showDocument ? <Icons.expand size={12} aria-hidden /> : <Icons.next size={12} aria-hidden />}
              {t('tasks.document')}
            </button>
            {showDocument && (
              <div className="mt-1.5 max-h-96 overflow-y-auto rounded-lg bg-sunk px-3 py-2 text-xs">
                <Markdown text={requirement.document} teamId={teamId} />
              </div>
            )}
          </section>
          <section aria-label={t('tasks.cards')}>
            <p className="text-xs font-semibold text-neutral-700 dark:text-neutral-200">{t('tasks.cards')}</p>
            {cards.length === 0 && <p className="mt-1 text-xs text-neutral-500 dark:text-neutral-400">{t('tasks.cardsEmpty')}</p>}
            <ul className="mt-1.5 space-y-1">
              {cards.map((item) => (
                <li key={item.id} id={`task-${item.id}`} className={cx('overflow-hidden rounded-lg', cardId === item.id && 'bg-sunk')}>
                  <button
                    type="button"
                    onClick={() => onCard(item.id)}
                    aria-expanded={cardId === item.id}
                    className={cx('flex w-full items-center gap-1.5 px-2 py-1.5 text-left text-xs transition-colors', interactive.hover)}
                  >
                    <Badge tone={statusTone(item.status)}>{t(`tasks.status.${item.status}`)}</Badge>
                    <span className="min-w-0 flex-1 truncate">{item.title}</span>
                    <span className="shrink-0 text-neutral-500 dark:text-neutral-400">{ownerLabel(item.ownerId, bots, userLabel)}</span>
                  </button>
                  {cardId === item.id && <TaskDetail task={item} bots={bots} />}
                </li>
              ))}
            </ul>
          </section>
          {canCancel(requirement) && (
            <div className="flex justify-end">
              <Button size="sm" variant="ghost" onClick={() => setConfirming(true)}>
                {t('tasks.cancelRequirement')}
              </Button>
            </div>
          )}
          {errorText && <Alert tone="danger">{errorText}</Alert>}
          <AlertDialog
            open={confirming}
            title={t('tasks.cancelRequirement')}
            description={t('tasks.cancelConfirmHint')}
            confirmLabel={t('tasks.cancelConfirm')}
            cancelLabel={t('tasks.cancelKeep')}
            busy={cancel.isPending}
            onCancel={() => setConfirming(false)}
            onConfirm={() => {
              setErrorText('');
              cancel.mutate(requirement.id, {
                onSuccess: () => setConfirming(false),
                onError: (error) => {
                  setConfirming(false);
                  setErrorText(apiErrorMessage(t, error));
                },
              });
            }}
          />
        </div>
      )}
    </li>
  );
}