// How member-to-member traffic reads in a conversation: one centered line,
// "发给 [avatar] 前端开发" or "来自 [avatar] 测试工程师". Clicking a member
// opens the whole exchange with it above the conversation. Task assignments
// and review notices are such messages too.
import { createContext, useContext } from 'react';
import { useTranslation } from 'react-i18next';
import type { Bot, Message } from '../../api/types';
import { ExchangeLine as Line, type PersonRef } from '../../components/ui';
import Avatar from '../avatars/Avatar';

/** The team's members by id, so a mention can show its member's avatar. */
export const TeamMembersContext = createContext<ReadonlyMap<string, Bot>>(new Map());

/** A member named in the conversation: its avatar and its name. */
export function usePerson(): (botId: string, fallbackName?: string) => PersonRef {
  const members = useContext(TeamMembersContext);
  return (botId, fallbackName) => {
    const bot = members.get(botId);
    return {
      avatar: bot ? <Avatar bot={bot} size="sm" /> : <span aria-hidden className="inline-block h-4 w-4 rounded-[30%] bg-neutral-300 dark:bg-neutral-600" />,
      name: bot?.name ?? fallbackName ?? botId,
    };
  };
}

/** What identifies an assignment; only its presence matters for drawing. */
interface TaskCardData {
  taskId: string;
  title: string;
}

// Assignments written before the task block existed carry the same facts in
// this fixed text form.
const legacyAssignment = /^你被指派了任务：(.*)\n任务 ID：(\S+)\n详细要求：[\s\S]*?\n完成标准：[\s\S]*$/;

/** Reads the task of an assignment message, if it is one. */
export function taskCardOf(blocks: Array<{ type: string; text: string }>): TaskCardData | null {
  const block = blocks.find((entry) => entry.type === 'task');
  if (!block) {
    const legacy = blocks.length === 1 ? legacyAssignment.exec(blocks[0].text) : null;
    return legacy ? { title: legacy[1], taskId: legacy[2] } : null;
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
  const person = usePerson();
  const label = t(direction === 'to' ? 'conversation.exchange.to' : 'conversation.exchange.from');
  return (
    <div data-testid="exchange-line">
      {peers.length === 0 ? (
        <Line label={label} people={[{ avatar: null, name: t('conversation.exchange.you') }]} />
      ) : (
        <Line label={label} people={peers.map((peer) => person(peer, nameOf(peer)))} onOpen={(index) => onOpen(peers[index])} />
      )}
    </div>
  );
}
