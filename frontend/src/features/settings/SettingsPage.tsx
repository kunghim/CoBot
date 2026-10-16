import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { ArrowLeft, Bot, Cpu, Info, Settings2, ShieldCheck, Users, type LucideIcon } from 'lucide-react';
import { Navigate, NavLink, useNavigate, useParams } from 'react-router-dom';
import { useSettings } from '../../api/queries';
import { workspacePath } from '../../app/workspacePath';
import {
  AboutSection,
  CollaborationSection,
  EngineSection,
  GeneralSection,
  ModelsSection,
  SecuritySection,
} from './SettingsSections';
import { useSettingSaver } from './useSettingSaver';

type SectionId = 'general' | 'engine' | 'models' | 'collaboration' | 'security' | 'about';

const sections: { id: SectionId; icon: LucideIcon }[] = [
  { id: 'general', icon: Settings2 },
  { id: 'engine', icon: Cpu },
  { id: 'models', icon: Bot },
  { id: 'collaboration', icon: Users },
  { id: 'security', icon: ShieldCheck },
  { id: 'about', icon: Info },
];

/**
 * SettingsPage is a page of its own, outside the workspace layout: a
 * category menu on the left and the chosen category on the right. Every
 * control saves on change; Back (or Escape) returns to the workspace
 * location the user came from.
 */
export default function SettingsPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { section } = useParams();
  const settings = useSettings(true);
  const { save, states } = useSettingSaver();

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
  if (settings.isPending) body = <p className="text-sm text-neutral-500">{t('status.loading')}</p>;
  else if (settings.isError || !settings.data) body = <p className="text-sm text-red-600">{t('status.error')}</p>;
  else {
    const props = { settings: settings.data, save, states };
    body = {
      general: <GeneralSection {...props} />,
      engine: <EngineSection />,
      models: <ModelsSection {...props} />,
      collaboration: <CollaborationSection {...props} />,
      security: <SecuritySection />,
      about: <AboutSection />,
    }[current.id];
  }

  return (
    <div className="flex h-full flex-col bg-neutral-50 text-neutral-900 dark:bg-neutral-950 dark:text-neutral-100 md:flex-row">
      <nav
        aria-label={t('settings.title')}
        className="shrink-0 border-b border-neutral-200 bg-white px-3 py-3 dark:border-neutral-800 dark:bg-neutral-900 md:w-56 md:border-b-0 md:border-r"
      >
        <button
          type="button"
          onClick={back}
          className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-neutral-600 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-800"
        >
          <ArrowLeft size={16} aria-hidden={true} />
          {t('settings.back')}
        </button>
        <h1 className="mt-4 hidden px-2 text-xs font-semibold uppercase tracking-wide text-neutral-400 md:block">
          {t('settings.title')}
        </h1>
        <ul className="mt-2 flex gap-1 overflow-x-auto md:flex-col md:overflow-visible">
          {sections.map(({ id, icon: Icon }) => (
            <li key={id} className="shrink-0">
              <NavLink
                to={`/settings/${id}`}
                replace
                className={({ isActive }) =>
                  `flex items-center gap-2 rounded-md px-2 py-1.5 text-sm ${
                    isActive
                      ? 'bg-neutral-200 font-medium text-neutral-900 dark:bg-neutral-800 dark:text-neutral-100'
                      : 'text-neutral-600 hover:bg-neutral-100 dark:text-neutral-400 dark:hover:bg-neutral-800'
                  }`
                }
              >
                <Icon size={16} aria-hidden={true} />
                {t(`settings.sections.${id}`)}
              </NavLink>
            </li>
          ))}
        </ul>
      </nav>
      <main className="min-w-0 flex-1 overflow-y-auto px-4 py-6 md:px-10 md:py-8">
        <div className="mx-auto max-w-2xl space-y-6">
          <header>
            <h2 className="text-lg font-semibold">{t(`settings.sections.${current.id}`)}</h2>
            <p className="mt-1 text-sm text-neutral-500">{t(`settings.sectionHints.${current.id}`)}</p>
          </header>
          {body}
        </div>
      </main>
    </div>
  );
}
