import LoadMore from '../../components/LoadMore';
// TaskBoardList is the "任务" page of the right panel: the team's board with
// honest status flow, dependency chips, evidence, and two-phase transfers.
// The server authorizes every action; this component only decides which
// controls to show for the current state.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ChevronDown, ChevronRight, Plus } from 'lucide-react';
import { useCreateTask, useTasks } from '../../api/queries';
import type { Bot, Task } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import TaskDetail from './TaskDetail';
import { boardOrder, ownerLabel, statusTone } from './taskFlow';

export default function TaskBoardList({ teamId, viewBotId, bots }: { teamId: string; viewBotId?: string; bots: Bot[] }) {
  const { t } = useTranslation();
  const tasks = useTasks(teamId, true, viewBotId);
  const create = useCreateTask(teamId);
  const [createOpen, setCreateOpen] = useState(false);
  const [expandedId, setExpandedId] = useState<string | null>(null);
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
    return <p className="px-3 py-2 text-sm text-neutral-500" role="status">{t('status.loading')}</p>;
  }
  if (tasks.isError && !tasks.data) {
    return <p className="px-3 py-2 text-sm text-red-600" role="alert">{t('tasks.loadFailed')}</p>;
  }

  return (
    <div>
      <div className="px-3 py-2">
        <button
          type="button"
          onClick={() => setCreateOpen((open) => !open)}
          aria-expanded={createOpen}
          className="flex w-full items-center gap-2 rounded-md border border-dashed border-neutral-300 px-3 py-2 text-sm text-neutral-600 transition-colors hover:border-neutral-400 hover:text-neutral-900 dark:border-neutral-700 dark:text-neutral-300 dark:hover:border-neutral-500 dark:hover:text-neutral-100"
        >
          <Plus size={16} aria-hidden="true" />
          {t('tasks.new')}
        </button>
      </div>

      {createOpen && (
        <form
          className="mx-3 mb-2 space-y-2 rounded-lg border border-neutral-200 p-2 dark:border-neutral-700"
          onSubmit={(event) => {
            event.preventDefault();
            submitCreate();
          }}
        >
          <label className="block text-xs text-neutral-500 dark:text-neutral-400">
            {t('tasks.titleLabel')}
            <input
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              className="mt-1 w-full rounded-md border border-neutral-300 bg-white px-2 py-1 text-sm text-neutral-900 focus:border-neutral-500 focus:outline-none dark:border-neutral-600 dark:bg-neutral-800 dark:text-neutral-100"
            />
          </label>
          <label className="block text-xs text-neutral-500 dark:text-neutral-400">
            {t('tasks.description')}
            <textarea value={description} onChange={(event) => setDescription(event.target.value)} rows={3} className="mt-1 w-full rounded-md border border-neutral-300 bg-transparent px-2 py-1 text-sm dark:border-neutral-600" />
          </label>
          <label className="block text-xs text-neutral-500 dark:text-neutral-400">
            {t('tasks.acceptance')}
            <textarea value={acceptanceCriteria} onChange={(event) => setAcceptanceCriteria(event.target.value)} rows={2} className="mt-1 w-full rounded-md border border-neutral-300 bg-transparent px-2 py-1 text-sm dark:border-neutral-600" />
          </label>
          <label className="block text-xs text-neutral-500 dark:text-neutral-400">
            {t('tasks.ownerLabel')}
            <select
              value={ownerId}
              onChange={(event) => setOwnerId(event.target.value)}
              className="mt-1 w-full rounded-md border border-neutral-300 bg-white px-2 py-1 text-sm text-neutral-900 focus:border-neutral-500 focus:outline-none dark:border-neutral-600 dark:bg-neutral-800 dark:text-neutral-100"
            >
              <option value="">{t('tasks.ownerPlaceholder')}</option>
              {bots.map((bot) => (
                <option key={bot.id} value={bot.id}>
                  {bot.name}
                </option>
              ))}
            </select>
          </label>
          {errorText && (
            <p className="text-xs text-red-600" role="alert">
              {errorText}
            </p>
          )}
          <button
            type="submit"
            disabled={!title.trim() || !ownerId || create.isPending}
            className="w-full rounded-md bg-neutral-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-neutral-700 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
          >
            {t('tasks.create')}
          </button>
        </form>
      )}

      {items.length === 0 && (
        <p className="px-3 py-2 text-sm text-neutral-500 dark:text-neutral-400">{t('tasks.empty')}</p>
      )}

      <ul className="space-y-1 px-2 pb-3">
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
  return (
    <li className="rounded-md border border-neutral-200 dark:border-neutral-700">
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
            <span className={`shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-medium ${statusTone(task.status)}`}>
              {t(`tasks.status.${task.status}`)}
            </span>
            <span className="min-w-0 truncate">{task.title}</span>
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
