// TaskDetail is an expanded card: what its stage must deliver, and every
// handoff of it: who handed it off and how, what they said, and what the
// engine saw them run and change.
import { useTranslation } from 'react-i18next';
import { useTaskDetail } from '../../api/queries';
import type { Bot, Task } from '../../api/types';
import { ownerLabel } from './taskFlow';

export default function TaskDetail({ task, bots }: { task: Task; bots: Bot[] }) {
  const { t } = useTranslation();
  const detail = useTaskDetail(task.id);
  const current = detail.data?.task ?? task;
  const records = detail.data?.evidence ?? [];
  const userLabel = t('tasks.ownerUser');

  return (
    <div className="space-y-2 px-2.5 pb-2.5 pt-1 text-xs">
      {current.stageKind && (
        <p className="text-neutral-500 dark:text-neutral-400">
          {t(`tasks.stageKind.${current.stageKind}`)}
          {current.reviewType ? `（${t(`tasks.reviewType.${current.reviewType}`)}）` : ''}
        </p>
      )}
      {current.acceptanceCriteria && (
        <p className="whitespace-pre-wrap text-neutral-600 dark:text-neutral-300">
          {t('tasks.acceptance')}: {current.acceptanceCriteria}
        </p>
      )}
      <section aria-label={t('tasks.handoffs')}>
        {records.length === 0 && <p className="text-neutral-500 dark:text-neutral-400">{t('tasks.handoffsEmpty')}</p>}
        <ul className="space-y-1">
          {records.map((item) => (
            <li key={item.id} className="space-y-0.5 rounded-lg bg-card px-2.5 py-2 shadow-sm ring-1 ring-neutral-900/[0.05] dark:ring-white/[0.06]">
              <p className="text-neutral-500 dark:text-neutral-400">
                {ownerLabel(item.authorId, bots, userLabel)} · {t(`tasks.outcome.${item.outcome ?? 'done'}`)}
                {item.mode ? ` · ${t(`tasks.mode.${item.mode}`)}` : ''}
              </p>
              <p className="whitespace-pre-wrap text-neutral-700 dark:text-neutral-300">{item.summary}</p>
              {item.suggestions && (
                <p className="whitespace-pre-wrap text-amber-700 dark:text-amber-400">
                  {t('tasks.suggestions')}: {item.suggestions}
                </p>
              )}
              {(item.artifactRefs ?? []).length > 0 && <p className="break-all font-mono text-neutral-500 dark:text-neutral-400">{(item.artifactRefs ?? []).join(', ')}</p>}
              {item.execution && (
                <div className="mt-0.5 text-neutral-500 dark:text-neutral-400">
                  <p className="font-medium">{t('tasks.execution')}</p>
                  {item.execution.files.length > 0 && <p className="break-all">{t('tasks.files', { files: item.execution.files.join(', ') })}</p>}
                  <ul className="font-mono">
                    {item.execution.commands.map((command, index) => (
                      <li key={index} className="break-all">
                        $ {command.command}
                        {command.exitCode !== undefined && (
                          <span className={command.exitCode === 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'}> → {command.exitCode}</span>
                        )}
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}
