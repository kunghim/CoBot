// How member-to-member traffic reads in a conversation: one centered line,
// "发给 [前端开发]" or "来自 [测试工程师]". The member name is a badge;
// clicking the line opens the whole exchange with that member above the
// conversation. Task assignments and review notices are such messages too.
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import type { Message } from '../../api/types';

export function RoleBadge({ name }: { name: string }) {
  return (
    <span className="inline-flex max-w-[12rem] shrink-0 items-center truncate rounded-md bg-neutral-200/80 px-1.5 py-px text-[11px] font-medium leading-4 text-neutral-700 dark:bg-neutral-700/80 dark:text-neutral-200">
      {name}
    </span>
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

/** Whether a message passed between members (or from the user's own task
 * assignment), and so reads as an exchange line rather than a bubble. */
export function isExchange(message: Message): boolean {
  if (message.kind === 'collaboration') return true;
  if (message.kind !== 'system') return false;
  if (message.sourceBotId) return message.sourceBotId !== message.botId;
  return taskCardOf(message.blocks) !== null;
}

/**
 * One exchange line: only who it went to or came from, the message itself is
 * in the record the line opens. `peers` are the other members (several when
 * one message went to many); an empty list means the user, whose line opens
 * nothing.
 */
export function ExchangeLine({
  direction,
  peers,
  nameOf,
  onOpen,
}: {
  direction: 'to' | 'from';
  peers: string[];
  nameOf: (botId: string) => string;
  onOpen: (peerId: string) => void;
}) {
  const { t } = useTranslation();
  const label = t(direction === 'to' ? 'conversation.exchange.to' : 'conversation.exchange.from');
  const lineClass = 'flex flex-wrap items-center gap-1.5 rounded-full px-2.5 py-1 text-xs text-neutral-500 dark:text-neutral-400';
  const content = (badges: ReactNode) => (
    <>
      <span>{label}</span>
      {badges}
    </>
  );
  if (peers.length === 0) {
    return (
      <div className="flex justify-center" data-testid="exchange-line">
        <div className={lineClass}>{content(<RoleBadge name={t('conversation.exchange.you')} />)}</div>
      </div>
    );
  }
  if (peers.length === 1) {
    return (
      <div className="flex justify-center" data-testid="exchange-line">
        <button type="button" onClick={() => onOpen(peers[0])} className={`${lineClass} hover:bg-neutral-100 hover:text-neutral-700 dark:hover:bg-neutral-800 dark:hover:text-neutral-200`}>
          {content(<RoleBadge name={nameOf(peers[0])} />)}
        </button>
      </div>
    );
  }
  // A message to several members: each name opens the exchange with that one.
  return (
    <div className="flex justify-center" data-testid="exchange-line">
      <div className={lineClass}>
        {content(
          peers.map((peer) => (
            <button key={peer} type="button" onClick={() => onOpen(peer)} className="shrink-0 rounded-md hover:ring-1 hover:ring-neutral-400">
              <RoleBadge name={nameOf(peer)} />
            </button>
          )),
        )}
      </div>
    </div>
  );
}
