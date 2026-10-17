// describeActivity turns one activity entry into a sentence plus what it
// links to. Pure, so every event type is covered by tests.
import type { ActivityItem } from '../../api/types';
import { parseNativeRequest } from '../approvals/nativeRequest';
import { displayCommand } from '../conversations/steps';

interface ActivityLine {
  text: string;
  /** A message or question excerpt shown under the sentence. */
  quote?: string;
  taskId?: string;
  conversationBotId?: string;
  approvals?: boolean;
}

type Translate = (key: string, params?: Record<string, unknown>) => string;

function str(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

/** A one-line description of what a native approval asked for. */
function approvalWhat(summary: string, t: Translate): string {
  const request = parseNativeRequest(summary);
  if (!request) return '';
  if (request.command !== undefined) return displayCommand(request.command);
  if (request.changes.length || request.paths.length) return t('activity.files', { count: request.changes.length || request.paths.length });
  if (request.permissions) return t('approvals.native.permissions');
  return '';
}

export function describeActivity(item: ActivityItem, nameOf: (id: unknown) => string, t: Translate): ActivityLine {
  const d = (item.detail ?? {}) as Record<string, unknown>;
  const actor = item.botId ? nameOf(item.botId) : '';
  const title = str(d.title) || t('activity.untitledTask');
  const taskId = item.type.startsWith('task.') ? item.resourceId : undefined;
  switch (item.type) {
    case 'task.created':
      return { text: t('activity.line.taskCreated', { title, owner: nameOf(d.ownerId) }), taskId };
    case 'task.updated':
      switch (d.change) {
        case 'status':
          return { text: t(actor ? 'activity.line.taskStatusBy' : 'activity.line.taskStatus', { title, actor, status: t(`tasks.status.${str(d.status)}`, { defaultValue: str(d.status) }) }), taskId };
        case 'assigned':
          return { text: t('activity.line.taskAssigned', { title, owner: nameOf(d.ownerId) }), taskId };
        case 'evidence':
          return { text: t('activity.line.taskEvidence', { title, actor: actor || t('activity.someone') }), taskId };
        default:
          return { text: t('activity.line.taskUpdated', { title }), taskId };
      }
    case 'transfer.offered':
      return { text: t('activity.line.transferOffered', { from: nameOf(d.fromBotId), to: nameOf(d.toBotId), title }) };
    case 'transfer.accepted':
      return { text: t('activity.line.transferAccepted', { to: nameOf(d.toBotId), title }) };
    case 'question.created':
      return { text: t('activity.line.questionCreated', { actor: actor || t('activity.someone') }), quote: str(d.question), conversationBotId: item.botId || undefined };
    case 'question.answered':
      return { text: t('activity.line.questionAnswered', { actor: actor || t('activity.someone') }), quote: str(d.question) };
    case 'approval.created': {
      const kind = t(`approvals.type.${str(d.approvalType)}`, { defaultValue: t('approvals.title') });
      const state = d.decision ? t(`activity.decision.${str(d.decision)}`, { defaultValue: t('activity.decided') }) : d.state === 'pending' ? t('activity.pending') : t('activity.closed');
      return { text: t('activity.line.approval', { actor: actor || t('activity.someone'), kind, state }), quote: approvalWhat(str(d.summary), t) || undefined, approvals: true };
    }
    case 'bot.added':
      return { text: t('activity.line.botAdded', { name: str(d.name) || nameOf(item.resourceId) }), conversationBotId: item.resourceId };
    case 'bot.deleted':
      return { text: t('activity.line.botDeleted', { name: str(d.name) || t('activity.someone') }) };
    case 'message.created':
      return { text: t('activity.line.message', { from: nameOf(d.fromBotId), to: nameOf(d.toBotId) }), quote: str(d.text), conversationBotId: str(d.toBotId) || undefined };
    case 'memory.changed':
      return { text: t(d.deleted ? 'activity.line.memoryDeleted' : 'activity.line.memory', { actor: actor || t('activity.someone') }), quote: str(d.text) };
    case 'budget.paused':
      return { text: t('activity.line.budgetPaused', { actor: actor || t('activity.someone') }) };
    case 'team.created':
    case 'team.archived':
    case 'team.restored':
      return { text: t(`activity.type.${item.type}`) };
    default:
      return { text: t(`activity.type.${item.type}`, { defaultValue: item.type }) };
  }
}
