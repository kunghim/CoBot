// What CoBot is, which build this is, and whether a newer one has been
// published. The update check runs only when the button is pressed.
import { useTranslation } from 'react-i18next';
import { useCheckForUpdate, useEngine, useStatus } from '../../api/queries';
import type { UpdateCheck } from '../../api/types';
import { Alert, Badge, BrandMark, Button, Card, DescriptionList, Icons } from '../../components/ui';
import { openProjectPage } from '../../platform/native-bridge';

const REPOSITORY = 'https://github.com/kunghim/CoBot';

/** A published beta or alpha carries its stage after the number: 0.1.0-beta.2. */
export function releaseStage(version: string | undefined): 'beta' | 'alpha' | null {
  const stage = /-(alpha|beta)(\.|$)/.exec(version ?? '')?.[1];
  return stage === 'alpha' || stage === 'beta' ? stage : null;
}

/** The sentence that says how the installed version stands. */
export function updateMessageKey(result: UpdateCheck): string {
  return `about.update.${result.state}`;
}

function ProjectLink({ href, children }: { href: string; children: string }) {
  return (
    <Button size="sm" icon={Icons.openExternal} onClick={() => openProjectPage(href)}>
      {children}
    </Button>
  );
}

export default function AboutSection() {
  const { t, i18n } = useTranslation();
  const status = useStatus();
  const engine = useEngine(true);
  const check = useCheckForUpdate();
  const s = status.data;
  const stage = releaseStage(s?.version);
  const result = check.data;
  const engineText = engine.data?.status === 'ready' ? engine.data.version : t('about.engineNotReady');

  return (
    <div className="space-y-6">
      <Card>
        <div className="flex items-start gap-4">
          <BrandMark size={48} label={t('app.title')} />
          <div className="min-w-0 flex-1">
            <h2 className="text-lg font-semibold tracking-tight text-neutral-900 dark:text-neutral-50">CoBot</h2>
            <p className="mt-0.5 text-sm text-neutral-600 dark:text-neutral-300">{t('about.tagline')}</p>
            <p className="mt-2 flex flex-wrap items-center gap-2 text-sm text-neutral-600 dark:text-neutral-300">
              <span>
                {t('settings.version')} <span className="font-mono">{s?.version}</span>
              </span>
              {stage && <Badge>{t(`about.stage.${stage}`)}</Badge>}
              {s?.profile === 'development' && <Badge tone="warning">{t('about.developmentBuild')}</Badge>}
            </p>
          </div>
          <Button variant="primary" icon={Icons.refresh} loading={check.isPending} onClick={() => check.mutate()}>
            {t('about.checkUpdate')}
          </Button>
        </div>

        {result && (
          <div className="mt-4 border-t border-neutral-200 pt-4 text-sm dark:border-neutral-800" role="status">
            {result.state === 'unreachable' || result.state === 'rejected' ? (
              <Alert tone="warning">{t(updateMessageKey(result))}</Alert>
            ) : (
              <div className="flex flex-wrap items-center gap-3">
                <span className="font-medium text-neutral-900 dark:text-neutral-50">{t(updateMessageKey(result), { latest: result.latest })}</span>
                {result.releaseUrl && result.state !== 'upToDate' && (
                  <Button size="sm" icon={Icons.openExternal} onClick={() => openProjectPage(result.releaseUrl!)}>
                    {t('about.viewRelease')}
                  </Button>
                )}
                <span className="text-xs text-neutral-500 dark:text-neutral-400">
                  {t('about.checkedAt', { time: new Date(result.checkedAt).toLocaleString(i18n.language) })}
                </span>
              </div>
            )}
          </div>
        )}
        {check.isError && (
          <div className="mt-4">
            <Alert tone="warning">{t('about.update.unreachable')}</Alert>
          </div>
        )}
      </Card>

      <Card>
        <DescriptionList
          items={[
            { term: t('about.engine'), value: engineText, mono: true },
            { term: t('settings.dataRoot'), value: s?.dataRoot, mono: true },
          ]}
        />
        <div className="mt-4 flex flex-wrap gap-2 border-t border-neutral-200 pt-4 dark:border-neutral-800">
          <ProjectLink href={REPOSITORY}>{t('about.repository')}</ProjectLink>
          <ProjectLink href={`${REPOSITORY}/releases`}>{t('about.releases')}</ProjectLink>
          <ProjectLink href={`${REPOSITORY}/issues`}>{t('about.issues')}</ProjectLink>
          <ProjectLink href={`${REPOSITORY}/blob/main/LICENSE`}>{t('about.license')}</ProjectLink>
        </div>
      </Card>
    </div>
  );
}
