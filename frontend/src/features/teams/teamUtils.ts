// Pure team/bot helpers shared by the team list, the conversation shell and
// the work panel. No React, no i18n — keys are translated by callers.
import type { Bot, Team, TeamStatus, TeamWithBots } from '../../api/types';

/** The leader always lists first; members keep their backend order. */
export function orderBotsLeaderFirst(bots: Bot[]): Bot[] {
  const leader = bots.filter((bot) => bot.isLeader);
  const members = bots.filter((bot) => !bot.isLeader);
  return [...leader, ...members];
}

export interface BotContext {
  item: TeamWithBots;
  team: Team;
  bot: Bot;
}

/** Locates the selected bot plus its team in the list projection. */
export function findBotContext(items: TeamWithBots[], botId: string): BotContext | null {
  for (const item of items) {
    const bot = item.bots.find((candidate) => candidate.id === botId);
    if (bot) return { item, team: item.team, bot };
  }
  return null;
}

/** i18n key that explains why the composer is disabled, or null when the
 * team accepts messages. Only "active" teams can be messaged; every other
 * state shows its honest reason. */
export function sendBlockReasonKey(status: TeamStatus): string | null {
  switch (status) {
    case 'active':
      return null;
    case 'archived':
      return 'conversation.blocked.archived';
    case 'directory_missing':
      return 'conversation.blocked.directory_missing';
    default:
      return 'conversation.blocked.notActive';
  }
}

/** i18n key of the small status badge for non-active teams, or null. */
export function teamBadgeKey(status: TeamStatus): string | null {
  switch (status) {
    case 'active':
    case 'deleted':
      return null;
    case 'archived':
    case 'directory_missing':
      return `team.badge.${status}`;
    default:
      return null;
  }
}
