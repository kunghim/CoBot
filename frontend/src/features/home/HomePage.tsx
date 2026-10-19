// HomePage hosts the conversation shell: with a selected member it renders
// the real session; without one it is a starting point — create the first
// team, or open a team's leader — built only from real data.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useTeams } from '../../api/queries';
import { useSelectedBot } from '../../app/SelectedBotContext';
import ConversationView from '../conversations/ConversationView';
import CreateTeamDialog from '../teams/CreateTeamDialog';
import Avatar from '../avatars/Avatar';
import { findBotContext } from '../teams/teamUtils';
import { ShellToggle, useShell } from '../../app/shell';
import { Button, Card, Icons, List, ListRow } from '../../components/ui';

// The built-in lineup a new team starts from, in the order a request flows.
const LINEUP = [
  { roleId: 'leader', isLeader: true },
  { roleId: 'product-manager', isLeader: false },
  { roleId: 'fullstack-engineer', isLeader: false },
  { roleId: 'qa-engineer', isLeader: false },
  { roleId: 'code-reviewer', isLeader: false },
];

export default function HomePage() {
  const { t } = useTranslation();
  const { botId, setBotId } = useSelectedBot();
  const teams = useTeams(true);
  const [createOpen, setCreateOpen] = useState(false);
  const shell = useShell();

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
    <div className="flex h-full flex-col">
      {!shell.titleBar && (
        <div className="flex min-h-14 items-center justify-between px-3 py-2">
          <ShellToggle side="left" />
          <ShellToggle side="right" />
        </div>
      )}
      <div className="flex min-h-0 flex-1 items-center justify-center overflow-y-auto px-8 pb-14">
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
            <div className="mt-6">
              <Button variant="primary" icon={Icons.add} onClick={() => setCreateOpen(true)}>
                {t('team.new')}
              </Button>
            </div>
          </>
        ) : (
          <>
            <h1 className="text-2xl font-semibold tracking-tight text-neutral-900 dark:text-neutral-50">{t('home.pickTitle')}</h1>
            <p className="mt-3 text-sm leading-6 text-neutral-600 dark:text-neutral-300">{t('home.selectBot')}</p>
            {leaders.length > 0 && (
              <Card className="mt-6" padded={false}>
                <div className="px-4 py-1">
                  <List>
                    {leaders.map(({ team, leader }) => (
                      <ListRow
                        key={team.id}
                        onClick={() => setBotId(leader.id)}
                        leading={<Avatar bot={leader} size="lg" />}
                        title={team.name}
                        description={<span className="font-mono">{team.directory}</span>}
                        trailing={<span className="text-xs text-neutral-500 dark:text-neutral-400">{t('home.openLeader')}</span>}
                      />
                    ))}
                  </List>
                </div>
              </Card>
            )}
          </>
        )}
      </div>
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
