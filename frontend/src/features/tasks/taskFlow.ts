// taskFlow holds the pure rules of the requirement board: where a requirement
// stands on its route and how its cards are shown. The server moves the work;
// these helpers only describe it.
import type { Tone } from '../../components/ui';
import type { Bot, Requirement, Task, TaskStatus } from '../../api/types';

// Each card status's tone in the component library (Badge, StatusDot).
const TONE: Record<TaskStatus, Tone> = {
  todo: 'neutral',
  doing: 'success',
  done: 'neutral',
  cancelled: 'danger',
};

export function statusTone(status: string): Tone {
  return TONE[status as TaskStatus] ?? TONE.todo;
}

const REQUIREMENT_TONE: Record<string, Tone> = {
  active: 'success',
  accepting: 'info',
  done: 'neutral',
  cancelled: 'danger',
};

export function requirementTone(status: string): Tone {
  return REQUIREMENT_TONE[status] ?? 'neutral';
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
