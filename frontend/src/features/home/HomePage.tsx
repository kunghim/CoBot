import { useTranslation } from 'react-i18next';

/**
 * HomePage is the no-team landing surface. It shows only real state: without
 * teams there is no fake data and no buttons for features that do not exist
 * yet (team creation ships with the teams milestone).
 */
export default function HomePage() {
  const { t } = useTranslation();
  return (
    <div className="flex h-full flex-col items-center justify-center gap-2 p-8 text-center">
      <h1 className="text-base font-semibold">{t('home.welcome')}</h1>
      <p className="max-w-sm text-sm leading-6 text-neutral-500 dark:text-neutral-400">{t('home.noTeams')}</p>
    </div>
  );
}
