// taskFlow mirrors the server's task lifecycle rules for the board UI. The
// server stays authoritative; these helpers only decide which buttons exist,
// never whether an action succeeds.
import type { Bot, Task, TaskStatus } from '../../api/types';

const TRANSITIONS: Record<TaskStatus, TaskStatus[]> = {
  todo: ['doing', 'cancelled'],
  doing: ['review', 'cancelled'],
  // review: done only through confirm (evidence-backed), so the status
  // patch menu never offers it.
  review: ['cancelled'],
  done: [],
  cancelled: [],
};

/** Statuses the user may request by patching; done is excluded everywhere. */
export function nextStatuses(status: TaskStatus): TaskStatus[] {
  return TRANSITIONS[status] ?? [];
}

/** Moving to review requires recorded evidence; the server rejects it
 * without, so the board only offers the button once evidence exists. */
export function canRequestReview(task: Task, hasEvidence: boolean): boolean {
  return task.status === 'doing' && hasEvidence;
}

/** Confirm is the only path to done, and only a review task with evidence
 * can be confirmed. */
export function canConfirm(task: Task, hasEvidence: boolean): boolean {
  return task.status === 'review' && hasEvidence;
}

/** Evidence may be added while the task is still open; terminal tasks keep
 * their record unchanged. */
export function canSubmitEvidence(status: TaskStatus): boolean {
  return status === 'todo' || status === 'doing' || status === 'review';
}

/** A transfer offer only makes sense while the task is still open; the
 * user may arrange any hand-over, the server enforces the rest. */
export function canOfferTransfer(task: Task): boolean {
  return task.status !== 'done' && task.status !== 'cancelled';
}

const TONE: Record<TaskStatus, string> = {
  todo: 'bg-neutral-100 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300',
  doing: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900 dark:text-emerald-200',
  review: 'bg-sky-100 text-sky-800 dark:bg-sky-900 dark:text-sky-200',
  done: 'bg-neutral-200 text-neutral-500 dark:bg-neutral-700 dark:text-neutral-400',
  cancelled: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300',
};

export function statusTone(status: TaskStatus): string {
  return TONE[status] ?? TONE.todo;
}

/** Display name for an owner id: member name when it is a bot of the team,
 * the user marker for the literal "user", otherwise a short id. */
export function ownerLabel(ownerId: string, bots: Bot[], userLabel: string): string {
  if (ownerId === 'user') return userLabel;
  const bot = bots.find((candidate) => candidate.id === ownerId);
  return bot ? bot.name : ownerId.slice(0, 8);
}

/** Dependency chips: resolved titles first (same board), raw short ids for
 * anything the list does not contain. */
export function dependencyLabels(task: Task, tasks: Task[]): string[] {
  return (task.dependencyIds ?? []).map((id) => {
    const found = tasks.find((candidate) => candidate.id === id);
    return found ? found.title : id.slice(0, 8);
  });
}

/** The board order: open work first by creation, finished work after. */
export function boardOrder(tasks: Task[]): Task[] {
  const weight = (status: TaskStatus) => (status === 'done' || status === 'cancelled' ? 1 : 0);
  return [...tasks].sort((a, b) => {
    const byWeight = weight(a.status) - weight(b.status);
    if (byWeight !== 0) return byWeight;
    return a.createdAt.localeCompare(b.createdAt);
  });
}
