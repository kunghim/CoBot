import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Navigate, useNavigate, useParams } from 'react-router-dom';
import { useSettings, useStatus } from '../../api/queries';
import { workspacePath } from '../../app/workspacePath';
import { scaleOf } from '../budget/budgetUnits';
import { Alert, Button, Icons, NavItem, NavSection, PageHeader, Skeleton } from '../../components/ui';
import {
  AboutSection,
  AppearanceSection,
  RolesSection,
  CollaborationSection,
  EngineSection,
  GeneralSection,
  ModelsSection,
  ProviderSection,
} from './SettingsSections';
import ArchivedTeamsSection from './ArchivedTeamsSection';
import { useSettingSaver } from './useSettingSaver';

type SectionId = 'general' | 'appearance' | 'roles' | 'engine' | 'models' | 'providers' | 'collaboration' | 'archived' | 'about';

const sections: { id: SectionId; icon: (typeof Icons)[keyof typeof Icons] }[] = [
  { id: 'general', icon: Icons.general },
  { id: 'appearance', icon: Icons.appearance },
  { id: 'roles', icon: Icons.roles },
  { id: 'engine', icon: Icons.engine },
  { id: 'providers', icon: Icons.provider },
  { id: 'models', icon: Icons.model },
  { id: 'collaboration', icon: Icons.team },
  { id: 'archived', icon: Icons.archive },
  { id: 'about', icon: Icons.info },
];

/**
 * SettingsPage is a page of its own, outside the workspace layout: a
 * category menu on the left and the chosen category on the right. Every
 * control saves on change and a toast confirms it; Back (or Escape) returns
 * to the workspace location the user came from.
 */
export default function SettingsPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { section } = useParams();
  const settings = useSettings(true);
  const status = useStatus();
  const { save, errors } = useSettingSaver();

  const back = () => navigate(workspacePath());

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented) return;
      const target = event.target as HTMLElement | null;
      if (target && ['INPUT', 'SELECT', 'TEXTAREA'].includes(target.tagName)) return;
      navigate(workspacePath());
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [navigate]);

  const current = sections.find((entry) => entry.id === section);
  if (!current) return <Navigate to="/settings/general" replace />;

  let body = null;
  if (settings.isPending) body = <Skeleton className="h-32 w-full rounded-xl" />;
  else if (settings.isError || !settings.data) body = <Alert tone="danger">{t('status.error')}</Alert>;
  else {
    const props = { settings: settings.data, save, errors, budgetScale: scaleOf(status.data?.profile) };
    body = {
      general: <GeneralSection {...props} />,
      appearance: <AppearanceSection {...props} />,
      roles: <RolesSection />,
      engine: <EngineSection />,
      models: <ModelsSection {...props} />,
      providers: <ProviderSection />,
      collaboration: <CollaborationSection {...props} />,
      archived: <ArchivedTeamsSection />,
      about: <AboutSection />,
    }[current.id];
  }

  return (
    <div className="flex h-full flex-col bg-side text-neutral-900 dark:text-neutral-100 md:flex-row">
      <nav
        aria-label={t('settings.title')}
        className="shrink-0 space-y-4 px-3 py-3 md:w-56"
      >
        <Button variant="ghost" size="sm" block className="justify-start" icon={Icons.back} onClick={back}>
          {t('settings.back')}
        </Button>
        <NavSection title={t('settings.title')}>
          <ul className="flex gap-0.5 overflow-x-auto md:flex-col md:overflow-visible">
            {sections.map(({ id, icon }) => (
              <li key={id} className="shrink-0">
                <NavItem
                  icon={icon}
                  label={t(`settings.sections.${id}`)}
                  href={`/settings/${id}`}
                  current={id === current.id}
                  onClick={() => navigate(`/settings/${id}`)}
                />
              </li>
            ))}
          </ul>
        </NavSection>
      </nav>
      <main className="min-w-0 flex-1 overflow-y-auto rounded-t-2xl bg-canvas px-4 py-6 shadow-sheet md:mt-2 md:px-10 md:py-8">
        <div className="mx-auto max-w-2xl space-y-6">
          <PageHeader eyebrow={t('settings.title')} title={t(`settings.sections.${current.id}`)} description={t(`settings.sectionHints.${current.id}`)} />
          {body}
        </div>
      </main>
    </div>
  );
}
