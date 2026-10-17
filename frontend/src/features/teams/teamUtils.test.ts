import { describe, expect, it } from 'vitest';
import type { Bot, Team, TeamWithBots } from '../../api/types';
import { findBotContext, orderBotsLeaderFirst, sendBlockReasonKey, teamBadgeKey } from './teamUtils';

function bot(id: string, isLeader = false): Bot {
  return {
    id,
    teamId: 'team-1',
    name: id,
    roleId: isLeader ? 'leader' : 'member',
    isLeader,
    status: 'idle',
    revision: 1,
    createdAt: '2026-01-01T00:00:00Z',
  };
}

const team: Team = {
  id: 'team-1',
  name: 'cobot',
  profileInstanceId: 'inst',
  leaderId: 'leader-1',
  directory: 'E:/work',
  status: 'active',
  revision: 1,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
};

describe('member ordering', () => {
  it('always lists the leader first and keeps the rest stable', () => {
    const bots = [bot('pm'), bot('fe'), bot('leader-1', true), bot('qa')];
    expect(orderBotsLeaderFirst(bots).map((entry) => entry.id)).toEqual(['leader-1', 'pm', 'fe', 'qa']);
  });
});

describe('bot context lookup', () => {
  const items: TeamWithBots[] = [{ team, bots: [bot('leader-1', true), bot('fe')] }];

  it('finds the bot together with its team', () => {
    const context = findBotContext(items, 'fe');
    expect(context?.bot.id).toBe('fe');
    expect(context?.team.id).toBe('team-1');
    expect(context?.item.team.id).toBe('team-1');
  });

  it('returns null for a bot that does not exist', () => {
    expect(findBotContext(items, 'ghost')).toBeNull();
    expect(findBotContext([], 'fe')).toBeNull();
  });
});

describe('sendability by team status', () => {
  it('only active teams accept messages', () => {
    expect(sendBlockReasonKey('active')).toBeNull();
  });

  it('gives an honest reason for every other state', () => {
    expect(sendBlockReasonKey('archived')).toBe('conversation.blocked.archived');
    expect(sendBlockReasonKey('directory_missing')).toBe('conversation.blocked.directory_missing');
    expect(sendBlockReasonKey('deleted')).toBe('conversation.blocked.notActive');
  });

  it('shows a badge for every non-active listed team state', () => {
    expect(teamBadgeKey('active')).toBeNull();
    expect(teamBadgeKey('archived')).toBe('team.badge.archived');
    expect(teamBadgeKey('directory_missing')).toBe('team.badge.directory_missing');
  });
});
