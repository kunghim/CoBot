// taskFlow holds the pure rules of the requirement board: where a requirement
// stands on its route and how its cards are shown. The server moves the work;
// these helpers only describe it.
import type { Bot, Requirement, Task, TaskStatus } from '../../api/types';

const TONE: Record<TaskStatus, string> = {
  todo: 'bg-neutral-100 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300',
  doing: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900 dark:text-emerald-200',
  done: 'bg-neutral-200 text-neutral-500 dark:bg-neutral-700 dark:text-neutral-400',
  cancelled: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300',
};

export function statusTone(status: string): string {
  return TONE[status as TaskStatus] ?? TONE.todo;
}

const REQUIREMENT_TONE: Record<string, string> = {
  active: TONE.doing,
  accepting: 'bg-sky-100 text-sky-800 dark:bg-sky-900 dark:text-sky-200',
  done: TONE.done,
  cancelled: TONE.cancelled,
};

export function requirementTone(status: string): string {
  return REQUIREMENT_TONE[status] ?? TONE.todo;
}

/** Display name for an owner id: member name when it is a bot of the team,
 * the user marker for the literal "user", otherwise a short id. */
export function ownerLabel(ownerId: string, bots: Bot[], userLabel: string): string {
  if (ownerId === 'user') return userLabel;
  const bot = bots.find((candidate) => candidate.id === ownerId);
  return bot ? bot.name : ownerId.slice(0, 8);
}

/** A requirement's cards in the order they were handed out. */
export function cardsOf(requirementId: string, cards: Task[]): Task[] {
  return cards
    .filter((card) => card.requirementId === requirementId)
    .sort((a, b) => a.createdAt.localeCompare(b.createdAt));
}

/** Where the work of a requirement is: the stage of its newest open card,
 * the stage after the newest finished one (a confirmation waiting for the
 * user), the last stage while the user accepts, or -1 for none (a cancelled
 * requirement, or its document being reworked). */
export function currentStage(requirement: Requirement, cards: Task[]): number {
  const last = requirement.stages.length - 1;
  if (requirement.status === 'cancelled') return -1;
  if (requirement.status === 'accepting' || requirement.status === 'done') return last;
  const own = cardsOf(requirement.id, cards);
  for (let i = own.length - 1; i >= 0; i--) {
    if (own[i].status === 'todo' || own[i].status === 'doing') return own[i].stageIndex;
  }
  for (let i = own.length - 1; i >= 0; i--) {
    if (own[i].status !== 'done') continue;
    const next = own[i].returnTo >= 0 ? own[i].returnTo : own[i].stageIndex + 1;
    return Math.min(next, last);
  }
  return 0;
}

/** The board order: requirements still running first, newest first. */
export function requirementOrder(requirements: Requirement[]): Requirement[] {
  const weight = (status: string) => (status === 'done' || status === 'cancelled' ? 1 : 0);
  return [...requirements].sort((a, b) => {
    const byWeight = weight(a.status) - weight(b.status);
    if (byWeight !== 0) return byWeight;
    return b.createdAt.localeCompare(a.createdAt);
  });
}

/** Whether the user may still cancel a requirement. */
export function canCancel(requirement: Requirement): boolean {
  return requirement.status === 'active' || requirement.status === 'accepting';
}
