// How member-to-member traffic reads in a conversation: a message from
// another member is a named bubble, a message this member sent is a compact
// "→ sent to X" card, and a task assignment is a task card that opens the
// task on the board.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ClipboardList, CornerUpRight } from 'lucide-react';
import { showInWorkPanel } from '../../app/workPanelNav';
import Markdown from './Markdown';

export function IncomingMessage({ from, text, teamId }: { from: string; text: string; teamId: string }) {
  return (
    <div className="flex justify-start">
      <div className="max-w-[85%] space-y-1">
        <p className="px-1 text-xs font-medium text-neutral-500 dark:text-neutral-400">{from}</p>
        <div className="break-words rounded-xl border border-neutral-200 bg-white px-3 py-2 text-sm text-neutral-900 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100">
          <Markdown text={text} teamId={teamId} />
        </div>
      </div>
    </div>
  );
}

/** notified: an information-only note, left for the recipient's next turn. */
export function OutgoingMessage({ to, text, teamId, notified = false }: { to: string; text: string; teamId: string; notified?: boolean }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  return (
    <div className="flex justify-start">
      <div className="max-w-[85%] rounded-xl border border-dashed border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700" data-testid="outgoing-message">
        <p className="flex items-center gap-1.5 text-xs font-medium text-neutral-500 dark:text-neutral-400">
          <CornerUpRight size={13} aria-hidden="true" />
          {t(notified ? 'conversation.notifiedTo' : 'conversation.sentTo', { name: to })}
        </p>
        <button type="button" onClick={() => setOpen(!open)} aria-expanded={open} className="mt-1 block w-full text-left text-neutral-700 dark:text-neutral-300">
          <div className={open ? '' : 'line-clamp-3'}>
            <Markdown text={text} teamId={teamId} />
          </div>
        </button>
      </div>
    </div>
  );
}

export interface TaskCardData {
  taskId: string;
  title: string;
  description?: string;
  acceptanceCriteria?: string;
  ownerId?: string;
}

// Assignments written before the task block existed carry the same facts in
// this fixed text form.
const legacyAssignment = /^你被指派了任务：(.*)\n任务 ID：(\S+)\n详细要求：([\s\S]*?)\n完成标准：([\s\S]*)$/;

/** Reads the task of an assignment message, if it is one. */
export function taskCardOf(blocks: Array<{ type: string; text: string }>): TaskCardData | null {
  const block = blocks.find((entry) => entry.type === 'task');
  if (!block) {
    const legacy = blocks.length === 1 ? legacyAssignment.exec(blocks[0].text) : null;
    return legacy ? { title: legacy[1], taskId: legacy[2], description: legacy[3], acceptanceCriteria: legacy[4] } : null;
  }
  try {
    const data = JSON.parse(block.text) as TaskCardData;
    return typeof data.taskId === 'string' && typeof data.title === 'string' ? data : null;
  } catch {
    return null;
  }
}

export function TaskCard({ card, owner, teamId }: { card: TaskCardData; owner: string; teamId: string }) {
  const { t } = useTranslation();
  return (
    <div className="flex justify-start">
      <div className="w-full max-w-[85%] rounded-xl border border-blue-200 bg-blue-50/50 px-3 py-2.5 text-sm dark:border-blue-900 dark:bg-blue-950/30" data-testid="task-card">
        <p className="flex items-center gap-1.5 text-xs font-medium text-blue-700 dark:text-blue-300">
          <ClipboardList size={13} aria-hidden="true" />
          {t('conversation.taskCard.assigned', { name: owner })}
        </p>
        <p className="mt-1 font-semibold text-neutral-900 dark:text-neutral-100">{card.title}</p>
        {card.description && (
          <div className="mt-1 line-clamp-3 text-neutral-700 dark:text-neutral-300">
            <Markdown text={card.description} teamId={teamId} />
          </div>
        )}
        {card.acceptanceCriteria && (
          <div className="mt-2 text-xs">
            <p className="font-medium text-neutral-600 dark:text-neutral-400">{t('conversation.taskCard.acceptance')}</p>
            <div className="text-neutral-700 dark:text-neutral-300">
              <Markdown text={card.acceptanceCriteria} teamId={teamId} />
            </div>
          </div>
        )}
        <button
          type="button"
          onClick={() => showInWorkPanel('tasks', card.taskId)}
          className="mt-2 text-xs font-medium text-blue-700 underline-offset-2 hover:underline dark:text-blue-300"
        >
          {t('conversation.taskCard.open')}
        </button>
      </div>
    </div>
  );
}
