import { describe, expect, it } from 'vitest';
import type { ActivityItem } from '../../api/types';
import { describeActivity } from './describe';
import { taskCardOf } from '../conversations/MessageCards';
import { messageText } from '../conversations/delivery';

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
    const moved = describeActivity(item('task.updated', { title: '登录页', change: 'status', status: 'review' }, { botId: 'fe' }), nameOf, t);
    expect(moved.text).toContain('activity.line.taskStatusBy');
    expect(moved.text).toContain('tasks.status.review');
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
  it('keeps a removed member by the name it had', () => {
    expect(describeActivity(item('bot.deleted', { name: '后端开发' }), nameOf, t).text).toContain('后端开发');
  });
});

describe('task card', () => {
  const blocks = [
    { type: 'text', text: '你被指派了任务：登录页' },
    { type: 'task', text: JSON.stringify({ taskId: 'task-1', title: '登录页', acceptanceCriteria: '能登录', ownerId: 'fe' }) },
  ];
  it('reads the card and keeps its JSON out of the text', () => {
    expect(taskCardOf(blocks)).toEqual(expect.objectContaining({ taskId: 'task-1', title: '登录页' }));
    expect(messageText({ blocks } as never)).toBe('你被指派了任务：登录页');
    expect(taskCardOf([{ type: 'task', text: '{broken' }])).toBeNull();
  });
  it('reads an assignment written before the card block existed', () => {
    const legacy = taskCardOf([{ type: 'text', text: '你被指派了任务：登录页\n任务 ID：task-9\n详细要求：做表单\n完成标准：能登录\n且能退出' }]);
    expect(legacy).toEqual({ title: '登录页', taskId: 'task-9', description: '做表单', acceptanceCriteria: '能登录\n且能退出' });
    expect(taskCardOf([{ type: 'text', text: '普通系统消息' }])).toBeNull();
  });
});
