// NewTabPage is what a fresh workspace tab shows: what can be opened here,
// and the pages the members already have running. Opening a member's page
// jumps to its tab, creating it when it is not open yet.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { apiErrorMessage } from '../../i18n/errors';
import { useOpenBrowserTab, useTeamBrowser } from '../../api/queries';
import type { Bot } from '../../api/types';
import { Alert, Icons } from '../../components/ui';
import { useWorkspace } from './WorkspaceProvider';
import { NEW_TAB_ID } from './state';

export default function NewTabPage({ teamId, bots }: { teamId: string; bots: Bot[] }) {
  const { t } = useTranslation();
  const browser = useTeamBrowser(teamId);
  const openOwn = useOpenBrowserTab(teamId);
  const { openTab, closeTab } = useWorkspace();
  const [error, setError] = useState<string | null>(null);

  const memberName = (id: string) => bots.find((bot) => bot.id === id)?.name ?? id;
  const memberPages = (browser.data?.tabs ?? []).filter((page) => page.owner === 'member');

  // Own page: the user asked for one, so the browser comes up for it.
  const createBrowser = () => {
    setError(null);
    openOwn.mutate('', {
      onSuccess: ({ tab }) => {
        openTab({ id: `browser:${tab.id}`, kind: 'browser', title: tab.title || t('browser.blank'), ref: tab.id }, true);
        closeTab(NEW_TAB_ID);
      },
      onError: (err) => setError(apiErrorMessage(t, err)),
    });
  };
  const openMemberPage = (id: string, title: string) => {
    openTab({ id: `browser:${id}`, kind: 'browser', title, ref: id }, true);
    closeTab(NEW_TAB_ID);
  };

  const option = (Icon: typeof Icons.add, title: string, hint: string, enabled: boolean, onPick: () => void) => (
    <button
      type="button"
      disabled={!enabled}
      onClick={onPick}
      className="flex w-full items-start gap-3 rounded-xl px-3 py-2.5 text-left transition-colors hover:bg-neutral-900/[0.05] disabled:cursor-not-allowed disabled:opacity-45 disabled:hover:bg-transparent dark:hover:bg-white/[0.06]"
    >
      <span className="mt-0.5 text-neutral-500 dark:text-neutral-400"><Icon size={17} aria-hidden /></span>
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-2 text-sm font-medium text-neutral-900 dark:text-neutral-50">
          {title}
          {!enabled && <span className="rounded bg-neutral-900/[0.06] px-1.5 py-0.5 text-2xs font-normal text-neutral-500 dark:bg-white/[0.08] dark:text-neutral-400">{t('workspace.newPage.later')}</span>}
        </span>
        <span className="mt-0.5 block text-xs leading-5 text-neutral-500 dark:text-neutral-400">{hint}</span>
      </span>
    </button>
  );

  return (
    <div className="mx-auto flex h-full min-h-0 w-full max-w-lg flex-col gap-1 overflow-y-auto px-4 py-4">
      {error && <Alert tone="danger">{error}</Alert>}
      {option(Icons.webSearch, t('workspace.newPage.browser'), t('workspace.newPage.browserHint'), true, createBrowser)}
      {option(Icons.view, t('workspace.newPage.file'), t('workspace.newPage.later'), false, () => {})}
      {option(Icons.diff, t('workspace.newPage.changes'), t('workspace.newPage.later'), false, () => {})}
      {option(Icons.command, t('workspace.newPage.terminal'), t('workspace.newPage.later'), false, () => {})}

      <p className="mt-4 px-3 text-2xs font-semibold uppercase tracking-[0.12em] text-neutral-500 dark:text-neutral-400">
        {t('workspace.newPage.memberPages')}
      </p>
      {memberPages.length === 0 ? (
        <p className="px-3 py-2 text-xs text-neutral-500 dark:text-neutral-400">{t('workspace.newPage.noMemberPages')}</p>
      ) : (
        <ul className="space-y-0.5">
          {memberPages.map((page) => {
            const title = `${memberName(page.memberId ?? '')} · ${page.title || page.url || t('browser.blank')}`;
            return (
              <li key={page.id}>
                <button
                  type="button"
                  onClick={() => openMemberPage(page.id, title)}
                  className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm transition-colors hover:bg-neutral-900/[0.05] dark:hover:bg-white/[0.06]"
                >
                  <span className="text-neutral-400 dark:text-neutral-500"><Icons.webSearch size={15} aria-hidden /></span>
                  <span className="min-w-0 flex-1 truncate">{title}</span>
                  {page.busy && <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" aria-hidden />}
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
