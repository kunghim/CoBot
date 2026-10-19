import { describe, expect, it } from 'vitest';
import type { Bot, Requirement, Task, TaskStatus } from '../../api/types';
import { canCancel, cardsOf, currentStage, ownerLabel, requirementOrder, statusTone } from './taskFlow';

function card(overrides: Partial<Task>): Task {
  return {
    id: 't1',
    teamId: 'team-1',
    ownerId: 'bot-1',
    title: '实现',
    status: 'todo',
    requirementId: 'r1',
    stageIndex: 1,
    returnTo: -1,
    revision: 1,
    createdBy: 'user',
    createdAt: '2026-10-15T01:00:00Z',
    updatedAt: '2026-10-15T01:00:00Z',
    ...overrides,
  };
}

function requirement(overrides: Partial<Requirement>): Requirement {
  return {
    id: 'r1',
    teamId: 'team-1',
    title: '登录',
    document: '# 目标',
    stages: [
      { name: '用户确认', kind: 'confirm' },
      { name: '实现', kind: 'implement', ownerId: 'bot-1' },
      { name: '代码评审', kind: 'review', ownerId: 'bot-2', reviewType: 'code' },
      { name: '验收汇报', kind: 'accept' },
    ],
    status: 'active',
    createdBy: 'bot-2',
    revision: 1,
    createdAt: '2026-10-15T00:00:00Z',
    updatedAt: '2026-10-15T00:00:00Z',
    ...overrides,
  };
}

describe('where a requirement stands on its route', () => {
  it('is at the stage of its newest open card', () => {
    const cards = [card({ id: 'a', stageIndex: 1, status: 'done' }), card({ id: 'b', stageIndex: 2, status: 'doing', createdAt: '2026-10-15T02:00:00Z' })];
    expect(currentStage(requirement({}), cards)).toBe(2);
  });

  it('waits on the user at the first stage before any card', () => {
    expect(currentStage(requirement({}), [])).toBe(0);
  });

  it('goes on after the newest finished card, or back to the stage a rework returns to', () => {
    expect(currentStage(requirement({}), [card({ stageIndex: 1, status: 'done' })])).toBe(2);
    expect(currentStage(requirement({}), [card({ stageIndex: 1, status: 'done', returnTo: 2 })])).toBe(2);
  });

  it('is at the end while the user accepts, nowhere once cancelled', () => {
    expect(currentStage(requirement({ status: 'accepting' }), [])).toBe(3);
    expect(currentStage(requirement({ status: 'cancelled' }), [card({ status: 'cancelled' })])).toBe(-1);
  });

  it('keeps a requirement\'s own cards in the order they were handed out', () => {
    const later = card({ id: 'b', createdAt: '2026-10-15T03:00:00Z' });
    const other = card({ id: 'x', requirementId: 'r2' });
    const early = card({ id: 'a', createdAt: '2026-10-15T01:00:00Z' });
    expect(cardsOf('r1', [later, other, early]).map((item) => item.id)).toEqual(['a', 'b']);
  });
});

describe('board presentation helpers', () => {
  const bots: Bot[] = [
    { id: 'bot-1', teamId: 'team-1', name: '全栈工程师', roleId: 'r1', isLeader: false, status: 'idle', createdAt: '', revision: 1 },
    { id: 'bot-2', teamId: 'team-1', name: '项目经理', roleId: 'r2', isLeader: true, status: 'idle', createdAt: '', revision: 1 },
  ] as Bot[];

  it('labels owners: bots by name, the user marker, strangers by short id', () => {
    expect(ownerLabel('bot-1', bots, '你')).toBe('全栈工程师');
    expect(ownerLabel('user', bots, '你')).toBe('你');
    expect(ownerLabel('bot-x-not-here', bots, '你')).toBe('bot-x-no');
  });

  it('orders running requirements first, newest first', () => {
    const old = requirement({ id: 'a', createdAt: '2026-10-14T00:00:00Z' });
    const fresh = requirement({ id: 'b', createdAt: '2026-10-15T00:00:00Z' });
    const done = requirement({ id: 'c', status: 'done', createdAt: '2026-10-16T00:00:00Z' });
    expect(requirementOrder([done, old, fresh]).map((item) => item.id)).toEqual(['b', 'a', 'c']);
  });

  it('lets the user cancel only a requirement still running', () => {
    expect(canCancel(requirement({ status: 'active' }))).toBe(true);
    expect(canCancel(requirement({ status: 'accepting' }))).toBe(true);
    expect(canCancel(requirement({ status: 'done' }))).toBe(false);
    expect(canCancel(requirement({ status: 'cancelled' }))).toBe(false);
  });

  it('gives every card status a tone', () => {
    const statuses: TaskStatus[] = ['todo', 'doing', 'done', 'cancelled'];
    for (const status of statuses) {
      expect(statusTone(status)).not.toBe('');
    }
  });
});
