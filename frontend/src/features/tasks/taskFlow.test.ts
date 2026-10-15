import { describe, expect, it } from 'vitest';
import type { Bot, Task, TaskStatus } from '../../api/types';
import { boardOrder, canConfirm, canOfferTransfer, canRequestReview, canSubmitEvidence, dependencyLabels, nextStatuses, ownerLabel, statusTone } from './taskFlow';

function task(overrides: Partial<Task>): Task {
  return {
    id: 't1',
    teamId: 'team-1',
    ownerId: 'bot-1',
    title: '示例任务',
    status: 'todo',
    dependencyIds: [],
    revision: 1,
    createdBy: 'user',
    createdAt: '2026-10-15T01:00:00Z',
    updatedAt: '2026-10-15T01:00:00Z',
    ...overrides,
  };
}

describe('task status transitions mirror the server', () => {
  it('never offers done as a patch: done only exists through confirm', () => {
    expect(nextStatuses('todo')).toEqual(['doing', 'cancelled']);
    expect(nextStatuses('doing')).toEqual(['review', 'cancelled']);
    expect(nextStatuses('review')).toEqual(['cancelled']);
    expect(nextStatuses('done')).toEqual([]);
    expect(nextStatuses('cancelled')).toEqual([]);
  });

  it('guards review behind recorded evidence and confirm behind review', () => {
    const doing = task({ status: 'doing' });
    expect(canRequestReview(doing, false)).toBe(false);
    expect(canRequestReview(doing, true)).toBe(true);
    expect(canRequestReview(task({ status: 'todo' }), true)).toBe(false);

    const review = task({ status: 'review' });
    expect(canConfirm(review, false)).toBe(false);
    expect(canConfirm(review, true)).toBe(true);
    expect(canConfirm(doing, true)).toBe(false);
  });

  it('keeps evidence and transfers off terminal tasks', () => {
    expect(canSubmitEvidence('todo')).toBe(true);
    expect(canSubmitEvidence('review')).toBe(true);
    expect(canSubmitEvidence('done')).toBe(false);
    expect(canSubmitEvidence('cancelled')).toBe(false);
    expect(canOfferTransfer(task({ status: 'done' }))).toBe(false);
    expect(canOfferTransfer(task({ status: 'doing' }))).toBe(true);
  });
});

describe('board presentation helpers', () => {
  const bots: Bot[] = [
    { id: 'bot-1', teamId: 'team-1', name: '前端开发', roleId: 'r1', isLeader: false, status: 'idle', createdAt: '', revision: 1 },
    { id: 'bot-2', teamId: 'team-1', name: '后端开发', roleId: 'r2', isLeader: true, status: 'idle', createdAt: '', revision: 1 },
  ] as Bot[];

  it('labels owners: bots by name, the user marker, strangers by short id', () => {
    expect(ownerLabel('bot-1', bots, '你')).toBe('前端开发');
    expect(ownerLabel('user', bots, '你')).toBe('你');
    expect(ownerLabel('bot-x-not-here', bots, '你')).toBe('bot-x-no');
  });

  it('resolves dependency titles from the same board and falls back to ids', () => {
    const dep = task({ id: 't9', title: '登录接口' });
    const withDep = task({ dependencyIds: ['t9', 't-missing-aaaaaaaa'] });
    expect(dependencyLabels(withDep, [dep])).toEqual(['登录接口', 't-missin']);
  });

  it('orders open work before finished work, then by creation', () => {
    const early = task({ id: 'a', status: 'doing', createdAt: '2026-10-15T01:00:00Z' });
    const later = task({ id: 'b', status: 'todo', createdAt: '2026-10-15T02:00:00Z' });
    const done = task({ id: 'c', status: 'done', createdAt: '2026-10-14T00:00:00Z' });
    expect(boardOrder([done, later, early]).map((item) => item.id)).toEqual(['a', 'b', 'c']);
  });

  it('gives every status a tone and unknown input the safe default', () => {
    const statuses: TaskStatus[] = ['todo', 'doing', 'review', 'done', 'cancelled'];
    for (const status of statuses) {
      expect(statusTone(status)).not.toBe('');
    }
  });
});
