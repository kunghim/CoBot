import { describe, expect, it } from 'vitest';
import type { ActivityItem } from '../../api/types';
import { describeActivity } from './describe';

const t = (key: string, params?: Record<string, unknown>) => (params ? `${key}${JSON.stringify(params)}` : key);
const names: Record<string, string> = { lead: '队长', qa: '测试工程师', fe: '前端开发' };
const nameOf = (id: unknown) => names[String(id)] ?? 'someone';
const item = (type: string, detail: Record<string, unknown>, extra: Partial<ActivityItem> = {}): ActivityItem =>
  ({ id: 'e', seq: 1, type, teamId: 't', botId: '', resourceId: 'r', revision: 0, payload: {}, createdAt: '2026-10-17T10:00:00Z', detail, ...extra }) as unknown as ActivityItem;

describe('describeActivity', () => {
  it('says what happened to a task and links to it', () => {
    const created = describeActivity(item('task.created', { title: '登录页', ownerId: 'fe' }), nameOf, t);
    expect(created.text).toContain('activity.line.taskCreated');
    expect(created.text).toContain('前端开发');
    expect(created.taskId).toBe('r');
    const moved = describeActivity(item('task.updated', { title: '登录页', change: 'status', status: 'doing' }, { botId: 'fe' }), nameOf, t);
    expect(moved.text).toContain('activity.line.taskStatusBy');
    expect(moved.text).toContain('tasks.status.doing');
  });
  it('says who took up a card and how it was handed off', () => {
    const started = describeActivity(item('card.started', { title: '登录页' }, { botId: 'fe' }), nameOf, t);
    expect(started.text).toContain('activity.line.cardStarted');
    expect(started.taskId).toBe('r');
    const back = describeActivity(item('task.updated', { title: '代码评审', change: 'handoff', outcome: 'back' }, { botId: 'qa' }), nameOf, t);
    expect(back.text).toContain('activity.line.cardHandoff');
    expect(back.text).toContain('tasks.outcome.back');
    const requirement = describeActivity(item('requirement.updated', { title: '登录', status: 'accepting' }), nameOf, t);
    expect(requirement.text).toContain('tasks.requirementStatus.accepting');
  });
  it('shows who told whom what and opens the recipient', () => {
    const line = describeActivity(item('message.created', { fromBotId: 'lead', toBotId: 'qa', text: '请验收' }), nameOf, t);
    expect(line.text).toBe('activity.line.message{"from":"队长","to":"测试工程师"}');
    expect(line.quote).toBe('请验收');
    expect(line.conversationBotId).toBe('qa');
  });
  it('describes an approval by what it asked and how it ended', () => {
    const line = describeActivity(item('approval.created', { approvalType: 'native_tool', state: 'decided', decision: 'accept', summary: JSON.stringify({ command: '"C:\\pwsh.exe" -Command npm test' }) }, { botId: 'fe' }), nameOf, t);
    expect(line.text).toContain('activity.decision.accept');
    expect(line.quote).toBe('npm test');
    expect(line.approvals).toBe(true);
  });
  it('says why a budget held a turn', () => {
    const line = (reason: string) => describeActivity(item('budget.paused', {}, { botId: 'fe', resourceId: reason }), nameOf, t).text;
    expect(line('exceeded')).toContain('activity.line.budgetPaused{');
    expect(line('usage_unknown')).toContain('activity.line.budgetPausedUnknown');
    expect(line('reserve')).toContain('activity.line.budgetPausedReserve');
  });
  it('keeps a removed member by the name it had', () => {
    expect(describeActivity(item('bot.deleted', { name: '后端开发' }), nameOf, t).text).toContain('后端开发');
  });
});
