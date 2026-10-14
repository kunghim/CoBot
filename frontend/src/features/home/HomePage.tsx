// HomePage hosts the conversation shell: with a selected member it renders
// the real session; without one it stays the honest empty state (no fake
// data, no placeholder buttons).
import { useTranslation } from 'react-i18next';
import { useTeams } from '../../api/queries';
import { useSelectedBot } from '../../app/SelectedBotContext';
import ConversationView from '../conversations/ConversationView';
import { findBotContext } from '../teams/teamUtils';

export default function HomePage() {
  const { t } = useTranslation();
  const { botId } = useSelectedBot();
  const teams = useTeams(true);

  const context = botId && teams.data ? findBotContext(teams.data.items, botId) : null;

  if (botId && context) {
    return <ConversationView item={context.item} bot={context.bot} />;
  }

  return (
    <div className="flex h-full flex-col items-center justify-center gap-2 p-8 text-center">
      <h1 className="text-base font-semibold">{t('home.welcome')}</h1>
      <p className="max-w-sm text-sm leading-6 text-neutral-500 dark:text-neutral-400">{t('home.selectBot')}</p>
    </div>
  );
}
