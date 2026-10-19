import LoadMore from '../../components/LoadMore';
// TaskBoardList is the "任务" page of the right panel: the team's board with
// honest status flow, dependency chips, evidence, and two-phase transfers.
// The server authorizes every action; this component only decides which
// controls to show for the current state.
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Badge, Button, cx, EmptyState, Field, Icons, interactive, Select, Skeleton, TextInput, Textarea } from '../../components/ui';
import { useCreateTask, useTasks } from '../../api/queries';
import type { Bot, Task } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import TaskDetail from './TaskDetail';
import { boardOrder, ownerLabel, statusTone } from './taskFlow';

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
  /** A task a conversation card or activity entry asked to show. */
  focusTaskId?: string;
  focusNonce?: number;
}) {
  const { t } = useTranslation();
  const tasks = useTasks(teamId, true, viewBotId);
  const create = useCreateTask(teamId);
  const [createOpen, setCreateOpen] = useState(false);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  useEffect(() => {
    if (!focusTaskId) return;
    setExpandedId(focusTaskId);
    requestAnimationFrame(() => document.getElementById(`task-${focusTaskId}`)?.scrollIntoView({ block: 'nearest' }));
  }, [focusTaskId, focusNonce]);
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [acceptanceCriteria, setAcceptanceCriteria] = useState('');
  const [ownerId, setOwnerId] = useState('');
  const [errorText, setErrorText] = useState('');

  const items = boardOrder(tasks.data?.items ?? []);
  const userLabel = t('tasks.ownerUser');

  const submitCreate = () => {
    const trimmed = title.trim();
    if (!trimmed || !ownerId) return;
    setErrorText('');
    create.mutate(
      { title: trimmed, ownerId, description, acceptanceCriteria },
      {
        onSuccess: (created) => {
          setTitle('');
          setDescription('');
          setAcceptanceCriteria('');
          setCreateOpen(false);
          setExpandedId(created.id);
        },
        onError: (error) => setErrorText(apiErrorMessage(t, error)),
      },
    );
  };

  if (tasks.isPending) {
    return (
      <div className="space-y-2 px-3 py-3" role="status" aria-label={t('status.loading')}>
        <Skeleton className="h-9 w-full" />
        <Skeleton className="h-12 w-full" />
        <Skeleton className="h-12 w-full" />
      </div>
    );
  }
  if (tasks.isError && !tasks.data) {
    return (
      <div className="px-3 py-3">
        <Alert tone="danger">{t('tasks.loadFailed')}</Alert>
      </div>
    );
  }

  return (
    <div>
      <div className="px-3 py-3">
        <Button block icon={Icons.add} aria-expanded={createOpen} onClick={() => setCreateOpen((open) => !open)}>
          {t('tasks.new')}
        </Button>
      </div>

      {createOpen && (
        <form
          className="mx-3 mb-3 space-y-3 rounded-xl border border-neutral-200 p-3 dark:border-neutral-800"
          onSubmit={(event) => {
            event.preventDefault();
            submitCreate();
          }}
        >
          <Field label={t('tasks.titleLabel')}>
            <TextInput size="sm" value={title} onChange={(event) => setTitle(event.target.value)} />
          </Field>
          <Field label={t('tasks.description')}>
            <Textarea value={description} onChange={(event) => setDescription(event.target.value)} rows={3} />
          </Field>
          <Field label={t('tasks.acceptance')}>
            <Textarea value={acceptanceCriteria} onChange={(event) => setAcceptanceCriteria(event.target.value)} rows={2} />
          </Field>
          <Field label={t('tasks.ownerLabel')}>
            <Select
              size="sm"
              value={ownerId}
              placeholder={t('tasks.ownerPlaceholder')}
              onChange={setOwnerId}
              options={bots.map((bot) => ({ value: bot.id, label: bot.name }))}
            />
          </Field>
          {errorText && <Alert tone="danger">{errorText}</Alert>}
          <Button type="submit" variant="primary" block disabled={!title.trim() || !ownerId} loading={create.isPending}>
            {t('tasks.create')}
          </Button>
        </form>
      )}

      {items.length === 0 && <EmptyState icon={<Icons.tasks size={20} aria-hidden />} title={t('tasks.empty')} />}

      <ul className="space-y-1.5 px-3 pb-3">
        {items.map((task) => (
          <TaskRow
            key={task.id}
            task={task}
            tasks={items}
            bots={bots}
            userLabel={userLabel}
            expanded={expandedId === task.id}
            onToggle={() => setExpandedId((current) => (current === task.id ? null : task.id))}
          />
        ))}
      </ul>
      <LoadMore page={tasks} />
    </div>
  );
}

function TaskRow({
  task,
  tasks,
  bots,
  userLabel,
  expanded,
  onToggle,
}: {
  task: Task;
  tasks: Task[];
  bots: Bot[];
  userLabel: string;
  expanded: boolean;
  onToggle: () => void;
}) {
  const { t } = useTranslation();
  const Chevron = expanded ? Icons.expand : Icons.next;
  return (
    <li id={`task-${task.id}`} className="overflow-hidden rounded-xl border border-neutral-200 bg-white dark:border-neutral-800 dark:bg-neutral-900">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        className={cx('flex w-full items-start gap-2 px-2.5 py-2 text-left text-sm transition-colors', interactive.hover)}
      >
        <span className="mt-0.5 shrink-0 text-neutral-400">
          <Chevron size={14} aria-hidden />
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-1.5">
            <Badge tone={statusTone(task.status)}>{t(`tasks.status.${task.status}`)}</Badge>
            <span className="min-w-0 truncate font-medium text-neutral-900 dark:text-neutral-50">{task.title}</span>
          </span>
          <span className="mt-0.5 block truncate text-xs text-neutral-500 dark:text-neutral-400">
            {ownerLabel(task.ownerId, bots, userLabel)}
            {(task.dependencyIds ?? []).length > 0 &&
              ` · ${t('tasks.dependencies', { count: (task.dependencyIds ?? []).length })}`}
          </span>
        </span>
      </button>
      {expanded && <TaskDetail task={task} tasks={tasks} bots={bots} />}
    </li>
  );
}