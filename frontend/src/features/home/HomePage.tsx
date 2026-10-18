// HomePage hosts the conversation shell: with a selected member it renders
// the real session; without one it is a starting point — create the first
// team, or open a team's leader — built only from real data.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Plus } from 'lucide-react';
import { useTeams } from '../../api/queries';
import { useSelectedBot } from '../../app/SelectedBotContext';
import ConversationView from '../conversations/ConversationView';
import CreateTeamDialog from '../teams/CreateTeamDialog';
import Avatar from '../avatars/Avatar';
import { findBotContext } from '../teams/teamUtils';

// The built-in lineup a new team starts from, in the order a request flows.
const LINEUP = [
  { roleId: 'leader', isLeader: true },
  { roleId: 'product-manager', isLeader: false },
  { roleId: 'frontend-dev', isLeader: false },
  { roleId: 'backend-dev', isLeader: false },
  { roleId: 'qa-engineer', isLeader: false },
  { roleId: 'code-reviewer', isLeader: false },
];

export default function HomePage() {
  const { t } = useTranslation();
  const { botId, setBotId } = useSelectedBot();
  const teams = useTeams(true);
  const [createOpen, setCreateOpen] = useState(false);

  const context = botId && teams.data ? findBotContext(teams.data.items, botId) : null;

  if (botId && context) {
    return <ConversationView key={context.bot.id} item={context.item} bot={context.bot} />;
  }

  const active = (teams.data?.items ?? []).filter((item) => item.team.status !== 'archived');
  const leaders = active.flatMap((item) => {
    const leader = item.bots.find((bot) => bot.isLeader);
    return leader ? [{ team: item.team, leader }] : [];
  });

  return (
    <div className="flex h-full items-center justify-center overflow-y-auto p-8">
      <div className="w-full max-w-lg">
        {teams.isSuccess && leaders.length === 0 ? (
          <>
            <div className="flex gap-1" aria-hidden="true">
              {LINEUP.map((entry) => (
                <Avatar key={entry.roleId} size="xl" bot={{ roleId: entry.roleId, isLeader: entry.isLeader }} />
              ))}
            </div>
            <h1 className="mt-6 text-2xl font-semibold tracking-tight text-neutral-900 dark:text-neutral-50">{t('home.emptyTitle')}</h1>
            <p className="mt-3 text-sm leading-6 text-neutral-600 dark:text-neutral-300">{t('home.emptyBody')}</p>
            <button
              type="button"
              onClick={() => setCreateOpen(true)}
              className="mt-6 inline-flex items-center gap-2 rounded-lg bg-neutral-900 px-4 py-2 text-sm font-medium text-white shadow-sm transition-colors hover:bg-neutral-700 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
            >
              <Plus size={16} aria-hidden="true" />
              {t('team.new')}
            </button>
          </>
        ) : (
          <>
            <h1 className="text-2xl font-semibold tracking-tight text-neutral-900 dark:text-neutral-50">{t('home.pickTitle')}</h1>
            <p className="mt-3 text-sm leading-6 text-neutral-600 dark:text-neutral-300">{t('home.selectBot')}</p>
            {leaders.length > 0 && (
              <ul className="mt-6 space-y-2">
                {leaders.map(({ team, leader }) => (
                  <li key={team.id}>
                    <button
                      type="button"
                      onClick={() => setBotId(leader.id)}
                      className="flex w-full items-center gap-3 rounded-xl bg-white px-3 py-2.5 text-left shadow-sm ring-1 ring-neutral-200 transition-colors hover:ring-neutral-300 dark:bg-neutral-900 dark:ring-neutral-800 dark:hover:ring-neutral-700"
                    >
                      <Avatar bot={leader} size="lg" />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-medium">{team.name}</span>
                        <span className="block truncate font-mono text-[11px] text-neutral-500 dark:text-neutral-400">{team.directory}</span>
                      </span>
                      <span className="shrink-0 text-xs text-neutral-500 dark:text-neutral-400">{t('home.openLeader')}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </>
        )}
      </div>
      <CreateTeamDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={(leaderBotId) => {
          setCreateOpen(false);
          setBotId(leaderBotId);
        }}
      />
    </div>
  );
}
